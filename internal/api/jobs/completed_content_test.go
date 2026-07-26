package jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type completedContentRepoStub struct {
	options database.CompletedContentListOptions
	items   []database.CompletedContent
	filters []database.CompletedContentActressFilter
	total   int64
	err     error
}

func (s *completedContentRepoStub) ListCompletedContent(
	_ context.Context,
	options database.CompletedContentListOptions,
) ([]database.CompletedContent, int64, error) {
	s.options = options
	return s.items, s.total, s.err
}

func (s *completedContentRepoStub) ListCompletedContentActressFilters(
	_ context.Context,
) ([]database.CompletedContentActressFilter, error) {
	return s.filters, s.err
}

func TestListCompletedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizedAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	repo := &completedContentRepoStub{
		items: []database.CompletedContent{{
			MovieID:     "MIUM-985",
			Title:       "제목",
			Actresses:   []models.Actress{{ID: 7, JapaneseName: "白岩冬萌"}},
			Paths:       []string{"/dest/MIUM-985.mp4"},
			LatestJobID: "job-1",
			OrganizedAt: organizedAt,
		}},
		filters: []database.CompletedContentActressFilter{{
			Actress: models.Actress{ID: 7, JapaneseName: "白岩冬萌"},
			Count:   3,
		}},
		total: 1,
	}
	deps := JobDeps{CompletedContentRepo: repo}
	router := gin.New()
	router.GET("/api/v1/completed-content", listCompletedContent(deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/completed-content?q=%20MIUM%20&actress_id=7&sort=metadata_updated_at&order=asc&limit=10&offset=20", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code)
	var body contracts.CompletedContentListResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	assert.Equal(t, "MIUM", repo.options.Query)
	assert.Equal(t, uint(7), repo.options.ActressID)
	assert.Equal(t, database.CompletedContentSortMetadataUpdated, repo.options.Sort)
	assert.Equal(t, database.CompletedContentSortAscending, repo.options.Order)
	assert.Equal(t, 10, repo.options.Limit)
	assert.Equal(t, 20, repo.options.Offset)
	assert.Equal(t, int64(1), body.Total)
	require.Len(t, body.Contents, 1)
	assert.Equal(t, "MIUM-985", body.Contents[0].MovieID)
	assert.Equal(t, organizedAt.Format(time.RFC3339), body.Contents[0].OrganizedAt)
	require.Len(t, body.ActressFilters, 1)
	assert.Equal(t, int64(3), body.ActressFilters[0].Count)
}

func TestListCompletedContent_ValidatesPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := JobDeps{CompletedContentRepo: &completedContentRepoStub{}}
	router := gin.New()
	router.GET("/api/v1/completed-content", listCompletedContent(deps))

	for _, target := range []string{
		"/api/v1/completed-content?limit=0",
		"/api/v1/completed-content?limit=101",
		"/api/v1/completed-content?limit=nope",
		"/api/v1/completed-content?offset=-1",
		"/api/v1/completed-content?offset=nope",
		"/api/v1/completed-content?actress_id=0",
		"/api/v1/completed-content?actress_id=nope",
		"/api/v1/completed-content?sort=unknown",
		"/api/v1/completed-content?order=sideways",
	} {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, target, nil))
		assert.Equal(t, http.StatusBadRequest, res.Code, target)
	}
}

func TestListCompletedContent_RepositoryUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/completed-content", listCompletedContent(JobDeps{}))

	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/completed-content", nil))

	assert.Equal(t, http.StatusServiceUnavailable, res.Code)
}
