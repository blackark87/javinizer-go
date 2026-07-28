package batch

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/translationreview"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
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
		if isBatchRetranslationRunning(jobID) {
			c.JSON(http.StatusConflict, contracts.ErrorResponse{Error: "job retranslation is already running"})
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

		if worker.IsTranslationFailure(result) {
			reviewTranslationFailure(c, rt, jobID, resultID, filePath, job, result, tc)
			return
		}

		candidate := reviewedFieldValue(result.Movie, req.Field)
		source := retainedJapaneseField(job.GetProvenance(filePath), result.Movie, req.Field)
		if strings.TrimSpace(source) == "" {
			c.JSON(http.StatusBadRequest, contracts.ErrorResponse{Error: fmt.Sprintf("retained Japanese source for %s is unavailable", req.Field)})
			return
		}
		reviewed, err := translationreview.ReviewField(
			c.Request.Context(),
			tc,
			req.Field,
			source,
			result.Movie.Actresses,
		)
		if err != nil {
			c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: err.Error()})
			return
		}

		changed := strings.TrimSpace(reviewed.Value) != strings.TrimSpace(candidate)
		if !changed {
			c.JSON(http.StatusOK, contracts.TranslationReviewResponse{
				Movie:   contracts.MovieViewFromModel(result.Movie),
				Changed: false,
			})
			return
		}

		updated, err := job.ApplyTranslationReview(
			c.Request.Context(),
			resultID,
			req.Field,
			reviewed.Value,
			reviewed.TargetLanguage,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: err.Error()})
			return
		}
		rt.Deps().GetJobStore().PersistJobByID(jobID)
		c.JSON(http.StatusOK, contracts.TranslationReviewResponse{Movie: contracts.MovieViewFromModel(updated.Movie), Changed: true})
	}
}

func reviewTranslationFailure(
	c *gin.Context,
	rt *core.APIRuntime,
	jobID string,
	resultID string,
	filePath string,
	job worker.BatchJobInterface,
	result *worker.MovieResult,
	tc config.TranslationConfig,
) {
	titleSource := retainedJapaneseField(job.GetProvenance(filePath), result.Movie, "title")
	descriptionSource := retainedJapaneseField(job.GetProvenance(filePath), result.Movie, "description")
	if strings.TrimSpace(descriptionSource) == "" {
		descriptionSource = result.Movie.Description
	}
	reviewed, err := translationreview.ReviewMovie(
		c.Request.Context(),
		tc,
		titleSource,
		descriptionSource,
		result.Movie.Actresses,
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, contracts.ErrorResponse{Error: err.Error()})
		return
	}

	changed := strings.TrimSpace(result.Movie.Title) != strings.TrimSpace(reviewed.Title.Value)
	targets := job.GetMovieResultsForMovieID(result.FileMatchInfo.MovieID)
	if len(targets) == 0 {
		targets = []*worker.MovieResult{result}
	}
	var updated *worker.MovieResult
	for _, target := range targets {
		if target == nil || target.Movie == nil {
			continue
		}
		if target.Status != models.JobStatusCompleted && !worker.IsTranslationFailure(target) {
			continue
		}
		targetUpdated, applyErr := job.ApplyTranslationReview(
			c.Request.Context(),
			target.ResultID,
			"title",
			reviewed.Title.Value,
			reviewed.Title.TargetLanguage,
		)
		if applyErr != nil {
			c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: applyErr.Error()})
			return
		}
		if reviewed.Description != nil {
			if strings.TrimSpace(result.Movie.Description) != strings.TrimSpace(reviewed.Description.Value) {
				changed = true
			}
			targetUpdated, applyErr = job.ApplyTranslationReview(
				c.Request.Context(),
				target.ResultID,
				"description",
				reviewed.Description.Value,
				reviewed.Description.TargetLanguage,
			)
			if applyErr != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: applyErr.Error()})
				return
			}
		}
		if worker.IsTranslationFailure(target) {
			targetUpdated, applyErr = job.ResolveTranslationFailure(target.ResultID)
			if applyErr != nil {
				c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: applyErr.Error()})
				return
			}
		}
		if target.ResultID == resultID {
			updated = targetUpdated
		}
	}
	if updated == nil {
		c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "reviewed result was not updated"})
		return
	}
	rt.Deps().GetJobStore().PersistJobByID(jobID)
	c.JSON(http.StatusOK, contracts.TranslationReviewResponse{
		Movie:     contracts.MovieViewFromModel(updated.Movie),
		Changed:   changed,
		Recovered: true,
	})
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
