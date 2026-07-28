package batch

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/translationreview"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type batchRetranslation struct {
	mu       sync.RWMutex
	response contracts.BatchRetranslateResponse
}

func newBatchRetranslation(jobID string, total int) *batchRetranslation {
	return &batchRetranslation{
		response: contracts.BatchRetranslateResponse{
			JobID:  jobID,
			Status: contracts.BatchRetranslateStatusRunning,
			Total:  total,
		},
	}
}

func (operation *batchRetranslation) snapshot() contracts.BatchRetranslateResponse {
	operation.mu.RLock()
	defer operation.mu.RUnlock()
	response := operation.response
	response.Errors = append([]contracts.BatchRetranslateError(nil), operation.response.Errors...)
	return response
}

func (operation *batchRetranslation) isRunning() bool {
	operation.mu.RLock()
	defer operation.mu.RUnlock()
	return operation.response.Status == contracts.BatchRetranslateStatusRunning
}

func (operation *batchRetranslation) record(movieID string, err error) {
	operation.mu.Lock()
	defer operation.mu.Unlock()
	operation.response.Processed++
	if err != nil {
		operation.response.Failed++
		operation.response.Errors = append(operation.response.Errors, contracts.BatchRetranslateError{
			MovieID: movieID,
			Error:   err.Error(),
		})
		return
	}
	operation.response.Succeeded++
}

func (operation *batchRetranslation) finish(ctxErr error) {
	operation.mu.Lock()
	defer operation.mu.Unlock()
	sort.Slice(operation.response.Errors, func(i, j int) bool {
		return operation.response.Errors[i].MovieID < operation.response.Errors[j].MovieID
	})
	if operation.response.Status == contracts.BatchRetranslateStatusFailed {
		return
	}
	if ctxErr != nil {
		operation.response.Status = contracts.BatchRetranslateStatusCancelled
		return
	}
	operation.response.Status = contracts.BatchRetranslateStatusCompleted
}

func (operation *batchRetranslation) fail(err error) {
	operation.mu.Lock()
	defer operation.mu.Unlock()
	operation.response.Status = contracts.BatchRetranslateStatusFailed
	operation.response.Errors = append(operation.response.Errors, contracts.BatchRetranslateError{
		Error: err.Error(),
	})
}

type batchRetranslationRegistry struct {
	mu         sync.RWMutex
	operations map[string]*batchRetranslation
}

func newBatchRetranslationRegistry() *batchRetranslationRegistry {
	return &batchRetranslationRegistry{operations: make(map[string]*batchRetranslation)}
}

func (registry *batchRetranslationRegistry) start(jobID string, total int) (*batchRetranslation, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if current := registry.operations[jobID]; current != nil && current.isRunning() {
		return current, false
	}
	operation := newBatchRetranslation(jobID, total)
	registry.operations[jobID] = operation
	return operation, true
}

func (registry *batchRetranslationRegistry) snapshot(jobID string) *contracts.BatchRetranslateResponse {
	registry.mu.RLock()
	operation := registry.operations[jobID]
	registry.mu.RUnlock()
	if operation == nil {
		return nil
	}
	response := operation.snapshot()
	return &response
}

func (registry *batchRetranslationRegistry) isRunning(jobID string) bool {
	registry.mu.RLock()
	operation := registry.operations[jobID]
	registry.mu.RUnlock()
	return operation != nil && operation.isRunning()
}

func (registry *batchRetranslationRegistry) remove(jobID string) {
	registry.mu.Lock()
	delete(registry.operations, jobID)
	registry.mu.Unlock()
}

var batchRetranslations = newBatchRetranslationRegistry()

type batchRetranslateTarget struct {
	resultID string
	filePath string
	result   *worker.MovieResult
}

type batchRetranslateGroup struct {
	movieID           string
	titleSource       string
	descriptionSource string
	actresses         []models.Actress
	targets           []batchRetranslateTarget
}

// retranslateBatchJob godoc
// @Summary Retranslate all identified movies in a completed batch job
// @Description Starts a background retranslation of retained title and description sources with the configured LLM and second-pass JAV reviewer. Recoverable translation failures are marked completed. Progress is exposed on the batch job response. Available before organization.
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 202 {object} contracts.BatchRetranslateResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 503 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/retranslate [post]
func retranslateBatchJob(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		job, ok := rt.Deps().GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}
		if job.GetJobStatus() != models.JobStatusCompleted {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "job retranslation is only available before organization"})
			return
		}

		tc := rt.Snapshot().APIConfig().TranslationConfig
		if err := translationreview.ValidateConfig(tc); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		groups := buildBatchRetranslateGroups(job)
		if len(groups) == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "job has no identified movies with retained metadata to retranslate"})
			return
		}

		serverCtx := rt.ServerCtx()
		if err := serverCtx.Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "server is shutting down"})
			return
		}
		operation, started := batchRetranslations.start(jobID, len(groups))
		if !started {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "job retranslation is already running"})
			return
		}

		// Acknowledge before launching the expensive LLM work. The operation is
		// deliberately tied to the server lifecycle rather than the request, so
		// a reverse-proxy timeout or browser disconnect cannot cancel it.
		c.JSON(http.StatusAccepted, operation.snapshot())
		go runBatchRetranslation(serverCtx, rt, job, tc, groups, operation)
	}
}

func runBatchRetranslation(
	ctx context.Context,
	rt *core.APIRuntime,
	job worker.BatchJobInterface,
	tc config.TranslationConfig,
	groups []batchRetranslateGroup,
	operation *batchRetranslation,
) {
	jobID := job.GetID()
	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("background retranslation panic: %v", recovered)
			logging.Errorf("Job %s retranslation failed: %v", jobID, err)
			operation.fail(err)
		}
	}()

	logging.Infof("Job %s background retranslation started: %d movies", jobID, len(groups))
	workers := tc.MaxConcurrency
	if workers <= 0 {
		workers = 3
	}
	if workers > len(groups) {
		workers = len(groups)
	}

	groupQueue := make(chan batchRetranslateGroup, len(groups))
	for _, group := range groups {
		groupQueue <- group
	}
	close(groupQueue)

	var waitGroup sync.WaitGroup
	var applyMu sync.Mutex
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					err := fmt.Errorf("background retranslation worker panic: %v", recovered)
					logging.Errorf("Job %s retranslation failed: %v", jobID, err)
					operation.fail(err)
					cancelWork()
				}
				waitGroup.Done()
			}()
			for {
				// Prefer cancellation over dequeuing more work. This prevents a
				// shutdown from turning every queued movie into a context error.
				if workCtx.Err() != nil {
					return
				}
				select {
				case <-workCtx.Done():
					return
				case group, ok := <-groupQueue:
					if !ok {
						return
					}
					reviewed, err := translateBatchGroup(workCtx, tc, group)
					if err == nil {
						// Applying and checkpointing are serialized so each DB
						// snapshot represents a complete movie group.
						err = func() error {
							applyMu.Lock()
							defer applyMu.Unlock()
							applyErr := applyBatchRetranslation(workCtx, job, group, reviewed)
							rt.Deps().GetJobStore().PersistJobByID(jobID)
							return applyErr
						}()
					}
					operation.record(group.movieID, err)
				}
			}
		}()
	}
	waitGroup.Wait()
	rt.Deps().GetJobStore().PersistJobByID(jobID)
	operation.finish(workCtx.Err())
	response := operation.snapshot()
	logging.Infof(
		"Job %s background retranslation %s: %d succeeded, %d failed, %d/%d processed",
		jobID,
		response.Status,
		response.Succeeded,
		response.Failed,
		response.Processed,
		response.Total,
	)
}

func isBatchRetranslationRunning(jobID string) bool {
	return batchRetranslations.isRunning(jobID)
}

func batchRetranslationSnapshot(jobID string) *contracts.BatchRetranslateResponse {
	return batchRetranslations.snapshot(jobID)
}

func removeBatchRetranslation(jobID string) {
	batchRetranslations.remove(jobID)
}

func translateBatchGroup(
	ctx context.Context,
	tc config.TranslationConfig,
	group batchRetranslateGroup,
) (translationreview.MovieResult, error) {
	return translationreview.ReviewMovie(
		ctx,
		tc,
		group.titleSource,
		group.descriptionSource,
		group.actresses,
	)
}

func applyBatchRetranslation(
	ctx context.Context,
	job worker.BatchJobInterface,
	group batchRetranslateGroup,
	reviewed translationreview.MovieResult,
) error {
	for _, target := range group.targets {
		if _, err := job.ApplyTranslationReview(
			ctx,
			target.resultID,
			"title",
			reviewed.Title.Value,
			reviewed.Title.TargetLanguage,
		); err != nil {
			return fmt.Errorf("%s: apply reviewed title: %w", target.filePath, err)
		}
		if reviewed.Description != nil {
			if _, err := job.ApplyTranslationReview(
				ctx,
				target.resultID,
				"description",
				reviewed.Description.Value,
				reviewed.Description.TargetLanguage,
			); err != nil {
				return fmt.Errorf("%s: apply reviewed description: %w", target.filePath, err)
			}
		}
		if worker.IsTranslationFailure(target.result) {
			if _, err := job.ResolveTranslationFailure(target.resultID); err != nil {
				return fmt.Errorf("%s: resolve translation failure: %w", target.filePath, err)
			}
		}
	}
	return nil
}

func buildBatchRetranslateGroups(job worker.BatchJobInterface) []batchRetranslateGroup {
	status := job.GetStatus()
	if status == nil {
		return nil
	}
	filePaths := make([]string, 0, len(status.Results))
	for filePath := range status.Results {
		filePaths = append(filePaths, filePath)
	}
	sort.Strings(filePaths)

	grouped := make(map[string]*batchRetranslateGroup)
	order := make([]string, 0)
	for _, filePath := range filePaths {
		result := status.Results[filePath]
		if result == nil || result.Movie == nil || status.Excluded[filePath] {
			continue
		}
		if result.Status != models.JobStatusCompleted && !worker.IsTranslationFailure(result) {
			continue
		}
		key, movieID := batchRetranslateGroupIdentity(result)
		group := grouped[key]
		if group == nil {
			group = &batchRetranslateGroup{
				movieID:   movieID,
				actresses: append([]models.Actress(nil), result.Movie.Actresses...),
			}
			grouped[key] = group
			order = append(order, key)
		}
		group.targets = append(group.targets, batchRetranslateTarget{
			resultID: result.ResultID,
			filePath: filePath,
			result:   result,
		})
		provenance := job.GetProvenance(filePath)
		if group.titleSource == "" {
			group.titleSource = retainedJapaneseField(provenance, result.Movie, "title")
		}
		if group.descriptionSource == "" {
			group.descriptionSource = retainedJapaneseField(provenance, result.Movie, "description")
			if group.descriptionSource == "" && worker.IsTranslationFailure(result) {
				group.descriptionSource = result.Movie.Description
			}
		}
	}

	groups := make([]batchRetranslateGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, *grouped[key])
	}
	return groups
}

func batchRetranslateGroupIdentity(result *worker.MovieResult) (string, string) {
	movieID := strings.TrimSpace(result.FileMatchInfo.MovieID)
	if movieID == "" && result.Movie != nil {
		movieID = strings.TrimSpace(result.Movie.ID)
	}
	if movieID == "" && result.Movie != nil {
		movieID = strings.TrimSpace(result.Movie.ContentID)
	}
	if movieID == "" {
		movieID = result.ResultID
	}
	identity := ""
	if result.Movie != nil {
		identity = strings.TrimSpace(result.Movie.ContentID)
		if identity == "" {
			identity = strings.TrimSpace(result.Movie.ID)
		}
	}
	if identity == "" {
		identity = movieID
	}
	return strings.ToLower(identity), movieID
}
