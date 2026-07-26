package movie

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/translationreview"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// reviewMovieTranslation godoc
// @Summary Retranslate one stored movie field with the configured LLM
// @Description Creates a fresh Korean translation from retained Japanese metadata, runs the second-pass JAV quality reviewer, and persists only the reviewed field.
// @Tags movies
// @Accept json
// @Produce json
// @Param id path string true "Movie ID or content ID"
// @Param request body contracts.TranslationReviewRequest true "Field to review"
// @Success 200 {object} contracts.TranslationReviewResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 502 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/movies/{id}/translation-review [post]
func reviewMovieTranslation(deps MovieDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.TranslationReviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		if deps.TranslationConfigFn == nil {
			c.JSON(http.StatusServiceUnavailable, contracts.ErrorResponse{Error: "translation configuration is unavailable"})
			return
		}

		movie, err := deps.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil {
			if database.IsNotFound(err) {
				c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Movie not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to retrieve movie"})
			return
		}

		source := retainedMovieJapaneseField(movie, req.Field)
		if source == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{
				Error: "retained Japanese source for " + req.Field + " is unavailable",
			})
			return
		}
		tc := deps.TranslationConfigFn()
		provider := strings.ToLower(strings.TrimSpace(tc.Provider))
		if !tc.Enabled {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "metadata translation is disabled"})
			return
		}
		if provider != "openai" && provider != "openai-compatible" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{
				Error: "translation review requires an OpenAI chat provider, got " + provider,
			})
			return
		}

		reviewed, err := translationreview.ReviewField(
			c.Request.Context(),
			tc,
			req.Field,
			source,
			movie.Actresses,
		)
		if err != nil {
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		current := movie.Description
		if req.Field == "title" {
			current = movie.Title
		}
		if strings.TrimSpace(current) == reviewed.Value {
			c.JSON(http.StatusOK, contracts.TranslationReviewResponse{
				Movie: contracts.MovieViewFromModel(movie), Changed: false,
			})
			return
		}

		oldTitle := movie.Title
		if req.Field == "title" {
			movie.Title = reviewed.Value
			refreshStoredDisplayTitle(movie, oldTitle)
		} else {
			movie.Description = reviewed.Value
		}
		updateStoredMovieTranslation(
			movie,
			reviewed.TargetLanguage,
			req.Field,
			reviewed.Value,
			reviewed.SettingsHash,
		)
		saved, err := deps.UpdateMetadata(c.Request.Context(), c.Param("id"), movie)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "Failed to persist reviewed translation"})
			return
		}
		c.JSON(http.StatusOK, contracts.TranslationReviewResponse{
			Movie: contracts.MovieViewFromModel(saved), Changed: true,
		})
	}
}

func retainedMovieJapaneseField(movie *models.Movie, field string) string {
	if movie == nil {
		return ""
	}
	for _, translation := range movie.Translations {
		if !strings.EqualFold(strings.TrimSpace(translation.Language), "ja") {
			continue
		}
		if field == "description" {
			return strings.TrimSpace(translation.Description)
		}
		if title := strings.TrimSpace(translation.Title); title != "" {
			return title
		}
		if title := strings.TrimSpace(translation.OriginalTitle); title != "" {
			return title
		}
	}
	if field == "title" {
		return strings.TrimSpace(movie.OriginalTitle)
	}
	return ""
}

func refreshStoredDisplayTitle(movie *models.Movie, oldTitle string) {
	if movie == nil {
		return
	}
	if oldTitle != "" && strings.HasSuffix(movie.DisplayTitle, oldTitle) {
		movie.DisplayTitle = strings.TrimSuffix(movie.DisplayTitle, oldTitle) + movie.Title
		return
	}
	movie.DisplayTitle = movie.Title
}

func updateStoredMovieTranslation(
	movie *models.Movie,
	language string,
	field string,
	value string,
	settingsHash string,
) {
	language = strings.ToLower(strings.TrimSpace(language))
	recordIndex := -1
	for i := range movie.Translations {
		if strings.EqualFold(strings.TrimSpace(movie.Translations[i].Language), language) {
			recordIndex = i
			break
		}
	}
	if recordIndex < 0 {
		movie.Translations = append(movie.Translations, models.MovieTranslation{
			MovieID:  movie.ContentID,
			Language: language,
		})
		recordIndex = len(movie.Translations) - 1
	}
	record := &movie.Translations[recordIndex]
	record.MovieID = movie.ContentID
	record.Language = language
	record.SettingsHash = settingsHash
	record.SourceName = "translation-review"
	if field == "title" {
		record.Title = value
	} else {
		record.Description = value
	}
}
