package actress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type actressSyncAPIResolver struct {
	result *models.ScraperResult
}

func (s *actressSyncAPIResolver) Name() string { return "sougouwiki" }
func (s *actressSyncAPIResolver) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *actressSyncAPIResolver) GetURL(string) (string, error) { return "", nil }
func (s *actressSyncAPIResolver) IsEnabled() bool               { return true }
func (s *actressSyncAPIResolver) Config() *config.ScraperSettings {
	return &config.ScraperSettings{Enabled: true}
}
func (s *actressSyncAPIResolver) Close() error { return nil }
func (s *actressSyncAPIResolver) ResolveActresses(context.Context, string) (*models.ScraperResult, error) {
	return s.result, nil
}

func newActressSyncAPITest(t *testing.T) (*gin.Engine, *core.ServerDependencies) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	deps := testkit.CreateTestDeps(t, cfg, "")
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), deps)
	return router, deps
}

func performActressSyncAPIRequest(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestListActressSyncCandidates(t *testing.T) {
	router, deps := newActressSyncAPITest(t)
	actresses := []*models.Actress{
		{DMMID: 1, JapaneseName: "Complete", ThumbURL: "complete.jpg"},
		{DMMID: 0, JapaneseName: "Missing ID", ThumbURL: "id.jpg"},
		{DMMID: 2, JapaneseName: "Missing thumbnail"},
	}
	for _, actress := range actresses {
		require.NoError(t, deps.ActressRepo.Create(actress))
	}

	response := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-candidates")
	assert.Equal(t, http.StatusOK, response.Code)
	var body actressSyncCandidatesResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, []uint{actresses[1].ID, actresses[2].ID}, body.IDs)
	assert.Equal(t, 2, body.Total)
}

func TestSyncActressAPIUpdatedSkippedAndConflict(t *testing.T) {
	t.Run("updated", func(t *testing.T) {
		router, deps := newActressSyncAPITest(t)
		actress := &models.Actress{JapaneseName: "Target", ThumbURL: "existing.jpg"}
		require.NoError(t, deps.ActressRepo.Create(actress))
		require.NoError(t, deps.MovieRepo.Create(&models.Movie{
			ContentID: "test001", ID: "TEST-001", ReleaseDate: timePtr(time.Now()), Actresses: []models.Actress{*actress},
		}))
		deps.Registry.Register(&actressSyncAPIResolver{result: &models.ScraperResult{
			Actresses: []models.ActressInfo{{DMMID: 101, JapaneseName: "Target"}},
		}})

		response := performActressSyncAPIRequest(router, http.MethodPost, "/api/v1/actresses/"+uintString(actress.ID)+"/sync")
		assert.Equal(t, http.StatusOK, response.Code)
		var body worker.ActressSyncResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		assert.Equal(t, worker.ActressSyncUpdated, body.Status)
		assert.Equal(t, 101, body.Actress.DMMID)
	})

	t.Run("skipped", func(t *testing.T) {
		router, deps := newActressSyncAPITest(t)
		actress := &models.Actress{DMMID: 202, JapaneseName: "Complete", ThumbURL: "complete.jpg"}
		require.NoError(t, deps.ActressRepo.Create(actress))

		response := performActressSyncAPIRequest(router, http.MethodPost, "/api/v1/actresses/"+uintString(actress.ID)+"/sync")
		assert.Equal(t, http.StatusOK, response.Code)
		var body worker.ActressSyncResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		assert.Equal(t, worker.ActressSyncSkipped, body.Status)
	})

	t.Run("conflict", func(t *testing.T) {
		router, deps := newActressSyncAPITest(t)
		target := &models.Actress{JapaneseName: "Target", ThumbURL: "target.jpg"}
		owner := &models.Actress{DMMID: 303, JapaneseName: "Owner", ThumbURL: "owner.jpg"}
		require.NoError(t, deps.ActressRepo.Create(target))
		require.NoError(t, deps.ActressRepo.Create(owner))
		require.NoError(t, deps.MovieRepo.Create(&models.Movie{
			ContentID: "test003", ID: "TEST-003", ReleaseDate: timePtr(time.Now()), Actresses: []models.Actress{*target},
		}))
		deps.Registry.Register(&actressSyncAPIResolver{result: &models.ScraperResult{
			Actresses: []models.ActressInfo{{DMMID: 303, JapaneseName: "Target"}},
		}})

		response := performActressSyncAPIRequest(router, http.MethodPost, "/api/v1/actresses/"+uintString(target.ID)+"/sync")
		assert.Equal(t, http.StatusOK, response.Code)
		var body worker.ActressSyncResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		assert.Equal(t, worker.ActressSyncConflict, body.Status)
		require.NotNil(t, body.ConflictActressID)
		assert.Equal(t, owner.ID, *body.ConflictActressID)
	})
}

func TestSyncActressAPIErrors(t *testing.T) {
	router, deps := newActressSyncAPITest(t)

	assert.Equal(t, http.StatusBadRequest,
		performActressSyncAPIRequest(router, http.MethodPost, "/api/v1/actresses/not-a-number/sync").Code)
	assert.Equal(t, http.StatusNotFound,
		performActressSyncAPIRequest(router, http.MethodPost, "/api/v1/actresses/999999/sync").Code)

	actress := &models.Actress{JapaneseName: "Canceled"}
	require.NoError(t, deps.ActressRepo.Create(actress))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/actresses/"+uintString(actress.ID)+"/sync", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusRequestTimeout, recorder.Code)
}

func TestListActressSyncCandidatesDatabaseError(t *testing.T) {
	router, deps := newActressSyncAPITest(t)
	require.NoError(t, deps.DB.Close())
	response := performActressSyncAPIRequest(router, http.MethodGet, "/api/v1/actresses/sync-candidates")
	assert.Equal(t, http.StatusInternalServerError, response.Code)
}

func timePtr(value time.Time) *time.Time { return &value }

func uintString(value uint) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = digits[value%10]
		value /= 10
	}
	return string(buf[index:])
}
