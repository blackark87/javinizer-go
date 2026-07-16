package actress

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
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
func listActressSyncCandidates(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt == nil || rt.Deps() == nil || rt.Deps().CoreDeps == nil || rt.Deps().CoreDeps.DB == nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "actress sync repository is unavailable"})
			return
		}
		actressRepo := database.NewActressRepository(rt.Deps().CoreDeps.DB)
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
