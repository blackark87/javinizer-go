package actress

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

type actressSyncCandidatesResponse struct {
	IDs       []uint           `json:"ids"`
	Actresses []models.Actress `json:"actresses"`
	Total     int              `json:"total"`
}

// listActressSyncCandidates godoc
// @Summary List actresses missing metadata
// @Description Return IDs of actresses missing a DMM ID or profile thumbnail
// @Tags actress
// @Produce json
// @Success 200 {object} actressSyncCandidatesResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/actresses/sync-candidates [get]
func listActressSyncCandidates(actressRepo *database.ActressRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		actresses, err := actressRepo.ListMissingMetadata()
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		ids := make([]uint, 0, len(actresses))
		for _, actress := range actresses {
			ids = append(ids, actress.ID)
		}
		c.JSON(http.StatusOK, actressSyncCandidatesResponse{IDs: ids, Actresses: actresses, Total: len(ids)})
	}
}

// syncActress godoc
// @Summary Sync missing actress metadata
// @Description Safely fill only a missing DMM ID and/or profile thumbnail for one actress
// @Tags actress
// @Produce json
// @Param id path uint true "Actress ID"
// @Success 200 {object} worker.ActressSyncResult
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Failure 504 {object} ErrorResponse
// @Router /api/v1/actresses/{id}/sync [post]
func syncActress(deps *core.ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseActressID(c)
		if !ok {
			return
		}

		cfg := deps.GetConfig()
		timeoutSeconds := cfg.Scrapers.RequestTimeoutSeconds
		if timeoutSeconds <= 0 {
			timeoutSeconds = 60
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(timeoutSeconds)*time.Second)
		defer cancel()

		result, err := worker.SyncActressMetadata(
			ctx,
			id,
			deps.ActressRepo,
			deps.GetRegistry(),
			cfg.Scrapers.Priority,
		)
		if err != nil {
			switch {
			case database.IsNotFound(err):
				c.JSON(http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("Actress #%d: actress not found", id)})
			case errors.Is(err, context.DeadlineExceeded):
				c.JSON(http.StatusGatewayTimeout, ErrorResponse{Error: actressSyncError(deps.ActressRepo, id, "sync timed out")})
			case errors.Is(err, context.Canceled):
				c.JSON(http.StatusRequestTimeout, ErrorResponse{Error: actressSyncError(deps.ActressRepo, id, "sync canceled")})
			default:
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: actressSyncError(deps.ActressRepo, id, err.Error())})
			}
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func actressSyncError(repo *database.ActressRepository, id uint, message string) string {
	label := fmt.Sprintf("Actress #%d", id)
	if repo != nil {
		if actress, err := repo.FindByID(id); err == nil {
			if name := strings.TrimSpace(actress.FullName()); name != "" {
				label = fmt.Sprintf("%s (#%d)", name, id)
			}
		}
	}
	return label + ": " + message
}
