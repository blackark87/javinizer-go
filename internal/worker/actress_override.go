package worker

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// buildActressOverrideResults preserves the raw scraper results for diagnostics
// while producing shallow copies in which only the verified resolver contributes
// actresses to aggregation and source attribution.
func buildActressOverrideResults(results []*models.ScraperResult, source string) []*models.ScraperResult {
	if strings.TrimSpace(source) == "" {
		return results
	}
	hasRegularResult := false
	for _, result := range results {
		if result != nil && !strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(source)) {
			hasRegularResult = true
			break
		}
	}

	copies := make([]*models.ScraperResult, 0, len(results))
	for _, result := range results {
		if result == nil {
			copies = append(copies, nil)
			continue
		}
		copyResult := *result
		if strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(source)) {
			if hasRegularResult {
				copyResult = models.ScraperResult{
					Source:    result.Source,
					SourceURL: result.SourceURL,
					Language:  result.Language,
					Actresses: result.Actresses,
				}
			}
		} else {
			copyResult.Actresses = nil
		}
		copies = append(copies, &copyResult)
	}
	return copies
}

// buildMovieCandidateResults keeps actress-only resolver results from creating
// false movie conflicts while retaining all original regular results for the
// diagnostic picker. A resolver-only scrape remains visible as its sole result.
func buildMovieCandidateResults(results []*models.ScraperResult, actressSource string) []*models.ScraperResult {
	if strings.TrimSpace(actressSource) == "" {
		return results
	}

	candidates := make([]*models.ScraperResult, 0, len(results))
	for _, result := range results {
		if result == nil || strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(actressSource)) {
			continue
		}
		candidates = append(candidates, result)
	}
	if len(candidates) == 0 {
		return results
	}
	return candidates
}

// reconcileVerifiedMovieActresses makes the resolver-confirmed name the
// canonical database identity before MovieRepository.Upsert sees the cast. It
// repairs an existing nickname row that owns the DMM ID, reuses an exact
// canonical-name row, and removes the duplicate when both rows exist.
func reconcileVerifiedMovieActresses(movie *models.Movie, repo *database.ActressRepository) error {
	if movie == nil || repo == nil {
		return nil
	}
	canonical := make([]models.Actress, 0, len(movie.Actresses))
	seen := make(map[uint]struct{}, len(movie.Actresses))
	for _, actress := range movie.Actresses {
		resolution, err := repo.ResolveVerifiedIdentity(0, actress, true)
		if err != nil {
			return fmt.Errorf("reconcile verified actress %q (DMM ID %d): %w", actress.JapaneseName, actress.DMMID, err)
		}
		if _, exists := seen[resolution.Actress.ID]; exists {
			continue
		}
		seen[resolution.Actress.ID] = struct{}{}
		canonical = append(canonical, resolution.Actress)
	}
	movie.Actresses = canonical
	return nil
}
