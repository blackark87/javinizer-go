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
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

var batchRetranslations sync.Map

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
// @Description Retranslates retained title and description sources with the configured LLM and second-pass JAV reviewer. Recoverable translation failures are marked completed. Available before organization.
// @Tags web
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} contracts.BatchRetranslateResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
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
		if _, loaded := batchRetranslations.LoadOrStore(jobID, struct{}{}); loaded {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "job retranslation is already running"})
			return
		}
		defer batchRetranslations.Delete(jobID)

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

		response := contracts.BatchRetranslateResponse{
			JobID: jobID,
			Total: len(groups),
		}
		requestCtx := c.Request.Context()
		workers := tc.MaxConcurrency
		if workers <= 0 {
			workers = 3
		}
		if workers > len(groups) {
			workers = len(groups)
		}

		groupQueue := make(chan batchRetranslateGroup)
		var waitGroup sync.WaitGroup
		var responseMu sync.Mutex
		for range workers {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				for group := range groupQueue {
					err := retranslateBatchGroup(requestCtx, job, tc, group)
					responseMu.Lock()
					if err != nil {
						response.Failed++
						response.Errors = append(response.Errors, contracts.BatchRetranslateError{
							MovieID: group.movieID,
							Error:   err.Error(),
						})
					} else {
						response.Succeeded++
					}
					responseMu.Unlock()
				}
			}()
		}
		for _, group := range groups {
			groupQueue <- group
		}
		close(groupQueue)
		waitGroup.Wait()

		sort.Slice(response.Errors, func(i, j int) bool {
			return response.Errors[i].MovieID < response.Errors[j].MovieID
		})
		rt.Deps().GetJobStore().PersistJobByID(jobID)
		c.JSON(http.StatusOK, response)
	}
}

func isBatchRetranslationRunning(jobID string) bool {
	_, running := batchRetranslations.Load(jobID)
	return running
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

func retranslateBatchGroup(
	ctx context.Context,
	job worker.BatchJobInterface,
	tc config.TranslationConfig,
	group batchRetranslateGroup,
) error {
	reviewed, err := translationreview.ReviewMovie(
		ctx,
		tc,
		group.titleSource,
		group.descriptionSource,
		group.actresses,
	)
	if err != nil {
		return err
	}
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
