package scrape

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

type candidateTitleTranslator interface {
	TranslateTitles(ctx context.Context, titles []string) ([]string, error)
}

func buildScrapeCandidates(results []*models.ScraperResult) ([]models.ScrapeCandidate, bool) {
	candidates := make([]models.ScrapeCandidate, 0, len(results))
	identities := make(map[string]struct{})

	for _, result := range results {
		if result == nil {
			continue
		}
		candidates = append(candidates, models.ScrapeCandidate{
			Source:        result.Source,
			MovieID:       result.ID,
			Title:         result.Title,
			OriginalTitle: result.Title,
			ActressCount:  len(result.Actresses),
			PosterURL:     result.PosterURL,
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

func translateCandidateTitles(ctx context.Context, translator Translator, candidates []models.ScrapeCandidate) {
	titleTranslator, ok := translator.(candidateTitleTranslator)
	if !ok || len(candidates) == 0 {
		return
	}
	titles := make([]string, len(candidates))
	for i := range candidates {
		titles[i] = candidates[i].OriginalTitle
	}
	translated, err := titleTranslator.TranslateTitles(ctx, titles)
	if err != nil || len(translated) != len(candidates) {
		logging.Debugf("Candidate title translation skipped: err=%v", err)
		return
	}
	for i := range candidates {
		if title := strings.TrimSpace(translated[i]); title != "" {
			candidates[i].Title = title
		}
	}
}
