package history

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const dashboardStatsWindowDays = 7

// getHistoryStats godoc
// @Summary Get history statistics
// @Description Get aggregated statistics about history records
// @Tags history
// @Produce json
// @Success 200 {object} HistoryStats
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/history/stats [get]
func getHistoryStats(historyRepo *database.HistoryRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		recentSince := time.Now().UTC().AddDate(0, 0, -dashboardStatsWindowDays)
		stats, err := historyRepo.StatsAggregate(recentSince)
		if err != nil {
			logging.Errorf("Failed to aggregate history statistics: %v", err)
			c.JSON(500, ErrorResponse{Error: "Failed to get statistics"})
			return
		}

		byOperation := make(map[string]int64, len(stats.ByOperation))
		for _, op := range []string{"scrape", "organize", "download", "nfo"} {
			byOperation[op] = stats.ByOperation[op]
		}
		for op, count := range stats.ByOperation {
			if _, ok := byOperation[op]; !ok {
				byOperation[op] = count
			}
		}

		success7d := stats.RecentByStatus["success"]
		failed7d := stats.RecentByStatus["failed"]
		total7d := int64(0)
		for _, count := range stats.RecentByStatus {
			total7d += count
		}

		successRate7d := 0
		if total7d > 0 {
			successRate7d = int(((success7d * 100) + (total7d / 2)) / total7d)
		}

		c.JSON(200, HistoryStats{
			Total:         stats.Total,
			Success:       stats.ByStatus["success"],
			Failed:        stats.ByStatus["failed"],
			Reverted:      stats.ByStatus["reverted"],
			ByOperation:   byOperation,
			RecentWindow:  dashboardStatsWindowDays,
			Total7d:       total7d,
			Success7d:     success7d,
			Failed7d:      failed7d,
			SuccessRate7d: successRate7d,
		})
	}
}
