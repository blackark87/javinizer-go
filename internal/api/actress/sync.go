package actress

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
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
