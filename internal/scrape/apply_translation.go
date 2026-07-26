package scrape

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// applyTranslation applies metadata translation using the Translator interface.
// Returns a warning string if translation partially failed, or empty string on success.
// This is a standalone function — it does not belong to the Aggregator, which is a
// pure merge operation. Translation is an orthogonal concern invoked after aggregation.
func applyTranslation(ctx context.Context, scraped *models.Movie, translator Translator) (string, *translation.TranslationOutput) {
	return applyTranslationWithOptions(ctx, scraped, translator, TranslationOptions{})
}

func applyTranslationWithOptions(ctx context.Context, scraped *models.Movie, translator Translator, options TranslationOptions) (string, *translation.TranslationOutput) {
	warning, output, _ := applyTranslationWithOptionsResult(ctx, scraped, translator, options)
	return warning, output
}

func applyTranslationWithOptionsResult(ctx context.Context, scraped *models.Movie, translator Translator, options TranslationOptions) (string, *translation.TranslationOutput, error) {
	if scraped == nil || translator == nil {
		return "", nil, nil
	}
	if resultTranslator, ok := translator.(translatorWithResult); ok {
		warning, _, output, err := resultTranslator.TranslateWithOptionsResult(ctx, scraped, options)
		return warning, output, err
	}
	if configurable, ok := translator.(translatorWithOptions); ok {
		warning, _, output := configurable.TranslateWithOptions(ctx, scraped, options)
		return warning, output, nil
	}
	warning, _, output := translator.Translate(ctx, scraped)
	return warning, output, nil
}

// translationService wraps a pre-constructed translation.Service to avoid
// creating HTTP clients and providers per invocation (WR-07 fix).
type translationService struct {
	service           *translation.Service
	provider          string
	sourceLanguage    string
	targetLanguage    string
	settingsHash      string
	overwriteExisting bool
	applyToPrimary    bool
}

func newTranslationService(provider string, sourceLanguage string, targetLanguage string, settingsHash string, overwriteExisting bool, applyToPrimary bool, svc *translation.Service) *translationService {
	return &translationService{
		service:           svc,
		provider:          provider,
		sourceLanguage:    sourceLanguage,
		targetLanguage:    targetLanguage,
		settingsHash:      settingsHash,
		overwriteExisting: overwriteExisting,
		applyToPrimary:    applyToPrimary,
	}
}

// translateWithContext performs the translation using the provided context.
// This is the context-accepting variant used by the Translator interface.
// Each provider request applies Metadata.Translation.TimeoutSeconds
// independently. The caller's context is preserved for cancellation without
// sharing one request's timeout budget across retries or review calls.
func (ts *translationService) translateWithContext(ctx context.Context, scraped *models.Movie, forceOverwrite bool) (string, *translation.TranslationOutput, error) {
	if scraped == nil {
		return "", nil, nil
	}

	logging.Debugf("Translation: starting (provider=%s, source=%s, target=%s, hash=%s)", ts.provider, ts.sourceLanguage, ts.targetLanguage, ts.settingsHash)
	translationInput := scraped
	if forceOverwrite {
		translationInput = translationRefreshSourceMovie(scraped, ts.sourceLanguage)
	}

	output, warning, err := ts.service.TranslateMovie(ctx, translationInput, ts.settingsHash)
	if err != nil {
		id := scraped.ID
		if id == "" {
			id = scraped.ContentID
		}
		logging.Warnf("[%s] Metadata translation failed: %v", id, err)
		return warning, nil, err
	}
	if output == nil || (output.Movie == nil && len(output.Movies) == 0) {
		logging.Debugf("Translation: returned nil record (no fields to translate or source==target)")
		return "", nil, nil
	}
	if forceOverwrite && ts.applyToPrimary && translationInput != scraped {
		copyTranslatedPrimary(scraped, translationInput, output)
	}

	records := output.Movies
	if len(records) == 0 && output.Movie != nil {
		records = []models.MovieTranslation{*output.Movie}
	}
	for i := range records {
		translatedRecord := records[i]
		logging.Debugf("Translation: appending %s translation (title=%q, hash=%s)", translatedRecord.Language, translatedRecord.Title, translatedRecord.SettingsHash)
		scraped.Translations = mergeOrAppendTranslation(scraped.Translations, translatedRecord, forceOverwrite || ts.overwriteExisting)
	}

	logging.Debugf("Translation: movie now has %d translation(s)", len(scraped.Translations))
	return warning, output, nil
}

// translationRefreshSourceMovie rebuilds request-scoped translation input from
// the cached source-language record. apply_to_primary may have replaced the
// movie's primary fields with a previous target-language result, while the
// original scraper text remains in Movie.Translations (normally language=ja).
func translationRefreshSourceMovie(movie *models.Movie, sourceLanguage string) *models.Movie {
	working := movie.Clone()
	if working == nil {
		return nil
	}

	sourceLanguage = strings.ToLower(strings.TrimSpace(sourceLanguage))
	if sourceLanguage == "" || sourceLanguage == "auto" {
		sourceLanguage = "ja"
	}
	for _, record := range movie.Translations {
		if strings.ToLower(strings.TrimSpace(record.Language)) != sourceLanguage {
			continue
		}
		working.Title = record.Title
		working.OriginalTitle = record.OriginalTitle
		working.Description = record.Description
		working.Director = record.Director
		working.Maker = record.Maker
		working.Label = record.Label
		working.Series = record.Series
		if sourceLanguage == "ja" && strings.TrimSpace(working.Title) == "" {
			working.Title = movie.OriginalTitle
		}
		if sourceLanguage == "ja" && strings.TrimSpace(working.OriginalTitle) == "" {
			working.OriginalTitle = movie.OriginalTitle
		}
		// MovieTranslation has no genre field. Do not feed already-translated
		// primary genre names back through the provider as source text.
		working.Genres = nil
		return working
	}

	// Legacy rows may lack a source-language translation association but still
	// preserve the Japanese title in movies.original_title.
	if sourceLanguage == "ja" && strings.TrimSpace(movie.OriginalTitle) != "" {
		working.Title = movie.OriginalTitle
		working.OriginalTitle = movie.OriginalTitle
		working.Description = ""
		working.Director = ""
		working.Maker = ""
		working.Label = ""
		working.Series = ""
		working.Genres = nil
	}
	return working
}

func copyTranslatedPrimary(dst, src *models.Movie, output *translation.TranslationOutput) {
	if dst == nil || src == nil || output == nil || output.Movie == nil {
		return
	}
	translated := output.Movie
	if translated.Title != "" {
		dst.Title = src.Title
	}
	// Keep movies.original_title as the authoritative source fallback for
	// future refreshes. Target-language OriginalTitle remains available on the
	// target MovieTranslation record.
	if translated.Description != "" {
		dst.Description = src.Description
	}
	if translated.Director != "" {
		dst.Director = src.Director
	}
	if translated.Maker != "" {
		dst.Maker = src.Maker
	}
	if translated.Label != "" {
		dst.Label = src.Label
	}
	if translated.Series != "" {
		dst.Series = src.Series
	}
	if len(output.GenreTranslations) > 0 {
		dst.Genres = append([]models.Genre(nil), src.Genres...)
	}
	if len(output.ActressTranslations) > 0 {
		dst.Actresses = append([]models.Actress(nil), src.Actresses...)
	}
}

func (ts *translationService) translateTitlesWithContext(ctx context.Context, titles []string) ([]string, error) {
	return ts.service.TranslateTitles(ctx, titles)
}

// newTranslationHTTPClient creates the shared HTTP client for translation providers.
func newTranslationHTTPClient(timeoutSeconds int) *http.Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	return &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			MaxIdleConnsPerHost: 2,
		},
	}
}

// mergeOrAppendTranslation merges or appends an incoming translation to existing translations.
// Moved from aggregator package — needed by the standalone applyConfiguredTranslation.
func mergeOrAppendTranslation(
	existing []models.MovieTranslation,
	incoming models.MovieTranslation,
	overwrite bool,
) []models.MovieTranslation {
	targetLanguage := strings.ToLower(strings.TrimSpace(incoming.Language))
	if targetLanguage == "" {
		return existing
	}

	for i := range existing {
		if strings.ToLower(strings.TrimSpace(existing[i].Language)) != targetLanguage {
			continue
		}

		if overwrite {
			existing[i] = mergeTranslationFields(existing[i], incoming)
		}
		return existing
	}

	return append(existing, incoming)
}

// mergeTranslationFields merges incoming translation fields into current translation.
func mergeTranslationFields(current, incoming models.MovieTranslation) models.MovieTranslation {
	merged := current
	merged.Language = incoming.Language

	if incoming.Title != "" {
		merged.Title = incoming.Title
	}
	if incoming.OriginalTitle != "" {
		merged.OriginalTitle = incoming.OriginalTitle
	}
	if incoming.Description != "" {
		merged.Description = incoming.Description
	}
	if incoming.Director != "" {
		merged.Director = incoming.Director
	}
	if incoming.Maker != "" {
		merged.Maker = incoming.Maker
	}
	if incoming.Label != "" {
		merged.Label = incoming.Label
	}
	if incoming.Series != "" {
		merged.Series = incoming.Series
	}
	if incoming.Actresses != nil {
		merged.Actresses = append([]string(nil), incoming.Actresses...)
	}
	if incoming.SourceName != "" {
		merged.SourceName = incoming.SourceName
	}
	if incoming.SettingsHash != "" {
		merged.SettingsHash = incoming.SettingsHash
	}

	return merged
}
