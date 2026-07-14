package worker

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type ActressSyncStatus string

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
	registry *models.ScraperRegistry,
	scraperPriority []string,
	movieRepos ...*database.MovieRepository,
) (*ActressSyncResult, error) {
	_ = scraperPriority
	_ = movieRepos
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
	if actress.DMMID <= 0 {
		result.Status = ActressSyncFailed
		result.Messages = append(result.Messages, "DMM-ID-less actresses must be synced through durable per-movie jobs")
		return result, nil
	}

	if strings.TrimSpace(actress.ThumbURL) == "" {
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
			actress.ThumbURL = thumbnail
			if err := actressRepo.Update(actress); err != nil {
				return nil, err
			}
			result.UpdatedFields = append(result.UpdatedFields, "thumb_url")
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
