package scrape

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// tryCache checks the movie database for a previously scraped result.
// On cache hit, returns a ScrapeResult with the cached movie data.
// Poster generation is intentionally NOT triggered for cache hits — the poster
// already exists on disk from the original scrape, and re-generating it would
// be redundant (posters are keyed by movie ID + format, not translation hash).
func (s *Scraper) tryCache(ctx context.Context, cmd ScrapeCmd, actressRepo database.ActressRepositoryInterface, startTime time.Time) *ScrapeResult {
	if s.movieRepo == nil {
		return nil
	}

	cached, err := s.movieRepo.FindByID(ctx, cmd.MovieID)
	if err != nil {
		if !database.IsNotFound(err) {
			logging.Debugf("[scrape] Cache lookup failed for %s: %v", cmd.MovieID, err)
		}
		return nil
	}

	logging.Debugf("[scrape] Found %s in cache (Title=%s, Maker=%s)", cmd.MovieID, cached.Title, cached.Maker)

	// Preserve the pre-repair cache payload for the source viewer. The returned
	// Movie may be normalized or replaced by SougouWiki below, but provenance
	// should still show what was originally stored.
	cachedSourceResult := ScraperResultFromCachedMovie(cached)
	actressesChanged, resolverResult := s.repairCachedActresses(ctx, cached, cachedSourceResult, actressRepo)

	needsPersistence := actressesChanged
	translationWarning := ""
	var translationOutput *translation.TranslationOutput
	if s.cfg != nil && s.cfg.TranslationEnabled {
		currentHash := s.cfg.TranslationSettingsHash
		targetLang := s.cfg.TranslationTargetLang
		hasValidTranslation := false
		for _, trans := range cached.Translations {
			if trans.Language == targetLang && trans.SettingsHash == currentHash {
				hasValidTranslation = true
				break
			}
		}
		if !hasValidTranslation || actressesChanged {
			logging.Infof("[scrape] Cached metadata or translation settings changed, re-translating result for %s", cmd.MovieID)
			warn, transOutput := applyTranslation(ctx, cached, s.translator)
			if warn != "" {
				translationWarning = warn
				logging.Warnf("[scrape] Partial translation warning for cached %s: %s", cmd.MovieID, warn)
			}
			translationOutput = transOutput
			needsPersistence = true
		}
	}

	scrapedToReturn := cached
	fieldSources := buildFieldSourcesFromCachedMovie(cached)
	actressSources := buildActressSourcesFromCachedMovie(cached)
	if hasCanonicalUnknownActressCast(cached) {
		fieldSources["actresses"] = "empty"
		actressSources = nil
	} else if resolverResult != nil {
		for _, actress := range cached.Actresses {
			if key := ActressSourceKey(actress); key != "" {
				if actressSources == nil {
					actressSources = make(map[string]string)
				}
				actressSources[key] = actressResolverScraperName
			}
		}
	}

	if actressRepo != nil {
		if enriched := enrichActressesFromDB(ctx, scrapedToReturn, actressRepo, s.cfg); enriched > 0 {
			logging.Debugf("[scrape] Enriched %d actresses from database after cache hit", enriched)
		}
	}

	scraperResults := []*models.ScraperResult{cachedSourceResult}
	if resolverResult != nil {
		scraperResults = append(scraperResults, resolverResult)
	}
	now := time.Now()
	return &ScrapeResult{
		Movie:              scrapedToReturn,
		FieldSources:       fieldSources,
		ActressSources:     actressSources,
		ScraperResults:     scraperResults,
		Cached:             true,
		TranslationWarning: translationWarning,
		TranslationOutput:  translationOutput,
		Status:             StatusCompleted,
		NeedsPersistence:   needsPersistence,
		StartedAt:          startTime,
		EndedAt:            now,
	}
}

func (s *Scraper) repairCachedActresses(
	ctx context.Context,
	cached *models.Movie,
	cachedSourceResult *models.ScraperResult,
	actressRepo database.ActressRepositoryInterface,
) (bool, *models.ScraperResult) {
	if cached == nil {
		return false, nil
	}

	changed := normalizeCachedActresses(cached)
	unverifiedMultiCastCount := allUnverifiedMultiCastCount([]*models.ScraperResult{cachedSourceResult})
	queryID := strings.TrimSpace(cached.ID)
	if queryID == "" {
		queryID = strings.TrimSpace(cached.ContentID)
	}
	resolved, failure := s.resolveMissingActresses(ctx, queryID, []*models.ScraperResult{cachedSourceResult})
	if failure != nil {
		if unverifiedMultiCastCount > 0 {
			setUnknownActressCast(cached)
			return true, nil
		}
		logging.Warnf("[scrape] Cached actress verification failed (movie=%s resolver=%s): %v; cleaned cached cast was preserved",
			queryID, failure.Scraper, failure.Cause)
		return changed, nil
	}
	if resolved == nil {
		if unverifiedMultiCastCount > 0 {
			setUnknownActressCast(cached)
			return true, nil
		}
		return changed, nil
	}
	if unverifiedMultiCastCount > 0 && !hasCompleteVerifiedCast(resolved, unverifiedMultiCastCount) {
		logging.Warnf("[scrape] Cached SougouWiki result verified fewer than %d actresses for %s; using Unknown cast", unverifiedMultiCastCount, queryID)
		setUnknownActressCast(cached)
		return true, nil
	}

	verified := actressModelsFromInfo(resolved.Actresses)
	if len(verified) == 0 {
		logging.Warnf("[scrape] Cached actress verification returned no usable DMM actresses for %s; cleaned cached cast was preserved", queryID)
		return changed, nil
	}

	cleanedFallback := append([]models.Actress(nil), cached.Actresses...)
	cached.Actresses = verified
	if err := reconcileVerifiedActresses(cached, actressRepo); err != nil {
		logging.Warnf("[scrape] Cached actress reconciliation failed for %s: %v; cleaned cached cast was preserved", queryID, err)
		cached.Actresses = cleanedFallback
		return changed, nil
	}
	return true, resolved
}

func actressModelsFromInfo(infos []models.ActressInfo) []models.Actress {
	actresses := make([]models.Actress, 0, len(infos))
	seen := make(map[int]struct{}, len(infos))
	for _, info := range infos {
		if info.DMMID <= 0 {
			continue
		}
		if _, exists := seen[info.DMMID]; exists {
			continue
		}
		name := strings.TrimSpace(info.JapaneseName)
		if name == "" {
			name = strings.TrimSpace(info.LastName + " " + info.FirstName)
		}
		if name == "" || models.IsUnknownActressName(name) || models.IsDescriptiveNonName(info.LastName, info.FirstName, info.JapaneseName) {
			continue
		}
		seen[info.DMMID] = struct{}{}
		actresses = append(actresses, models.Actress{
			DMMID: info.DMMID, FirstName: strings.TrimSpace(info.FirstName), LastName: strings.TrimSpace(info.LastName),
			JapaneseName: strings.TrimSpace(info.JapaneseName), ThumbURL: strings.TrimSpace(info.ThumbURL),
		})
	}
	return actresses
}

func normalizeCachedActresses(movie *models.Movie) bool {
	if movie == nil || len(movie.Actresses) == 0 {
		return false
	}
	before := append([]models.Actress(nil), movie.Actresses...)
	byDMMID := make(map[int]int)
	byName := make(map[string]int)
	cleaned := make([]models.Actress, 0, len(movie.Actresses))

	for _, raw := range movie.Actresses {
		actress := raw
		translation.CleanStoredActress(&actress)
		nameKey := cachedActressNameKey(actress)
		index := -1
		if actress.DMMID > 0 {
			if found, ok := byDMMID[actress.DMMID]; ok {
				index = found
			}
		}
		if index < 0 && nameKey != "" {
			if found, ok := byName[nameKey]; ok {
				index = found
			}
		}
		if index >= 0 {
			mergeCachedActress(&cleaned[index], actress)
			if cleaned[index].DMMID > 0 {
				byDMMID[cleaned[index].DMMID] = index
			}
			continue
		}

		index = len(cleaned)
		cleaned = append(cleaned, actress)
		if actress.DMMID > 0 {
			byDMMID[actress.DMMID] = index
		}
		if nameKey != "" {
			byName[nameKey] = index
		}
	}

	hasReal := false
	for _, actress := range cleaned {
		if !models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
			hasReal = true
			break
		}
	}
	if hasReal {
		filtered := cleaned[:0]
		for _, actress := range cleaned {
			if !models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
				filtered = append(filtered, actress)
			}
		}
		cleaned = filtered
	}

	movie.Actresses = cleaned
	return !reflect.DeepEqual(before, cleaned)
}

func cachedActressNameKey(actress models.Actress) string {
	if name := models.NormalizeActressNameKey(actress.JapaneseName); name != "" {
		return name
	}
	return models.NormalizeActressNameKey(strings.TrimSpace(actress.LastName + " " + actress.FirstName))
}

func mergeCachedActress(target *models.Actress, incoming models.Actress) {
	if target.DMMID <= 0 && incoming.DMMID > 0 {
		target.DMMID = incoming.DMMID
	}
	if target.FirstName == "" {
		target.FirstName = incoming.FirstName
	}
	if target.LastName == "" {
		target.LastName = incoming.LastName
	}
	if target.JapaneseName == "" {
		target.JapaneseName = incoming.JapaneseName
	}
	if target.ThumbURL == "" {
		target.ThumbURL = incoming.ThumbURL
	}
}
