package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActressSyncAPITest(t *testing.T) (*gin.Engine, *core.APIDeps, *core.APIRuntime) {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	deps := testkit.CreateTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	router := gin.New()
	actressDeps := NewActressDeps(deps.Repos.ContentRepos, deps.Repos.TranslationRepos)
	RegisterRoutes(router.Group("/api/v1"), actressDeps, rt)
	return router, deps, rt
}

func performActressSyncAPIRequest(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func performActressSyncAPIJSONRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestListActressSyncCandidates(t *testing.T) {
	router, deps, _ := newActressSyncAPITest(t)
	actresses := []*models.Actress{
		{DMMID: 1, JapaneseName: "Complete", ThumbURL: "complete.jpg"},
		{DMMID: 0, JapaneseName: "Missing ID", ThumbURL: "id.jpg"},
		{DMMID: 2, JapaneseName: "Missing thumbnail"},
	}
	for _, actress := range actresses {
		require.NoError(t, deps.Repos.ActressRepo.Create(context.Background(), actress))
	}

	response := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-candidates")
	assert.Equal(t, http.StatusOK, response.Code)
	var body actressSyncCandidatesResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, []uint{actresses[1].ID, actresses[2].ID}, body.IDs)
	require.Len(t, body.Actresses, 2)
	assert.Equal(t, "Missing ID", body.Actresses[0].JapaneseName)
	assert.Equal(t, "Missing thumbnail", body.Actresses[1].JapaneseName)
	assert.Equal(t, 2, body.Total)
}

func TestListActressSyncCandidatesDatabaseError(t *testing.T) {
	router, deps, _ := newActressSyncAPITest(t)
	require.NoError(t, deps.CoreDeps.DB.Close())
	response := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-candidates")
	assert.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestActressSyncJobAPIRoutesCreateReadListAndCancel(t *testing.T) {
	router, deps, rt := newActressSyncAPITest(t)
	t.Cleanup(rt.Shutdown)
	actress := &models.Actress{DMMID: 123, JapaneseName: "Complete", ThumbURL: "complete.jpg"}
	require.NoError(t, deps.Repos.ActressRepo.Create(context.Background(), actress))

	invalid := performActressSyncAPIJSONRequest(router, http.MethodPost, "/api/v1/actresses/sync-jobs", `{}`)
	assert.Equal(t, http.StatusBadRequest, invalid.Code)
	assert.Equal(t, http.StatusNotFound, performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-jobs/missing").Code)

	created := performActressSyncAPIJSONRequest(router, http.MethodPost, "/api/v1/actresses/sync-jobs", fmt.Sprintf(`{"scope":"selected","actress_ids":[%d]}`, actress.ID))
	assert.Equal(t, http.StatusAccepted, created.Code)
	var createBody actressSyncJobResponse
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createBody))
	require.NotEmpty(t, createBody.Job.ID)

	getResponse := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-jobs/"+createBody.Job.ID)
	assert.Equal(t, http.StatusOK, getResponse.Code)
	tasksResponse := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-jobs/"+createBody.Job.ID+"/tasks")
	assert.Equal(t, http.StatusOK, tasksResponse.Code)
	var tasks actressSyncTasksResponse
	require.NoError(t, json.Unmarshal(tasksResponse.Body.Bytes(), &tasks))
	assert.Equal(t, 1, tasks.Total)

	cancelResponse := performActressSyncAPIRequest(router, http.MethodPost, "/api/v1/actresses/sync-jobs/"+createBody.Job.ID+"/cancel")
	assert.Equal(t, http.StatusOK, cancelResponse.Code)
	activeResponse := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-jobs/active")
	assert.Equal(t, http.StatusOK, activeResponse.Code)
}
