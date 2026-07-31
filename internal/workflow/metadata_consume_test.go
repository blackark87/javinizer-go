package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataConsumeScraper struct {
	source      string
	productCode string
	consumeURL  string
}

func (s *metadataConsumeScraper) Scrape(context.Context, scrape.ScrapeCmd, scrape.ProgressFunc) (*scrape.ScrapeResult, error) {
	return nil, nil
}

func (s *metadataConsumeScraper) ConsumeMetadata(_ context.Context, source, productCode, consumeURL string) error {
	s.source = source
	s.productCode = productCode
	s.consumeURL = consumeURL
	return nil
}

func TestWorkflow_ConsumeMetadataDelegatesToScrapeProvider(t *testing.T) {
	provider := &metadataConsumeScraper{}
	wf := &Workflow{
		scrape: newScrapeOrchestrator(provider, nil, "", nil, nfo.NFONameConfig{}, nil),
	}

	err := wf.ConsumeMetadata(
		context.Background(),
		"fanzamcp",
		"MDVR-338",
		"http://fanza-mcp:8000/api/v1/metadata/MDVR-338/consume",
	)

	require.NoError(t, err)
	assert.Equal(t, "fanzamcp", provider.source)
	assert.Equal(t, "MDVR-338", provider.productCode)
	assert.Equal(t, "http://fanza-mcp:8000/api/v1/metadata/MDVR-338/consume", provider.consumeURL)
}
