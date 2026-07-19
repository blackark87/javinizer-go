package actress

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

type aliasChoiceRequest struct {
	ActressID uint `json:"actress_id" binding:"required"`
}

// resolveAliasChoice returns the complete persisted actress identity selected
// for one movie. Missing/stale actress translations are resolved immediately
// with the current global translation settings.
func resolveAliasChoice(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req aliasChoiceRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.ActressID == 0 {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "a positive actress_id is required"})
			return
		}
		deps := rt.Deps()
		actress, err := deps.Repos.ActressRepo.FindByID(c.Request.Context(), req.ActressID)
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "actress not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		cfg := deps.CoreDeps.GetConfig().Metadata.Translation
		translationRepo := deps.Repos.ActressTranslationRepo
		if translationRepo == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "actress translation repository is unavailable"})
			return
		}
		stored, lookupErr := translationRepo.FindAllByActress(c.Request.Context(), actress.ID)
		if lookupErr != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: lookupErr.Error()})
			return
		}
		settingsHash := cfg.SettingsHash()
		service := translation.New(cfg)
		targetLanguages := service.TargetLanguages()
		primaryLanguage := ""
		if len(targetLanguages) > 0 {
			primaryLanguage = targetLanguages[0]
		}
		var current *models.ActressTranslation
		for i := range stored {
			if strings.EqualFold(stored[i].Language, primaryLanguage) && stored[i].SettingsHash == settingsHash {
				current = &stored[i]
				break
			}
		}
		if current != nil {
			if current.FirstName != "" || current.LastName != "" {
				actress.FirstName = current.FirstName
				actress.LastName = current.LastName
			}
			actress.Translations = stored
			c.JSON(http.StatusOK, actress)
			return
		}

		if !cfg.Enabled || !cfg.Fields.Actresses {
			actress.Translations = stored
			c.JSON(http.StatusOK, actress)
			return
		}

		translated, records, warning, translateErr := service.TranslateActresses(c.Request.Context(), []models.Actress{*actress}, settingsHash)
		if translateErr != nil || len(translated) != 1 {
			message := strings.TrimSpace(warning)
			if message == "" && translateErr != nil {
				message = translateErr.Error()
			}
			if message == "" {
				message = "actress translation returned no result"
			}
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: message})
			return
		}
		translated[0].ID = actress.ID
		translated[0].DMMID = actress.DMMID
		translated[0].JapaneseName = actress.JapaneseName
		translated[0].ThumbURL = actress.ThumbURL
		translated[0].Aliases = actress.Aliases
		if err := deps.Repos.ActressRepo.Update(c.Request.Context(), &translated[0]); err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		for _, record := range records {
			if len(record.Actresses) == 0 || strings.TrimSpace(record.Actresses[0]) == "" {
				continue
			}
			firstName, lastName := models.SplitActressName(record.Actresses[0])
			if err := translationRepo.Upsert(c.Request.Context(), &models.ActressTranslation{
				ActressID: actress.ID, Language: record.Language,
				FirstName: firstName, LastName: lastName, JapaneseName: actress.JapaneseName,
				DisplayName: record.Actresses[0], SourceName: record.SourceName, SettingsHash: settingsHash,
			}); err != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
				return
			}
		}
		stored, _ = translationRepo.FindAllByActress(c.Request.Context(), actress.ID)
		translated[0].Translations = stored
		c.JSON(http.StatusOK, translated[0])
	}
}
