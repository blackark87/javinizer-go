package movie

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type actressBackqueueWorkflow struct {
	queueIDs []uint
}

func (w *actressBackqueueWorkflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	if cmd.QueueActressSync != nil {
		if err := cmd.QueueActressSync(ctx, w.queueIDs); err != nil {
			return nil, nil, err
		}
	}
	return &scrape.ScrapeResult{
		Movie:  &models.Movie{ID: "ALIAS-001"},
		Status: scrape.StatusCompleted,
	}, &workflow.OrchestrationMeta{}, nil
}

func (*actressBackqueueWorkflow) Apply(context.Context, workflow.ApplyCmd, scrape.ProgressFunc) (*workflow.ApplyResult, error) {
	return nil, nil
}

func (*actressBackqueueWorkflow) Preview(context.Context, workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (*actressBackqueueWorkflow) Compare(context.Context, workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (*actressBackqueueWorkflow) ScanAndMatch(context.Context, workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func TestScrapeMovieQueuesVerifiedActivityNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wf := &actressBackqueueWorkflow{queueIDs: []uint{41, 42}}
	var queued []uint
	deps := NewMovieDeps(nil,
		WithWorkflow(func() workflow.WorkflowInterface { return wf }),
		WithActressSyncEnqueuer(func(_ context.Context, ids []uint) error {
			queued = append([]uint(nil), ids...)
			return nil
		}),
	)
	router := gin.New()
	router.POST("/scrape", scrapeMovie(deps))

	req := httptest.NewRequest(http.MethodPost, "/scrape", bytes.NewBufferString(`{"id":"ALIAS-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []uint{41, 42}, queued)
}

func TestRescrapeMovieQueuesVerifiedActivityNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wf := &actressBackqueueWorkflow{queueIDs: []uint{51, 52}}
	var queued []uint
	deps := NewMovieDeps(nil,
		WithWorkflow(func() workflow.WorkflowInterface { return wf }),
		WithActressSyncEnqueuer(func(_ context.Context, ids []uint) error {
			queued = append([]uint(nil), ids...)
			return nil
		}),
	)
	router := gin.New()
	router.POST("/movies/:id/rescrape", rescrapeMovie(deps))

	req := httptest.NewRequest(http.MethodPost, "/movies/ALIAS-001/rescrape", bytes.NewBufferString(`{"selected_scrapers":["dmm"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []uint{51, 52}, queued)
}
