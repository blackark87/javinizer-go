package batch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRefreshTranslationOnlyInput(t *testing.T) {
	t.Run("ordinary scrape remains valid when translation is disabled", func(t *testing.T) {
		require.NoError(t, validateRefreshTranslationOnlyInput(StartScrapeInput{}, false))
	})

	t.Run("translation must be enabled", func(t *testing.T) {
		err := validateRefreshTranslationOnlyInput(StartScrapeInput{RefreshTranslationOnly: true}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "translation")
	})

	tests := []struct {
		name  string
		input StartScrapeInput
	}{
		{
			name:  "force",
			input: StartScrapeInput{RefreshTranslationOnly: true, Force: true},
		},
		{
			name: "selected scrapers",
			input: StartScrapeInput{
				RefreshTranslationOnly: true,
				SelectedScrapers:       []string{"dmm"},
			},
		},
		{
			name: "manual inputs",
			input: StartScrapeInput{
				RefreshTranslationOnly: true,
				ManualInputs:           map[string]string{"/media/ABC-001.mp4": "ABC-001"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRefreshTranslationOnlyInput(tt.input, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "mutually exclusive")
		})
	}

	require.NoError(t, validateRefreshTranslationOnlyInput(
		StartScrapeInput{RefreshTranslationOnly: true},
		true,
	))
}
