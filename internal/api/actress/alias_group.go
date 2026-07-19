package actress

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// aliasGroupResponse is the API representation of an actress's known names.
type aliasGroupResponse struct {
	Canonical string                     `json:"canonical"`
	Names     []string                   `json:"names"`
	Members   []aliasGroupMemberResponse `json:"members"`
}

type aliasGroupMemberResponse struct {
	Name      string          `json:"name"`
	Canonical bool            `json:"canonical"`
	Available bool            `json:"available"`
	Actress   *models.Actress `json:"actress,omitempty"`
}

// getAliasGroup handles GET /api/v1/actresses/alias-group?name=
// @Summary Get actress alias group
// @Description Resolve a name to its full set of known names (canonical plus all aliases that resolve to it). Returns empty canonical/names when the name is unknown to the alias table.
// @Tags actress
// @Produce json
// @Param name query string true "Actress name (alias or canonical) to resolve"
// @Success 200 {object} actress.aliasGroupResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/actresses/alias-group [get]
func getAliasGroup(deps ActressDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := strings.TrimSpace(c.Query("name"))
		if name == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "name query parameter is required"})
			return
		}

		repo := deps.ActressAliasRepo
		if repo == nil {
			// No alias repository configured — nothing to choose between.
			c.JSON(http.StatusOK, aliasGroupResponse{Canonical: "", Names: nil})
			return
		}

		group, err := repo.GetAliasGroup(c.Request.Context(), name)
		if err != nil {
			logging.Errorf("actress alias-group lookup failed for %q: %v", name, err)
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "internal server error"})
			return
		}

		names := group.Names
		if names == nil {
			names = []string{}
		}
		members := make([]aliasGroupMemberResponse, 0, len(group.Members))
		for _, member := range group.Members {
			response := aliasGroupMemberResponse{Name: member.Name, Canonical: member.Canonical, Available: member.Available}
			if member.Available {
				actress := member.Actress
				if deps.ActressTranslationRepo != nil {
					translations, lookupErr := deps.ActressTranslationRepo.FindAllByActress(c.Request.Context(), actress.ID)
					if lookupErr != nil {
						logging.Warnf("actress alias translations lookup failed for %d: %v", actress.ID, lookupErr)
					} else {
						actress.Translations = translations
					}
				}
				response.Actress = &actress
			}
			members = append(members, response)
		}
		c.JSON(http.StatusOK, aliasGroupResponse{
			Canonical: group.Canonical,
			Names:     names,
			Members:   members,
		})
	}
}
