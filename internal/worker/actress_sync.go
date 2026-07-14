package worker

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

type ActressSyncStatus string

const (
	ActressSyncUpdated  ActressSyncStatus = "updated"
	ActressSyncSkipped  ActressSyncStatus = "skipped"
	ActressSyncConflict ActressSyncStatus = "conflict"
	ActressSyncFailed   ActressSyncStatus = "failed"
)

// ActressSyncResult describes the outcome of enriching one actress. A conflict
// can still include an independently updated thumbnail, but never changes the
// target actress's DMM ID.
type ActressSyncResult struct {
	Actress           models.Actress    `json:"actress"`
	Status            ActressSyncStatus `json:"status"`
	UpdatedFields     []string          `json:"updated_fields"`
	Messages          []string          `json:"messages"`
	Source            string            `json:"source,omitempty"`
	SourceQuery       string            `json:"source_query,omitempty"`
	ConflictActressID *uint             `json:"conflict_actress_id,omitempty"`
}

type resolvedActressCandidate struct {
	info   models.ActressInfo
	source string
	query  string
}

// SyncActressMetadata fills missing identity metadata. A resolver-confirmed
// canonical name replaces a nickname/decorated source row, preserves that old
// value as an alias, and reuses or merges an existing canonical actress.
func SyncActressMetadata(
	ctx context.Context,
	actressID uint,
	actressRepo *database.ActressRepository,
	registry *models.ScraperRegistry,
	scraperPriority []string,
	movieRepos ...*database.MovieRepository,
) (*ActressSyncResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actress, err := actressRepo.FindByID(actressID)
	if err != nil {
		return nil, err
	}

	result := &ActressSyncResult{
		Actress:       *actress,
		Status:        ActressSyncSkipped,
		UpdatedFields: []string{},
		Messages:      []string{},
	}
	missingDMMID := actress.DMMID <= 0
	missingThumbnail := strings.TrimSpace(actress.ThumbURL) == ""
	preserveExistingProfile := actress.DMMID > 0 && hasUsableActressIdentityProfile(*actress)

	var candidate *resolvedActressCandidate
	var profileCandidate *resolvedActressCandidate
	// A complete existing profile is authoritative. Its Japanese and display
	// names are never re-verified or translated during metadata sync; only a
	// missing thumbnail may be filled below.
	if !preserveExistingProfile && !missingDMMID && len(movieRepos) > 0 && movieRepos[0] != nil {
		candidate, result.Messages, err = resolveActressFromRecentMovies(
			ctx, *actress, movieRepos[0], registry, result.Messages,
		)
		if err != nil {
			return nil, err
		}
	}
	if !preserveExistingProfile && candidate == nil {
		var identityFailed bool
		candidate, result.Messages, identityFailed, err = resolveMissingActressDMMID(
			ctx, actress, registry, scraperPriority, result.Messages,
		)
		if err != nil {
			return nil, err
		}
		if identityFailed && missingDMMID {
			result.Status = ActressSyncFailed
		}
		if candidate == nil && missingDMMID && len(movieRepos) > 0 && movieRepos[0] != nil {
			candidate, result.Messages, err = resolveActressFromRecentMovies(
				ctx, *actress, movieRepos[0], registry, result.Messages,
			)
			if err != nil {
				return nil, err
			}
		}
	}
	if !preserveExistingProfile && missingThumbnail && candidate == nil {
		profileCandidate, result.Messages = resolveMissingActressProfile(ctx, *actress, registry, scraperPriority, result.Messages)
		if profileCandidate != nil {
			if result.Source == "" {
				result.Source = profileCandidate.source
				result.SourceQuery = profileCandidate.query
			}
			before := len(result.UpdatedFields)
			fillMissingActressNames(actress, profileCandidate.info, &result.UpdatedFields)
			if len(result.UpdatedFields) > before {
				if err := actressRepo.Update(actress); err != nil {
					return nil, err
				}
			}
		}
	}

	if candidate != nil {
		result.Source = candidate.source
		result.SourceQuery = candidate.query
		before := *actress
		resolution, resolveErr := actressRepo.ResolveVerifiedIdentity(actress.ID, actressModelFromInfo(candidate.info), false)
		if conflict, ok := database.AsActressDMMIDConflict(resolveErr); ok {
			conflictID := conflict.ExistingID
			result.ConflictActressID = &conflictID
			result.Status = ActressSyncConflict
			result.Messages = append(result.Messages, conflict.Error()+"; mappings were not changed")
		} else if resolveErr != nil {
			return nil, resolveErr
		} else {
			actress = &resolution.Actress
			appendActressIdentityUpdates(&result.UpdatedFields, before, *actress, resolution)
		}
	}
	missingThumbnail = strings.TrimSpace(actress.ThumbURL) == ""

	if missingThumbnail {
		thumbnail := ""
		if candidate != nil {
			thumbnail = strings.TrimSpace(candidate.info.ThumbURL)
		}
		if thumbnail == "" && profileCandidate != nil {
			thumbnail = strings.TrimSpace(profileCandidate.info.ThumbURL)
		}
		thumbnailResolver := findActressThumbnailResolver(registry)
		if thumbnail == "" && thumbnailResolver == nil {
			result.Messages = append(result.Messages, "No actress thumbnail resolver is available")
		} else if thumbnail == "" {
			lookupInfo := actressInfoForThumbnail(*actress, candidate)
			thumbnail = strings.TrimSpace(safeResolveActressThumbnail(ctx, thumbnailResolver, lookupInfo))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if thumbnail != "" {
			actress.ThumbURL = thumbnail
			if err := actressRepo.Update(actress); err != nil {
				return nil, err
			}
			result.UpdatedFields = appendUnique(result.UpdatedFields, "thumb_url")
		} else if thumbnailResolver != nil {
			result.Messages = append(result.Messages, "Profile thumbnail could not be resolved")
		}
	}

	if len(result.UpdatedFields) > 0 && result.Status != ActressSyncConflict {
		result.Status = ActressSyncUpdated
	}
	if result.Status == ActressSyncUpdated {
		result.Messages = actressSyncFailureMessages(result.Messages)
	}
	if len(result.Messages) == 0 && len(result.UpdatedFields) == 0 {
		result.Messages = append(result.Messages, "No metadata could be safely updated")
	}
	result.Actress = *actress
	return result, nil
}

func actressSyncFailureMessages(messages []string) []string {
	markers := []string{
		" failed:",
		"lookup failed",
		"could not be resolved",
		"resolver is unavailable",
		"disabled or unavailable",
		"no actress thumbnail resolver",
		"returned no result",
	}
	filtered := make([]string, 0, len(messages))
	for _, message := range messages {
		normalized := strings.ToLower(strings.TrimSpace(message))
		for _, marker := range markers {
			if strings.Contains(normalized, marker) {
				filtered = append(filtered, message)
				break
			}
		}
	}
	return filtered
}

func actressModelFromInfo(info models.ActressInfo) models.Actress {
	return models.Actress{
		DMMID:        info.DMMID,
		FirstName:    strings.TrimSpace(info.FirstName),
		LastName:     strings.TrimSpace(info.LastName),
		JapaneseName: strings.TrimSpace(info.JapaneseName),
		ThumbURL:     strings.TrimSpace(info.ThumbURL),
	}
}

func appendActressIdentityUpdates(fields *[]string, before, after models.Actress, resolution *database.VerifiedActressResolution) {
	if before.DMMID != after.DMMID {
		*fields = appendUnique(*fields, "dmm_id")
	}
	if before.JapaneseName != after.JapaneseName {
		*fields = appendUnique(*fields, "japanese_name")
	}
	if before.FirstName != after.FirstName {
		*fields = appendUnique(*fields, "first_name")
	}
	if before.LastName != after.LastName {
		*fields = appendUnique(*fields, "last_name")
	}
	if before.ThumbURL != after.ThumbURL {
		*fields = appendUnique(*fields, "thumb_url")
	}
	if before.Aliases != after.Aliases {
		*fields = appendUnique(*fields, "aliases")
	}
	if resolution != nil && (resolution.UpdatedMovies > 0 || len(resolution.MergedFromIDs) > 0) {
		*fields = appendUnique(*fields, "movie_actresses")
	}
}

func resolveMissingActressProfile(
	ctx context.Context,
	target models.Actress,
	registry *models.ScraperRegistry,
	priority []string,
	messages []string,
) (*resolvedActressCandidate, []string) {
	query := models.ActressIdentityQuery{Names: actressIdentityNames(target), ThumbURL: strings.TrimSpace(target.ThumbURL)}
	for _, source := range enabledActressIdentitySources(registry, priority) {
		if err := ctx.Err(); err != nil {
			return nil, messages
		}
		resolved, err := safeResolveActressIdentity(ctx, source, query)
		if err != nil || resolved == nil {
			continue
		}
		candidate, ok := exactActressProfileMatch(target, resolved.Actresses)
		if !ok || strings.TrimSpace(candidate.ThumbURL) == "" {
			continue
		}
		return &resolvedActressCandidate{info: candidate, source: source.Name(), query: strings.TrimSpace(resolved.ID)}, messages
	}
	return nil, messages
}

func exactActressProfileMatch(target models.Actress, candidates []models.ActressInfo) (models.ActressInfo, bool) {
	targetNames := actressNameKeys(target)
	var matches []models.ActressInfo
	for _, candidate := range candidates {
		if nameSetsIntersect(targetNames, actressInfoNameKeys(candidate)) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return models.ActressInfo{}, false
	}
	return matches[0], true
}

func fillMissingActressNames(actress *models.Actress, incoming models.ActressInfo, updatedFields *[]string) {
	if actress == nil {
		return
	}
	wasUnknown := models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName)
	if (strings.TrimSpace(actress.JapaneseName) == "" || models.IsUnknownActressName(actress.JapaneseName)) &&
		strings.TrimSpace(incoming.JapaneseName) != "" && !models.IsUnknownActressName(incoming.JapaneseName) {
		actress.JapaneseName = strings.TrimSpace(incoming.JapaneseName)
		*updatedFields = append(*updatedFields, "japanese_name")
		if wasUnknown {
			if actress.FirstName != "" {
				actress.FirstName = ""
				*updatedFields = append(*updatedFields, "first_name")
			}
			if actress.LastName != "" {
				actress.LastName = ""
				*updatedFields = append(*updatedFields, "last_name")
			}
		}
	}
	if (strings.TrimSpace(actress.FirstName) == "" || wasUnknown) && strings.TrimSpace(incoming.FirstName) != "" {
		actress.FirstName = strings.TrimSpace(incoming.FirstName)
		*updatedFields = append(*updatedFields, "first_name")
	}
	if (strings.TrimSpace(actress.LastName) == "" || wasUnknown) && strings.TrimSpace(incoming.LastName) != "" {
		actress.LastName = strings.TrimSpace(incoming.LastName)
		*updatedFields = append(*updatedFields, "last_name")
	}
}

func resolveActressFromRecentMovies(
	ctx context.Context,
	target models.Actress,
	movieRepo *database.MovieRepository,
	registry *models.ScraperRegistry,
	messages []string,
) (*resolvedActressCandidate, []string, error) {
	if registry == nil {
		return nil, append(messages, "SougouWiki fallback is unavailable"), nil
	}
	scraper, ok := registry.Get("sougouwiki")
	if !ok || scraper == nil || !scraper.IsEnabled() {
		return nil, append(messages, "SougouWiki fallback is disabled or unavailable"), nil
	}
	resolver, ok := scraper.(models.ActressResolver)
	if !ok {
		return nil, append(messages, "SougouWiki does not support movie actress resolution"), nil
	}
	movies, err := movieRepo.ListByActressID(target.ID, 5, 0)
	if err != nil {
		return nil, messages, err
	}
	if len(movies) == 0 {
		return nil, append(messages, "No linked movies are available for SougouWiki fallback"), nil
	}

	for _, movie := range movies {
		if err := ctx.Err(); err != nil {
			return nil, messages, err
		}
		queryID := strings.TrimSpace(movie.ID)
		if queryID == "" {
			queryID = strings.TrimSpace(movie.ContentID)
		}
		resolved, resolveErr := safeResolveActresses(ctx, resolver, queryID)
		if resolveErr != nil {
			if err := ctx.Err(); err != nil {
				return nil, messages, err
			}
			messages = append(messages, fmt.Sprintf("sougouwiki: %s failed: %v", queryID, resolveErr))
			continue
		}
		if resolved == nil || len(resolved.Actresses) == 0 {
			messages = append(messages, fmt.Sprintf("sougouwiki: %s returned no actresses", queryID))
			continue
		}
		if match, matched := exactActressDMMIDMatch(target.DMMID, resolved.Actresses); matched {
			return &resolvedActressCandidate{info: match, source: scraper.Name(), query: queryID}, messages, nil
		}
		if match, matched := exactActressMatch(target, resolved.Actresses); matched {
			return &resolvedActressCandidate{info: match, source: scraper.Name(), query: queryID}, messages, nil
		}
		if match, matched := safeSingleRemainingActress(target.ID, movie.Actresses, resolved.Actresses); matched {
			return &resolvedActressCandidate{info: match, source: scraper.Name(), query: queryID}, messages, nil
		}
		messages = append(messages, fmt.Sprintf("sougouwiki: %s was ambiguous", queryID))
	}
	return nil, append(messages, "No safe SougouWiki fallback match was found in up to 5 linked movies"), nil
}

func exactActressDMMIDMatch(dmmID int, candidates []models.ActressInfo) (models.ActressInfo, bool) {
	if dmmID <= 0 {
		return models.ActressInfo{}, false
	}
	var matched models.ActressInfo
	count := 0
	for _, candidate := range candidates {
		if candidate.DMMID != dmmID || !isVerifiedActressInfo(candidate) {
			continue
		}
		matched = candidate
		count++
	}
	return matched, count == 1
}

func safeSingleRemainingActress(targetID uint, linked []models.Actress, candidates []models.ActressInfo) (models.ActressInfo, bool) {
	missing := 0
	confirmed := make(map[int]struct{})
	for _, actress := range linked {
		if actress.ID == targetID {
			if actress.DMMID <= 0 {
				missing++
			}
			continue
		}
		if actress.DMMID > 0 {
			confirmed[actress.DMMID] = struct{}{}
		} else {
			missing++
		}
	}
	if missing != 1 {
		return models.ActressInfo{}, false
	}
	remaining := make(map[int]models.ActressInfo)
	for _, candidate := range candidates {
		if !isVerifiedActressInfo(candidate) {
			continue
		}
		if _, exists := confirmed[candidate.DMMID]; exists {
			continue
		}
		remaining[candidate.DMMID] = candidate
	}
	if len(remaining) != 1 {
		return models.ActressInfo{}, false
	}
	for _, candidate := range remaining {
		return candidate, true
	}
	return models.ActressInfo{}, false
}

func resolveMissingActressDMMID(
	ctx context.Context,
	actress *models.Actress,
	registry *models.ScraperRegistry,
	scraperPriority []string,
	messages []string,
) (*resolvedActressCandidate, []string, bool, error) {
	query := models.ActressIdentityQuery{
		Names:    actressIdentityNames(*actress),
		ThumbURL: strings.TrimSpace(actress.ThumbURL),
	}
	if len(query.Names) == 0 && query.ThumbURL == "" {
		return nil, append(messages, "No actress name or thumbnail is available for identity lookup"), true, nil
	}

	sources := enabledActressIdentitySources(registry, scraperPriority)
	if len(sources) == 0 {
		return nil, append(messages, "No enabled actress identity resolver is available"), true, nil
	}

	hadResolverFailure := false
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, messages, hadResolverFailure, err
		}
		sourceResult, resolveErr := safeResolveActressIdentity(ctx, source, query)
		if resolveErr != nil {
			if err := ctx.Err(); err != nil {
				return nil, messages, hadResolverFailure, err
			}
			if scraperErr, ok := models.AsScraperError(resolveErr); ok && scraperErr.Kind == models.ScraperErrorKindNotFound {
				continue
			}
			hadResolverFailure = true
			messages = append(messages, fmt.Sprintf("%s: identity lookup failed: %v", source.Name(), resolveErr))
			continue
		}
		if sourceResult == nil {
			messages = append(messages, fmt.Sprintf("%s: identity lookup returned no result", source.Name()))
			continue
		}
		match, ok := exactActressMatch(*actress, sourceResult.Actresses)
		if !ok {
			messages = append(messages,
				fmt.Sprintf("%s: rejected %d result(s) because there was no unique exact name match", source.Name(), len(sourceResult.Actresses)))
			continue
		}
		matchedQuery := strings.TrimSpace(sourceResult.ID)
		if matchedQuery == "" {
			matchedQuery = strings.Join(query.Names, " | ")
		}
		return &resolvedActressCandidate{info: match, source: source.Name(), query: matchedQuery}, messages, false, nil
	}
	return nil, messages, hadResolverFailure, nil
}

func enabledActressIdentitySources(registry *models.ScraperRegistry, priority []string) []models.Scraper {
	if registry == nil {
		return nil
	}
	var sources []models.Scraper
	for _, scraper := range registry.GetByPriority(priority) {
		if _, ok := scraper.(models.ActressIdentityResolver); ok {
			sources = append(sources, scraper)
		}
	}
	return sources
}

func exactActressMatch(target models.Actress, candidates []models.ActressInfo) (models.ActressInfo, bool) {
	targetNames := actressNameKeys(target)
	if len(targetNames) == 0 {
		return models.ActressInfo{}, false
	}
	matched := make(map[int]models.ActressInfo)
	for _, candidate := range candidates {
		if !isVerifiedActressInfo(candidate) || !nameSetsIntersect(targetNames, actressInfoNameKeys(candidate)) {
			continue
		}
		matched[candidate.DMMID] = candidate
	}
	if len(matched) != 1 {
		return models.ActressInfo{}, false
	}
	for _, candidate := range matched {
		return candidate, true
	}
	return models.ActressInfo{}, false
}

func isVerifiedActressInfo(actress models.ActressInfo) bool {
	if actress.DMMID <= 0 ||
		models.IsUnknownActressFields(actress.LastName, actress.FirstName, actress.JapaneseName) ||
		models.IsDescriptiveNonName(actress.LastName, actress.FirstName, actress.JapaneseName) {
		return false
	}
	return strings.TrimSpace(actress.JapaneseName) != "" ||
		strings.TrimSpace(actress.FirstName) != "" ||
		strings.TrimSpace(actress.LastName) != ""
}

func actressNameKeys(actress models.Actress) map[string]struct{} {
	keys := make(map[string]struct{})
	addNameKey(keys, actress.JapaneseName)
	for _, alias := range strings.Split(actress.Aliases, "|") {
		addNameKey(keys, alias)
	}
	addEnglishNameKeys(keys, actress.FirstName, actress.LastName)
	return keys
}

func actressIdentityNames(actress models.Actress) []string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		key := normalizeActressSyncName(name)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	add(actress.JapaneseName)
	for _, alias := range strings.Split(actress.Aliases, "|") {
		add(alias)
	}
	firstName := strings.TrimSpace(actress.FirstName)
	lastName := strings.TrimSpace(actress.LastName)
	if firstName != "" || lastName != "" {
		add(strings.TrimSpace(firstName + " " + lastName))
		add(strings.TrimSpace(lastName + " " + firstName))
	}
	for _, thumbnailName := range actressThumbnailNameCandidates(actress.ThumbURL) {
		add(thumbnailName)
	}
	return names
}

func actressThumbnailNameCandidates(rawURL string) []string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.Contains(strings.ToLower(parsed.Path), "/actjpgs/") {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "pics.dmm.co.jp" && host != "awsimgsrc.dmm.co.jp" && host != "awsimgsrc.dmm.com" {
		return nil
	}
	filename := path.Base(parsed.Path)
	name := strings.TrimSuffix(filename, path.Ext(filename))
	name = strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(name))
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return nil
	}
	result := []string{strings.Join(parts, " ")}
	if len(parts) == 2 {
		result = append(result, parts[1]+" "+parts[0])
	}
	return result
}

func actressInfoNameKeys(actress models.ActressInfo) map[string]struct{} {
	keys := make(map[string]struct{})
	addNameKey(keys, actress.JapaneseName)
	addEnglishNameKeys(keys, actress.FirstName, actress.LastName)
	return keys
}

func addEnglishNameKeys(keys map[string]struct{}, firstName, lastName string) {
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)
	if firstName == "" && lastName == "" {
		return
	}
	addNameKey(keys, strings.TrimSpace(firstName+" "+lastName))
	addNameKey(keys, strings.TrimSpace(lastName+" "+firstName))
}

func addNameKey(keys map[string]struct{}, name string) {
	name = normalizeActressSyncName(name)
	if name != "" {
		keys[name] = struct{}{}
	}
}

func normalizeActressSyncName(name string) string {
	return strings.ToLower(scraperutil.CleanActressName(name))
}

func nameSetsIntersect(left, right map[string]struct{}) bool {
	for key := range left {
		if _, ok := right[key]; ok {
			return true
		}
	}
	return false
}

func actressInfoForThumbnail(actress models.Actress, candidate *resolvedActressCandidate) models.ActressInfo {
	info := models.ActressInfo{
		DMMID:        actress.DMMID,
		FirstName:    actress.FirstName,
		LastName:     actress.LastName,
		JapaneseName: actress.JapaneseName,
	}
	if candidate == nil {
		return info
	}
	if info.DMMID <= 0 {
		info.DMMID = candidate.info.DMMID
	}
	if info.FirstName == "" {
		info.FirstName = candidate.info.FirstName
	}
	if info.LastName == "" {
		info.LastName = candidate.info.LastName
	}
	if info.JapaneseName == "" {
		info.JapaneseName = candidate.info.JapaneseName
	}
	return info
}
