package fanzamcp

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_ExposesDefaultsOptionsAndPriority(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	Register(registry)

	registration, ok := registry.Get(scraperName)
	require.True(t, ok)
	assert.Equal(t, "FANZA MCP", registration.Description)
	assert.Equal(t, 98, registration.Priority)
	assert.False(t, registration.Defaults.Enabled)
	assert.Equal(t, defaultBaseURL, registration.Defaults.BaseURL)
	assert.Equal(t, defaultTimeoutSeconds, registration.Defaults.Timeout)
	assert.NotNil(t, registration.Constructor)
	assert.NotNil(t, registration.ValidateFn)

	optionKeys := make([]string, 0, len(registration.Options))
	for _, option := range registration.Options {
		optionKeys = append(optionKeys, option.Key)
	}
	assert.ElementsMatch(t, []string{"base_url", "rate_limit", "timeout"}, optionKeys)

	instance, err := registration.Constructor(scraperutil.ScraperDeps{
		Settings: models.ScraperSettings{
			Enabled: true,
			BaseURL: defaultBaseURL,
			Timeout: defaultTimeoutSeconds,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, instance)
	assert.Equal(t, scraperName, instance.Name())
	assert.True(t, instance.IsEnabled())
}
