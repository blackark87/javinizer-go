package jobs

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
)

const (
	defaultCompletedContentLimit = 20
	maxCompletedContentLimit     = 100
)

// listCompletedContent godoc
// @Summary List completed content
// @Description List successfully organized content grouped by movie, searchable by title or actress
// @Tags jobs
// @Produce json
// @Param q query string false "Search title, movie ID, path, or actress name"
// @Param actress_id query int false "Filter by exact actress ID" minimum(1)
// @Param sort query string false "Sort field" Enums(organized_at,metadata_created_at,metadata_updated_at) default(organized_at)
// @Param order query string false "Sort direction" Enums(asc,desc) default(desc)
// @Param limit query int false "Maximum number of movies to return" default(20) minimum(1) maximum(100)
// @Param offset query int false "Number of movies to skip" default(0) minimum(0)
// @Success 200 {object} contracts.CompletedContentListResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/completed-content [get]
func listCompletedContent(deps JobDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.CompletedContentRepo == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "Completed content repository is unavailable"})
			return
		}

		limit, err := completedContentIntQuery(c, "limit", defaultCompletedContentLimit)
		if err != nil || limit < 1 || limit > maxCompletedContentLimit {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "limit must be between 1 and 100"})
			return
		}
		offset, err := completedContentIntQuery(c, "offset", 0)
		if err != nil || offset < 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "offset must be zero or greater"})
			return
		}
		actressID, err := completedContentUintQuery(c, "actress_id")
		if err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "actress_id must be a positive integer"})
			return
		}
		sortBy := database.CompletedContentSort(strings.TrimSpace(c.DefaultQuery("sort", string(database.CompletedContentSortOrganized))))
		if sortBy != database.CompletedContentSortOrganized &&
			sortBy != database.CompletedContentSortMetadataCreated &&
			sortBy != database.CompletedContentSortMetadataUpdated {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "sort must be organized_at, metadata_created_at, or metadata_updated_at"})
			return
		}
		order := database.CompletedContentSortOrder(strings.TrimSpace(c.DefaultQuery("order", string(database.CompletedContentSortDescending))))
		if order != database.CompletedContentSortAscending && order != database.CompletedContentSortDescending {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "order must be asc or desc"})
			return
		}

		contents, total, err := deps.CompletedContentRepo.ListCompletedContent(
			c.Request.Context(),
			database.CompletedContentListOptions{
				Query:     strings.TrimSpace(c.Query("q")),
				ActressID: actressID,
				Sort:      sortBy,
				Order:     order,
				Limit:     limit,
				Offset:    offset,
			},
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve completed content"})
			return
		}
		actressFilters, err := deps.CompletedContentRepo.ListCompletedContentActressFilters(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve completed content actress filters"})
			return
		}

		items := make([]contracts.CompletedContentItem, len(contents))
		for i, content := range contents {
			items[i] = contracts.CompletedContentItem{
				MovieID:                  content.MovieID,
				ContentID:                content.ContentID,
				DisplayTitle:             content.DisplayTitle,
				Title:                    content.Title,
				OriginalTitle:            content.OriginalTitle,
				PosterURL:                content.PosterURL,
				CoverURL:                 content.CoverURL,
				CroppedPosterURL:         content.CroppedPosterURL,
				OriginalPosterURL:        content.OriginalPosterURL,
				OriginalCroppedPosterURL: content.OriginalCroppedPosterURL,
				OriginalCoverURL:         content.OriginalCoverURL,
				Actresses:                content.Actresses,
				Paths:                    content.Paths,
				LatestJobID:              content.LatestJobID,
				OrganizedAt:              content.OrganizedAt.Format(time.RFC3339),
				MetadataCreatedAt:        completedContentTimeString(content.MetadataCreatedAt),
				MetadataUpdatedAt:        completedContentTimeString(content.MetadataUpdatedAt),
			}
		}
		filterItems := make([]contracts.CompletedContentActressFilter, len(actressFilters))
		for i, filter := range actressFilters {
			filterItems[i] = contracts.CompletedContentActressFilter{
				Actress: filter.Actress,
				Count:   filter.Count,
			}
		}

		c.JSON(http.StatusOK, contracts.CompletedContentListResponse{
			Contents:       items,
			ActressFilters: filterItems,
			Total:          total,
			Limit:          limit,
			Offset:         offset,
		})
	}
}

func completedContentUintQuery(c *gin.Context, name string) (uint, error) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return uint(value), nil
}

func completedContentTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func completedContentIntQuery(c *gin.Context, name string, fallback int) (int, error) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}
