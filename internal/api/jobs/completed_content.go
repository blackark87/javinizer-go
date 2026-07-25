package jobs

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
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

		contents, total, err := deps.CompletedContentRepo.ListCompletedContent(
			c.Request.Context(),
			strings.TrimSpace(c.Query("q")),
			limit,
			offset,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve completed content"})
			return
		}

		items := make([]contracts.CompletedContentItem, len(contents))
		for i, content := range contents {
			items[i] = contracts.CompletedContentItem{
				MovieID:          content.MovieID,
				ContentID:        content.ContentID,
				DisplayTitle:     content.DisplayTitle,
				Title:            content.Title,
				OriginalTitle:    content.OriginalTitle,
				PosterURL:        content.PosterURL,
				CroppedPosterURL: content.CroppedPosterURL,
				Actresses:        content.Actresses,
				Paths:            content.Paths,
				LatestJobID:      content.LatestJobID,
				OrganizedAt:      content.OrganizedAt.Format(time.RFC3339),
			}
		}

		c.JSON(http.StatusOK, contracts.CompletedContentListResponse{
			Contents: items,
			Total:    total,
			Limit:    limit,
			Offset:   offset,
		})
	}
}

func completedContentIntQuery(c *gin.Context, name string, fallback int) (int, error) {
	raw := c.Query(name)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}
