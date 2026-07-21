package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stagedTranslationWorkflow struct {
	stubWorkflow
	total              int32
	metadataCompleted  int32
	translationActive  int32
	translationPeak    int32
	translationEarly   int32
	releaseTranslation chan struct{}
}

func (w *stagedTranslationWorkflow) Scrape(_ context.Context, cmd scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	if !cmd.SkipTranslation {
		atomic.StoreInt32(&w.translationEarly, 1)
	}
	atomic.AddInt32(&w.metadataCompleted, 1)
	return makeScrapeResult(cmd.MovieID), &workflow.OrchestrationMeta{}, nil
}

func (w *stagedTranslationWorkflow) TranslateScrapeResult(_ context.Context, result *scrape.ScrapeResult, _ string) (*workflow.OrchestrationMeta, error) {
	if atomic.LoadInt32(&w.metadataCompleted) != w.total {
		atomic.StoreInt32(&w.translationEarly, 1)
	}
	active := atomic.AddInt32(&w.translationActive, 1)
	for {
		peak := atomic.LoadInt32(&w.translationPeak)
		if active <= peak || atomic.CompareAndSwapInt32(&w.translationPeak, peak, active) {
			break
		}
	}
	<-w.releaseTranslation
	atomic.AddInt32(&w.translationActive, -1)
	result.Movie.Title = "translated"
	return &workflow.OrchestrationMeta{}, nil
}

type countingCheckpointPersister struct{ count int32 }

func (p *countingCheckpointPersister) Persist() { atomic.AddInt32(&p.count, 1) }

func TestScrapePhase_StagesMetadataBeforeTranslationAndCheckpointsEveryRecord(t *testing.T) {
	const total = 4
	wf := &stagedTranslationWorkflow{total: total, releaseTranslation: make(chan struct{}, total)}
	files := []string{"A-001.mp4", "A-002.mp4", "A-003.mp4", "A-004.mp4"}
	fileInfo := make(map[string]models.FileMatchInfo, len(files))
	for _, file := range files {
		fileInfo[file] = models.FileMatchInfo{Path: file, MovieID: file[:5]}
	}
	persister := &countingCheckpointPersister{}
	repo := &serializingPersistRepo{}
	inputs := scrapePhaseInputs{
		JobID:                  "staged-job",
		Concurrency:            concurrencyConfig{MaxWorkers: total},
		WF:                     wf,
		Matcher:                &stubMatcher{},
		FileMatchInfo:          fileInfo,
		MovieRepo:              repo,
		DeferredTranslation:    true,
		TranslationConcurrency: 2,
		Broadcaster:            &stubBroadcaster{},
		Updater:                newStubUpdater(),
		Lifecycle:              &stubLifecycle{},
		persister:              persister,
	}
	translationProgress := make(chan string, total)
	cfg := ScrapePhaseConfig{
		FileMatchInfo: fileInfo,
		OnScrapeStepProgress: func(_ string, step string, pct float64, msg string) {
			if step == string(StepTranslate) {
				assert.Equal(t, 0.85, pct)
				translationProgress <- msg
			}
		},
	}

	done := make(chan struct{})
	go func() {
		(&scrapePhase{}).Run(context.Background(), inputs, files, cfg)
		close(done)
	}()

	require.Eventually(t, func() bool { return atomic.LoadInt32(&wf.translationPeak) == 2 }, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(total), atomic.LoadInt32(&wf.metadataCompleted))
	assert.Zero(t, atomic.LoadInt32(&wf.translationEarly))
	assert.Equal(t, int32(total), atomic.LoadInt32(&repo.upserts), "raw metadata must be durable before translation is released")
	running, pending := 0, 0
	for _, file := range files {
		result := inputs.Updater.(*stubUpdater).getResult(file)
		require.NotNil(t, result)
		switch result.Status {
		case models.JobStatusRunning:
			running++
		case models.JobStatusPending:
			pending++
		}
	}
	assert.Equal(t, 2, running, "only translation workers should be reported as running")
	assert.Equal(t, 2, pending, "metadata-complete files waiting for translation should be pending")
	require.Eventually(t, func() bool { return len(translationProgress) == 2 }, 3*time.Second, 10*time.Millisecond)
	for i := 0; i < 2; i++ {
		assert.Contains(t, <-translationProgress, "1-pass LLM translation in progress for")
	}

	for range files {
		wf.releaseTranslation <- struct{}{}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("staged scrape did not complete")
	}

	assert.Equal(t, int32(total*2), atomic.LoadInt32(&repo.upserts), "each record must be persisted after metadata and translation")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&persister.count), int32(total*2), "job JSON must be checkpointed after every stage record")
	for _, file := range files {
		result := inputs.Updater.(*stubUpdater).getResult(file)
		require.NotNil(t, result)
		assert.Equal(t, models.JobStatusCompleted, result.Status)
		assert.Equal(t, "translated", result.Movie.Title)
	}
}
