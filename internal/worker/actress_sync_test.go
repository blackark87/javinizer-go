package worker

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type actressSyncTestScraper struct {
	name          string
	enabled       bool
	resolveResult *models.ScraperResult
	resolveErr    error
	resolveFn     func(context.Context, string) (*models.ScraperResult, error)
	thumbnailURL  string
	thumbnailFn   func(context.Context, models.ActressInfo) string

	mu               sync.Mutex
	resolvedMovieIDs []string
	thumbnailInfos   []models.ActressInfo
}

func (s *actressSyncTestScraper) Name() string { return s.name }
func (s *actressSyncTestScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *actressSyncTestScraper) GetURL(string) (string, error) { return "", nil }
func (s *actressSyncTestScraper) IsEnabled() bool               { return s.enabled }
func (s *actressSyncTestScraper) Config() *config.ScraperSettings {
	return &config.ScraperSettings{Enabled: s.enabled}
}
func (s *actressSyncTestScraper) Close() error { return nil }
func (s *actressSyncTestScraper) ResolveActresses(ctx context.Context, id string) (*models.ScraperResult, error) {
	s.mu.Lock()
	s.resolvedMovieIDs = append(s.resolvedMovieIDs, id)
	s.mu.Unlock()
	if s.resolveFn != nil {
		return s.resolveFn(ctx, id)
	}
	return s.resolveResult, s.resolveErr
}
func (s *actressSyncTestScraper) ResolveActressThumbnail(ctx context.Context, actress models.ActressInfo) string {
	s.mu.Lock()
	s.thumbnailInfos = append(s.thumbnailInfos, actress)
	s.mu.Unlock()
	if s.thumbnailFn != nil {
		return s.thumbnailFn(ctx, actress)
	}
	return s.thumbnailURL
}

func newActressSyncTestRepos(t *testing.T) (*database.ActressRepository, *database.MovieRepository) {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "actress-sync.db")},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return database.NewActressRepository(db), database.NewMovieRepository(db)
}

func createActressSyncMovie(t *testing.T, repo *database.MovieRepository, actress models.Actress, id string, release time.Time, others ...models.Actress) {
	t.Helper()
	actresses := append([]models.Actress{actress}, others...)
	require.NoError(t, repo.Create(&models.Movie{
		ContentID:   id,
		ID:          id,
		ReleaseDate: &release,
		Actresses:   actresses,
	}))
}

func TestExactActressMatchUsesJapaneseAliasesAndBothEnglishOrders(t *testing.T) {
	tests := []models.Actress{
		{JapaneseName: "波多野結衣"},
		{Aliases: "別名|波多野結衣"},
		{FirstName: "Yui", LastName: "Hatano"},
		{FirstName: "Hatano", LastName: "Yui"},
	}
	for _, target := range tests {
		match, ok := exactActressMatch(target, []models.ActressInfo{{
			DMMID: 123, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣",
		}})
		assert.True(t, ok)
		assert.Equal(t, 123, match.DMMID)
	}

	_, ok := exactActressMatch(models.Actress{JapaneseName: "波多野結衣"}, []models.ActressInfo{
		{DMMID: 123, JapaneseName: "波多野結衣"},
		{DMMID: 456, JapaneseName: "波多野結衣"},
	})
	assert.False(t, ok, "multiple exact candidates must be rejected")
}

func TestSafeSingleActressMatchRejectsAmbiguity(t *testing.T) {
	target := models.Actress{ID: 1, JapaneseName: "Unknown"}
	known := models.Actress{ID: 2, DMMID: 20}

	match, ok := safeSingleActressMatch(target, []models.Actress{target, known}, []models.ActressInfo{
		{DMMID: 20, JapaneseName: "Known"},
		{DMMID: 30, JapaneseName: "Resolved"},
	})
	require.True(t, ok)
	assert.Equal(t, 30, match.DMMID)

	_, ok = safeSingleActressMatch(target, []models.Actress{target, known}, []models.ActressInfo{
		{DMMID: 30}, {DMMID: 40},
	})
	assert.False(t, ok)

	otherUnknown := models.Actress{ID: 3}
	_, ok = safeSingleActressMatch(target, []models.Actress{target, otherUnknown}, []models.ActressInfo{{DMMID: 30}})
	assert.False(t, ok)
}

func TestSyncActressMetadataUpdatesOnlyMissingFields(t *testing.T) {
	actressRepo, movieRepo := newActressSyncTestRepos(t)
	actress := &models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Aliases: "別名"}
	require.NoError(t, actressRepo.Create(actress))
	createActressSyncMovie(t, movieRepo, *actress, "TEST-001", time.Now())

	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, resolveResult: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 123, JapaneseName: "波多野結衣"}},
	}}
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "https://example.com/123.jpg"}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	registry.Register(thumbnail)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.ElementsMatch(t, []string{"dmm_id", "thumb_url"}, result.UpdatedFields)
	assert.Equal(t, "TEST-001", result.SourceMovieID)
	assert.Equal(t, 123, result.Actress.DMMID)
	assert.Equal(t, "https://example.com/123.jpg", result.Actress.ThumbURL)
	assert.Equal(t, "Yui", result.Actress.FirstName)
	assert.Equal(t, "Hatano", result.Actress.LastName)
	assert.Equal(t, "波多野結衣", result.Actress.JapaneseName)
	assert.Equal(t, "別名", result.Actress.Aliases)
	require.Len(t, thumbnail.thumbnailInfos, 1)
	assert.Equal(t, 123, thumbnail.thumbnailInfos[0].DMMID)
}

func TestSyncActressMetadataReportsDMMIDConflictAndCanStillUpdateThumbnail(t *testing.T) {
	actressRepo, movieRepo := newActressSyncTestRepos(t)
	target := &models.Actress{JapaneseName: "Target"}
	owner := &models.Actress{DMMID: 777, JapaneseName: "Owner", ThumbURL: "owner.jpg"}
	require.NoError(t, actressRepo.Create(target))
	require.NoError(t, actressRepo.Create(owner))
	createActressSyncMovie(t, movieRepo, *target, "TEST-002", time.Now())

	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, resolveResult: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "Target"}},
	}}
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "target.jpg"}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	registry.Register(thumbnail)

	result, err := SyncActressMetadata(context.Background(), target.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncConflict, result.Status)
	require.NotNil(t, result.ConflictActressID)
	assert.Equal(t, owner.ID, *result.ConflictActressID)
	assert.Equal(t, 0, result.Actress.DMMID)
	assert.Equal(t, "target.jpg", result.Actress.ThumbURL)
	assert.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
}

func TestSyncActressMetadataThumbnailOnlyDoesNotOverwriteDMMIDOrNames(t *testing.T) {
	actressRepo, movieRepo := newActressSyncTestRepos(t)
	actress := &models.Actress{DMMID: 321, FirstName: "Existing", LastName: "Name", JapaneseName: "既存"}
	require.NoError(t, actressRepo.Create(actress))
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "thumb.jpg"}
	registry := models.NewScraperRegistry()
	registry.Register(thumbnail)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
	assert.Equal(t, 321, result.Actress.DMMID)
	assert.Equal(t, "Existing", result.Actress.FirstName)
	assert.Equal(t, "Name", result.Actress.LastName)
}

func TestSyncActressMetadataSkipsWithoutMoviesOrEnabledResolver(t *testing.T) {
	actressRepo, movieRepo := newActressSyncTestRepos(t)
	actress := &models.Actress{JapaneseName: "No Movies", ThumbURL: "existing.jpg"}
	require.NoError(t, actressRepo.Create(actress))

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, models.NewScraperRegistry(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncSkipped, result.Status)
	assert.Contains(t, result.Messages[0], "No linked movies")

	createActressSyncMovie(t, movieRepo, *actress, "TEST-003", time.Now())
	disabled := &actressSyncTestScraper{name: "sougouwiki", enabled: false}
	registry := models.NewScraperRegistry()
	registry.Register(disabled)
	result, err = SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncSkipped, result.Status)
	assert.Contains(t, result.Messages[0], "No enabled actress resolver")

	failed := &actressSyncTestScraper{name: "sougouwiki", enabled: true, resolveErr: errors.New("resolver failed")}
	registry = models.NewScraperRegistry()
	registry.Register(failed)
	result, err = SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncSkipped, result.Status)
	assert.Contains(t, result.Messages[0], "resolver failed")
}

func TestSyncActressMetadataLimitsMoviesAndPropagatesTimeout(t *testing.T) {
	t.Run("uses at most five recent movies", func(t *testing.T) {
		actressRepo, movieRepo := newActressSyncTestRepos(t)
		actress := &models.Actress{JapaneseName: "Unmatched", ThumbURL: "existing.jpg"}
		require.NoError(t, actressRepo.Create(actress))
		for i := 0; i < 6; i++ {
			createActressSyncMovie(t, movieRepo, *actress, "TEST-00"+string(rune('1'+i)), time.Now().Add(time.Duration(i)*time.Hour))
		}
		resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, resolveResult: &models.ScraperResult{}}
		registry := models.NewScraperRegistry()
		registry.Register(resolver)

		_, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
		require.NoError(t, err)
		assert.Len(t, resolver.resolvedMovieIDs, 5)
		assert.NotContains(t, resolver.resolvedMovieIDs, "TEST-001")
	})

	t.Run("propagates deadline exceeded", func(t *testing.T) {
		actressRepo, movieRepo := newActressSyncTestRepos(t)
		actress := &models.Actress{JapaneseName: "Timeout", ThumbURL: "existing.jpg"}
		require.NoError(t, actressRepo.Create(actress))
		createActressSyncMovie(t, movieRepo, *actress, "TIMEOUT-001", time.Now())
		resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, resolveFn: func(ctx context.Context, _ string) (*models.ScraperResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		registry := models.NewScraperRegistry()
		registry.Register(resolver)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := SyncActressMetadata(ctx, actress.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
	})

	t.Run("persists DMM ID before a thumbnail timeout", func(t *testing.T) {
		actressRepo, movieRepo := newActressSyncTestRepos(t)
		actress := &models.Actress{JapaneseName: "Partial"}
		require.NoError(t, actressRepo.Create(actress))
		createActressSyncMovie(t, movieRepo, *actress, "PARTIAL-001", time.Now())
		resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, resolveResult: &models.ScraperResult{
			Actresses: []models.ActressInfo{{DMMID: 808, JapaneseName: "Partial"}},
		}}
		thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailFn: func(ctx context.Context, _ models.ActressInfo) string {
			<-ctx.Done()
			return ""
		}}
		registry := models.NewScraperRegistry()
		registry.Register(resolver)
		registry.Register(thumbnail)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := SyncActressMetadata(ctx, actress.ID, actressRepo, movieRepo, registry, []string{"sougouwiki"})
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
		saved, findErr := actressRepo.FindByID(actress.ID)
		require.NoError(t, findErr)
		assert.Equal(t, 808, saved.DMMID)
		assert.Empty(t, saved.ThumbURL)
	})
}
