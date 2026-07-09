package worker

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// earlyStopMin returns the number of successful scraper results to collect before
// stopping early. Defaults to 2 and is floored at 1.
func earlyStopMin(cfg *config.Config) int {
	if cfg == nil || cfg.Scrapers.EarlyStopMinResults < 1 {
		return 2
	}
	return cfg.Scrapers.EarlyStopMinResults
}

// shouldEarlyStop reports whether scraping can stop before querying the remaining
// lower-priority scrapers. It requires the feature to be enabled, at least
// earlyStopMin(cfg) successful results, and — if metadata.required_fields is set —
// that those fields are already covered by the collected results.
func shouldEarlyStop(cfg *config.Config, results []*models.ScraperResult) bool {
	if cfg == nil || !cfg.Scrapers.EarlyStop {
		return false
	}
	if len(results) < earlyStopMin(cfg) {
		return false
	}
	return resultsCoverRequiredFields(results, cfg.Metadata.RequiredFields)
}

// resultsCoverRequiredFields reports whether every configured required field has a
// non-empty value in at least one of the collected results. An empty requiredFields
// list is trivially covered. Unknown field names are ignored (forward-compatible),
// mirroring aggregator.validateRequiredFields.
func resultsCoverRequiredFields(results []*models.ScraperResult, requiredFields []string) bool {
	for _, fieldName := range requiredFields {
		fieldLower := strings.ToLower(strings.TrimSpace(fieldName))
		if fieldLower == "" {
			continue
		}
		covered := false
		for _, r := range results {
			if r != nil && resultCoversField(r, fieldLower) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

// resultCoversField reports whether a single scraper result has a non-empty value for
// the given (lowercased) required-field name. Field-name aliases match
// aggregator.validateRequiredFields. Unknown field names are treated as covered.
func resultCoversField(r *models.ScraperResult, fieldLower string) bool {
	switch fieldLower {
	case "id":
		return r.ID != ""
	case "contentid", "content_id":
		return r.ContentID != ""
	case "title":
		return r.Title != ""
	case "originaltitle", "original_title":
		return r.OriginalTitle != ""
	case "description", "plot":
		return r.Description != ""
	case "director":
		return r.Director != ""
	case "maker", "studio":
		return r.Maker != ""
	case "label":
		return r.Label != ""
	case "series", "set":
		return r.Series != ""
	case "releasedate", "release_date", "premiered":
		return r.ReleaseDate != nil
	case "runtime":
		return r.Runtime != 0
	case "coverurl", "cover_url", "cover":
		return r.CoverURL != ""
	case "posterurl", "poster_url", "poster":
		return r.PosterURL != ""
	case "trailerurl", "trailer_url", "trailer":
		return r.TrailerURL != ""
	case "screenshots", "screenshot_url", "screenshoturl":
		return len(r.ScreenshotURL) > 0
	case "actresses", "actress":
		return len(r.Actresses) > 0
	case "genres", "genre":
		return len(r.Genres) > 0
	default:
		// Unknown/unsupported field name — do not block early-stop.
		return true
	}
}
