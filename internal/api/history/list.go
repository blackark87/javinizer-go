package history

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// toHistoryRecord converts a models.History to a HistoryRecord for API responses.
func toHistoryRecord(h models.History) HistoryRecord {
	return HistoryRecord{
		ID:           h.ID,
		MovieID:      h.MovieID,
		Operation:    h.Operation,
		OriginalPath: h.OriginalPath,
		NewPath:      h.NewPath,
		Status:       h.Status,
		ErrorMessage: h.ErrorMessage,
		Metadata:     h.Metadata,
		DryRun:       h.DryRun,
		CreatedAt:    h.CreatedAt.Format(time.RFC3339),
	}
}

// getHistory godoc
// @Summary List history records
// @Description Get a paginated list of history records with optional filtering
// @Tags history
// @Produce json
// @Param limit query int false "Number of records to return (default: 50, max: 500)"
// @Param offset query int false "Number of records to skip (default: 0)"
// @Param operation query string false "Filter by operation type (scrape, organize, download, nfo)"
// @Param status query string false "Filter by status (success, failed, reverted)"
// @Param movie_id query string false "Filter by movie ID"
// @Success 200 {object} HistoryListResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/history [get]
func getHistory(historyRepo *database.HistoryRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, offset := core.ParsePagination(c, 50, 500)

		operation := c.Query("operation")
		status := c.Query("status")
		movieID := c.Query("movie_id")

		filter := database.HistoryFilter{
			Operation: operation,
			Status:    status,
			MovieID:   movieID,
		}

		total, err := historyRepo.CountFiltered(filter)
		if err != nil {
			logging.Errorf("Failed to count history: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to count history"})
			return
		}

		history, findErr := historyRepo.ListFiltered(filter, limit, offset)
		if findErr != nil {
			logging.Errorf("Failed to list history: %v", findErr)
			c.JSON(500, ErrorResponse{Error: "Failed to retrieve history"})
			return
		}

		records := make([]HistoryRecord, 0, len(history))
		for _, h := range history {
			records = append(records, toHistoryRecord(h))
		}

		c.JSON(200, HistoryListResponse{
			Records: records,
			Total:   total,
			Limit:   limit,
			Offset:  offset,
		})
	}
}
