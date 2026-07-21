package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOverrideJob(t *testing.T) (*core.APIDeps, *worker.BatchJob, string) {
	t.Helper()
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/IPX-535.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	resultID := "IPX-535"
	setJobResult(job, filePath, &worker.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", ContentID: "IPX-535", Title: "Aggregated", Maker: "AggregatedMaker"},
		StartedAt:     time.Now(),
	})
	job.ResultsWriter().SetProvenance(filePath, &worker.ProvenanceData{
		FieldSources: map[string]string{"maker": "r18dev"},
		SourceOutcomes: []*models.ScraperOutcome{
			{Source: "r18dev", Status: "success", Result: &models.ScraperResult{Source: "r18dev", Maker: "R18Maker", Title: "R18Title"}},
			{Source: "javlibrary", Status: "no_match"},
			{Source: "dmm", Status: "failed", Error: "request failed"},
		},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Maker: "R18Maker", Title: "R18Title"},
			{
				Source: "dmm", Maker: "DMMMaker", Title: "DMMTitle",
				Translations: []models.MovieTranslation{{Language: "ko", Title: "DMM 번역 제목"}},
			},
		},
	})
	return deps, job, resultID
}

func TestGetBatchMovieSources_Success(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)

	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/"+resultID+"/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp contracts.SourceResultsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Results, 2)
	assert.Equal(t, "r18dev", resp.Results[0].Source)
	assert.Equal(t, "dmm", resp.Results[1].Source)
	require.Len(t, resp.Results[1].Translations, 1)
	assert.Equal(t, "DMM 번역 제목", resp.Results[1].Translations[0].Title)
	require.Len(t, resp.Outcomes, 3)
	assert.Equal(t, "no_match", resp.Outcomes[1].Status)
	assert.Equal(t, "request failed", resp.Outcomes[2].Error)
}

func TestGetBatchMovieSources_JobNotFound(t *testing.T) {
	deps, _, _ := setupOverrideJob(t)
	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/nonexistent/results/ABC/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestGetBatchMovieSources_ResultNotFound(t *testing.T) {
	deps, job, _ := setupOverrideJob(t)
	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/NONEXISTENT/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestGetBatchMovieSources_EmptyProvenance(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/ABC-123.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &worker.MovieResult{
		ResultID:      "ABC-123",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-123"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-123"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/ABC-123/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp contracts.SourceResultsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// No in-memory ScraperResults, but the fallback synthesizes a single-source
	// result from the aggregated movie so the viewer is never empty.
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "scraper", resp.Results[0].Source)
	assert.Equal(t, "ABC-123", resp.Results[0].ID)
}

func TestOverrideBatchMovieField_Success(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code, "body: %s", w.Body.String())
	var resp contracts.FieldOverrideResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Movie)
	assert.Equal(t, "DMMMaker", resp.Movie.Maker)
	assert.Equal(t, "dmm", resp.FieldSources["maker"])
}

func TestOverrideBatchMovieField_UsesRetainedTranslation(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	filePath := "/path/to/IPX-535.mp4"
	job.ResultsWriter().SetProvenance(filePath, &worker.ProvenanceData{
		FieldSources: map[string]string{"title": "r18dev", "maker": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Title: "R18 raw title"},
			{
				Source: "dmm", Title: "DMM raw title",
				Translations: []models.MovieTranslation{{Language: "ko", Title: "DMM translated title"}},
			},
		},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.FieldOverrideRequest{Field: "title", Source: "dmm"})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code, w.Body.String())
	var response contracts.FieldOverrideResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotNil(t, response.Movie)
	assert.Equal(t, "DMM translated title", response.Movie.Title)
	assert.Equal(t, "AggregatedMaker", response.Movie.Maker)
	assert.Equal(t, "dmm", response.FieldSources["title"])
}

func TestOverrideBatchMovieField_RehydratesDisplayTitleConfigBeforeEditingStoredJob(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.NFO.Format.DisplayTitle = "[<ID>] <TITLE>"
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/IPX-535.mp4"

	// Create the job directly in the store to mirror a DB-restored job whose
	// runtime-only BatchJobConfig has not been hydrated by the lazy factory yet.
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	resultID := "stored-result"
	setJobResult(job, filePath, &worker.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:           "IPX-535",
			ContentID:    "ipx00535",
			Title:        "Aggregated title",
			DisplayTitle: "Aggregated title",
		},
		StartedAt: time.Now(),
	})
	job.ResultsWriter().SetProvenance(filePath, &worker.ProvenanceData{
		ScraperResults: []*models.ScraperResult{{
			Source: "dmm",
			Title:  "DMM raw title",
			Translations: []models.MovieTranslation{{
				Language: "ko",
				Title:    "DMM translated title",
			}},
		}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.FieldOverrideRequest{Field: "title", Source: "dmm"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code, w.Body.String())
	var response contracts.FieldOverrideResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.NotNil(t, response.Movie)
	assert.Equal(t, "DMM translated title", response.Movie.Title)
	assert.Equal(t, "[IPX-535] DMM translated title", response.Movie.DisplayTitle)
}

func TestOverrideBatchMovieField_BadJSON(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

func TestOverrideBatchMovieField_JobNotFound(t *testing.T) {
	deps, _, _ := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/nonexistent/results/ABC/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestOverrideBatchMovieField_ResultNotFound(t *testing.T) {
	deps, job, _ := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/NONEXISTENT/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestOverrideBatchMovieField_UnknownSource(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "nonexistent"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

func TestOverrideBatchMovieField_UnsupportedField(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "bogus_field", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}
