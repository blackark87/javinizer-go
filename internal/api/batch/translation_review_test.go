package batch

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewBatchMovieTranslation_Title(t *testing.T) {
	var requestBody string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<quality_review_title>>>\n교정 제목"}}]}`))
	}))
	defer llm.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.NFO.Format.DisplayTitle = "[<ID>] <TITLE>"
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "openai-compatible"
	cfg.Metadata.Translation.TargetLanguage = "ko"
	cfg.Metadata.Translation.TimeoutSeconds = 10
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = llm.URL
	cfg.Metadata.Translation.OpenAICompatible.Model = "test-model"
	deps := createTestDeps(t, cfg, "")
	filePath := "/media/IPX-535.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		ResultID:      "review-result",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:            "IPX-535",
			ContentID:     "ipx00535",
			Title:         "기존 제목",
			OriginalTitle: "日本語原題",
			DisplayTitle:  "기존 제목",
			Translations:  []models.MovieTranslation{{Language: "ko", Title: "기존 제목"}},
		},
		StartedAt: time.Now(),
	})
	job.ResultsWriter().SetProvenance(filePath, &worker.ProvenanceData{
		FieldSources: map[string]string{"title": "dmm"},
		ScraperResults: []*models.ScraperResult{{
			Source: "dmm",
			Title:  "日本語原題",
		}},
	})
	job.Controller().SetJobStatus(models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/translation-review", reviewBatchMovieTranslation(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.TranslationReviewRequest{Field: "title"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/review-result/translation-review", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response contracts.TranslationReviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotNil(t, response.Movie)
	assert.Equal(t, "교정 제목", response.Movie.Title)
	assert.Equal(t, "[IPX-535] 교정 제목", response.Movie.DisplayTitle)
	assert.Contains(t, requestBody, "mandatory second-pass quality reviewer")
	assert.Contains(t, requestBody, "日本語原題")
	assert.Contains(t, requestBody, "기존 제목")
}

func TestRetainedJapaneseField_UsesSelectedSourceAndTitleFallback(t *testing.T) {
	prov := &worker.ProvenanceData{
		FieldSources: map[string]string{"description": "dmm"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Description: "다른 설명"},
			{Source: "DMM", Description: "選択した説明"},
		},
	}
	assert.Equal(t, "選択した説明", retainedJapaneseField(prov, nil, "description"))
	assert.Equal(t, "原題", retainedJapaneseField(nil, &models.Movie{OriginalTitle: "原題"}, "title"))
	assert.Empty(t, retainedJapaneseField(nil, &models.Movie{}, "description"))
}

func TestReviewBatchMovieTranslation_RejectsUnsupportedField(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/translation-review", reviewBatchMovieTranslation(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+resultID+"/translation-review", strings.NewReader(`{"field":"maker"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
