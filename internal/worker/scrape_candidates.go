package worker

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
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
			Source:        r.Source,
			MovieID:       r.ID,
			Title:         r.Title,
			OriginalTitle: r.Title,
			ActressCount:  len(r.Actresses),
			PosterURL:     r.PosterURL,
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

// translateCandidateTitles fills each candidate's Title with a translated form so the
// multi-result picker shows readable titles instead of raw Japanese. It is a no-op
// unless translation is enabled; the raw title stays in OriginalTitle. One batch call.
func translateCandidateTitles(ctx context.Context, cfg *config.Config, candidates []models.ScrapeCandidate) {
	if cfg == nil || !cfg.Metadata.Translation.Enabled || len(candidates) == 0 {
		return
	}
	titles := make([]string, len(candidates))
	for i := range candidates {
		titles[i] = candidates[i].OriginalTitle
	}
	svc := translation.New(cfg.Metadata.Translation)
	translated, err := svc.TranslateTitles(ctx, titles)
	if err != nil || len(translated) != len(candidates) {
		logging.Debugf("Candidate title translation skipped: err=%v", err)
		return
	}
	for i := range candidates {
		if t := strings.TrimSpace(translated[i]); t != "" {
			candidates[i].Title = t
		}
	}
}
