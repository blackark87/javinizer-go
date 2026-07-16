package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type actressResolverScraper struct {
	name       string
	enabled    bool
	result     *models.ScraperResult
	err        error
	calls      int
	thumbnail  string
	thumbCalls int
}

func (s *actressResolverScraper) Name() string { return s.name }
func (s *actressResolverScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return s.result, s.err
}
func (s *actressResolverScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *actressResolverScraper) IsEnabled() bool                                { return s.enabled }
func (s *actressResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
}
func (s *actressResolverScraper) Close() error { return nil }
func (s *actressResolverScraper) ResolveActresses(context.Context, string) (*models.ScraperResult, error) {
	s.calls++
	return s.result, s.err
}
func (s *actressResolverScraper) ResolveActressThumbnail(context.Context, models.ActressInfo) string {
	s.thumbCalls++
	return s.thumbnail
}

func TestResolveMissingActressesRunsOnlyWithoutVerifiedDMMIdentity(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "正式名"}},
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "TEST-001", []*models.ScraperResult{{
		Source: "regular", Actresses: []models.ActressInfo{{JapaneseName: "별명"}},
	}})
	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, 1, resolver.calls)
	assert.Equal(t, "TEST-001", result.ID)

	result, failure = s.resolveMissingActresses(context.Background(), "TEST-002", []*models.ScraperResult{{
		Source: "regular", Actresses: []models.ActressInfo{{DMMID: 1, JapaneseName: "정식명"}},
	}})
	assert.Nil(t, result)
	assert.Nil(t, failure)
	assert.Equal(t, 1, resolver.calls)
}

func TestResolveMissingActressesFailureIsOptionalAndAttributed(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, err: errors.New("lookup failed")}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "TEST-001", []*models.ScraperResult{{Source: "regular", Title: "keep"}})
	assert.Nil(t, result)
	require.NotNil(t, failure)
	assert.Equal(t, actressResolverScraperName, failure.Scraper)
	assert.ErrorContains(t, failure.Cause, "lookup failed")
}

func TestResolveMissingActressesEnrichesThumbnail(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "正式名"}},
	}}
	thumbnail := &actressResolverScraper{name: "dmm", enabled: true, thumbnail: "https://example.com/actress.jpg"}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	registry.RegisterInstance(thumbnail)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "TEST-001", nil)
	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, "https://example.com/actress.jpg", result.Actresses[0].ThumbURL)
	assert.Equal(t, 1, thumbnail.thumbCalls)
}

func TestActressOverrideResultsPreservesRawMetadata(t *testing.T) {
	raw := []*models.ScraperResult{
		{Source: "regular", ID: "CANONICAL-001", Title: "Existing", Actresses: []models.ActressInfo{{JapaneseName: "별명"}}},
		{Source: actressResolverScraperName, ID: "RAW-001", Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "正式名"}}},
	}
	overrides := actressOverrideResults(raw, actressResolverScraperName)
	require.Len(t, overrides, 2)
	assert.Len(t, raw[0].Actresses, 1)
	assert.Empty(t, overrides[0].Actresses)
	assert.Equal(t, "Existing", overrides[0].Title)
	assert.Empty(t, overrides[1].ID)
	assert.Empty(t, overrides[1].Title)
	assert.Equal(t, 777, overrides[1].Actresses[0].DMMID)

	resolverOnly := actressOverrideResults(raw[1:], actressResolverScraperName)
	assert.Same(t, raw[1], resolverOnly[0])
	assert.Equal(t, "RAW-001", resolverOnly[0].ID)
}
