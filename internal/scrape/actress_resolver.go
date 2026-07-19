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
	if identityResolver, identityOK := instance.(models.ActressIdentityResolver); identityOK {
		primaryCast := firstUsableRegularCast(results, actressResolverScraperName)
		if resolvedByName, complete := resolvePrimaryCastIdentities(ctx, identityResolver, movieID, primaryCast); complete {
			inheritResolvedActressAssets(resolvedByName, results)
			s.enrichResolvedActressProfiles(ctx, resolvedByName)
			return resolvedByName, nil
		}
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

func resolvePrimaryCastIdentities(
	ctx context.Context,
	resolver models.ActressIdentityResolver,
	movieID string,
	primary []models.ActressInfo,
) (*models.ScraperResult, bool) {
	if resolver == nil || len(primary) == 0 {
		return nil, false
	}
	resolved := make([]models.ActressInfo, 0, len(primary))
	seenPrimary := make(map[string]struct{}, len(primary))
	var sourceURL string
	for _, actress := range primary {
		keys := actressIdentityKeys(actress)
		primaryKey := ""
		if len(keys) > 0 {
			primaryKey = keys[0]
		}
		if primaryKey != "" {
			if _, exists := seenPrimary[primaryKey]; exists {
				continue
			}
			seenPrimary[primaryKey] = struct{}{}
		}
		if actress.DMMID > 0 && hasUsableResolvedJapaneseName(actress) {
			resolved = append(resolved, actress)
			continue
		}

		identity, err := callActressIdentityResolver(ctx, resolver, models.ActressIdentityQuery{
			Names:    actressIdentityNames(actress),
			ThumbURL: strings.TrimSpace(actress.ThumbURL),
		})
		if err != nil || identity == nil || len(identity.Actresses) != 1 || identity.Actresses[0].DMMID <= 0 {
			return nil, false
		}
		verified := identity.Actresses[0]
		if observed := strings.TrimSpace(actress.JapaneseName); isObservedActressAlias(observed, verified.JapaneseName) {
			verified.ObservedAliases = appendUniqueActressAlias(verified.ObservedAliases, observed)
		}
		if strings.TrimSpace(verified.ThumbURL) == "" {
			verified.ThumbURL = strings.TrimSpace(actress.ThumbURL)
		}
		resolved = append(resolved, verified)
		if sourceURL == "" {
			sourceURL = identity.SourceURL
		}
	}
	if len(resolved) == 0 {
		return nil, false
	}
	return &models.ScraperResult{
		Source:    actressResolverScraperName,
		SourceURL: sourceURL,
		Language:  "ja",
		ID:        movieID,
		Actresses: resolved,
	}, true
}

func callActressIdentityResolver(ctx context.Context, resolver models.ActressIdentityResolver, query models.ActressIdentityQuery) (result *models.ScraperResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("actress identity resolver panicked: %v", recovered)
		}
	}()
	return resolver.ResolveActressIdentity(ctx, query)
}

func actressIdentityNames(actress models.ActressInfo) []string {
	values := []string{
		strings.TrimSpace(actress.JapaneseName),
		strings.TrimSpace(actress.LastName + " " + actress.FirstName),
		strings.TrimSpace(actress.FirstName + " " + actress.LastName),
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := models.NormalizeActressNameKey(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
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
				observedName := strings.TrimSpace(actress.JapaneseName)
				profileName := strings.TrimSpace(profile.JapaneseName)
				if isObservedActressAlias(observedName, profileName) {
					actress.ObservedAliases = appendUniqueActressAlias(actress.ObservedAliases, observedName)
				}
				actress.JapaneseName = profileName
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

type verifiedActressProfileResolver interface {
	ResolveVerifiedProfile(sourceID uint, verified models.Actress, observedAliases []string, allowCreate bool) (*database.VerifiedActressResolution, error)
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
		// Reconciliation verifies identities that already carry a DMM ID. An
		// unverified cast entry is still a valid scrape result and must not make
		// the whole movie fail with "verified actress requires a positive DMM ID".
		if actress.DMMID <= 0 {
			canonical = append(canonical, actress)
			continue
		}
		var resolution *database.VerifiedActressResolution
		var err error
		observedAliases := splitActressAliases(actress.Aliases)
		if profileResolver, profileOK := repo.(verifiedActressProfileResolver); profileOK && len(observedAliases) > 0 {
			resolution, err = profileResolver.ResolveVerifiedProfile(0, actress, observedAliases, true)
		} else {
			resolution, err = resolver.ResolveVerifiedIdentity(0, actress, true)
		}
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

func isObservedActressAlias(observed, canonical string) bool {
	observed = strings.TrimSpace(observed)
	canonical = strings.TrimSpace(canonical)
	return observed != "" && canonical != "" &&
		!models.IsUnknownActressName(observed) && !models.IsDescriptiveNonName("", "", observed) &&
		!strings.EqualFold(observed, canonical)
}

func appendUniqueActressAlias(existing []string, value string) []string {
	value = strings.TrimSpace(value)
	for _, current := range existing {
		if strings.EqualFold(strings.TrimSpace(current), value) {
			return existing
		}
	}
	return append(existing, value)
}

func splitActressAliases(value string) []string {
	aliases := make([]string, 0)
	for _, alias := range strings.Split(value, "|") {
		alias = strings.TrimSpace(alias)
		if alias != "" {
			aliases = append(aliases, alias)
		}
	}
	return aliases
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

	primaryCast := firstUsableRegularCast(results, resolverSource)
	resolverCast, useResolverCast := constrainedResolverCast(results, resolverSource, primaryCast)

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
			if useResolverCast {
				copy.Actresses = append([]models.ActressInfo(nil), resolverCast...)
			} else {
				copy.Actresses = nil
			}
		} else if useResolverCast {
			copy.Actresses = nil
		}
		overrides = append(overrides, copy)
	}
	return overrides
}

// firstUsableRegularCast returns the cast from the highest-priority regular
// provider. queryAll preserves provider order, so this is the same cast source
// the aggregator would select without the automatic resolver.
func firstUsableRegularCast(results []*models.ScraperResult, resolverSource string) []models.ActressInfo {
	for _, result := range results {
		if result == nil || strings.EqualFold(strings.TrimSpace(result.Source), resolverSource) {
			continue
		}
		usable := make([]models.ActressInfo, 0, len(result.Actresses))
		for _, raw := range result.Actresses {
			actress := raw
			translation.CleanActressInfo(&actress)
			if models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) {
				continue
			}
			if len(actressIdentityKeys(actress)) > 0 || actress.DMMID > 0 {
				usable = append(usable, actress)
			}
		}
		if len(usable) > 0 {
			return usable
		}
	}
	return nil
}

// constrainedResolverCast prevents a broad movie-ID search from adding cast
// belonging to another product with the same catalog number. A single resolver
// result remains a valid fallback (for example JNT-042); when multiple results
// are returned, only identities matching the primary provider cast are used.
func constrainedResolverCast(results []*models.ScraperResult, resolverSource string, primary []models.ActressInfo) ([]models.ActressInfo, bool) {
	var resolved []models.ActressInfo
	for _, result := range results {
		if result != nil && strings.EqualFold(strings.TrimSpace(result.Source), resolverSource) {
			resolved = result.Actresses
			break
		}
	}
	if len(resolved) == 0 {
		return nil, false
	}
	if len(resolved) == 1 || len(primary) == 0 {
		return append([]models.ActressInfo(nil), resolved...), true
	}
	// For a multi-actress primary cast, the existing completeness guard verifies
	// that SougouWiki resolved the full selected cast. The names may legitimately
	// be aliases and therefore need not match before reconciliation.
	if len(primary) > 1 && len(resolved) == len(primary) {
		return append([]models.ActressInfo(nil), resolved...), true
	}

	primaryDMMIDs := make(map[int]struct{})
	primaryKeys := make(map[string]struct{})
	for _, actress := range primary {
		if actress.DMMID > 0 {
			primaryDMMIDs[actress.DMMID] = struct{}{}
		}
		for _, key := range actressIdentityKeys(actress) {
			primaryKeys[key] = struct{}{}
		}
	}

	matched := make([]models.ActressInfo, 0, len(primary))
	for _, actress := range resolved {
		if _, ok := primaryDMMIDs[actress.DMMID]; actress.DMMID > 0 && ok {
			matched = append(matched, actress)
			continue
		}
		for _, key := range actressIdentityKeys(actress) {
			if _, ok := primaryKeys[key]; ok {
				matched = append(matched, actress)
				break
			}
		}
	}
	if len(matched) == 0 {
		return nil, false
	}
	return matched, true
}

func actressIdentityKeys(actress models.ActressInfo) []string {
	values := []string{
		actress.JapaneseName,
		strings.TrimSpace(actress.LastName + " " + actress.FirstName),
		strings.TrimSpace(actress.FirstName + " " + actress.LastName),
	}
	keys := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := models.NormalizeActressNameKey(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}
