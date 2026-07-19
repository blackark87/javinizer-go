package worker

import (
	"context"
	"path/filepath"
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

	mu              sync.Mutex
	identityQueries []models.ActressIdentityQuery
	thumbnailInfos  []models.ActressInfo
	resolveQueries  []string
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
func (s *actressSyncTestScraper) Search(context.Context, string) (*models.ScraperResult, error) {
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
