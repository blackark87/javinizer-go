package scrape

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func buildScrapeCandidates(results []*models.ScraperResult) ([]models.ScrapeCandidate, bool) {
	candidates := make([]models.ScrapeCandidate, 0, len(results))
	identities := make(map[string]struct{})

	for _, result := range results {
		if result == nil {
			continue
		}
		candidates = append(candidates, models.ScrapeCandidate{
			Source:              result.Source,
			MovieID:             result.ID,
			Title:               result.Title,
			OriginalTitle:       result.Title,
			Description:         result.Description,
			OriginalDescription: result.Description,
			ActressCount:        len(result.Actresses),
			PosterURL:           result.PosterURL,
		})
		if identity := scrapeCandidateIdentity(result); identity != "" {
			identities[identity] = struct{}{}
		}
	}

	return candidates, len(identities) > 1
}

func scrapeCandidateIdentity(result *models.ScraperResult) string {
	if title := normalizeScrapeCandidateIdentity(result.Title); title != "" {
		return title
	}
	return normalizeScrapeCandidateIdentity(result.ID)
}

func normalizeScrapeCandidateIdentity(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func movieCandidateResults(results []*models.ScraperResult, resolverSource string) []*models.ScraperResult {
	if strings.TrimSpace(resolverSource) == "" {
		return results
	}
	candidates := make([]*models.ScraperResult, 0, len(results))
	for _, result := range results {
		if result == nil || strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(resolverSource)) {
			continue
		}
		candidates = append(candidates, result)
	}
	if len(candidates) == 0 {
		return results
	}
	return candidates
}

func translateCandidateMetadata(ctx context.Context, translator Translator, candidates []models.ScrapeCandidate) string {
	if translator == nil || len(candidates) == 0 {
		return ""
	}
	var warnings []string
	for i := range candidates {
		movie := &models.Movie{
			ID:          candidates[i].MovieID,
			Title:       candidates[i].OriginalTitle,
			Description: candidates[i].OriginalDescription,
		}
		warning, _, output := translator.Translate(ctx, movie)
		if warning != "" {
			warnings = append(warnings, candidates[i].Source+": "+warning)
		}
		if title := strings.TrimSpace(movie.Title); title != "" {
			candidates[i].Title = title
		}
		if description := strings.TrimSpace(movie.Description); description != "" {
			candidates[i].Description = description
		}
		if output != nil && len(output.Movies) > 0 {
			candidates[i].Translations = cloneMovieTranslations(output.Movies)
		} else if len(movie.Translations) > 0 {
			candidates[i].Translations = cloneMovieTranslations(movie.Translations)
		}
	}
	if len(warnings) > 0 {
		logging.Debugf("Candidate metadata translation warning: %s", strings.Join(warnings, "; "))
		return "candidate metadata: " + strings.Join(warnings, "; ")
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
