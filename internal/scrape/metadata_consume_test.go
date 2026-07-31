package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataConsumerStub struct {
	productCode string
	consumeURL  string
}

func (*metadataConsumerStub) Name() string { return "fanzamcp" }
func (*metadataConsumerStub) IsEnabled() bool {
	return true
}
func (*metadataConsumerStub) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (*metadataConsumerStub) Close() error { return nil }
func (*metadataConsumerStub) GetURL(context.Context, string) (string, error) {
	return "", nil
}
func (*metadataConsumerStub) Search(context.Context, string) (*models.ScraperResult, error) {
	return nil, nil
}
func (s *metadataConsumerStub) ConsumeMetadata(_ context.Context, productCode, consumeURL string) error {
	s.productCode = productCode
	s.consumeURL = consumeURL
	return nil
}

func TestScraper_ConsumeMetadataUsesOriginatingProvider(t *testing.T) {
	provider := &metadataConsumerStub{}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(provider)
	engine := &Scraper{registry: registry}

	err := engine.ConsumeMetadata(
		context.Background(),
		"fanzamcp",
		"MDVR-338",
		"http://fanza-mcp:8000/api/v1/metadata/MDVR-338/consume",
	)

	require.NoError(t, err)
	assert.Equal(t, "MDVR-338", provider.productCode)
	assert.Equal(t, "http://fanza-mcp:8000/api/v1/metadata/MDVR-338/consume", provider.consumeURL)
}

func TestScraper_ConsumeMetadataRejectsUnknownProvider(t *testing.T) {
	engine := &Scraper{registry: scraperutil.NewScraperRegistry()}

	err := engine.ConsumeMetadata(context.Background(), "missing", "MDVR-338", "http://example/consume")

	require.Error(t, err)
	assert.ErrorContains(t, err, `metadata consumer "missing" is not registered`)
}
