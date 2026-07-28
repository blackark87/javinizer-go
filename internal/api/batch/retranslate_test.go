package batch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestRetranslateBatchJob_GroupsMultipartAndResolvesTranslationFailure(t *testing.T) {
	var requestMu sync.Mutex
	var requestBodies []string
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var blockFirstRequest sync.Once
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestBody := string(body)
		requestMu.Lock()
		requestBodies = append(requestBodies, requestBody)
		requestMu.Unlock()
		blockFirstRequest.Do(func() {
			close(requestStarted)
			<-releaseRequest
		})
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(requestBody, "mandatory second-pass quality reviewer") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<quality_review_title>>>\n교정 제목\n<<<quality_review_description>>>\n교정 설명\n<<<JZ_DONE>>>"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<title>>>\n새 제목\n<<<description>>>\n새 설명\n<<<JZ_DONE>>>"}}]}`))
	}))
	defer llm.Close()

	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.Provider = "openai-compatible"
	cfg.Metadata.Translation.TargetLanguage = "ko"
	cfg.Metadata.Translation.TimeoutSeconds = 10
	cfg.Metadata.Translation.MaxConcurrency = 2
	cfg.Metadata.Translation.OpenAICompatible.BaseURL = llm.URL
	cfg.Metadata.Translation.OpenAICompatible.Model = "test-model"
	deps := createTestDeps(t, cfg, "")

	files := []string{"/media/IPX-535-CD1.mp4", "/media/IPX-535-CD2.mp4"}
	job := deps.JobStore.CreateJobBatch(files)
	for index, filePath := range files {
		status := models.JobStatusCompleted
		errMessage := ""
		if index == 1 {
			status = models.JobStatusFailed
			errMessage = "translation stage failed: invalid description"
		}
		setJobResult(job, filePath, &worker.MovieResult{
			ResultID: "result-" + string(rune('1'+index)),
			FileMatchInfo: models.FileMatchInfo{
				Path:        filePath,
				MovieID:     "IPX-535",
				IsMultiPart: true,
				PartNumber:  index + 1,
			},
			Status: status,
			Error:  errMessage,
			Movie: &models.Movie{
				ID:            "IPX-535",
				ContentID:     "ipx00535",
				Title:         "日本語題名",
				OriginalTitle: "日本語題名",
				Description:   "日本語説明",
			},
			StartedAt: time.Now(),
		})
		job.ResultsWriter().SetProvenance(filePath, &worker.ProvenanceData{
			FieldSources: map[string]string{
				"title":       "dmm",
				"description": "dmm",
			},
			ScraperResults: []*models.ScraperResult{{
				Source:      "dmm",
				Title:       "日本語題名",
				Description: "日本語説明",
			}},
		})
	}
	job.Controller().SetJobStatus(models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/retranslate", retranslateBatchJob(testkit.GetTestRuntime(deps)))
	router.DELETE("/batch/:id", deleteBatchJob(testkit.GetTestRuntime(deps)))
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/retranslate", nil).WithContext(requestCtx)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var response contracts.BatchRetranslateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, 1, response.Total)
	assert.Equal(t, contracts.BatchRetranslateStatusRunning, response.Status)
	assert.Zero(t, response.Processed)
	assert.Zero(t, response.Succeeded)
	assert.Zero(t, response.Failed)

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("background retranslation did not reach the LLM")
	}
	runningResponse := buildBatchJobResponse(job.GetStatus())
	require.NotNil(t, runningResponse.Retranslation)
	assert.Equal(t, contracts.BatchRetranslateStatusRunning, runningResponse.Retranslation.Status)

	duplicateRequest := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/retranslate", nil)
	duplicateResponse := httptest.NewRecorder()
	router.ServeHTTP(duplicateResponse, duplicateRequest)
	assert.Equal(t, http.StatusConflict, duplicateResponse.Code)
	assert.Contains(t, duplicateResponse.Body.String(), "already running")

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/batch/"+job.GetID(), nil)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteRequest)
	assert.Equal(t, http.StatusConflict, deleteResponse.Code)
	assert.Contains(t, deleteResponse.Body.String(), "retranslation is running")

	// Cancelling the HTTP request after its 202 response must not cancel the
	// server-owned background operation.
	cancelRequest()
	close(releaseRequest)
	require.Eventually(t, func() bool {
		current := batchRetranslationSnapshot(job.GetID())
		return current != nil && current.Status == contracts.BatchRetranslateStatusCompleted
	}, 2*time.Second, 10*time.Millisecond)

	response = *batchRetranslationSnapshot(job.GetID())
	assert.Equal(t, 1, response.Processed)
	assert.Equal(t, 1, response.Succeeded)
	assert.Zero(t, response.Failed)
	status := job.GetStatus()
	assert.Equal(t, 2, status.Completed)
	assert.Zero(t, status.Failed)
	for _, result := range status.Results {
		assert.Equal(t, models.JobStatusCompleted, result.Status)
		assert.Empty(t, result.Error)
		assert.Equal(t, "교정 제목", result.Movie.Title)
		assert.Equal(t, "교정 설명", result.Movie.Description)
	}
	requestMu.Lock()
	assert.Len(t, requestBodies, 2)
	requestMu.Unlock()
}

func TestRetranslateBatchJob_RejectsOrganizedJob(t *testing.T) {
	deps := createTestDeps(t, config.DefaultConfig(nil, nil), "")
	job := deps.JobStore.CreateJobBatch([]string{"/media/IPX-535.mp4"})
	job.Controller().SetJobStatus(models.JobStatusOrganized)

	router := gin.New()
	router.POST("/batch/:id/retranslate", retranslateBatchJob(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/retranslate", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "before organization")
}
