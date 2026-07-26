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
// @Summary List actresses missing metadata or translation
// @Description Return IDs of actresses missing a DMM ID, profile thumbnail, or configured actress translation
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
		cfg := rt.Deps().CoreDeps.GetConfig()
		targetLanguages := []string(nil)
		if cfg.Metadata.Translation.Enabled && cfg.Metadata.Translation.Fields.Actresses {
			targetLanguages = append(targetLanguages, cfg.Metadata.Translation.TargetLanguages...)
			if len(targetLanguages) == 0 {
				targetLanguages = []string{cfg.Metadata.Translation.TargetLanguage}
			}
		}
		ids, err := actressRepo.ListMissingMetadataOrTranslationIDs(targetLanguages)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		actresses, err := actressRepo.ListByIDs(c.Request.Context(), ids)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, actressSyncCandidatesResponse{IDs: ids, Actresses: actresses, Total: len(ids)})
	}
}
