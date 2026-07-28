package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAll_IncludesFANZAMCPBeforeExternalDMMProviders(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	RegisterAll(registry)

	fanzaMCP, ok := registry.Get("fanzamcp")
	require.True(t, ok)
	libreDMM, ok := registry.Get("libredmm")
	require.True(t, ok)
	dmm, ok := registry.Get("dmm")
	require.True(t, ok)

	assert.Greater(t, fanzaMCP.Priority, libreDMM.Priority)
	assert.Greater(t, fanzaMCP.Priority, dmm.Priority)
	assert.Contains(t, registry.Priorities(), "fanzamcp")
}
