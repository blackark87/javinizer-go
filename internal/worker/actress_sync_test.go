package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
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
	searchFn       func(context.Context, string) (*models.ScraperResult, error)

	mu              sync.Mutex
	identityQueries []models.ActressIdentityQuery
	thumbnailInfos  []models.ActressInfo
	resolveQueries  []string
	searchQueries   []string
}

type actressProfileSyncTestScraper struct {
	*actressSyncTestScraper
	profile    models.ActressInfo
	profileErr error
}

func (s *actressProfileSyncTestScraper) ResolveActressProfile(context.Context, models.ActressInfo) (models.ActressInfo, error) {
	return s.profile, s.profileErr
}

func (s *actressSyncTestScraper) Name() string { return s.name }
func (s *actressSyncTestScraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	s.mu.Lock()
	s.searchQueries = append(s.searchQueries, id)
	s.mu.Unlock()
	if s.searchFn != nil {
		return s.searchFn(ctx, id)
	}
	return nil, nil
}
func (s *actressSyncTestScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *actressSyncTestScraper) IsEnabled() bool                                { return s.enabled }
func (s *actressSyncTestScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
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
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return database.NewActressRepository(db)
}

func TestResolveThumbnailFromRecentMoviesStopsAtExactDMMIDAndCapsAtFive(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	movieRepo := database.NewMovieRepository(actressRepo.GetDB())
	actress := &models.Actress{DMMID: 1075313, JapaneseName: "ちびとり"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	for index := 0; index < 6; index++ {
		movie := &models.Movie{
			ContentID: fmt.Sprintf("recent%03d", index),
			ID:        fmt.Sprintf("RECENT-%03d", index),
			Actresses: []models.Actress{*actress},
		}
		require.NoError(t, movieRepo.Create(context.Background(), movie))
	}

	dmm := &actressSyncTestScraper{name: "dmm", enabled: true}
	dmm.searchFn = func(_ context.Context, id string) (*models.ScraperResult, error) {
		if id == "RECENT-002" {
			return &models.ScraperResult{Actresses: []models.ActressInfo{{
				DMMID: 1075313, ThumbURL: "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/tibitori.jpg",
			}}}, nil
		}
		return &models.ScraperResult{}, nil
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(dmm)

	thumbnail := resolveThumbnailFromRecentMovies(context.Background(), registry, movieRepo, *actress)
	assert.Equal(t, "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/tibitori.jpg", thumbnail)
	assert.LessOrEqual(t, len(dmm.searchQueries), maxActressSyncMovies)
	assert.Contains(t, dmm.searchQueries, "RECENT-002")

	dmm.mu.Lock()
	dmm.searchQueries = nil
	dmm.searchFn = func(context.Context, string) (*models.ScraperResult, error) {
		return &models.ScraperResult{}, nil
	}
	dmm.mu.Unlock()
	assert.Empty(t, resolveThumbnailFromRecentMovies(context.Background(), registry, movieRepo, *actress))
	assert.Len(t, dmm.searchQueries, maxActressSyncMovies)
}

func TestSyncActressMetadataThumbnailOnlyDoesNotLookupIdentityOrOverwriteNames(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{DMMID: 321, FirstName: "Existing", LastName: "Name", JapaneseName: "既存"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	thumbnail := &actressSyncTestScraper{name: "dmm", enabled: false, thumbnailURL: "thumb.jpg"}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(thumbnail)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, []string{"thumb_url"}, result.UpdatedFields)
	assert.Equal(t, 321, result.Actress.DMMID)
	assert.Equal(t, "Existing", result.Actress.FirstName)
	assert.Empty(t, thumbnail.identityQueries)
	assert.Empty(t, thumbnail.resolveQueries)
}

func TestSyncActressMetadataDMMProfileOverwritesExistingThumbnail(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{
		DMMID: 321, FirstName: "레나", LastName: "미야시타", JapaneseName: "宮下玲奈",
		ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/miyasita_rena.jpg",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &actressProfileSyncTestScraper{
		actressSyncTestScraper: &actressSyncTestScraper{name: "dmm", enabled: true},
		profile: models.ActressInfo{
			DMMID: 321, JapaneseName: "宮下玲奈",
			ThumbURL: "https://awsimgsrc.dmm.co.jp/mono/actjpgs/miyasita_rena2.jpg",
		},
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Contains(t, result.UpdatedFields, "thumb_url")
	assert.Equal(t, "https://awsimgsrc.dmm.co.jp/mono/actjpgs/miyasita_rena2.jpg", result.Actress.ThumbURL)
	stored, err := actressRepo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	assert.Equal(t, result.Actress.ThumbURL, stored.ThumbURL)
}

func TestSyncActressMetadataDMMProfileUpdatesCanonicalNameAndAliasMappings(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{
		DMMID: 411, FirstName: "마히나", LastName: "아마네", JapaneseName: "天音まひな",
		ThumbURL: "old.jpg", Aliases: "기존별명",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &actressProfileSyncTestScraper{
		actressSyncTestScraper: &actressSyncTestScraper{name: "dmm", enabled: true},
		profile:                models.ActressInfo{DMMID: 411, JapaneseName: "星まりあ", ThumbURL: "current.jpg"},
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Contains(t, result.UpdatedFields, "japanese_name")
	assert.Contains(t, result.UpdatedFields, "thumb_url")
	assert.Contains(t, result.UpdatedFields, "aliases")
	assert.Equal(t, "星まりあ", result.Actress.JapaneseName)
	assert.Equal(t, "current.jpg", result.Actress.ThumbURL)
	assert.ElementsMatch(t, []string{"기존별명", "天音まひな"}, strings.Split(result.Actress.Aliases, "|"))

	aliasRepo := database.NewActressAliasRepository(actressRepo.GetDB())
	group, err := aliasRepo.GetAliasGroup(context.Background(), "天音まひな")
	require.NoError(t, err)
	assert.Equal(t, "星まりあ", group.Canonical)
}

func TestSyncActressMetadataRepairsMissingAliasTableMappingWithoutProfileChanges(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{
		DMMID: 413, JapaneseName: "現在名", FirstName: "현재", LastName: "이름",
		ThumbURL: "current.jpg", Aliases: "旧名",
	}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &actressProfileSyncTestScraper{
		actressSyncTestScraper: &actressSyncTestScraper{name: "dmm", enabled: true},
		profile:                models.ActressInfo{DMMID: 413, JapaneseName: "現在名", ThumbURL: "current.jpg"},
	}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, nil)
	require.NoError(t, err)
	assert.Equal(t, ActressSyncUpdated, result.Status)
	assert.Equal(t, []string{"aliases"}, result.UpdatedFields)
	assert.Equal(t, "현재", result.Actress.FirstName)
	assert.Equal(t, "이름", result.Actress.LastName)

	aliasRepo := database.NewActressAliasRepository(actressRepo.GetDB())
	stored, err := aliasRepo.FindByAliasName(context.Background(), "旧名")
	require.NoError(t, err)
	assert.Equal(t, "現在名", stored.CanonicalName)
}

func TestSyncActressMetadataRequiresDurableMovieJobForMissingDMMID(t *testing.T) {
	actressRepo := newActressSyncTestRepo(t)
	actress := &models.Actress{JapaneseName: "미검증 가명"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	resolver := &actressSyncTestScraper{name: "sougouwiki", enabled: true}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)

	result, err := SyncActressMetadata(context.Background(), actress.ID, actressRepo, registry, []string{"sougouwiki"})
	require.NoError(t, err)
	assert.Equal(t, ActressSyncFailed, result.Status)
	assert.Contains(t, result.Messages, "DMM-ID-less actresses must be synced through durable per-movie jobs")
	assert.Empty(t, resolver.identityQueries)
	assert.Empty(t, resolver.resolveQueries)
}
