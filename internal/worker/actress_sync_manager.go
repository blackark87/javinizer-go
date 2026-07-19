package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// ActressSyncManagerDeps contains the durable sync manager's dependencies.
type ActressSyncManagerDeps struct {
	DB              *database.DB
	ActressRepo     *database.ActressRepository
	MovieRepo       *database.MovieRepository
	HistoryRepo     *database.HistoryRepository
	BatchFileOpRepo *database.BatchFileOperationRepository
	GetConfig       func() *config.Config
	GetRegistry     func() *scraperutil.ScraperRegistry
}

// ActressSyncCreateRequest selects actresses for a durable background sync.
type ActressSyncCreateRequest struct {
	Scope      string `json:"scope"`
	ActressIDs []uint `json:"actress_ids"`
	Missing    bool   `json:"missing"`
}

// ActressSyncManager claims and executes durable actress metadata sync tasks.
type ActressSyncManager struct {
	deps  ActressSyncManagerDeps
	repo  *database.ActressSyncRepository
	owner string

	mu      sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	wake    chan struct{}
	active  atomic.Int32

	llmMu     sync.Mutex
	llmActive int
}

// NewActressSyncManager constructs a durable actress sync manager.
func NewActressSyncManager(deps ActressSyncManagerDeps) *ActressSyncManager {
	manager := &ActressSyncManager{
		deps: deps, owner: uuid.NewString(), wake: make(chan struct{}, 1),
	}
	if deps.DB != nil {
		manager.repo = database.NewActressSyncRepository(deps.DB)
	}
	return manager
}

// Start begins dispatching pending actress sync tasks.
func (m *ActressSyncManager) Start() {
	if m == nil || m.repo == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.started = true
	if err := m.repo.RecoverExpiredLeases(time.Now().UTC()); err != nil {
		logging.Warnf("Actress sync: failed to recover expired tasks: %v", err)
	}
	if err := m.repo.NormalizeActiveMovieTasks(time.Now().UTC()); err != nil {
		logging.Warnf("Actress sync: failed to normalize active movie tasks: %v", err)
	}
	m.wg.Add(1)
	go m.dispatch()
}

// Stop waits for workers and releases their task leases.
func (m *ActressSyncManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	cancel := m.cancel
	m.started = false
	m.mu.Unlock()
	cancel()
	m.wg.Wait()
	if err := m.repo.ReleaseOwnerLeases(m.owner); err != nil {
		logging.Warnf("Actress sync: failed to release worker leases: %v", err)
	}
}

func (m *ActressSyncManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *ActressSyncManager) dispatch() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}

		for int(m.active.Load()) < m.maxWorkers() {
			leaseDuration := m.taskTimeout() + 30*time.Second
			task, err := m.repo.ClaimNext(m.owner, time.Now().UTC().Add(leaseDuration))
			if err != nil {
				logging.Warnf("Actress sync: claim failed: %v", err)
				break
			}
			if task == nil {
				break
			}
			m.active.Add(1)
			m.wg.Add(1)
			go m.runTask(task)
		}
	}
}

func (m *ActressSyncManager) maxWorkers() int {
	if m.deps.GetConfig != nil {
		if cfg := m.deps.GetConfig(); cfg != nil && cfg.Performance.MaxWorkers > 0 {
			return cfg.Performance.MaxWorkers
		}
	}
	return 5
}

func (m *ActressSyncManager) taskTimeout() time.Duration {
	if m.deps.GetConfig != nil {
		if cfg := m.deps.GetConfig(); cfg != nil && cfg.Scrapers.RequestTimeoutSeconds > 0 {
			return time.Duration(cfg.Scrapers.RequestTimeoutSeconds) * time.Second
		}
	}
	return 60 * time.Second
}

// CreateJob persists and starts a durable actress sync job.
func (m *ActressSyncManager) CreateJob(ctx context.Context, req ActressSyncCreateRequest) (*models.ActressSyncJob, error) {
	if m == nil || m.repo == nil || m.deps.ActressRepo == nil || m.deps.MovieRepo == nil {
		return nil, fmt.Errorf("actress sync manager is unavailable")
	}
	ids := uniqueActressIDs(req.ActressIDs)
	if req.Missing {
		var err error
		ids, err = m.deps.ActressRepo.ListMissingMetadataIDs()
		if err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no actresses were selected for sync")
	}

	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: strings.TrimSpace(req.Scope), CreatedAt: now}
	if job.Scope == "" {
		if req.Missing {
			job.Scope = "missing"
		} else {
			job.Scope = "selected"
		}
	}
	var tasks []models.ActressSyncTask
	queuedMovies := make(map[string]struct{})
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		actress, err := m.deps.ActressRepo.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if actress.DMMID <= 0 {
			movies, err := m.deps.MovieRepo.ListByActressID(ctx, id, 0, 0)
			if err != nil {
				return nil, err
			}
			if len(movies) == 0 {
				tasks = append(tasks, m.skippedTask(job.ID, fmt.Sprintf("actress:%d:no-movies", id), id, "", "", "Actress has no DMM ID and no linked movies for SougouWiki lookup"))
				continue
			}
			for _, movie := range movies {
				if _, exists := queuedMovies[movie.ContentID]; exists {
					continue
				}
				queuedMovies[movie.ContentID] = struct{}{}
				actressID := id
				task := models.ActressSyncTask{
					ID: uuid.NewString(), JobID: job.ID, Kind: models.ActressSyncTaskKindUnknownMovie,
					ActressID: &actressID, MovieContentID: movie.ContentID, MovieID: movie.ID,
					Label:     fmt.Sprintf("%s / %s", actressSyncActressLabel(*actress), movie.ID),
					DedupeKey: fmt.Sprintf("movie:%s:missing-dmm", movie.ContentID), Status: models.ActressSyncTaskPending,
					Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now,
				}
				tasks = append(tasks, m.deduplicateTask(task))
			}
			continue
		}
		actressID := id
		task := models.ActressSyncTask{
			ID: uuid.NewString(), JobID: job.ID, Kind: models.ActressSyncTaskKindActress, ActressID: &actressID,
			Label: actressSyncActressLabel(*actress), DedupeKey: fmt.Sprintf("actress:%d", id),
			Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now,
		}
		tasks = append(tasks, m.deduplicateTask(task))
	}

	job.TotalTasks = len(tasks)
	for _, task := range tasks {
		if task.Status == models.ActressSyncTaskSkipped {
			job.Completed++
			job.Skipped++
		}
	}
	if job.Completed == job.TotalTasks {
		job.Status = models.ActressSyncJobCompleted
		job.CompletedAt = &now
	}
	if err := m.repo.CreateJob(job, tasks); err != nil {
		return nil, err
	}
	m.Start()
	m.signal()
	return job, nil
}

func (m *ActressSyncManager) deduplicateTask(task models.ActressSyncTask) models.ActressSyncTask {
	active, err := m.repo.HasActiveTask(task.DedupeKey)
	if err == nil && active {
		now := time.Now().UTC()
		task.Status = models.ActressSyncTaskSkipped
		task.Stage = "completed"
		task.Outcome = string(ActressSyncSkipped)
		task.Messages = []string{"An equivalent actress sync item is already pending or running"}
		task.CompletedAt = &now
		task.DedupeKey += ":duplicate:" + task.ID
	}
	return task
}

func (m *ActressSyncManager) skippedTask(jobID, dedupe string, actressID uint, movieContentID, movieID, message string) models.ActressSyncTask {
	now := time.Now().UTC()
	return models.ActressSyncTask{
		ID: uuid.NewString(), JobID: jobID, Kind: models.ActressSyncTaskKindActress, ActressID: &actressID,
		MovieContentID: movieContentID, MovieID: movieID, Label: message, DedupeKey: dedupe + ":" + uuid.NewString(),
		Status: models.ActressSyncTaskSkipped, Stage: "completed", Outcome: string(ActressSyncSkipped), Messages: []string{message},
		UpdatedFields: []string{}, CreatedAt: now, CompletedAt: &now,
	}
}

// GetJob returns a durable actress sync job by ID.
func (m *ActressSyncManager) GetJob(id string) (*models.ActressSyncJob, error) {
	return m.repo.FindJob(id)
}

// ListActiveJobs returns pending and running actress sync jobs.
func (m *ActressSyncManager) ListActiveJobs() ([]models.ActressSyncJob, error) {
	return m.repo.ListActiveJobs()
}

// ListTasks returns all durable tasks for an actress sync job.
func (m *ActressSyncManager) ListTasks(jobID string) ([]models.ActressSyncTask, error) {
	if _, err := m.repo.FindJob(jobID); err != nil {
		return nil, err
	}
	return m.repo.ListTasks(jobID)
}

// CancelJob requests cancellation of an actress sync job.
func (m *ActressSyncManager) CancelJob(jobID string) error {
	if err := m.repo.CancelJob(jobID); err != nil {
		return err
	}
	m.signal()
	return nil
}

func (m *ActressSyncManager) runTask(task *models.ActressSyncTask) {
	defer m.wg.Done()
	defer func() {
		m.active.Add(-1)
		m.signal()
	}()

	timeout := m.taskTimeout()
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go m.heartbeat(ctx, task.ID, task.LeaseToken, timeout, heartbeatDone)
	defer close(heartbeatDone)

	defer func() {
		if recovered := recover(); recovered != nil {
			task.Status = models.ActressSyncTaskFailed
			task.Outcome = "failed"
			task.ErrorMessage = fmt.Sprintf("panic: %v", recovered)
			logging.Errorf("Actress sync task panicked (task=%s label=%q stage=%s): %s", task.ID, task.Label, task.Stage, task.ErrorMessage)
			_ = m.repo.CompleteTask(task, task.LeaseToken)
		}
	}()

	var err error
	switch task.Kind {
	case models.ActressSyncTaskKindUnknownMovie:
		err = m.processUnknownMovie(ctx, task)
	default:
		err = m.processActress(ctx, task)
	}
	if err != nil {
		if m.ctx.Err() != nil && errors.Is(err, context.Canceled) {
			return
		}
		task.Status = models.ActressSyncTaskFailed
		task.Outcome = "failed"
		task.ErrorMessage = err.Error()
		logging.Errorf("Actress sync task failed (task=%s label=%q stage=%s): %v", task.ID, task.Label, task.Stage, err)
	}
	if completeErr := m.repo.CompleteTask(task, task.LeaseToken); completeErr != nil {
		logging.Errorf("Actress sync: failed to complete task %s: %v", task.ID, completeErr)
	}
}

func (m *ActressSyncManager) heartbeat(ctx context.Context, taskID, leaseToken string, timeout time.Duration, done <-chan struct{}) {
	interval := timeout / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			_ = m.repo.Heartbeat(taskID, leaseToken, time.Now().UTC().Add(timeout+30*time.Second))
		}
	}
}

func (m *ActressSyncManager) setStage(task *models.ActressSyncTask, stage string) {
	task.Stage = stage
	_ = m.repo.UpdateStage(task.ID, task.LeaseToken, stage, task.Messages)
}

func (m *ActressSyncManager) processActress(ctx context.Context, task *models.ActressSyncTask) error {
	if task.ActressID == nil {
		return fmt.Errorf("actress task has no actress ID")
	}
	existing, err := m.deps.ActressRepo.FindByID(ctx, *task.ActressID)
	if err != nil {
		return err
	}
	preserveExistingProfile := existing.DMMID > 0 && hasUsableActressIdentityProfile(*existing)
	cfg := m.deps.GetConfig()
	m.setStage(task, "resolving")
	result, err := SyncActressMetadata(ctx, *task.ActressID, m.deps.ActressRepo, m.deps.GetRegistry(), cfg.Scrapers.Priority, m.deps.MovieRepo)
	if err != nil {
		return err
	}
	task.Messages = append(task.Messages, result.Messages...)
	task.UpdatedFields = append(task.UpdatedFields, result.UpdatedFields...)
	if result.Status == ActressSyncUpdated && len(result.Messages) > 0 {
		metadataWarning := strings.Join(result.Messages, "; ")
		task.Warning = appendWarning(task.Warning, metadataWarning)
		logging.Warnf("Actress sync task warning (task=%s label=%q stage=%s): %s", task.ID, task.Label, task.Stage, metadataWarning)
	}
	if result.Status == ActressSyncConflict {
		task.Status = models.ActressSyncTaskConflict
		task.Outcome = "conflict"
		return nil
	}
	canonical := result.Actress

	if !preserveExistingProfile || containsAnyField(result.UpdatedFields, "japanese_name") {
		m.setStage(task, "romanizing")
		if translation.ApplyDMMHepburnName(&canonical) {
			if err := m.deps.ActressRepo.Update(ctx, &canonical); err != nil {
				return err
			}
			task.UpdatedFields = append(task.UpdatedFields, "hepburn_name")
		}

		warning, translateErr := m.translateAndStore(ctx, task, []models.Actress{canonical})
		if translateErr != nil {
			warning = translateErr.Error()
		}
		if warning != "" {
			task.Warning = appendWarning(task.Warning, warning)
			logging.Warnf("Actress sync task warning (task=%s label=%q stage=%s): %s", task.ID, task.Label, task.Stage, warning)
		}
	}

	displayChanged := containsAnyField(task.UpdatedFields, "hepburn_name", "translated_name", "japanese_name", "first_name", "last_name")
	translationChanged := containsAnyField(task.UpdatedFields, "actress_translations")
	thumbnailChanged := containsAnyField(task.UpdatedFields, "thumb_url")
	if displayChanged || translationChanged || thumbnailChanged {
		m.setStage(task, "mapping")
		movies, listErr := m.deps.MovieRepo.ListByActressID(ctx, canonical.ID, 0, 0)
		if listErr != nil {
			return listErr
		}
		nfoMovieIDs := make(map[string]struct{})
		if displayChanged || thumbnailChanged {
			for _, movie := range movies {
				nfoMovieIDs[movie.ContentID] = struct{}{}
			}
		}
		m.refreshAffectedMovies(ctx, task, movies, nfoMovieIDs)
	}

	switch {
	case len(task.UpdatedFields) > 0 && task.Warning != "":
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated_with_warning"
	case len(task.UpdatedFields) > 0:
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated"
	case result.Status == ActressSyncConflict:
		task.Status, task.Outcome = models.ActressSyncTaskConflict, "conflict"
	case result.Status == ActressSyncFailed:
		task.Status, task.Outcome = models.ActressSyncTaskFailed, "failed"
	default:
		task.Status, task.Outcome = models.ActressSyncTaskSkipped, string(ActressSyncSkipped)
	}
	return nil
}

func (m *ActressSyncManager) processUnknownMovie(ctx context.Context, task *models.ActressSyncTask) error {
	if task.ActressID == nil || task.MovieContentID == "" {
		return fmt.Errorf("placeholder movie task is incomplete")
	}
	movie, err := m.deps.MovieRepo.FindByContentID(ctx, task.MovieContentID)
	if err != nil {
		return err
	}
	if m.deps.GetRegistry == nil {
		return fmt.Errorf("movie %s: SougouWiki resolver registry is unavailable", movie.ID)
	}
	registry := m.deps.GetRegistry()
	if registry == nil {
		return fmt.Errorf("movie %s: SougouWiki resolver registry is unavailable", movie.ID)
	}
	scraper, ok := registry.GetInstance("sougouwiki")
	if !ok || scraper == nil {
		return fmt.Errorf("movie %s: SougouWiki resolver is unavailable", movie.ID)
	}
	resolver, ok := scraper.(models.ActressResolver)
	if !ok {
		return fmt.Errorf("SougouWiki does not support actress resolution")
	}
	queryID := strings.TrimSpace(movie.ID)
	if queryID == "" {
		queryID = movie.ContentID
	}
	m.setStage(task, "resolving")
	resolved, err := safeResolveActresses(ctx, resolver, queryID)
	if err != nil {
		return fmt.Errorf("movie %s via sougouwiki at resolving stage: %w", queryID, err)
	}
	profileResolver := findActressProfileResolver(registry)
	profileResolved := make(map[int]bool, len(resolved.Actresses))
	observedAliasesByDMMID := make(map[int][]string, len(resolved.Actresses))
	if profileResolver != nil {
		for index := range resolved.Actresses {
			observedName := strings.TrimSpace(resolved.Actresses[index].JapaneseName)
			profile, profileErr := safeResolveActressProfile(ctx, profileResolver, resolved.Actresses[index])
			if profileErr != nil || strings.TrimSpace(profile.JapaneseName) == "" {
				continue
			}
			resolved.Actresses[index].JapaneseName = strings.TrimSpace(profile.JapaneseName)
			resolved.Actresses[index].FirstName = strings.TrimSpace(profile.FirstName)
			resolved.Actresses[index].LastName = strings.TrimSpace(profile.LastName)
			if strings.TrimSpace(profile.ThumbURL) != "" {
				resolved.Actresses[index].ThumbURL = strings.TrimSpace(profile.ThumbURL)
			}
			if isObservedSyncAlias(observedName, profile.JapaneseName) {
				observedAliasesByDMMID[profile.DMMID] = append(observedAliasesByDMMID[profile.DMMID], observedName)
			}
			profileResolved[profile.DMMID] = true
		}
	}
	verified := verifiedActresses(resolved)
	if len(verified) == 0 {
		return m.cleanFallbackMovieActresses(ctx, task, *movie)
	}

	thumbnailResolver := findActressThumbnailResolver(registry)
	canonical := make([]models.Actress, 0, len(verified))
	enrichmentIndexes := make([]int, 0, len(verified))
	refreshCanonicalIDs := make(map[uint]struct{})
	nfoCanonicalIDs := make(map[uint]struct{})
	for _, info := range verified {
		existing, findErr := m.deps.ActressRepo.FindByDMMID(ctx, info.DMMID)
		if findErr != nil && !database.IsNotFound(findErr) {
			return findErr
		}
		if info.ThumbURL == "" && thumbnailResolver != nil && (existing == nil || strings.TrimSpace(existing.ThumbURL) == "") {
			info.ThumbURL = safeResolveActressThumbnail(ctx, thumbnailResolver, info)
		}
		needsNameEnrichment := existing == nil || !hasUsableActressIdentityProfile(*existing)
		var resolution *database.VerifiedActressResolution
		var resolveErr error
		if profileResolved[info.DMMID] {
			observedAliases := append([]string(nil), observedAliasesByDMMID[info.DMMID]...)
			if existing != nil {
				observedAliases = append(observedAliases, splitStoredActressAliases(existing.Aliases)...)
				observedAliases = append(observedAliases, existing.JapaneseName)
			}
			resolution, resolveErr = m.deps.ActressRepo.ResolveVerifiedProfile(0, actressModelFromInfo(info), observedAliases, true)
		} else {
			resolution, resolveErr = m.deps.ActressRepo.ResolveVerifiedIdentity(0, actressModelFromInfo(info), true)
		}
		if resolveErr != nil {
			return resolveErr
		}
		if resolution.NameChanged {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "japanese_name")
			needsNameEnrichment = true
		}
		if existing != nil && strings.TrimSpace(existing.ThumbURL) != strings.TrimSpace(resolution.Actress.ThumbURL) {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "thumb_url")
		}
		canonical = append(canonical, resolution.Actress)
		if resolution.Created || resolution.Promoted {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "dmm_id")
		}
		if len(resolution.AliasesAdded) > 0 || len(resolution.AliasMappingsAdded) > 0 {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "aliases")
		}
		if len(resolution.AliasConflicts) > 0 {
			task.Warning = appendWarning(task.Warning, "Existing manual alias mappings were retained for: "+strings.Join(resolution.AliasConflicts, ", "))
		}
		if resolution.NameChanged || len(resolution.MergedFromIDs) > 0 {
			refreshCanonicalIDs[resolution.Actress.ID] = struct{}{}
			nfoCanonicalIDs[resolution.Actress.ID] = struct{}{}
		}
		if len(resolution.MergedFromIDs) > 0 {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "merged_actresses")
		}
		if resolution.Created || resolution.Promoted || needsNameEnrichment {
			enrichmentIndexes = append(enrichmentIndexes, len(canonical)-1)
		}
	}

	var translationRecords []models.MovieTranslation
	warning := ""
	if len(enrichmentIndexes) > 0 {
		m.setStage(task, "romanizing")
		toEnrich := make([]models.Actress, len(enrichmentIndexes))
		for index, canonicalIndex := range enrichmentIndexes {
			toEnrich[index] = canonical[canonicalIndex]
			if translation.ApplyDMMHepburnName(&toEnrich[index]) {
				task.UpdatedFields = appendUnique(task.UpdatedFields, "hepburn_name")
			}
		}

		translated, records, translateWarning, translateErr := m.translateActresses(ctx, task, toEnrich)
		translationRecords = records
		warning = translateWarning
		if translateErr != nil {
			warning = appendWarning(warning, translateErr.Error())
		}
		if len(translated) == len(toEnrich) {
			toEnrich = translated
		}
		for index, canonicalIndex := range enrichmentIndexes {
			beforeEnrichment := canonical[canonicalIndex]
			canonical[canonicalIndex] = toEnrich[index]
			if updateErr := m.deps.ActressRepo.Update(ctx, &canonical[canonicalIndex]); updateErr != nil {
				return updateErr
			}
			if beforeEnrichment.FirstName != canonical[canonicalIndex].FirstName ||
				beforeEnrichment.LastName != canonical[canonicalIndex].LastName ||
				beforeEnrichment.JapaneseName != canonical[canonicalIndex].JapaneseName {
				refreshCanonicalIDs[canonical[canonicalIndex].ID] = struct{}{}
				nfoCanonicalIDs[canonical[canonicalIndex].ID] = struct{}{}
			}
		}
	}

	m.setStage(task, "mapping")
	removedIDs, err := m.deps.MovieRepo.ReplaceUnverifiedActressesForMovie(ctx, movie.ContentID, canonical)
	if err != nil {
		return err
	}
	if len(removedIDs) > 0 {
		task.UpdatedFields = appendUnique(task.UpdatedFields, "movie_actresses")
	}
	if len(enrichmentIndexes) > 0 {
		translatedActresses := make([]models.Actress, len(enrichmentIndexes))
		for index, canonicalIndex := range enrichmentIndexes {
			translatedActresses[index] = canonical[canonicalIndex]
		}
		if err := m.storeActressTranslations(ctx, translationRecords, translatedActresses); err != nil {
			warning = appendWarning(warning, err.Error())
		} else if len(translationRecords) > 0 {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "actress_translations")
			for _, actress := range translatedActresses {
				refreshCanonicalIDs[actress.ID] = struct{}{}
			}
		}
	}
	if warning != "" {
		task.Warning = appendWarning(task.Warning, warning)
		logging.Warnf("Actress sync task warning (task=%s label=%q stage=%s): %s", task.ID, task.Label, task.Stage, warning)
	}

	refreshMovies := make(map[string]models.Movie)
	nfoMovieIDs := make(map[string]struct{})
	if len(removedIDs) > 0 {
		updatedMovie, findErr := m.deps.MovieRepo.FindByContentID(ctx, movie.ContentID)
		if findErr != nil {
			return findErr
		}
		refreshMovies[updatedMovie.ContentID] = *updatedMovie
		nfoMovieIDs[updatedMovie.ContentID] = struct{}{}
	}
	for actressID := range refreshCanonicalIDs {
		movies, listErr := m.deps.MovieRepo.ListByActressID(ctx, actressID, 0, 0)
		if listErr != nil {
			return listErr
		}
		for _, affected := range movies {
			refreshMovies[affected.ContentID] = affected
			if _, needsNFO := nfoCanonicalIDs[actressID]; needsNFO {
				nfoMovieIDs[affected.ContentID] = struct{}{}
			}
		}
	}
	if len(refreshMovies) > 0 {
		movies := make([]models.Movie, 0, len(refreshMovies))
		for _, affected := range refreshMovies {
			movies = append(movies, affected)
		}
		m.refreshAffectedMovies(ctx, task, movies, nfoMovieIDs)
	}
	for _, removedID := range removedIDs {
		if count, countErr := m.deps.MovieRepo.CountByActressID(ctx, removedID); countErr == nil && count == 0 {
			_ = m.deps.ActressRepo.Delete(ctx, removedID)
		}
	}
	if len(task.UpdatedFields) == 0 && task.Warning == "" {
		task.Status, task.Outcome = models.ActressSyncTaskSkipped, string(ActressSyncSkipped)
	} else if task.Warning != "" {
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated_with_warning"
	} else {
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated"
	}
	return nil
}

func (m *ActressSyncManager) cleanFallbackMovieActresses(ctx context.Context, task *models.ActressSyncTask, movie models.Movie) error {
	changed := make([]models.Actress, 0)
	affectedMovies := make(map[string]models.Movie)
	for _, mapped := range movie.Actresses {
		if mapped.DMMID > 0 {
			continue
		}
		actress, err := m.deps.ActressRepo.FindByID(ctx, mapped.ID)
		if err != nil {
			if database.IsNotFound(err) {
				continue
			}
			return err
		}
		if !translation.CleanStoredActress(actress) {
			continue
		}
		if err := m.deps.ActressRepo.Update(ctx, actress); err != nil {
			return err
		}
		changed = append(changed, *actress)
		task.UpdatedFields = appendUnique(task.UpdatedFields, "japanese_name")
		movies, listErr := m.deps.MovieRepo.ListByActressID(ctx, actress.ID, 0, 0)
		if listErr != nil {
			return listErr
		}
		for _, affected := range movies {
			affectedMovies[affected.ContentID] = affected
		}
	}

	if len(changed) == 0 {
		task.Status, task.Outcome = models.ActressSyncTaskSkipped, string(ActressSyncSkipped)
		task.Messages = append(task.Messages, "SougouWiki returned no verified actresses; the cleaned fallback cast was unchanged")
		return nil
	}

	warning, translateErr := m.translateAndStore(ctx, task, changed)
	if translateErr != nil {
		warning = appendWarning(warning, translateErr.Error())
	}
	if warning != "" {
		task.Warning = appendWarning(task.Warning, warning)
		logging.Warnf("Actress sync task warning (task=%s movie=%s stage=%s): %s", task.ID, movie.ID, task.Stage, warning)
	}
	if len(affectedMovies) > 0 {
		movies := make([]models.Movie, 0, len(affectedMovies))
		nfoMovieIDs := make(map[string]struct{}, len(affectedMovies))
		for _, affected := range affectedMovies {
			movies = append(movies, affected)
			nfoMovieIDs[affected.ContentID] = struct{}{}
		}
		m.refreshAffectedMovies(ctx, task, movies, nfoMovieIDs)
	}
	if task.Warning != "" {
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated_with_warning"
	} else {
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated"
	}
	return nil
}

func (m *ActressSyncManager) translateActresses(ctx context.Context, task *models.ActressSyncTask, actresses []models.Actress) ([]models.Actress, []models.MovieTranslation, string, error) {
	cfg := m.deps.GetConfig().Metadata.Translation
	if !cfg.Enabled || !cfg.Fields.Actresses || len(actresses) == 0 {
		return actresses, nil, "", nil
	}
	m.setStage(task, "translating")
	service := translation.NewWithProviderLimiter(cfg, m.acquireLLM, m.releaseLLM)
	translated, records, warning, err := service.TranslateActresses(ctx, actresses, cfg.SettingsHash())
	translated, records = preserveResolvedActressTranslations(actresses, translated, records)
	return translated, records, warning, err
}

func (m *ActressSyncManager) translateAndStore(ctx context.Context, task *models.ActressSyncTask, actresses []models.Actress) (string, error) {
	translated, records, warning, err := m.translateActresses(ctx, task, actresses)
	if len(translated) > 0 {
		for i := range translated {
			if translated[i].JapaneseName == "" {
				translated[i].JapaneseName = actresses[i].JapaneseName
			}
			nameChanged := translated[i].FirstName != actresses[i].FirstName || translated[i].LastName != actresses[i].LastName
			if updateErr := m.deps.ActressRepo.Update(ctx, &translated[i]); updateErr != nil {
				return warning, updateErr
			}
			if nameChanged {
				task.UpdatedFields = appendUnique(task.UpdatedFields, "translated_name")
			}
		}
		if storeErr := m.storeActressTranslations(ctx, records, translated); storeErr != nil {
			return appendWarning(warning, storeErr.Error()), err
		} else if len(records) > 0 {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "actress_translations")
		}
	}
	return warning, err
}

func (m *ActressSyncManager) storeActressTranslations(ctx context.Context, records []models.MovieTranslation, actresses []models.Actress) error {
	if len(records) == 0 {
		return nil
	}
	repo := database.NewActressTranslationRepository(m.deps.DB)
	for _, record := range records {
		for i, name := range record.Actresses {
			if i >= len(actresses) || strings.TrimSpace(name) == "" {
				continue
			}
			firstName, lastName := models.SplitActressName(name)
			if err := repo.Upsert(ctx, &models.ActressTranslation{
				ActressID: actresses[i].ID, Language: record.Language,
				FirstName: firstName, LastName: lastName, JapaneseName: actresses[i].JapaneseName,
				DisplayName: name, SourceName: record.SourceName, SettingsHash: record.SettingsHash,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *ActressSyncManager) refreshAffectedMovies(ctx context.Context, task *models.ActressSyncTask, movies []models.Movie, nfoMovieIDs map[string]struct{}) {
	cfg := m.deps.GetConfig()
	translationCfg := cfg.Metadata.Translation
	translationRepo := database.NewMovieTranslationRepository(m.deps.DB)
	actressTranslationRepo := database.NewActressTranslationRepository(m.deps.DB)
	for i := range movies {
		movie, err := m.deps.MovieRepo.FindByContentID(ctx, movies[i].ContentID)
		if err != nil {
			task.Warning = appendWarning(task.Warning, err.Error())
			continue
		}
		if translationCfg.Enabled && translationCfg.Fields.Actresses {
			service := translation.New(translationCfg)
			for _, language := range service.TargetLanguages() {
				ids := make([]uint, 0, len(movie.Actresses))
				for _, actress := range movie.Actresses {
					ids = append(ids, actress.ID)
				}
				stored, lookupErr := actressTranslationRepo.FindByActressIDsAndLanguage(ctx, ids, language)
				if lookupErr != nil {
					task.Warning = appendWarning(task.Warning, lookupErr.Error())
					continue
				}
				names := make([]string, len(movie.Actresses))
				for idx, actress := range movie.Actresses {
					if translations := stored[actress.ID]; len(translations) > 0 && translations[0].DisplayName != "" {
						names[idx] = translations[0].DisplayName
					} else {
						names[idx] = actress.FullName()
					}
				}
				record, findErr := translationRepo.FindByMovieAndLanguage(ctx, movie.ContentID, language)
				if findErr != nil && !database.IsNotFound(findErr) {
					task.Warning = appendWarning(task.Warning, findErr.Error())
					continue
				}
				if record == nil {
					record = &models.MovieTranslation{MovieID: movie.ContentID, Language: language, SourceName: "translation:" + translationCfg.Provider}
				}
				record.Actresses = names
				record.SettingsHash = translationCfg.SettingsHash()
				if err := translationRepo.Upsert(ctx, record); err != nil {
					task.Warning = appendWarning(task.Warning, err.Error())
				} else {
					task.UpdatedFields = appendUnique(task.UpdatedFields, "movie_translation_actresses")
				}
			}
		}

		if _, updateNFO := nfoMovieIDs[movie.ContentID]; !updateNFO {
			continue
		}
		m.setStage(task, "nfo")
		path, nfoErr := syncMovieNFO(ctx, movie, cfg, m.deps.HistoryRepo, m.deps.BatchFileOpRepo)
		if nfoErr != nil {
			task.Warning = appendWarning(task.Warning, fmt.Sprintf("%s: %v", movie.ID, nfoErr))
		} else if path != "" {
			task.UpdatedFields = appendUnique(task.UpdatedFields, "nfo")
		}
	}
}

func (m *ActressSyncManager) acquireLLM(ctx context.Context) error {
	for {
		limit := 3
		if cfg := m.deps.GetConfig(); cfg != nil && cfg.Metadata.Translation.MaxConcurrency > 0 {
			limit = cfg.Metadata.Translation.MaxConcurrency
		}
		m.llmMu.Lock()
		if m.llmActive < limit {
			m.llmActive++
			m.llmMu.Unlock()
			return nil
		}
		m.llmMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (m *ActressSyncManager) releaseLLM() {
	m.llmMu.Lock()
	if m.llmActive > 0 {
		m.llmActive--
	}
	m.llmMu.Unlock()
}

func verifiedActresses(result *models.ScraperResult) []models.ActressInfo {
	if result == nil {
		return nil
	}
	seen := make(map[int]struct{})
	verified := make([]models.ActressInfo, 0, len(result.Actresses))
	for _, actress := range result.Actresses {
		if actress.DMMID <= 0 || models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) ||
			models.IsDescriptiveNonName(actress.LastName, actress.FirstName, actress.JapaneseName) {
			continue
		}
		if _, exists := seen[actress.DMMID]; exists {
			continue
		}
		seen[actress.DMMID] = struct{}{}
		verified = append(verified, actress)
	}
	return verified
}

func hasUsableActressIdentityProfile(actress models.Actress) bool {
	return hasUsableActressJapaneseName(actress.JapaneseName) && hasUsableActressPrimaryProfile(actress)
}

func hasUsableActressJapaneseName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !models.IsUnknownActressName(name) && !models.IsDescriptiveNonName("", "", name)
}

func hasUsableActressPrimaryProfile(actress models.Actress) bool {
	return hasUsableActressPrimaryFields(actress.LastName, actress.FirstName)
}

func hasUsableActressPrimaryFields(lastName, firstName string) bool {
	lastName = strings.TrimSpace(lastName)
	firstName = strings.TrimSpace(firstName)
	if lastName == "" && firstName == "" {
		return false
	}
	for _, value := range []string{lastName, firstName, strings.TrimSpace(lastName + " " + firstName), strings.TrimSpace(firstName + " " + lastName)} {
		if value != "" && models.IsUnknownActressName(value) {
			return false
		}
	}
	return !models.IsDescriptiveNonName(lastName, firstName, "")
}

func uniqueActressIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func actressSyncActressLabel(actress models.Actress) string {
	name := strings.TrimSpace(actress.FullName())
	if name == "" {
		name = fmt.Sprintf("Actress #%d", actress.ID)
	}
	return fmt.Sprintf("%s (#%d)", name, actress.ID)
}

func appendWarning(current, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	return current + "; " + next
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func containsAnyField(values []string, candidates ...string) bool {
	wanted := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		wanted[candidate] = struct{}{}
	}
	for _, value := range values {
		if _, exists := wanted[value]; exists {
			return true
		}
	}
	return false
}

func preserveResolvedActressTranslations(original, translated []models.Actress, records []models.MovieTranslation) ([]models.Actress, []models.MovieTranslation) {
	if len(translated) != len(original) {
		translated = append([]models.Actress(nil), original...)
	}
	for i := range original {
		// JapaneseName is the authoritative source identity. Translation only
		// updates FirstName and LastName.
		translated[i].JapaneseName = original[i].JapaneseName
		translatedUnknown := models.IsUnknownActressFields(translated[i].LastName, translated[i].FirstName, translated[i].JapaneseName) ||
			models.IsUnknownActressName(translated[i].FirstName) || models.IsUnknownActressName(translated[i].LastName)
		originalHasHangul := containsHangul(translatedActressPrimaryName(original[i]))
		translatedHasHangul := containsHangul(translatedActressPrimaryName(translated[i]))
		if translatedUnknown || (originalHasHangul && !translatedHasHangul) {
			translated[i].FirstName = original[i].FirstName
			translated[i].LastName = original[i].LastName
		}
	}
	for recordIndex := range records {
		for actressIndex := range records[recordIndex].Actresses {
			if actressIndex >= len(translated) {
				break
			}
			name := strings.TrimSpace(records[recordIndex].Actresses[actressIndex])
			if name == "" || models.IsUnknownActressName(name) {
				records[recordIndex].Actresses[actressIndex] = resolvedActressDisplayName(translated[actressIndex])
			}
		}
	}
	return translated, records
}

func translatedActressPrimaryName(actress models.Actress) string {
	return strings.TrimSpace(actress.LastName + " " + actress.FirstName)
}

func resolvedActressDisplayName(actress models.Actress) string {
	if primary := translatedActressPrimaryName(actress); primary != "" && !models.IsUnknownActressName(primary) {
		return primary
	}
	return strings.TrimSpace(actress.JapaneseName)
}

func containsHangul(value string) bool {
	for _, char := range value {
		if unicode.In(char, unicode.Hangul) {
			return true
		}
	}
	return false
}
