package scrape

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

const actressResolverScraperName = "sougouwiki"

// resolveMissingActresses asks the dedicated actress resolver when the regular
// results contain no cast or at least one actress without a verified DMM
// identity or usable Japanese name. Resolver failures are optional when another
// scraper returned movie metadata, and fatal only when no result remains at all.
func (s *Scraper) resolveMissingActresses(ctx context.Context, movieID string, results []*models.ScraperResult) (*models.ScraperResult, *models.ScraperError) {
	if !needsActressResolution(results) || s.registry == nil {
		return nil, nil
	}
	instance, ok := s.registry.GetInstance(actressResolverScraperName)
	if !ok || instance == nil {
		return nil, nil
	}
	resolver, ok := instance.(models.ActressResolver)
	if !ok {
		return nil, nil
	}

	resolved, err := callActressResolver(ctx, resolver, movieID)
	if err != nil {
		return nil, &models.ScraperError{Scraper: actressResolverScraperName, Cause: err}
	}
	if resolved == nil {
		return nil, nil
	}
	if strings.TrimSpace(resolved.Source) == "" {
		resolved.Source = actressResolverScraperName
	}
	if strings.TrimSpace(resolved.ID) == "" {
		resolved.ID = movieID
	}
	inheritResolvedActressAssets(resolved, results)
	s.enrichResolvedActressProfiles(ctx, resolved)
	return resolved, nil
}

func callActressResolver(ctx context.Context, resolver models.ActressResolver, movieID string) (result *models.ScraperResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("actress resolver panicked: %v", recovered)
		}
	}()
	return resolver.ResolveActresses(ctx, movieID)
}

func needsActressResolution(results []*models.ScraperResult) bool {
	hasActress := false
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, actress := range result.Actresses {
			hasActress = true
			if actress.DMMID <= 0 || !hasUsableResolvedJapaneseName(actress) {
				return true
			}
		}
	}
	return !hasActress
}

func hasUsableResolvedJapaneseName(actress models.ActressInfo) bool {
	name := translation.CleanActressName(actress.JapaneseName)
	return name != "" && !models.IsUnknownActressName(name) &&
		!models.IsDescriptiveNonName(actress.LastName, actress.FirstName, name)
}

// inheritResolvedActressAssets keeps source assets that SougouWiki does not
// provide. Identity fields remain resolver-owned, while a matching verified
// DMM ID can safely retain its already-scraped thumbnail.
func inheritResolvedActressAssets(resolved *models.ScraperResult, results []*models.ScraperResult) {
	if resolved == nil {
		return
	}
	thumbByDMMID := make(map[int]string)
	for _, result := range results {
		if result == nil {
			continue
		}
		for _, actress := range result.Actresses {
			if actress.DMMID <= 0 || strings.TrimSpace(actress.ThumbURL) == "" {
				continue
			}
			if _, exists := thumbByDMMID[actress.DMMID]; !exists {
				thumbByDMMID[actress.DMMID] = strings.TrimSpace(actress.ThumbURL)
			}
		}
	}
	for i := range resolved.Actresses {
		actress := &resolved.Actresses[i]
		if strings.TrimSpace(actress.ThumbURL) == "" {
			actress.ThumbURL = thumbByDMMID[actress.DMMID]
		}
	}
}

// allUnverifiedMultiCastCount returns the largest cleaned, distinct cast size
// reported by one regular source when no regular source supplied any verified
// DMM identity. Counting per source avoids treating the same actress reported
// by several scrapers as a multi-actress movie.
func allUnverifiedMultiCastCount(results []*models.ScraperResult) int {
	for _, result := range results {
		if result == nil || strings.EqualFold(strings.TrimSpace(result.Source), actressResolverScraperName) {
			continue
		}
		for _, actress := range result.Actresses {
			if actress.DMMID > 0 {
				return 0
			}
		}
	}

	maxCount := 0
	for _, result := range results {
		if result == nil || strings.EqualFold(strings.TrimSpace(result.Source), actressResolverScraperName) {
			continue
		}
		seen := make(map[string]struct{}, len(result.Actresses))
		for _, raw := range result.Actresses {
			actress := raw
			translation.CleanActressInfo(&actress)
			if models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
				continue
			}
			name := strings.TrimSpace(actress.JapaneseName)
			if name == "" {
				name = strings.TrimSpace(actress.LastName + " " + actress.FirstName)
			}
			key := models.NormalizeActressNameKey(name)
			if key != "" {
				seen[key] = struct{}{}
			}
		}
		if len(seen) > maxCount {
			maxCount = len(seen)
		}
	}
	if maxCount < 2 {
		return 0
	}
	return maxCount
}

func hasCompleteVerifiedCast(result *models.ScraperResult, expected int) bool {
	if expected <= 0 || result == nil {
		return expected <= 0
	}
	verified := make(map[int]struct{}, len(result.Actresses))
	for _, actress := range result.Actresses {
		if actress.DMMID > 0 {
			verified[actress.DMMID] = struct{}{}
		}
	}
	return len(verified) >= expected
}

func setUnknownActressCast(movie *models.Movie) {
	if movie == nil {
		return
	}
	movie.Actresses = []models.Actress{{
		FirstName:    models.UnknownActressName,
		JapaneseName: models.UnknownActressName,
	}}
}

func hasCanonicalUnknownActressCast(movie *models.Movie) bool {
	return movie != nil && len(movie.Actresses) == 1 && models.IsUnknownActressFields(
		movie.Actresses[0].LastName,
		movie.Actresses[0].FirstName,
		movie.Actresses[0].JapaneseName,
	)
}

func (s *Scraper) enrichScrapedActressProfiles(ctx context.Context, results []*models.ScraperResult) {
	for _, result := range results {
		s.enrichResolvedActressProfiles(ctx, result)
	}
}

func (s *Scraper) enrichResolvedActressProfiles(ctx context.Context, result *models.ScraperResult) {
	if result == nil || s.registry == nil {
		return
	}
	var profileResolver models.ActressProfileResolver
	var thumbnailResolver models.ActressThumbnailResolver
	for _, instance := range s.registry.GetAllInstances() {
		if resolver, ok := instance.(models.ActressProfileResolver); ok && profileResolver == nil {
			profileResolver = resolver
		}
		if resolver, ok := instance.(models.ActressThumbnailResolver); ok && thumbnailResolver == nil {
			thumbnailResolver = resolver
		}
	}
	for i := range result.Actresses {
		actress := &result.Actresses[i]
		if actress.DMMID <= 0 {
			continue
		}
		if profileResolver != nil {
			profile, err := safeActressProfile(ctx, profileResolver, *actress)
			if err == nil && strings.TrimSpace(profile.JapaneseName) != "" {
				actress.JapaneseName = strings.TrimSpace(profile.JapaneseName)
				actress.FirstName = strings.TrimSpace(profile.FirstName)
				actress.LastName = strings.TrimSpace(profile.LastName)
				if strings.TrimSpace(profile.ThumbURL) != "" {
					actress.ThumbURL = strings.TrimSpace(profile.ThumbURL)
				}
			} else if strings.TrimSpace(actress.JapaneseName) == "" {
				if err != nil {
					logging.Warnf("Actress profile lookup failed for DMM ID %d; SougouWiki fallback required: %v", actress.DMMID, err)
				} else {
					logging.Warnf("Actress profile lookup returned no usable name for DMM ID %d; SougouWiki fallback required", actress.DMMID)
				}
			} else if err != nil {
				logging.Debugf("Actress profile lookup failed for DMM ID %d; keeping existing Japanese name: %v", actress.DMMID, err)
			}
		}
		if strings.TrimSpace(actress.ThumbURL) == "" && thumbnailResolver != nil {
			actress.ThumbURL = safeActressThumbnail(ctx, thumbnailResolver, *actress)
		}
	}
}

func safeActressProfile(ctx context.Context, resolver models.ActressProfileResolver, actress models.ActressInfo) (profile models.ActressInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			profile = models.ActressInfo{}
			err = fmt.Errorf("actress profile resolver panicked: %v", recovered)
		}
	}()
	return resolver.ResolveActressProfile(ctx, actress)
}

func safeActressThumbnail(ctx context.Context, resolver models.ActressThumbnailResolver, actress models.ActressInfo) (thumbnail string) {
	defer func() {
		if recover() != nil {
			thumbnail = ""
		}
	}()
	return strings.TrimSpace(resolver.ResolveActressThumbnail(ctx, actress))
}

type verifiedActressIdentityResolver interface {
	ResolveVerifiedIdentity(sourceID uint, verified models.Actress, allowCreate bool) (*database.VerifiedActressResolution, error)
}

func hasScraperSource(results []*models.ScraperResult, source string) bool {
	for _, result := range results {
		if result != nil && strings.EqualFold(strings.TrimSpace(result.Source), strings.TrimSpace(source)) {
			return true
		}
	}
	return false
}

func reconcileVerifiedActresses(movie *models.Movie, repo database.ActressRepositoryInterface) error {
	resolver, ok := repo.(verifiedActressIdentityResolver)
	if movie == nil || !ok {
		return nil
	}
	canonical := make([]models.Actress, 0, len(movie.Actresses))
	seen := make(map[uint]struct{}, len(movie.Actresses))
	for _, actress := range movie.Actresses {
		resolution, err := resolver.ResolveVerifiedIdentity(0, actress, true)
		if err != nil {
			return fmt.Errorf("reconcile verified actress %q (DMM ID %d): %w", actress.JapaneseName, actress.DMMID, err)
		}
		if resolution == nil {
			continue
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

// actressOverrideResults creates aggregation-only copies. Raw scraper results
// remain untouched for the review source viewer. When ordinary movie metadata
// exists, the resolver contributes only the cast; when it is the sole result,
// its identifier is retained so resolver-only scraping still succeeds.
func actressOverrideResults(results []*models.ScraperResult, resolverSource string) []*models.ScraperResult {
	hasRegular := false
	hasResolver := false
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.Source == resolverSource {
			hasResolver = true
		} else {
			hasRegular = true
		}
	}
	if !hasRegular || !hasResolver {
		return results
	}

	overrides := make([]*models.ScraperResult, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		copy := result.Clone()
		if copy.Source == resolverSource {
			copy.ID = ""
			copy.ContentID = ""
			copy.Title = ""
			copy.OriginalTitle = ""
			copy.Description = ""
			copy.ReleaseDate = nil
			copy.Runtime = 0
			copy.Director = ""
			copy.Maker = ""
			copy.Label = ""
			copy.Series = ""
			copy.Rating = nil
			copy.PosterURL = ""
			copy.CoverURL = ""
			copy.ShouldCropPoster = false
			copy.ScreenshotURL = nil
			copy.TrailerURL = ""
			copy.Genres = nil
		} else {
			copy.Actresses = nil
		}
		overrides = append(overrides, copy)
	}
	return overrides
}
