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
	ActressSyncUpdated  ActressSyncStatus = "updated"
	ActressSyncSkipped  ActressSyncStatus = "skipped"
	ActressSyncConflict ActressSyncStatus = "conflict"
	ActressSyncFailed   ActressSyncStatus = "failed"
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
	_ = movieRepos
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
			profileChanged := false
			if name := strings.TrimSpace(profile.JapaneseName); name != strings.TrimSpace(actress.JapaneseName) {
				actress.JapaneseName = name
				actress.FirstName = strings.TrimSpace(profile.FirstName)
				actress.LastName = strings.TrimSpace(profile.LastName)
				result.UpdatedFields = append(result.UpdatedFields, "japanese_name")
				profileChanged = true
			}
			if thumbnail := strings.TrimSpace(profile.ThumbURL); thumbnail != "" {
				profileThumbnailResolved = true
				if thumbnail != strings.TrimSpace(actress.ThumbURL) {
					actress.ThumbURL = thumbnail
					result.UpdatedFields = appendUnique(result.UpdatedFields, "thumb_url")
					profileChanged = true
				}
			}
			if profileChanged {
				if err := actressRepo.Update(ctx, actress); err != nil {
					return nil, err
				}
			}
		} else if profileErr != nil {
			result.Messages = append(result.Messages, "DMM actress profile could not be resolved; existing name was retained")
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
		if thumbnail != "" && thumbnail != strings.TrimSpace(actress.ThumbURL) {
			actress.ThumbURL = thumbnail
			if err := actressRepo.Update(ctx, actress); err != nil {
				return nil, err
			}
			result.UpdatedFields = appendUnique(result.UpdatedFields, "thumb_url")
		} else if thumbnailResolver != nil {
			result.Messages = append(result.Messages, "Profile thumbnail could not be resolved")
		}
	}

	if len(result.UpdatedFields) > 0 {
		result.Status = ActressSyncUpdated
		result.Messages = nil
	} else if len(result.Messages) == 0 {
		result.Messages = append(result.Messages, "No metadata could be safely updated")
	}
	result.Actress = *actress
	return result, nil
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
		ThumbURL:     strings.TrimSpace(info.ThumbURL),
	}
}

func actressInfoForThumbnail(actress models.Actress) models.ActressInfo {
	return models.ActressInfo{
		DMMID:        actress.DMMID,
		FirstName:    actress.FirstName,
		LastName:     actress.LastName,
		JapaneseName: actress.JapaneseName,
	}
}
