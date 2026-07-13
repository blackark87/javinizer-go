package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const maxActressSyncMovies = 5

type ActressSyncStatus string

const (
	ActressSyncUpdated  ActressSyncStatus = "updated"
	ActressSyncSkipped  ActressSyncStatus = "skipped"
	ActressSyncConflict ActressSyncStatus = "conflict"
)

// ActressSyncResult describes the outcome of enriching one actress. A conflict
// can still include an independently updated thumbnail, but never changes the
// target actress's DMM ID.
type ActressSyncResult struct {
	Actress           models.Actress    `json:"actress"`
	Status            ActressSyncStatus `json:"status"`
	UpdatedFields     []string          `json:"updated_fields"`
	Messages          []string          `json:"messages"`
	SourceMovieID     string            `json:"source_movie_id,omitempty"`
	ConflictActressID *uint             `json:"conflict_actress_id,omitempty"`
}

type resolvedActressCandidate struct {
	info    models.ActressInfo
	movieID string
}

// SyncActressMetadata fills only a missing DMM ID and/or thumbnail. Names and
// existing metadata are deliberately preserved.
func SyncActressMetadata(
	ctx context.Context,
	actressID uint,
	actressRepo *database.ActressRepository,
	movieRepo *database.MovieRepository,
	registry *models.ScraperRegistry,
	scraperPriority []string,
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
	if !missingDMMID && !missingThumbnail {
		result.Messages = append(result.Messages, "DMM ID and profile thumbnail are already present")
		return result, nil
	}

	var candidate *resolvedActressCandidate
	if missingDMMID {
		candidate, result.Messages, err = resolveMissingActressDMMID(
			ctx, actress, movieRepo, registry, scraperPriority, result.Messages,
		)
		if err != nil {
			return nil, err
		}
	}

	if candidate != nil {
		result.SourceMovieID = candidate.movieID
		existing, lookupErr := actressRepo.FindByDMMID(candidate.info.DMMID)
		switch {
		case lookupErr == nil && existing.ID != actress.ID:
			conflictID := existing.ID
			result.ConflictActressID = &conflictID
			result.Status = ActressSyncConflict
			result.Messages = append(result.Messages,
				fmt.Sprintf("DMM ID %d is already assigned to actress %d", candidate.info.DMMID, existing.ID))
		case lookupErr == nil:
			// The current row already owns the ID. This is harmless and leaves it unchanged.
		case database.IsNotFound(lookupErr):
			actress.DMMID = candidate.info.DMMID
			if err := actressRepo.Update(actress); err != nil {
				return nil, err
			}
			result.UpdatedFields = append(result.UpdatedFields, "dmm_id")
			result.Messages = append(result.Messages,
				fmt.Sprintf("DMM ID %d verified from movie %s", candidate.info.DMMID, candidate.movieID))
		default:
			return nil, lookupErr
		}
	}

	if missingThumbnail {
		thumbnailResolver := findActressThumbnailResolver(registry)
		if thumbnailResolver == nil {
			result.Messages = append(result.Messages, "No actress thumbnail resolver is available")
		} else {
			lookupInfo := actressInfoForThumbnail(*actress, candidate)
			thumbnail := strings.TrimSpace(safeResolveActressThumbnail(ctx, thumbnailResolver, lookupInfo))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if thumbnail != "" {
				actress.ThumbURL = thumbnail
				if err := actressRepo.Update(actress); err != nil {
					return nil, err
				}
				result.UpdatedFields = append(result.UpdatedFields, "thumb_url")
				result.Messages = append(result.Messages, "Profile thumbnail resolved")
			} else {
				result.Messages = append(result.Messages, "Profile thumbnail could not be resolved")
			}
		}
	}

	if len(result.UpdatedFields) > 0 && result.Status != ActressSyncConflict {
		result.Status = ActressSyncUpdated
	}
	if len(result.Messages) == 0 {
		result.Messages = append(result.Messages, "No metadata could be safely updated")
	}
	result.Actress = *actress
	return result, nil
}

func resolveMissingActressDMMID(
	ctx context.Context,
	actress *models.Actress,
	movieRepo *database.MovieRepository,
	registry *models.ScraperRegistry,
	scraperPriority []string,
	messages []string,
) (*resolvedActressCandidate, []string, error) {
	if movieRepo == nil {
		return nil, append(messages, "No movie repository is available"), nil
	}
	movies, err := movieRepo.ListByActressID(actress.ID, maxActressSyncMovies, 0)
	if err != nil {
		return nil, messages, err
	}
	if len(movies) == 0 {
		return nil, append(messages, "No linked movies are available for DMM ID verification"), nil
	}

	sources := enabledActressSources(registry, scraperPriority)
	if len(sources) == 0 {
		return nil, append(messages, "No enabled scraper is available for DMM ID verification"), nil
	}

	for _, movie := range movies {
		movieID := syncMovieID(movie)
		if movieID == "" {
			continue
		}
		for _, source := range sources {
			if err := ctx.Err(); err != nil {
				return nil, messages, err
			}
			sourceResult, resolveErr := resolveActressesFromSource(ctx, source, movieID)
			if resolveErr != nil {
				if err := ctx.Err(); err != nil {
					return nil, messages, err
				}
				messages = append(messages,
					fmt.Sprintf("Scraper %s failed for movie %s: %v", source.Name(), movieID, resolveErr))
				continue
			}
			if sourceResult == nil {
				continue
			}
			if match, ok := exactActressMatch(*actress, sourceResult.Actresses); ok {
				return &resolvedActressCandidate{info: match, movieID: movieID}, messages, nil
			}
			if match, ok := safeSingleActressMatch(*actress, movie.Actresses, sourceResult.Actresses); ok {
				return &resolvedActressCandidate{info: match, movieID: movieID}, messages, nil
			}
		}
	}
	return nil, append(messages, "No unambiguous DMM actress match was found"), nil
}

func enabledActressSources(registry *models.ScraperRegistry, priority []string) []models.Scraper {
	if registry == nil {
		return nil
	}
	return registry.GetByPriority(priority)
}

func resolveActressesFromSource(ctx context.Context, source models.Scraper, movieID string) (*models.ScraperResult, error) {
	if resolver, ok := source.(models.ActressResolver); ok {
		return safeResolveActresses(ctx, resolver, movieID)
	}
	return safeSearch(ctx, source, movieID)
}

func syncMovieID(movie models.Movie) string {
	if id := strings.TrimSpace(movie.ID); id != "" {
		return id
	}
	return strings.TrimSpace(movie.ContentID)
}

func exactActressMatch(target models.Actress, candidates []models.ActressInfo) (models.ActressInfo, bool) {
	targetNames := actressNameKeys(target)
	if len(targetNames) == 0 {
		return models.ActressInfo{}, false
	}
	matched := make(map[int]models.ActressInfo)
	for _, candidate := range candidates {
		if candidate.DMMID <= 0 || !nameSetsIntersect(targetNames, actressInfoNameKeys(candidate)) {
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

func safeSingleActressMatch(target models.Actress, linked []models.Actress, candidates []models.ActressInfo) (models.ActressInfo, bool) {
	missingCount := 0
	knownOtherDMMIDs := make(map[int]struct{})
	for _, linkedActress := range linked {
		if linkedActress.DMMID <= 0 {
			missingCount++
			continue
		}
		if linkedActress.ID != target.ID {
			knownOtherDMMIDs[linkedActress.DMMID] = struct{}{}
		}
	}
	if missingCount != 1 {
		return models.ActressInfo{}, false
	}

	remaining := make(map[int]models.ActressInfo)
	for _, candidate := range candidates {
		if candidate.DMMID <= 0 {
			continue
		}
		if _, known := knownOtherDMMIDs[candidate.DMMID]; known {
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

func actressNameKeys(actress models.Actress) map[string]struct{} {
	keys := make(map[string]struct{})
	addNameKey(keys, actress.JapaneseName)
	for _, alias := range strings.Split(actress.Aliases, "|") {
		addNameKey(keys, alias)
	}
	addEnglishNameKeys(keys, actress.FirstName, actress.LastName)
	return keys
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
