package batch

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// reviewBatchMovieTranslation godoc
// @Summary Retranslate one movie field with the configured LLM
// @Description Creates a fresh Korean translation from the retained Japanese scraper source, runs it through the second-pass JAV quality reviewer, then persists only the reviewed field. Available before organization.
// @Tags web
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Param resultId path string true "Result ID"
// @Param request body contracts.TranslationReviewRequest true "Field to review"
// @Success 200 {object} contracts.TranslationReviewResponse
// @Failure 400 {object} contracts.ErrorResponse
// @Failure 404 {object} contracts.ErrorResponse
// @Failure 409 {object} contracts.ErrorResponse
// @Failure 502 {object} contracts.ErrorResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/batch/{id}/results/{resultId}/translation-review [post]
func reviewBatchMovieTranslation(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req contracts.TranslationReviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		jobID := c.Param("id")
		resultID := c.Param("resultId")
		snap := rt.Snapshot()
		if snap.BatchJobFactory() == nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "batch job runtime unavailable"})
			return
		}
		job, ok := rt.Deps().GetJobStore().GetBatchJob(jobID)
		if !ok {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: "Job not found"})
			return
		}
		status := job.GetStatus().Status
		if status != models.JobStatusCompleted {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "translation review is only available before organization"})
			return
		}

		result, filePath, found := job.GetFileResultByResultID(resultID)
		if !found || result == nil || result.Movie == nil {
			c.JSON(http.StatusNotFound, contracts.ErrorResponse{Error: fmt.Sprintf("Result %s not found in job", resultID)})
			return
		}

		tc := snap.APIConfig().TranslationConfig
		provider := strings.ToLower(strings.TrimSpace(tc.Provider))
		if !tc.Enabled {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: "metadata translation is disabled"})
			return
		}
		if provider != "openai" && provider != "openai-compatible" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("translation review requires an OpenAI chat provider, got %s", provider)})
			return
		}

		candidate := reviewedFieldValue(result.Movie, req.Field)
		source := retainedJapaneseField(job.GetProvenance(filePath), result.Movie, req.Field)
		if strings.TrimSpace(source) == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("retained Japanese source for %s is unavailable", req.Field)})
			return
		}
		if strings.TrimSpace(candidate) == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("current Korean %s is empty", req.Field)})
			return
		}

		freshConfig := translationReviewConfig(tc, req.Field)
		service := translation.New(freshConfig)
		freshMovie := &models.Movie{Actresses: result.Movie.Actresses}
		if req.Field == "title" {
			freshMovie.Title = source
		} else {
			freshMovie.Description = source
		}
		freshCtx, freshCancel := translationReviewContext(c.Request.Context(), tc.TimeoutSeconds)
		freshOutput, _, err := service.TranslateMovie(freshCtx, freshMovie, freshConfig.SettingsHash())
		freshCancel()
		if err != nil {
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: fmt.Sprintf("fresh translation failed: %v", err)})
			return
		}
		freshCandidate := translatedReviewFieldValue(freshOutput, req.Field)
		if strings.TrimSpace(freshCandidate) == "" {
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: "fresh translator returned an empty result"})
			return
		}

		reviewCtx, reviewCancel := translationReviewContext(c.Request.Context(), tc.TimeoutSeconds)
		reviewed, err := service.ReviewJAVTranslations(reviewCtx, []translation.QualityReviewField{{
			FieldName: "quality_review_" + req.Field,
			Source:    source,
			Candidate: freshCandidate,
			Actresses: result.Movie.Actresses,
		}})
		reviewCancel()
		if err != nil {
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: fmt.Sprintf("translation review failed: %v", err)})
			return
		}
		if len(reviewed) != 1 || strings.TrimSpace(reviewed[0]) == "" {
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: "translation reviewer returned an empty result"})
			return
		}

		changed := strings.TrimSpace(reviewed[0]) != strings.TrimSpace(candidate)
		if !changed {
			c.JSON(http.StatusOK, contracts.TranslationReviewResponse{
				Movie:   contracts.MovieViewFromModel(result.Movie),
				Changed: false,
			})
			return
		}

		updated, err := job.ApplyTranslationReview(c.Request.Context(), resultID, req.Field, reviewed[0], freshConfig.TargetLanguage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		rt.Deps().GetJobStore().PersistJobByID(jobID)
		c.JSON(http.StatusOK, contracts.TranslationReviewResponse{Movie: contracts.MovieViewFromModel(updated.Movie), Changed: true})
	}
}

func translationReviewConfig(tc config.TranslationConfig, field string) config.TranslationConfig {
	tc.ApplyToPrimary = false
	tc.Fields = config.TranslationFieldsConfig{
		Title:       field == "title",
		Description: field == "description",
	}
	if len(tc.TargetLanguages) > 0 {
		tc.TargetLanguage = tc.TargetLanguages[0]
		tc.TargetLanguages = []string{tc.TargetLanguage}
	}
	return tc
}

func translationReviewContext(parent context.Context, timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}
	return context.WithTimeout(parent, time.Duration(timeoutSeconds)*time.Second)
}

func translatedReviewFieldValue(output *translation.TranslationOutput, field string) string {
	if output == nil || output.Movie == nil {
		return ""
	}
	if field == "title" {
		return output.Movie.Title
	}
	if field == "description" {
		return output.Movie.Description
	}
	return ""
}

func reviewedFieldValue(movie *models.Movie, field string) string {
	if movie == nil {
		return ""
	}
	if field == "title" {
		return movie.Title
	}
	if field == "description" {
		return movie.Description
	}
	return ""
}

func retainedJapaneseField(prov *worker.ProvenanceData, movie *models.Movie, field string) string {
	if prov != nil {
		selectedSource := strings.TrimSpace(prov.FieldSources[field])
		for _, source := range prov.ScraperResults {
			if source == nil || !strings.EqualFold(strings.TrimSpace(source.Source), selectedSource) {
				continue
			}
			if field == "title" {
				if strings.TrimSpace(source.Title) != "" {
					return source.Title
				}
				return source.OriginalTitle
			}
			if field == "description" {
				return source.Description
			}
		}
	}
	if field == "title" && movie != nil {
		return movie.OriginalTitle
	}
	return ""
}
