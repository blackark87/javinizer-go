package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActressSyncManagerTest(t *testing.T, cfg *config.Config, registry *models.ScraperRegistry) (*ActressSyncManager, *database.DB, *database.ActressRepository, *database.MovieRepository) {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.Database = config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "manager.db")}
	if cfg.Performance.MaxWorkers == 0 {
		cfg.Performance.MaxWorkers = 5
	}
	if cfg.Scrapers.RequestTimeoutSeconds == 0 {
		cfg.Scrapers.RequestTimeoutSeconds = 5
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate())
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo,
		GetConfig: func() *config.Config { return cfg }, GetRegistry: func() *models.ScraperRegistry { return registry },
	})
	t.Cleanup(func() {
		manager.Stop()
		_ = db.Close()
	})
	return manager, db, actressRepo, movieRepo
}

func waitForActressSyncJob(t *testing.T, manager *ActressSyncManager, jobID string) *models.ActressSyncJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := manager.GetJob(jobID)
		require.NoError(t, err)
		if job.Status == models.ActressSyncJobCompleted || job.Status == models.ActressSyncJobCancelled {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("actress sync job %s did not finish", jobID)
	return nil
}

func TestActressSyncManagerUsesFiveGeneralWorkers(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 5)
	release := make(chan struct{})
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.identityFn = func(ctx context.Context, query models.ActressIdentityQuery) (*models.ScraperResult, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		name := query.Names[0]
		return &models.ScraperResult{ID: name, Actresses: []models.ActressInfo{{DMMID: 1000 + int(current), JapaneseName: name}}}, nil
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	cfg := &config.Config{Performance: config.PerformanceConfig{MaxWorkers: 5}}
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	manager, _, actressRepo, _ := newActressSyncManagerTest(t, cfg, registry)

	ids := make([]uint, 0, 5)
	for index := 0; index < 5; index++ {
		actress := &models.Actress{JapaneseName: fmt.Sprintf("女優%d", index), ThumbURL: "existing.jpg"}
		require.NoError(t, actressRepo.Create(actress))
		ids = append(ids, actress.ID)
	}
	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: ids})
	require.NoError(t, err)
	for index := 0; index < 5; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("five actress tasks did not enter the resolver concurrently")
		}
	}
	assert.EqualValues(t, 5, maximum.Load())
	close(release)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 5, completed.Completed)
}

func TestActressSyncManagerCancelStopsPendingAfterRunningItems(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var sequence atomic.Int32
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.identityFn = func(ctx context.Context, query models.ActressIdentityQuery) (*models.ScraperResult, error) {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		name := query.Names[0]
		return &models.ScraperResult{ID: name, Actresses: []models.ActressInfo{{DMMID: 2000 + int(sequence.Add(1)), JapaneseName: name}}}, nil
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	cfg := &config.Config{Performance: config.PerformanceConfig{MaxWorkers: 2}}
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	manager, _, actressRepo, _ := newActressSyncManagerTest(t, cfg, registry)

	ids := make([]uint, 0, 4)
	for index := 0; index < 4; index++ {
		actress := &models.Actress{JapaneseName: fmt.Sprintf("取消女優%d", index), ThumbURL: "existing.jpg"}
		require.NoError(t, actressRepo.Create(actress))
		ids = append(ids, actress.ID)
	}
	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: ids})
	require.NoError(t, err)
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("running tasks did not start")
		}
	}
	require.NoError(t, manager.CancelJob(job.ID))
	close(release)
	cancelled := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, models.ActressSyncJobCancelled, cancelled.Status)
	assert.Equal(t, 2, cancelled.Updated)
	assert.Equal(t, 2, cancelled.Cancelled)
	resolver.mu.Lock()
	queryCount := len(resolver.identityQueries)
	resolver.mu.Unlock()
	assert.Equal(t, 2, queryCount, "cancelled pending tasks must never be claimed")
}

func TestActressSyncManagerUnknownMoviesAreIsolatedAndReuseExistingActress(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		if id != "OK-001" {
			return nil, errors.New("lookup failed")
		}
		return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{{DMMID: 101, JapaneseName: "確認女優"}}}, nil
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	unknown := &models.Actress{FirstName: models.UnknownActressName, JapaneseName: models.UnknownActressName}
	require.NoError(t, actressRepo.Create(unknown))
	existing := &models.Actress{DMMID: 101, JapaneseName: "確認女優", FirstName: "Kakunin", LastName: "Joyu"}
	require.NoError(t, actressRepo.Create(existing))
	require.NoError(t, movieRepo.Create(&models.Movie{ContentID: "ok001", ID: "OK-001", Actresses: []models.Actress{*unknown}}))
	for index := 2; index <= 6; index++ {
		require.NoError(t, movieRepo.Create(&models.Movie{
			ContentID: fmt.Sprintf("fail%03d", index), ID: fmt.Sprintf("FAIL-%03d", index), Actresses: []models.Actress{*unknown},
		}))
	}

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{unknown.ID}})
	require.NoError(t, err)
	assert.Equal(t, 6, job.TotalTasks, "Unknown sync must expand every linked movie without the five-movie fallback limit")
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	assert.Equal(t, 5, completed.Failed)

	successMovie, err := movieRepo.FindByContentID("ok001")
	require.NoError(t, err)
	require.Len(t, successMovie.Actresses, 1)
	assert.Equal(t, existing.ID, successMovie.Actresses[0].ID, "verified actress must reuse the existing DMM row")

	failedMovie, err := movieRepo.FindByContentID("fail002")
	require.NoError(t, err)
	require.Len(t, failedMovie.Actresses, 1)
	assert.Equal(t, unknown.ID, failedMovie.Actresses[0].ID, "failed movie must preserve its Unknown mapping")
	_, err = actressRepo.FindByID(unknown.ID)
	require.NoError(t, err, "shared Unknown row must remain while another movie still uses it")

	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	assert.Len(t, tasks, 6)
	assert.Contains(t, resolver.resolveQueries, "OK-001")
	assert.Contains(t, resolver.resolveQueries, "FAIL-002")
}

func TestActressSyncManagerReusesMatchingDMMOwnerAndBackfillsIt(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityResult: &models.ScraperResult{
		ID: "同一女優", Actresses: []models.ActressInfo{{DMMID: 501, JapaneseName: "同一女優", ThumbURL: "https://example.com/501.jpg"}},
	}}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	cfg := &config.Config{}
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, cfg, registry)

	target := &models.Actress{JapaneseName: "同一女優"}
	owner := &models.Actress{DMMID: 501, JapaneseName: "同一女優"}
	require.NoError(t, actressRepo.Create(target))
	require.NoError(t, actressRepo.Create(owner))
	require.NoError(t, movieRepo.Create(&models.Movie{ContentID: "same001", ID: "SAME-001", Actresses: []models.Actress{*target}}))

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{target.ID}})
	require.NoError(t, err)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	movie, err := movieRepo.FindByContentID("same001")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, owner.ID, movie.Actresses[0].ID)
	updatedOwner, err := actressRepo.FindByID(owner.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/501.jpg", updatedOwner.ThumbURL)
}

func TestActressSyncManagerTranslationLimiterDefaultsToThreeAndReloadsConfig(t *testing.T) {
	cfg := &config.Config{}
	manager := NewActressSyncManager(ActressSyncManagerDeps{GetConfig: func() *config.Config { return cfg }})
	for index := 0; index < 3; index++ {
		require.NoError(t, manager.acquireLLM(context.Background()))
	}
	assert.Equal(t, 3, manager.llmActive)

	acquired := make(chan struct{})
	go func() {
		_ = manager.acquireLLM(context.Background())
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("fourth provider call bypassed the default concurrency limit")
	case <-time.After(75 * time.Millisecond):
	}
	manager.releaseLLM()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("blocked provider call did not resume after a slot was released")
	}
	for manager.llmActive > 0 {
		manager.releaseLLM()
	}

	cfg.Metadata.Translation.MaxConcurrency = 2
	require.NoError(t, manager.acquireLLM(context.Background()))
	require.NoError(t, manager.acquireLLM(context.Background()))
	assert.Equal(t, 2, manager.llmActive)
	manager.releaseLLM()
	manager.releaseLLM()
}

func TestPreserveResolvedActressTranslationsNeverReturnsUnknownOrDowngradesHangul(t *testing.T) {
	original := []models.Actress{{ID: 1, FirstName: "마유키", LastName: "이토", JapaneseName: "伊藤舞雪"}}
	translated := []models.Actress{{ID: 1, FirstName: models.UnknownActressName, JapaneseName: models.UnknownActressName}}
	records := []models.MovieTranslation{{Language: "ko", Actresses: []string{models.UnknownActressName}}}
	translated, records = preserveResolvedActressTranslations(original, translated, records)
	assert.Equal(t, "마유키", translated[0].FirstName)
	assert.Equal(t, "이토", translated[0].LastName)
	assert.Equal(t, "伊藤舞雪", translated[0].JapaneseName)
	assert.Equal(t, "이토 마유키", records[0].Actresses[0])
}

func TestActressSyncManagerConcurrentLimiterDoesNotLeak(t *testing.T) {
	// Keep race builds honest: all limiter mutations are protected even while the
	// configured maximum is being read by concurrent provider requests.
	cfg := &config.Config{}
	manager := NewActressSyncManager(ActressSyncManagerDeps{GetConfig: func() *config.Config { return cfg }})
	var wg sync.WaitGroup
	for index := 0; index < 20; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, manager.acquireLLM(context.Background()))
			manager.releaseLLM()
		}()
	}
	wg.Wait()
	assert.Zero(t, manager.llmActive)
}
