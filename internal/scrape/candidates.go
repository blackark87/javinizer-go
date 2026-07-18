package scrape

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// translateSourceMetadata translates each retained scraper's title and
// description once during the scrape. Raw source fields remain untouched;
// Sources uses the attached language records for display and field overrides.
func translateSourceMetadata(ctx context.Context, translator Translator, results []*models.ScraperResult) string {
	if translator == nil || len(results) == 0 {
		return ""
	}

	var warnings []string
	for _, result := range results {
		if result == nil || (strings.TrimSpace(result.Title) == "" && strings.TrimSpace(result.Description) == "") {
			continue
		}

		movie := &models.Movie{
			ID:          result.ID,
			ContentID:   result.ContentID,
			Title:       result.Title,
			Description: result.Description,
		}
		warning, _, output := translator.Translate(ctx, movie)
		if warning != "" {
			warnings = append(warnings, result.Source+": "+warning)
		}
		if output != nil && len(output.Movies) > 0 {
			result.Translations = cloneMovieTranslations(output.Movies)
		} else if len(movie.Translations) > 0 {
			result.Translations = cloneMovieTranslations(movie.Translations)
		}
	}

	if len(warnings) > 0 {
		logging.Debugf("Source metadata translation warning: %s", strings.Join(warnings, "; "))
		return "source metadata: " + strings.Join(warnings, "; ")
	}
	return ""
}

func cloneMovieTranslations(translations []models.MovieTranslation) []models.MovieTranslation {
	cloned := append([]models.MovieTranslation(nil), translations...)
	for i := range cloned {
		cloned[i].Actresses = append([]string(nil), translations[i].Actresses...)
	}
	return cloned
}
