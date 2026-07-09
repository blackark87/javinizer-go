package worker

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// buildScrapeCandidates summarizes each successful scraper result and reports whether
// the providers disagree on the movie. Conflict is detected when two or more results
// carry a different normalized identity (title, falling back to ID), e.g. one provider
// returns "AIKA" while another returns "佐々木あき" for the same query. The summaries are
// small (no field values beyond title/poster/actress count) so retaining them on the
// job result is cheap.
func buildScrapeCandidates(results []*models.ScraperResult) ([]models.ScrapeCandidate, bool) {
	candidates := make([]models.ScrapeCandidate, 0, len(results))
	identities := make(map[string]struct{})

	for _, r := range results {
		if r == nil {
			continue
		}
		candidates = append(candidates, models.ScrapeCandidate{
			Source:       r.Source,
			MovieID:      r.ID,
			Title:        r.Title,
			ActressCount: len(r.Actresses),
			PosterURL:    r.PosterURL,
		})

		if id := candidateIdentity(r); id != "" {
			identities[id] = struct{}{}
		}
	}

	return candidates, len(identities) > 1
}

// candidateIdentity returns a normalized identity for conflict comparison: the title
// if present, otherwise the movie ID. Empty when neither is available.
func candidateIdentity(r *models.ScraperResult) string {
	if t := normalizeIdentityKey(r.Title); t != "" {
		return t
	}
	return normalizeIdentityKey(r.ID)
}

func normalizeIdentityKey(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
