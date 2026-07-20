package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/javinizer/javinizer-go/internal/worker"
	"gorm.io/gorm"
)

// JobReprocessReport summarizes a stored-source maintenance pass.
type JobReprocessReport struct {
	JobID             string `json:"job_id"`
	Results           int    `json:"results"`
	SourcesTranslated int    `json:"sources_translated"`
	UnknownCasts      int    `json:"unknown_casts"`
	Applied           bool   `json:"applied"`
}

// JobReprocessOptions controls persisted-source reprocessing.
type JobReprocessOptions struct {
	Apply            bool
	AdditionalModels []string
}

// ReprocessStoredJob retranslates and remaps a completed, not-yet-organized
// job exclusively from its persisted source envelope. It never calls a metadata
// scraper. Network access is limited to the configured translation provider.
func ReprocessStoredJob(ctx context.Context, db *database.DB, cfg *config.Config, jobID string, apply bool) (*JobReprocessReport, error) {
	return ReprocessStoredJobWithOptions(ctx, db, cfg, jobID, JobReprocessOptions{Apply: apply})
}

// ReprocessStoredJobWithOptions retranslates with optional additional models.
func ReprocessStoredJobWithOptions(ctx context.Context, db *database.DB, cfg *config.Config, jobID string, options JobReprocessOptions) (*JobReprocessReport, error) {
	if db == nil || cfg == nil {
		return nil, fmt.Errorf("job reprocess requires database and config")
	}
	var job models.Job
	if err := db.WithContext(ctx).First(&job, "id = ?", strings.TrimSpace(jobID)).Error; err != nil {
		return nil, fmt.Errorf("load job %s: %w", jobID, err)
	}
	if job.OrganizedAt != nil {
		return nil, fmt.Errorf("job %s is already organized", jobID)
	}
	parsed, err := worker.ParseJobResultsJSON([]byte(job.Results))
	if err != nil {
		return nil, fmt.Errorf("parse job %s results: %w", jobID, err)
	}
	if len(parsed.Results) == 0 {
		return nil, fmt.Errorf("job %s has no results", jobID)
	}

	index, err := loadActressIndex(ctx, db, cfg.Metadata.Translation.TargetLanguage)
	if err != nil {
		return nil, err
	}
	checkpointPath := reprocessCheckpointPath(jobID)
	translatedCount, err := retranslateSelectedFields(ctx, cfg.Metadata.Translation, parsed, index, options.AdditionalModels, checkpointPath)
	if err != nil {
		return nil, err
	}

	report := &JobReprocessReport{JobID: jobID, Results: len(parsed.Results), SourcesTranslated: translatedCount, Applied: options.Apply}
	for filePath, result := range parsed.Results {
		if result == nil || result.Movie == nil || result.Status != models.JobStatusCompleted {
			continue
		}
		prov := parsed.Provenance[filePath]
		if prov == nil {
			return nil, fmt.Errorf("%s: persisted provenance is missing", result.FileMatchInfo.MovieID)
		}
		oldTitle := result.Movie.Title
		titleSource := findStoredSource(prov.ScraperResults, prov.FieldSources["title"])
		descriptionSource := findStoredSource(prov.ScraperResults, prov.FieldSources["description"])
		actressSourceName := strings.TrimSpace(prov.FieldSources["actresses"])
		actressSource := findStoredSource(prov.ScraperResults, actressSourceName)

		if titleSource == nil {
			return nil, fmt.Errorf("%s: selected title source %q is unavailable", result.FileMatchInfo.MovieID, prov.FieldSources["title"])
		}
		result.Movie.Title = translatedSourceField(titleSource, cfg.Metadata.Translation.TargetLanguage, "title")
		if result.Movie.Title == "" {
			return nil, fmt.Errorf("%s: translated title is empty", result.FileMatchInfo.MovieID)
		}
		if descriptionSource != nil {
			result.Movie.Description = translatedSourceField(descriptionSource, cfg.Metadata.Translation.TargetLanguage, "description")
		}

		actresses, unknown := index.resolveCast(actressSource)
		if unknown {
			report.UnknownCasts++
		}
		result.Movie.Actresses = actresses
		prov.ActressSources = make(map[string]string, len(actresses))
		for _, actress := range actresses {
			if key := scrape.ActressSourceKey(actress); key != "" {
				prov.ActressSources[key] = actressSourceName
			}
		}
		refreshMovieTranslation(result.Movie, cfg.Metadata.Translation, titleSource, descriptionSource)
		refreshDisplayTitleWithoutMedia(result.Movie, oldTitle)
		result.Revision++
	}

	envelope := worker.JobResultsEnvelope{Domain: parsed.Results, Provenance: parsed.Provenance}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal reprocessed job: %w", err)
	}
	if !options.Apply {
		return report, nil
	}

	movieTranslationRepo := database.NewMovieTranslationRepository(db)
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, result := range parsed.Results {
			if result == nil || result.Movie == nil || result.Status != models.JobStatusCompleted {
				continue
			}
			movie := result.Movie
			updates := map[string]any{
				"title": movie.Title, "description": movie.Description,
				"display_title": movie.DisplayTitle, "updated_at": time.Now().UTC(),
			}
			updated := tx.Model(&models.Movie{}).Where("content_id = ?", movie.ContentID).Updates(updates)
			if updated.Error != nil || updated.RowsAffected != 1 {
				if updated.Error != nil {
					return updated.Error
				}
				return fmt.Errorf("movie %s was not updated", movie.ContentID)
			}
			if err := tx.Exec("DELETE FROM movie_actresses WHERE movie_content_id = ?", movie.ContentID).Error; err != nil {
				return err
			}
			for _, actress := range movie.Actresses {
				if actress.ID == 0 {
					return fmt.Errorf("movie %s has an unpersisted actress", movie.ContentID)
				}
				if err := tx.Exec("INSERT INTO movie_actresses(movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error; err != nil {
					return err
				}
			}
			for i := range movie.Translations {
				movie.Translations[i].MovieID = movie.ContentID
				if err := movieTranslationRepo.UpsertTx(tx, &movie.Translations[i]); err != nil {
					return err
				}
			}
		}
		return tx.Model(&models.Job{}).Where("id = ? AND organized_at IS NULL", jobID).Update("results", string(encoded)).Error
	})
	if err != nil {
		return nil, fmt.Errorf("persist reprocessed job: %w", err)
	}
	if err := os.Remove(checkpointPath); err != nil && !os.IsNotExist(err) {
		logging.Warnf("Stored job reprocess: remove checkpoint %s: %v", checkpointPath, err)
	}
	return report, nil
}

type actressIndex struct {
	byDMM       map[int]models.Actress
	byID        map[uint]models.Actress
	byJapanese  map[string][]models.Actress
	byAlias     map[string]models.Actress
	linkedIDs   map[uint][]uint
	unknown     models.Actress
	targetNames map[uint]models.ActressTranslation
}

func loadActressIndex(ctx context.Context, db *database.DB, language string) (*actressIndex, error) {
	var actresses []models.Actress
	if err := db.WithContext(ctx).Find(&actresses).Error; err != nil {
		return nil, err
	}
	idx := &actressIndex{byDMM: map[int]models.Actress{}, byID: map[uint]models.Actress{}, byJapanese: map[string][]models.Actress{}, byAlias: map[string]models.Actress{}, linkedIDs: map[uint][]uint{}, targetNames: map[uint]models.ActressTranslation{}}
	byID := make(map[uint]models.Actress, len(actresses))
	for _, actress := range actresses {
		byID[actress.ID] = actress
		idx.byID[actress.ID] = actress
		if actress.DMMID > 0 {
			idx.byDMM[actress.DMMID] = actress
		}
		key := models.NormalizeActressNameKey(actress.JapaneseName)
		if key != "" {
			idx.byJapanese[key] = append(idx.byJapanese[key], actress)
		}
		if models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
			idx.unknown = actress
		}
	}
	if idx.unknown.ID == 0 {
		return nil, fmt.Errorf("canonical Unknown actress is missing")
	}
	var aliases []models.ActressAlias
	if err := db.WithContext(ctx).Find(&aliases).Error; err != nil {
		return nil, err
	}
	for _, alias := range aliases {
		candidate, ok := byID[alias.AliasActressID]
		if !ok {
			candidate, ok = byID[alias.CanonicalActressID]
		}
		if ok {
			idx.byAlias[models.NormalizeActressNameKey(alias.AliasName)] = candidate
		}
		if alias.AliasActressID != 0 && alias.CanonicalActressID != 0 {
			idx.linkedIDs[alias.AliasActressID] = append(idx.linkedIDs[alias.AliasActressID], alias.CanonicalActressID)
			idx.linkedIDs[alias.CanonicalActressID] = append(idx.linkedIDs[alias.CanonicalActressID], alias.AliasActressID)
		}
	}
	var translations []models.ActressTranslation
	if err := db.WithContext(ctx).Where("language = ?", normalizeLanguage(language)).Find(&translations).Error; err != nil {
		return nil, err
	}
	for _, item := range translations {
		idx.targetNames[item.ActressID] = item
	}
	return idx, nil
}

func (idx *actressIndex) localized(actress models.Actress) models.Actress {
	if translated, ok := idx.targetNames[actress.ID]; ok && strings.TrimSpace(translated.FirstName+translated.LastName) != "" {
		actress.FirstName = translated.FirstName
		actress.LastName = translated.LastName
	}
	return actress
}

func (idx *actressIndex) translationVariants(actresses []models.Actress) []models.Actress {
	seen := make(map[uint]struct{})
	variants := make([]models.Actress, 0, len(actresses))
	add := func(actress models.Actress) {
		if actress.ID == 0 {
			return
		}
		if _, exists := seen[actress.ID]; exists {
			return
		}
		seen[actress.ID] = struct{}{}
		variants = append(variants, idx.localized(actress))
	}
	for _, actress := range actresses {
		add(actress)
		for _, linkedID := range idx.linkedIDs[actress.ID] {
			if linked, ok := idx.byID[linkedID]; ok {
				add(linked)
			}
		}
	}
	return variants
}

func (idx *actressIndex) resolveCast(source *models.ScraperResult) ([]models.Actress, bool) {
	if source == nil || len(source.Actresses) == 0 {
		return []models.Actress{idx.localized(idx.unknown)}, true
	}
	// Multiple performers with no verified identity are not safely separable.
	if len(source.Actresses) > 1 {
		allUnverified := true
		for _, actress := range source.Actresses {
			if actress.DMMID > 0 {
				allUnverified = false
				break
			}
		}
		if allUnverified {
			return []models.Actress{idx.localized(idx.unknown)}, true
		}
	}
	seen := make(map[uint]struct{})
	resolved := make([]models.Actress, 0, len(source.Actresses))
	for _, raw := range source.Actresses {
		if models.IsDescriptiveNonName(raw.LastName, raw.FirstName, raw.JapaneseName) || models.IsUnknownActressFields(raw.LastName, raw.FirstName, raw.JapaneseName) {
			return []models.Actress{idx.localized(idx.unknown)}, true
		}
		var actress models.Actress
		var ok bool
		if raw.DMMID > 0 {
			actress, ok = idx.byDMM[raw.DMMID]
		}
		if !ok {
			key := models.NormalizeActressNameKey(raw.JapaneseName)
			matches := idx.byJapanese[key]
			if len(matches) == 1 {
				actress, ok = matches[0], true
			} else if alias, exists := idx.byAlias[key]; exists {
				actress, ok = alias, true
			}
		}
		if !ok || actress.ID == 0 {
			return []models.Actress{idx.localized(idx.unknown)}, true
		}
		if _, exists := seen[actress.ID]; exists {
			continue
		}
		seen[actress.ID] = struct{}{}
		resolved = append(resolved, idx.localized(actress))
	}
	if len(resolved) == 0 {
		return []models.Actress{idx.localized(idx.unknown)}, true
	}
	return resolved, false
}

type reprocessTranslationItem struct {
	groupKey          string
	movieID           string
	titleSource       *models.ScraperResult
	descriptionSource *models.ScraperResult
	actresses         []models.Actress
}

type reprocessTranslationCheckpointEntry struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	SourceName   string `json:"source_name"`
	SettingsHash string `json:"settings_hash"`
}

type reprocessTranslationCheckpoint struct {
	mu      sync.Mutex
	path    string
	entries map[string]reprocessTranslationCheckpointEntry
}

func reprocessCheckpointPath(jobID string) string {
	return filepath.Join(os.TempDir(), "javinizer-reprocess-"+strings.TrimSpace(jobID)+".json")
}

func loadReprocessTranslationCheckpoint(path string) (*reprocessTranslationCheckpoint, error) {
	checkpoint := &reprocessTranslationCheckpoint{path: path, entries: map[string]reprocessTranslationCheckpointEntry{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return checkpoint, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &checkpoint.entries); err != nil {
		return nil, fmt.Errorf("decode checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (c *reprocessTranslationCheckpoint) store(current []reprocessTranslationItem, targetLanguage string) error {
	if c == nil || len(current) == 0 {
		return nil
	}
	first := current[0]
	entry := reprocessTranslationCheckpointEntry{
		Title:       translatedSourceField(first.titleSource, targetLanguage, "title"),
		Description: translatedSourceField(first.descriptionSource, targetLanguage, "description"),
	}
	for _, record := range first.titleSource.Translations {
		if normalizeLanguage(record.Language) == normalizeLanguage(targetLanguage) {
			entry.SourceName, entry.SettingsHash = record.SourceName, record.SettingsHash
			break
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[first.groupKey] = entry
	data, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	temporary := c.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, c.path)
}

func applyCheckpointEntry(current []reprocessTranslationItem, entry reprocessTranslationCheckpointEntry, targetLanguage string) {
	incoming := models.MovieTranslation{Language: targetLanguage, Title: entry.Title, Description: entry.Description, SourceName: entry.SourceName, SettingsHash: entry.SettingsHash}
	for _, target := range current {
		mergeSourceTranslationField(target.titleSource, incoming, "title")
		if target.descriptionSource != nil {
			mergeSourceTranslationField(target.descriptionSource, incoming, "description")
		}
	}
}

func retranslateSelectedFields(ctx context.Context, tc config.TranslationConfig, parsed *worker.ParsedJobResults, index *actressIndex, additionalModels []string, checkpointPath string) (int, error) {
	if !tc.Enabled {
		return 0, fmt.Errorf("metadata translation is disabled")
	}
	tc.ApplyToPrimary = false
	tc.OverwriteExistingTarget = true
	tc.Fields = config.TranslationFieldsConfig{Title: true, Description: true}
	grouped := make(map[string][]reprocessTranslationItem)
	for filePath, result := range parsed.Results {
		if result == nil || result.Status != models.JobStatusCompleted {
			continue
		}
		prov := parsed.Provenance[filePath]
		if prov == nil {
			return 0, fmt.Errorf("%s: persisted provenance is missing", result.FileMatchInfo.MovieID)
		}
		titleSource := findStoredSource(prov.ScraperResults, prov.FieldSources["title"])
		if titleSource == nil || strings.TrimSpace(titleSource.Title) == "" {
			return 0, fmt.Errorf("%s: selected title source %q is unavailable", result.FileMatchInfo.MovieID, prov.FieldSources["title"])
		}
		descriptionSource := findStoredSource(prov.ScraperResults, prov.FieldSources["description"])
		actressSource := findStoredSource(prov.ScraperResults, prov.FieldSources["actresses"])
		actresses, _ := index.resolveCast(actressSource)
		translationActresses := index.translationVariants(actresses)
		title := strings.TrimSpace(titleSource.Title)
		description := ""
		if descriptionSource != nil {
			description = strings.TrimSpace(descriptionSource.Description)
		}
		var actressKey strings.Builder
		for _, actress := range translationActresses {
			_, _ = fmt.Fprintf(&actressKey, "\x00%d:%s:%s:%s", actress.ID, actress.JapaneseName, actress.LastName, actress.FirstName)
		}
		key := title + "\x00" + description + actressKey.String()
		grouped[key] = append(grouped[key], reprocessTranslationItem{
			groupKey: key,
			movieID:  result.FileMatchInfo.MovieID, titleSource: titleSource,
			descriptionSource: descriptionSource, actresses: translationActresses,
		})
	}
	groups := make([][]reprocessTranslationItem, 0, len(grouped))
	checkpoint, err := loadReprocessTranslationCheckpoint(checkpointPath)
	if err != nil {
		return 0, fmt.Errorf("load translation checkpoint: %w", err)
	}
	resumed := 0
	for key, items := range grouped {
		if entry, ok := checkpoint.entries[key]; ok && strings.TrimSpace(entry.Title) != "" {
			applyCheckpointEntry(items, entry, tc.TargetLanguage)
			resumed++
			continue
		}
		groups = append(groups, items)
	}
	if resumed > 0 {
		logging.Infof("Stored job reprocess: resumed %d translated groups from %s", resumed, checkpointPath)
	}
	workers := tc.MaxConcurrency
	if workers <= 0 {
		workers = 3
	}
	translationConfigs := translationModelConfigs(tc, additionalModels)
	work := make(chan []reprocessTranslationItem)
	var wg sync.WaitGroup
	var failedMu sync.Mutex
	failed := make([][]reprocessTranslationItem, 0)
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		workerConfig := translationConfigs[workerIndex%len(translationConfigs)]
		service := translation.New(workerConfig)
		wg.Add(1)
		go func(service *translation.Service, workerConfig config.TranslationConfig) {
			defer wg.Done()
			for current := range work {
				if err := translateAndReviewGroup(ctx, tc, current, service, workerConfig); err != nil {
					logging.Warnf("Stored job reprocess: deferring failed group %s for model fallback: %v", current[0].movieID, err)
					failedMu.Lock()
					failed = append(failed, current)
					failedMu.Unlock()
				} else if err := checkpoint.store(current, tc.TargetLanguage); err != nil {
					logging.Warnf("Stored job reprocess: save checkpoint for %s: %v", current[0].movieID, err)
				}
			}
		}(service, workerConfig)
	}
	for _, current := range groups {
		select {
		case <-ctx.Done():
			close(work)
			wg.Wait()
			return 0, ctx.Err()
		case work <- current:
		}
	}
	close(work)
	wg.Wait()

	for round := 0; len(failed) > 0 && round < len(translationConfigs)*2; round++ {
		retryWork := make(chan []reprocessTranslationItem)
		retryFailed := make([][]reprocessTranslationItem, 0, len(failed))
		var retryWG sync.WaitGroup
		var retryFailedMu sync.Mutex
		for workerIndex := 0; workerIndex < workers; workerIndex++ {
			workerConfig := translationConfigs[workerIndex%len(translationConfigs)]
			service := translation.New(workerConfig)
			retryWG.Add(1)
			go func(service *translation.Service, workerConfig config.TranslationConfig) {
				defer retryWG.Done()
				for current := range retryWork {
					if err := translateAndReviewGroup(ctx, tc, current, service, workerConfig); err != nil {
						logging.Warnf("Stored job reprocess: fallback round %d failed for %s with model %s: %v", round+1, current[0].movieID, workerConfig.OpenAICompatible.Model, err)
						retryFailedMu.Lock()
						retryFailed = append(retryFailed, current)
						retryFailedMu.Unlock()
					} else if err := checkpoint.store(current, tc.TargetLanguage); err != nil {
						logging.Warnf("Stored job reprocess: save fallback checkpoint for %s: %v", current[0].movieID, err)
					}
				}
			}(service, workerConfig)
		}
		for _, current := range failed {
			select {
			case <-ctx.Done():
				close(retryWork)
				retryWG.Wait()
				return 0, ctx.Err()
			case retryWork <- current:
			}
		}
		close(retryWork)
		retryWG.Wait()
		failed = retryFailed
	}
	if len(failed) > 0 {
		return 0, fmt.Errorf("translate selected fields for %s: all model fallbacks failed", failed[0][0].movieID)
	}
	return len(grouped), nil
}

func translateAndReviewGroup(ctx context.Context, tc config.TranslationConfig, current []reprocessTranslationItem, service *translation.Service, workerConfig config.TranslationConfig) error {
	representative := current[0]
	description := ""
	if representative.descriptionSource != nil {
		description = representative.descriptionSource.Description
	}
	movie := &models.Movie{Title: representative.titleSource.Title, Description: description, Actresses: representative.actresses}
	callCtx, callCancel := reprocessCallContext(ctx, tc)
	output, _, err := service.TranslateMovie(callCtx, movie, workerConfig.SettingsHash())
	callCancel()
	if err == nil && output != nil && output.Movie != nil {
		reviewFields := []translation.QualityReviewField{{FieldName: "quality_review_title", Source: movie.Title, Candidate: output.Movie.Title}}
		if strings.TrimSpace(movie.Description) != "" && strings.TrimSpace(output.Movie.Description) != "" {
			reviewFields = append(reviewFields, translation.QualityReviewField{FieldName: "quality_review_description", Source: movie.Description, Candidate: output.Movie.Description})
		}
		var reviewed []string
		reviewCtx, reviewCancel := reprocessCallContext(ctx, tc)
		reviewed, err = service.ReviewJAVTranslations(reviewCtx, reviewFields)
		reviewCancel()
		if err == nil && len(reviewed) == len(reviewFields) {
			output.Movie.Title = strings.TrimSpace(reviewed[0])
			if len(reviewFields) > 1 {
				output.Movie.Description = strings.TrimSpace(reviewed[1])
			}
		} else if err == nil {
			err = fmt.Errorf("quality reviewer returned %d items for %d inputs", len(reviewed), len(reviewFields))
		}
	}
	if err != nil || output == nil || output.Movie == nil {
		if err == nil {
			err = fmt.Errorf("empty translation result")
		}
		return fmt.Errorf("translate selected fields for %s: %w", representative.movieID, err)
	}
	for _, target := range current {
		mergeSourceTranslationField(target.titleSource, *output.Movie, "title")
		if target.descriptionSource != nil {
			mergeSourceTranslationField(target.descriptionSource, *output.Movie, "description")
		}
	}
	return nil
}

func reprocessCallContext(ctx context.Context, tc config.TranslationConfig) (context.Context, context.CancelFunc) {
	if tc.TimeoutSeconds <= 0 {
		return context.WithCancel(ctx)
	}
	timeout := time.Duration(tc.TimeoutSeconds) * time.Second
	if timeout < 5*time.Minute {
		timeout = 5 * time.Minute
	}
	return context.WithTimeout(ctx, timeout)
}

func translationModelConfigs(tc config.TranslationConfig, additionalModels []string) []config.TranslationConfig {
	configs := []config.TranslationConfig{tc}
	if !strings.EqualFold(strings.TrimSpace(tc.Provider), "openai-compatible") {
		return configs
	}
	seen := map[string]struct{}{strings.TrimSpace(tc.OpenAICompatible.Model): {}}
	for _, model := range additionalModels {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		current := tc
		current.OpenAICompatible.Model = model
		configs = append(configs, current)
	}
	return configs
}

func mergeSourceTranslationField(result *models.ScraperResult, incoming models.MovieTranslation, field string) {
	if result == nil {
		return
	}
	target := normalizeLanguage(incoming.Language)
	for i := range result.Translations {
		if normalizeLanguage(result.Translations[i].Language) == target {
			if field == "description" {
				result.Translations[i].Description = incoming.Description
			} else {
				result.Translations[i].Title = incoming.Title
			}
			result.Translations[i].SourceName = incoming.SourceName
			result.Translations[i].SettingsHash = incoming.SettingsHash
			return
		}
	}
	record := models.MovieTranslation{Language: incoming.Language, SourceName: incoming.SourceName, SettingsHash: incoming.SettingsHash}
	if field == "description" {
		record.Description = incoming.Description
	} else {
		record.Title = incoming.Title
	}
	result.Translations = append(result.Translations, record)
}

func findStoredSource(results []*models.ScraperResult, source string) *models.ScraperResult {
	for _, result := range results {
		if result != nil && strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(source)) {
			return result
		}
	}
	return nil
}

func translatedSourceField(source *models.ScraperResult, language, field string) string {
	if source == nil {
		return ""
	}
	for _, record := range source.Translations {
		if normalizeLanguage(record.Language) != normalizeLanguage(language) {
			continue
		}
		if field == "description" {
			return strings.TrimSpace(record.Description)
		}
		return strings.TrimSpace(record.Title)
	}
	if field == "description" {
		return strings.TrimSpace(source.Description)
	}
	return strings.TrimSpace(source.Title)
}

func refreshMovieTranslation(movie *models.Movie, tc config.TranslationConfig, titleSource, descriptionSource *models.ScraperResult) {
	language := normalizeLanguage(tc.TargetLanguage)
	record := models.MovieTranslation{Language: language, SourceName: "translation:" + strings.ToLower(strings.TrimSpace(tc.Provider)), SettingsHash: tc.SettingsHash()}
	for _, current := range movie.Translations {
		if normalizeLanguage(current.Language) == language {
			record = current
			break
		}
	}
	record.Title = translatedSourceField(titleSource, language, "title")
	record.Description = translatedSourceField(descriptionSource, language, "description")
	record.SourceName = "translation:" + strings.ToLower(strings.TrimSpace(tc.Provider))
	record.SettingsHash = tc.SettingsHash()
	record.Actresses = make([]string, 0, len(movie.Actresses))
	for _, actress := range movie.Actresses {
		record.Actresses = append(record.Actresses, strings.TrimSpace(actress.LastName+" "+actress.FirstName))
	}
	for i := range movie.Translations {
		if normalizeLanguage(movie.Translations[i].Language) == language {
			movie.Translations[i] = record
			return
		}
	}
	movie.Translations = append(movie.Translations, record)
}

func refreshDisplayTitleWithoutMedia(movie *models.Movie, oldTitle string) {
	if movie == nil {
		return
	}
	if oldTitle != "" && strings.HasSuffix(movie.DisplayTitle, oldTitle) {
		movie.DisplayTitle = strings.TrimSuffix(movie.DisplayTitle, oldTitle) + movie.Title
		return
	}
	movie.DisplayTitle = movie.Title
}

func normalizeLanguage(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
