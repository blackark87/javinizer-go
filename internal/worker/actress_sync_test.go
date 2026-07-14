package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	name           string
	enabled        bool
	identityResult *models.ScraperResult
	identityErr    error
	identityFn     func(context.Context, models.ActressIdentityQuery) (*models.ScraperResult, error)
	thumbnailURL   string
	thumbnailFn    func(context.Context, models.ActressInfo) string
	resolveFn      func(context.Context, string) (*models.ScraperResult, error)

	mu              sync.Mutex
	identityQueries []models.ActressIdentityQuery
	thumbnailInfos  []models.ActressInfo
	resolveQueries  []string
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
func (s *actressSyncTestScraper) ResolveActressIdentity(ctx context.Context, query models.ActressIdentityQuery) (*models.ScraperResult, error) {
	s.mu.Lock()
	s.identityQueries = append(s.identityQueries, query)
	s.mu.Unlock()
	if s.identityFn != nil {
		return s.identityFn(ctx, query)
	}
	return s.identityResult, s.identityErr
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
func (s *actressSyncTestScraper) ResolveActresses(ctx context.Context, id string) (*models.ScraperResult, error) {
	s.mu.Lock()
	s.resolveQueries = append(s.resolveQueries, id)
	s.mu.Unlock()
	if s.resolveFn != nil {
		return s.resolveFn(ctx, id)
	}
	return nil, nil
}

func newActressSyncTestRepo(t *testing.T) *database.ActressRepository {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "actress-sync.db")},
		Logging:  config.LoggingConfig{Level: "error"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return database.NewActressRepository(db)
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

func TestSafeSingleRemainingActressRequiresOneUnresolvedLinkAndOneCandidate(t *testing.T) {
	linked := []models.Actress{{ID: 1}, {ID: 2, DMMID: 20}}
	candidate, ok := safeSingleRemainingActress(1, linked, []models.ActressInfo{{DMMID: 20}, {DMMID: 30, JapaneseName: "対象"}})
	assert.True(t, ok)
	assert.Equal(t, 30, candidate.DMMID)

	_, ok = safeSingleRemainingActress(1, append(linked, models.Actress{ID: 3}), []models.ActressInfo{{DMMID: 30}})
	assert.False(t, ok, "another unresolved linked actress makes the match ambiguous")
	_, ok = safeSingleRemainingActress(1, linked, []models.ActressInfo{{DMMID: 30}, {DMMID: 40}})
	assert.False(t, ok, "multiple remaining candidates must be rejected")
}

func TestActressIdentityNamesIncludesNamesAliasesAndEnglishOrders(t *testing.T) {
	assert.Equal(t, []string{"波多野結衣", "別名", "Yui Hatano", "Hatano Yui"}, actressIdentityNames(models.Actress{
		JapaneseName: "波多野結衣",
		Aliases:      "別名|波多野結衣",
		FirstName:    "Yui",
		LastName:     "Hatano",
	}))
	assert.Equal(t, []string{"hatano yui", "yui hatano"}, actressIdentityNames(models.Actress{
		ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/hatano_yui.jpg",
	}))
}

func TestSyncActressMetadataUsesDirectIdentityLookupAndUpdatesMissingFields(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Aliases: "別名"}
	require.NoError(t, actressRepo.Create(actress))

	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityResult: &models.ScraperResult{
		ID: "波多野結衣", Actresses: []models.ActressInfo{{DMMID: 123, JapaneseName: "波多野結衣"}},
	}}
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "https://example.com/123.jpg"}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	registry.Register(thumbnail)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.ElementsMatch(t, []string{"dmm_id", "aliases", "thumb_url"}, result.UpdatedFields)
	assert.Equal(t, "sougouwiki", result.Source)
	assert.Equal(t, "波多野結衣", result.SourceQuery)
	assert.Equal(t, 123, result.Actress.DMMID)
	assert.Equal(t, "https://example.com/123.jpg", result.Actress.ThumbURL)
	assert.Empty(t, result.Messages, "successful sync should not emit verbose detail messages")
	require.Len(t, resolver.identityQueries, 1)
	assert.Equal(t, []string{"波多野結衣", "別名", "Yui Hatano", "Hatano Yui"}, resolver.identityQueries[0].Names)
	require.Len(t, thumbnail.thumbnailInfos, 1)
	assert.Equal(t, 123, thumbnail.thumbnailInfos[0].DMMID)
}

func TestSyncActressMetadataPreservesExistingThumbnailWhileResolvingDMMID(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{JapaneseName: "波多野結衣", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/hatano_yui.jpg"}
	require.NoError(t, actressRepo.Create(actress))

	resolver := &actressSyncTestScraper{name: "dmm", enabled: true, identityResult: &models.ScraperResult{
		ID: "波多野結衣", Actresses: []models.ActressInfo{{DMMID: 123, JapaneseName: "波多野結衣"}},
	}}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, []string{"dmm"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, []string{"dmm_id"}, result.UpdatedFields)
	assert.Equal(t, 123, result.Actress.DMMID)
	assert.Equal(t, actress.ThumbURL, result.Actress.ThumbURL)
	require.Len(t, resolver.identityQueries, 1)
	assert.Equal(t, actress.ThumbURL, resolver.identityQueries[0].ThumbURL)
	assert.Empty(t, resolver.thumbnailInfos)
}

func TestSyncActressMetadataRejectsAmbiguousExactIdentityMatches(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{JapaneseName: "同名", ThumbURL: "existing.jpg"}
	require.NoError(t, actressRepo.Create(actress))

	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityResult: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 10, JapaneseName: "同名"}, {DMMID: 20, JapaneseName: "同名"}},
	}}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncSkipped, result.Status)
	assert.Equal(t, 0, result.Actress.DMMID)
	assert.Contains(t, strings.Join(result.Messages, "\n"), "rejected 2 result")
}

func TestSyncActressMetadataReportsDMMIDConflictAndCanStillUpdateThumbnail(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	target := &models.Actress{JapaneseName: "Target"}
	owner := &models.Actress{DMMID: 111, JapaneseName: "Target", ThumbURL: "owner.jpg"}
	require.NoError(t, actressRepo.Create(target))
	require.NoError(t, actressRepo.Create(owner))

	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityResult: &models.ScraperResult{
		ID: "Target", Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "Target"}},
	}}
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "target.jpg"}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	registry.Register(thumbnail)

	result, err := SyncActressMetadata(context.Background(), target.ID, actressRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncConflict, result.Status)
	require.NotNil(t, result.ConflictActressID)
	assert.Equal(t, owner.ID, *result.ConflictActressID)
	assert.Equal(t, 0, result.Actress.DMMID)
	assert.Equal(t, "target.jpg", result.Actress.ThumbURL)
	assert.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
}

func TestSyncActressMetadataFallbackMergesNicknameIntoExistingCanonicalActress(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	movieRepo := database.NewMovieRepository(actressRepo.GetDB())
	nickname := &models.Actress{JapaneseName: "もな", ThumbURL: "nickname.jpg"}
	canonical := &models.Actress{JapaneseName: "弥生みづき"}
	require.NoError(t, actressRepo.Create(nickname))
	require.NoError(t, actressRepo.Create(canonical))
	require.NoError(t, movieRepo.Create(&models.Movie{
		ContentID: "jnt051", ID: "JNT-051", Actresses: []models.Actress{*nickname},
	}))

	resolver := &actressSyncTestScraper{
		name: "sougouwiki", enabled: true,
		identityErr: models.NewScraperNotFoundError("sougouwiki", "no direct nickname match"),
		resolveFn: func(_ context.Context, id string) (*models.ScraperResult, error) {
			require.Equal(t, "JNT-051", id)
			return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{{
				DMMID: 777, JapaneseName: "弥生みづき", ThumbURL: "canonical.jpg",
			}}}, nil
		},
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	result, err := SyncActressMetadata(
		context.Background(), nickname.ID, actressRepo, registry, []string{"sougouwiki"}, movieRepo,
	)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, canonical.ID, result.Actress.ID)
	assert.Equal(t, 777, result.Actress.DMMID)
	assert.Equal(t, "弥生みづき", result.Actress.JapaneseName)
	assert.Contains(t, strings.Split(result.Actress.Aliases, "|"), "もな")
	assert.Contains(t, result.UpdatedFields, "movie_actresses")
	assert.Empty(t, result.Messages, "successful fallback resolution should not retain lookup progress")

	_, err = actressRepo.FindByID(nickname.ID)
	assert.True(t, database.IsNotFound(err))
	movie, err := movieRepo.FindByContentID("jnt051")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, canonical.ID, movie.Actresses[0].ID)
}

func TestSyncSelectedRepairsCompletePollutedActressByExistingDMMID(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	movieRepo := database.NewMovieRepository(actressRepo.GetDB())
	polluted := &models.Actress{DMMID: 777, JapaneseName: "もな", ThumbURL: "nickname.jpg"}
	canonical := &models.Actress{JapaneseName: "弥生みづき"}
	require.NoError(t, actressRepo.Create(polluted))
	require.NoError(t, actressRepo.Create(canonical))
	require.NoError(t, movieRepo.Create(&models.Movie{
		ContentID: "jnt051", ID: "JNT-051", Actresses: []models.Actress{*polluted},
	}))
	resolver := &actressSyncTestScraper{
		name: "sougouwiki", enabled: true,
		resolveFn: func(_ context.Context, id string) (*models.ScraperResult, error) {
			return &models.ScraperResult{ID: id, Actresses: []models.ActressInfo{{
				DMMID: 777, JapaneseName: "弥生みづき", ThumbURL: "canonical.jpg",
			}}}, nil
		},
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	result, err := SyncActressMetadata(
		context.Background(), polluted.ID, actressRepo, registry, []string{"sougouwiki"}, movieRepo,
	)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, canonical.ID, result.Actress.ID)
	assert.Equal(t, "弥生みづき", result.Actress.JapaneseName)
	assert.Contains(t, strings.Split(result.Actress.Aliases, "|"), "もな")
	assert.Contains(t, result.UpdatedFields, "movie_actresses")
	assert.Empty(t, result.Messages, "successful selected sync should not emit detail logs")
	_, err = actressRepo.FindByID(polluted.ID)
	assert.True(t, database.IsNotFound(err))
}

func TestSyncActressMetadataThumbnailOnlyDoesNotLookupIdentityOrOverwriteNames(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{DMMID: 321, FirstName: "Existing", LastName: "Name", JapaneseName: "既存"}
	require.NoError(t, actressRepo.Create(actress))
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "thumb.jpg"}
	registry := models.NewScraperRegistry()
	registry.Register(thumbnail)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
	assert.Equal(t, 321, result.Actress.DMMID)
	assert.Equal(t, "Existing", result.Actress.FirstName)
	assert.Empty(t, thumbnail.identityQueries)
}

func TestSyncActressMetadataReportsUnavailableAndFailedIdentityResolvers(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{JapaneseName: "Target", ThumbURL: "existing.jpg"}
	require.NoError(t, actressRepo.Create(actress))

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, models.NewScraperRegistry(), nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncFailed, result.Status)
	assert.Contains(t, result.Messages[0], "No enabled actress identity resolver")

	failed := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityErr: errors.New("resolver failed")}
	registry := models.NewScraperRegistry()
	registry.Register(failed)
	result, err = SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncFailed, result.Status)
	assert.Contains(t, strings.Join(result.Messages, "\n"), "resolver failed")
}

func TestSyncActressMetadataPropagatesTimeoutAndPersistsPartialUpdate(t *testing.T) {
	t.Run("identity timeout", func(t *testing.T) {
		actressRepo := newActressSyncTestRepo(t)
		actress := &models.Actress{JapaneseName: "Timeout", ThumbURL: "existing.jpg"}
		require.NoError(t, actressRepo.Create(actress))
		resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityFn: func(ctx context.Context, _ models.ActressIdentityQuery) (*models.ScraperResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		registry := models.NewScraperRegistry()
		registry.Register(resolver)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := SyncActressMetadata(ctx, actress.ID, actressRepo, registry, []string{"sougouwiki"})
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
	})

	t.Run("thumbnail timeout after saved DMM ID", func(t *testing.T) {
		actressRepo := newActressSyncTestRepo(t)
		actress := &models.Actress{JapaneseName: "Partial"}
		require.NoError(t, actressRepo.Create(actress))
		resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true, identityResult: &models.ScraperResult{
			ID: "Partial", Actresses: []models.ActressInfo{{DMMID: 808, JapaneseName: "Partial"}},
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

		_, err := SyncActressMetadata(ctx, actress.ID, actressRepo, registry, []string{"sougouwiki"})
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
		saved, findErr := actressRepo.FindByID(actress.ID)
		require.NoError(t, findErr)
		assert.Equal(t, 808, saved.DMMID)
		assert.Empty(t, saved.ThumbURL)
	})
}

func TestSyncActressMetadataChecksAtMostFiveRecentMoviesAfterDirectLookupFails(t *testing.T) {
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "fallback.db")}}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	target := &models.Actress{JapaneseName: "対象女優", ThumbURL: "existing.jpg"}
	require.NoError(t, actressRepo.Create(target))
	for index := 1; index <= 6; index++ {
		movie := &models.Movie{ContentID: fmt.Sprintf("movie-%d", index), ID: fmt.Sprintf("TEST-%03d", index), Actresses: []models.Actress{*target}}
		require.NoError(t, movieRepo.Create(movie))
	}

	resolver := &actressSyncTestScraper{
		name: "sougouwiki", enabled: true,
		identityErr: models.NewScraperNotFoundError("sougouwiki", "no direct match"),
		resolveFn: func(_ context.Context, _ string) (*models.ScraperResult, error) {
			return &models.ScraperResult{Actresses: []models.ActressInfo{
				{DMMID: 100, JapaneseName: "다른 배우"}, {DMMID: 200, JapaneseName: "또 다른 배우"},
			}}, nil
		},
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)
	result, err := SyncActressMetadata(context.Background(), target.ID, actressRepo, registry, []string{"sougouwiki"}, movieRepo)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncSkipped, result.Status)
	assert.Len(t, resolver.resolveQueries, 5)
	assert.NotContains(t, resolver.resolveQueries, "TEST-006", "the sixth linked movie must not be queried")
}
