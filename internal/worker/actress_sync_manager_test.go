package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActressSyncManagerTest(t *testing.T, cfg *config.Config, registry *scraperutil.ScraperRegistry) (*ActressSyncManager, *database.DB, *database.ActressRepository, *database.MovieRepository) {
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
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo,
		GetConfig: func() *config.Config { return cfg }, GetRegistry: func() *scraperutil.ScraperRegistry { return registry },
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
	resolver.resolveFn = func(ctx context.Context, movieID string) (*models.ScraperResult, error) {
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
		return &models.ScraperResult{ID: movieID, Actresses: []models.ActressInfo{{DMMID: 1000 + int(current), JapaneseName: "実名" + movieID}}}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	cfg := &config.Config{Performance: config.PerformanceConfig{MaxWorkers: 5}}
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, cfg, registry)

	ids := make([]uint, 0, 5)
	for index := 0; index < 5; index++ {
		actress := &models.Actress{JapaneseName: fmt.Sprintf("仮名女優%d", index)}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{
			ContentID: fmt.Sprintf("worker-%d", index), ID: fmt.Sprintf("WORKER-%03d", index), Actresses: []models.Actress{*actress},
		}))
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
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(ctx context.Context, movieID string) (*models.ScraperResult, error) {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		var index int
		_, _ = fmt.Sscanf(movieID, "CANCEL-%03d", &index)
		return &models.ScraperResult{ID: movieID, Actresses: []models.ActressInfo{{
			DMMID: 3000 + index, JapaneseName: fmt.Sprintf("取消実名%d", index), ThumbURL: "resolved.jpg",
		}}}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	cfg := &config.Config{Performance: config.PerformanceConfig{MaxWorkers: 2}}
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, cfg, registry)

	ids := make([]uint, 0, 4)
	for index := 0; index < 4; index++ {
		actress := &models.Actress{JapaneseName: fmt.Sprintf("取消仮名%d", index)}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{
			ContentID: fmt.Sprintf("cancel-%d", index), ID: fmt.Sprintf("CANCEL-%03d", index), Actresses: []models.Actress{*actress},
		}))
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
	queryCount := len(resolver.resolveQueries)
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
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	unknown := &models.Actress{FirstName: models.UnknownActressName, JapaneseName: models.UnknownActressName}
	require.NoError(t, actressRepo.Create(context.Background(), unknown))
	existing := &models.Actress{DMMID: 101, JapaneseName: "確認女優", FirstName: "Kakunin", LastName: "Joyu"}
	require.NoError(t, actressRepo.Create(context.Background(), existing))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{ContentID: "ok001", ID: "OK-001", Actresses: []models.Actress{*unknown}}))
	for index := 2; index <= 6; index++ {
		require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{
			ContentID: fmt.Sprintf("fail%03d", index), ID: fmt.Sprintf("FAIL-%03d", index), Actresses: []models.Actress{*unknown},
		}))
	}

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{unknown.ID}})
	require.NoError(t, err)
	assert.Equal(t, 6, job.TotalTasks, "Unknown sync must expand every linked movie without the five-movie fallback limit")
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	assert.Equal(t, 5, completed.Failed)

	successMovie, err := movieRepo.FindByContentID(context.Background(), "ok001")
	require.NoError(t, err)
	require.Len(t, successMovie.Actresses, 1)
	assert.Equal(t, existing.ID, successMovie.Actresses[0].ID, "verified actress must reuse the existing DMM row")

	failedMovie, err := movieRepo.FindByContentID(context.Background(), "fail002")
	require.NoError(t, err)
	require.Len(t, failedMovie.Actresses, 1)
	assert.Equal(t, unknown.ID, failedMovie.Actresses[0].ID, "failed movie must preserve its Unknown mapping")
	_, err = actressRepo.FindByID(context.Background(), unknown.ID)
	require.NoError(t, err, "shared Unknown row must remain while another movie still uses it")

	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	assert.Len(t, tasks, 6)
	assert.Contains(t, resolver.resolveQueries, "OK-001")
	assert.Contains(t, resolver.resolveQueries, "FAIL-002")
}

func TestActressSyncManagerDeduplicatesMissingDMMTasksByMovie(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{{DMMID: 801, JapaneseName: "確認女優"}}}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	first := &models.Actress{JapaneseName: "仮名一"}
	second := &models.Actress{JapaneseName: "仮名二"}
	require.NoError(t, actressRepo.Create(context.Background(), first))
	require.NoError(t, actressRepo.Create(context.Background(), second))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{
		ContentID: "dedupe001", ID: "300MIUM-921", Actresses: []models.Actress{*first, *second},
	}))

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{
		Scope: "selected", ActressIDs: []uint{first.ID, second.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, job.TotalTasks)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	resolver.mu.Lock()
	queries := append([]string(nil), resolver.resolveQueries...)
	resolver.mu.Unlock()
	assert.Equal(t, []string{"300MIUM-921"}, queries)
}

func TestActressSyncManagerFallbackCleansDecoratedNamesAndUnknownDescriptions(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		return &models.ScraperResult{ID: id}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	decorated := &models.Actress{JapaneseName: "あいり 21歳 大学3年生", ThumbURL: "decorated.jpg"}
	description := &models.Actress{JapaneseName: "欲求不満セレブ妻", ThumbURL: "untrusted.jpg"}
	require.NoError(t, actressRepo.Create(context.Background(), decorated))
	require.NoError(t, actressRepo.Create(context.Background(), description))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{ContentID: "clean001", ID: "300MIUM-834", Actresses: []models.Actress{*decorated}}))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{ContentID: "clean002", ID: "JNT-051", Actresses: []models.Actress{*description}}))

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{
		Scope: "selected", ActressIDs: []uint{decorated.ID, description.ID},
	})
	require.NoError(t, err)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 2, completed.Updated)

	cleaned, err := actressRepo.FindByID(context.Background(), decorated.ID)
	require.NoError(t, err)
	assert.Equal(t, "あいり", cleaned.JapaneseName)
	unknown, err := actressRepo.FindByID(context.Background(), description.ID)
	require.NoError(t, err)
	assert.Equal(t, models.UnknownActressName, unknown.JapaneseName)
	assert.Equal(t, models.UnknownActressName, unknown.FirstName)
	assert.Empty(t, unknown.ThumbURL)
}

func TestActressSyncManagerResolverFailureKeepsMappingsAndStoresContext(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, _ string) (*models.ScraperResult, error) {
		return nil, errors.New("upstream timeout")
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	placeholder := &models.Actress{JapaneseName: "仮名"}
	require.NoError(t, actressRepo.Create(context.Background(), placeholder))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{ContentID: "failure001", ID: "FAILURE-001", Actresses: []models.Actress{*placeholder}}))
	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{placeholder.ID}})
	require.NoError(t, err)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Failed)

	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, "resolving", tasks[0].Stage)
	assert.Contains(t, tasks[0].ErrorMessage, "FAILURE-001")
	assert.Contains(t, tasks[0].ErrorMessage, "sougouwiki")
	assert.Contains(t, tasks[0].ErrorMessage, "resolving stage")
	assert.Contains(t, tasks[0].ErrorMessage, "upstream timeout")
	movie, err := movieRepo.FindByContentID(context.Background(), "failure001")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, placeholder.ID, movie.Actresses[0].ID)
}

func TestActressSyncManagerReplacesOnlyUnverifiedMovieMappings(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{{DMMID: 902, JapaneseName: "新規確認女優"}}}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	verified := &models.Actress{DMMID: 901, JapaneseName: "既存確認女優"}
	first := &models.Actress{JapaneseName: "仮名一"}
	second := &models.Actress{JapaneseName: "仮名二"}
	require.NoError(t, actressRepo.Create(context.Background(), verified))
	require.NoError(t, actressRepo.Create(context.Background(), first))
	require.NoError(t, actressRepo.Create(context.Background(), second))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{
		ContentID: "replace001", ID: "REPLACE-001", Actresses: []models.Actress{*verified, *first, *second},
	}))

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{first.ID}})
	require.NoError(t, err)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	movie, err := movieRepo.FindByContentID(context.Background(), "replace001")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 2)
	assert.ElementsMatch(t, []int{901, 902}, []int{movie.Actresses[0].DMMID, movie.Actresses[1].DMMID})
	_, err = actressRepo.FindByID(context.Background(), first.ID)
	assert.True(t, database.IsNotFound(err))
	_, err = actressRepo.FindByID(context.Background(), second.ID)
	assert.True(t, database.IsNotFound(err))
}

func TestActressSyncManagerLLMFailureKeepsVerifiedIdentityAndMapping(t *testing.T) {
	translationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "translation unavailable", http.StatusInternalServerError)
	}))
	defer translationServer.Close()

	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{{DMMID: 9901, JapaneseName: "響蓮"}}}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	cfg := &config.Config{}
	cfg.Metadata.Translation = config.TranslationConfig{
		Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{Actresses: true},
		OpenAI: config.OpenAITranslationConfig{BaseURL: translationServer.URL, APIKey: "test"},
	}
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, cfg, registry)

	placeholder := &models.Actress{JapaneseName: "仮名"}
	require.NoError(t, actressRepo.Create(context.Background(), placeholder))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{ContentID: "llm-failure", ID: "LLM-FAIL-001", Actresses: []models.Actress{*placeholder}}))
	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{placeholder.ID}})
	require.NoError(t, err)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	assert.Equal(t, 1, completed.Warnings)
	assert.Zero(t, completed.Failed)

	movie, err := movieRepo.FindByContentID(context.Background(), "llm-failure")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, 9901, movie.Actresses[0].DMMID)
	assert.Equal(t, "響蓮", movie.Actresses[0].JapaneseName)
	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, "updated_with_warning", tasks[0].Outcome)
	assert.Contains(t, tasks[0].Warning, "translation")
}

func TestActressSyncManagerRepairsMissingDMMIDAndSkipsNormalExistingProfile(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		require.Equal(t, "MIUM-123", id)
		return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{
			{DMMID: 701, FirstName: "Ichika", LastName: "Matsumoto", JapaneseName: "松本いちか", ThumbURL: "resolver-profile.jpg"},
			{DMMID: 702, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
		}}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, &config.Config{}, registry)

	existingCanonical := &models.Actress{
		DMMID: 701, FirstName: "이치카", LastName: "마츠모토", JapaneseName: "기존 정상 일본어명",
		ThumbURL: "existing-profile.jpg", Aliases: "기존 별칭",
	}
	require.NoError(t, actressRepo.Create(context.Background(), existingCanonical))
	missingDMMID := &models.Actress{JapaneseName: "잘못 남아 있던 가명"}
	require.NoError(t, actressRepo.Create(context.Background(), missingDMMID))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{
		ContentID: "mium00123", ID: "MIUM-123", Actresses: []models.Actress{*missingDMMID},
	}))

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{
		Scope: "selected", ActressIDs: []uint{missingDMMID.ID},
	})
	require.NoError(t, err)
	require.Equal(t, 1, job.TotalTasks)
	tasks, err := manager.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, models.ActressSyncTaskKindUnknownMovie, tasks[0].Kind)

	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	updatedMovie, err := movieRepo.FindByContentID(context.Background(), "mium00123")
	require.NoError(t, err)
	require.Len(t, updatedMovie.Actresses, 2)
	assert.ElementsMatch(t, []int{701, 702}, []int{updatedMovie.Actresses[0].DMMID, updatedMovie.Actresses[1].DMMID})
	byDMMID := make(map[int]models.Actress, len(updatedMovie.Actresses))
	for _, actress := range updatedMovie.Actresses {
		byDMMID[actress.DMMID] = actress
	}
	reused := byDMMID[701]
	assert.Equal(t, existingCanonical.ID, reused.ID, "the existing DMM-ID owner must be reused")
	assert.Equal(t, existingCanonical.JapaneseName, reused.JapaneseName, "a normal existing profile must not be overwritten from SougouWiki")
	assert.Equal(t, existingCanonical.FirstName, reused.FirstName)
	assert.Equal(t, existingCanonical.LastName, reused.LastName)
	assert.Equal(t, existingCanonical.ThumbURL, reused.ThumbURL)
	assert.ElementsMatch(t, []string{"기존 별칭", "松本いちか"}, strings.Split(reused.Aliases, "|"))
	created := byDMMID[702]
	assert.Equal(t, "波多野結衣", created.JapaneseName)
	assert.Equal(t, "Yui", created.FirstName)
	assert.Equal(t, "Hatano", created.LastName)
	for _, actress := range updatedMovie.Actresses {
		assert.NotEqual(t, "잘못 남아 있던 가명", actress.JapaneseName)
	}
	_, err = actressRepo.FindByID(context.Background(), missingDMMID.ID)
	assert.True(t, database.IsNotFound(err), "the stale missing-DMMID row must be deleted after its final movie mapping is replaced")
	assert.Empty(t, resolver.identityQueries, "missing-DMMID actresses with linked movies must use movie cast resolution, not name identity lookup")
	assert.Contains(t, resolver.resolveQueries, "MIUM-123")
}

func TestActressSyncManagerDoesNotFallBackToNameResolverWithoutLinkedMovie(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityResult: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 999, JapaneseName: "직접 조회 결과"}},
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	manager, _, actressRepo, _ := newActressSyncManagerTest(t, &config.Config{}, registry)

	missingDMMID := &models.Actress{JapaneseName: "연결 작품 없는 배우"}
	require.NoError(t, actressRepo.Create(context.Background(), missingDMMID))
	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{
		Scope: "selected", ActressIDs: []uint{missingDMMID.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, models.ActressSyncJobCompleted, job.Status)
	assert.Equal(t, 1, job.Skipped)
	assert.Empty(t, resolver.identityQueries, "DMM ID가 없으면 이름 resolver로 우회하면 안 된다")
	assert.Empty(t, resolver.resolveQueries, "연결 작품 ID가 없으면 SougouWiki 작품 조회를 만들 수 없다")
}

func TestActressSyncManagerReusesMatchingDMMOwnerAndBackfillsIt(t *testing.T) {
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	resolver.resolveFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		return &models.ScraperResult{
			ID: id, Actresses: []models.ActressInfo{{DMMID: 501, JapaneseName: "同一女優", ThumbURL: "https://example.com/501.jpg"}},
		}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	cfg := &config.Config{}
	cfg.Scrapers.Priority = []string{"sougouwiki"}
	manager, _, actressRepo, movieRepo := newActressSyncManagerTest(t, cfg, registry)

	target := &models.Actress{JapaneseName: "同一女優"}
	owner := &models.Actress{DMMID: 501, JapaneseName: "同一女優"}
	require.NoError(t, actressRepo.Create(context.Background(), target))
	require.NoError(t, actressRepo.Create(context.Background(), owner))
	require.NoError(t, movieRepo.Create(context.Background(), &models.Movie{ContentID: "same001", ID: "SAME-001", Actresses: []models.Actress{*target}}))

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{target.ID}})
	require.NoError(t, err)
	completed := waitForActressSyncJob(t, manager, job.ID)
	assert.Equal(t, 1, completed.Updated)
	movie, err := movieRepo.FindByContentID(context.Background(), "same001")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, owner.ID, movie.Actresses[0].ID)
	updatedOwner, err := actressRepo.FindByID(context.Background(), owner.ID)
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
