package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// ActressSyncStatus describes the outcome of an actress metadata sync.
type ActressSyncStatus string

// Actress metadata sync outcomes.
const (
	ActressSyncUpdated   ActressSyncStatus = "updated"
	ActressSyncSkipped   ActressSyncStatus = "skipped"
	ActressSyncConflict  ActressSyncStatus = "conflict"
	ActressSyncFailed    ActressSyncStatus = "failed"
	maxActressSyncMovies                   = 5
)

// ActressSyncResult is the internal result used by a durable actress sync task.
type ActressSyncResult struct {
	Actress           models.Actress    `json:"actress"`
	Status            ActressSyncStatus `json:"status"`
	UpdatedFields     []string          `json:"updated_fields"`
	Messages          []string          `json:"messages"`
	Source            string            `json:"source,omitempty"`
	SourceQuery       string            `json:"source_query,omitempty"`
	ConflictActressID *uint             `json:"conflict_actress_id,omitempty"`
}

// SyncActressMetadata handles only a verified actress whose thumbnail is
// missing. DMM-ID-less actresses must use the durable per-movie SougouWiki
// tasks so the old direct-name and five-recent-movie paths cannot run.
func SyncActressMetadata(
	ctx context.Context,
	actressID uint,
	actressRepo *database.ActressRepository,
	registry scraperutil.ScraperInstancesInterface,
	scraperPriority []string,
	movieRepos ...*database.MovieRepository,
) (*ActressSyncResult, error) {
	_ = scraperPriority
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actress, err := actressRepo.FindByID(ctx, actressID)
	if err != nil {
		return nil, err
	}

	result := &ActressSyncResult{
		Actress:       *actress,
		Status:        ActressSyncSkipped,
		UpdatedFields: []string{},
		Messages:      []string{},
	}
	if actress.DMMID <= 0 {
		result.Status = ActressSyncFailed
		result.Messages = append(result.Messages, "DMM-ID-less actresses must be synced through durable per-movie jobs")
		return result, nil
	}

	profileThumbnailResolved := false
	if profileResolver := findActressProfileResolver(registry); profileResolver != nil {
		profile, profileErr := safeResolveActressProfile(ctx, profileResolver, actressInfoForThumbnail(*actress))
		if profileErr == nil && strings.TrimSpace(profile.JapaneseName) != "" {
			profile.DMMID = actress.DMMID
			profile.ThumbURL = strings.TrimSpace(profile.ThumbURL)
			if profile.ThumbURL != "" {
				profileThumbnailResolved = true
			}
			observedAliases := splitStoredActressAliases(actress.Aliases)
			if oldName := strings.TrimSpace(actress.JapaneseName); oldName != "" {
				observedAliases = append(observedAliases, oldName)
			}
			resolution, resolveErr := actressRepo.ResolveVerifiedProfile(actress.ID, actressModelFromInfo(profile), observedAliases, false)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if resolution.NameChanged {
				result.UpdatedFields = appendUnique(result.UpdatedFields, "japanese_name")
			}
			if strings.TrimSpace(resolution.Actress.Reading) != strings.TrimSpace(actress.Reading) {
				result.UpdatedFields = appendUnique(result.UpdatedFields, "reading")
			}
			if strings.TrimSpace(resolution.Actress.ThumbURL) != strings.TrimSpace(actress.ThumbURL) {
				result.UpdatedFields = appendUnique(result.UpdatedFields, "thumb_url")
			}
			if len(resolution.AliasesAdded) > 0 || len(resolution.AliasMappingsAdded) > 0 {
				result.UpdatedFields = appendUnique(result.UpdatedFields, "aliases")
			}
			if len(resolution.AliasConflicts) > 0 {
				result.Messages = append(result.Messages, "Existing manual alias mappings were retained for: "+strings.Join(resolution.AliasConflicts, ", "))
			}
			*actress = resolution.Actress
		} else if profileErr != nil {
			result.Messages = append(result.Messages, "DMM actress profile could not be resolved; existing name was retained")
		}
	}

	if !profileThumbnailResolved {
		if movieRepo := firstMovieRepository(movieRepos); movieRepo != nil {
			thumbnail := resolveThumbnailFromRecentMovies(ctx, registry, movieRepo, *actress)
			if thumbnail != "" {
				profileThumbnailResolved = true
				if thumbnail != strings.TrimSpace(actress.ThumbURL) {
					actress.ThumbURL = thumbnail
					if err := actressRepo.Update(ctx, actress); err != nil {
						return nil, err
					}
					result.UpdatedFields = appendUnique(result.UpdatedFields, "thumb_url")
				}
			}
		}
	}

	if !profileThumbnailResolved {
		thumbnailResolver := findActressThumbnailResolver(registry)
		thumbnail := ""
		if thumbnailResolver == nil {
			result.Messages = append(result.Messages, "No actress thumbnail resolver is available")
		} else {
			thumbnail = strings.TrimSpace(safeResolveActressThumbnail(ctx, thumbnailResolver, actressInfoForThumbnail(*actress)))
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if thumbnail != "" {
			profileThumbnailResolved = true
			if thumbnail != strings.TrimSpace(actress.ThumbURL) {
				actress.ThumbURL = thumbnail
				if err := actressRepo.Update(ctx, actress); err != nil {
					return nil, err
				}
				result.UpdatedFields = appendUnique(result.UpdatedFields, "thumb_url")
			}
		} else if thumbnailResolver != nil {
			result.Messages = append(result.Messages, "Profile thumbnail could not be resolved")
		}
	}

	if len(result.UpdatedFields) > 0 {
		result.Status = ActressSyncUpdated
	} else if len(result.Messages) == 0 {
		result.Messages = append(result.Messages, "No metadata could be safely updated")
	}
	result.Actress = *actress
	return result, nil
}

func firstMovieRepository(repos []*database.MovieRepository) *database.MovieRepository {
	for _, repo := range repos {
		if repo != nil {
			return repo
		}
	}
	return nil
}

func resolveThumbnailFromRecentMovies(
	ctx context.Context,
	registry scraperutil.ScraperInstancesInterface,
	movieRepo *database.MovieRepository,
	actress models.Actress,
) string {
	if registry == nil || movieRepo == nil || actress.ID == 0 || actress.DMMID <= 0 {
		return ""
	}
	dmmScraper, ok := registry.GetInstance("dmm")
	if !ok || dmmScraper == nil {
		return ""
	}
	movies, err := movieRepo.ListByActressID(ctx, actress.ID, maxActressSyncMovies, 0)
	if err != nil {
		return ""
	}
	for _, movie := range movies {
		if err := ctx.Err(); err != nil {
			return ""
		}
		var scraped *models.ScraperResult
		if handler, supportsURL := dmmScraper.(models.URLHandler); supportsURL &&
			strings.EqualFold(strings.TrimSpace(movie.SourceName), "dmm") &&
			handler.CanHandleURL(strings.TrimSpace(movie.SourceURL)) {
			scraped, err = handler.ScrapeURL(ctx, strings.TrimSpace(movie.SourceURL))
		} else {
			queryID := strings.TrimSpace(movie.ID)
			if queryID == "" {
				queryID = strings.TrimSpace(movie.ContentID)
			}
			if queryID == "" {
				continue
			}
			scraped, err = dmmScraper.Search(ctx, queryID)
		}
		if err != nil || scraped == nil {
			continue
		}
		for _, candidate := range scraped.Actresses {
			if candidate.DMMID == actress.DMMID && strings.TrimSpace(candidate.ThumbURL) != "" {
				return strings.TrimSpace(candidate.ThumbURL)
			}
		}
	}
	return ""
}

func findActressThumbnailResolver(registry scraperutil.ScraperInstancesInterface) models.ActressThumbnailResolver {
	if registry == nil {
		return nil
	}
	for _, instance := range registry.GetAllInstances() {
		if resolver, ok := instance.(models.ActressThumbnailResolver); ok {
			return resolver
		}
	}
	return nil
}

func findActressProfileResolver(registry scraperutil.ScraperInstancesInterface) models.ActressProfileResolver {
	if registry == nil {
		return nil
	}
	for _, instance := range registry.GetAllInstances() {
		if resolver, ok := instance.(models.ActressProfileResolver); ok {
			return resolver
		}
	}
	return nil
}

func safeResolveActressProfile(ctx context.Context, resolver models.ActressProfileResolver, actress models.ActressInfo) (result models.ActressInfo, err error) {
	if resolver == nil {
		return models.ActressInfo{}, errors.New("actress profile resolver is unavailable")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = models.ActressInfo{}
			err = fmt.Errorf("actress profile resolver panicked: %v", recovered)
		}
	}()
	return resolver.ResolveActressProfile(ctx, actress)
}

func safeResolveActressThumbnail(ctx context.Context, resolver models.ActressThumbnailResolver, actress models.ActressInfo) (result string) {
	if resolver == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			result = ""
		}
	}()
	return resolver.ResolveActressThumbnail(ctx, actress)
}

func safeResolveActresses(ctx context.Context, resolver models.ActressResolver, id string) (result *models.ScraperResult, err error) {
	if resolver == nil {
		return nil, errors.New("actress resolver is unavailable")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("actress resolver panicked: %v", recovered)
		}
	}()
	return resolver.ResolveActresses(ctx, id)
}

func actressModelFromInfo(info models.ActressInfo) models.Actress {
	return models.Actress{
		DMMID:        info.DMMID,
		FirstName:    strings.TrimSpace(info.FirstName),
		LastName:     strings.TrimSpace(info.LastName),
		JapaneseName: strings.TrimSpace(info.JapaneseName),
		Reading:      strings.TrimSpace(info.Reading),
		ThumbURL:     strings.TrimSpace(info.ThumbURL),
	}
}

func splitStoredActressAliases(value string) []string {
	aliases := make([]string, 0)
	for _, alias := range strings.Split(value, "|") {
		alias = strings.TrimSpace(alias)
		if alias != "" {
			aliases = append(aliases, alias)
		}
	}
	return aliases
}

func isObservedSyncAlias(observed, canonical string) bool {
	observed = strings.TrimSpace(observed)
	canonical = strings.TrimSpace(canonical)
	return observed != "" && canonical != "" &&
		!models.IsUnknownActressName(observed) && !models.IsDescriptiveNonName("", "", observed) &&
		!strings.EqualFold(observed, canonical)
}

func actressInfoForThumbnail(actress models.Actress) models.ActressInfo {
	return models.ActressInfo{
		DMMID:        actress.DMMID,
		FirstName:    actress.FirstName,
		LastName:     actress.LastName,
		JapaneseName: actress.JapaneseName,
		Reading:      actress.Reading,
	}
}
