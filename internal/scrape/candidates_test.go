package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sourceTranslatorStub struct {
	movies []models.Movie
}

func (s *sourceTranslatorStub) Translate(_ context.Context, movie *models.Movie) (string, bool, *translation.TranslationOutput) {
	s.movies = append(s.movies, *movie.Clone())
	record := models.MovieTranslation{
		Language:     "ko",
		Title:        "번역 " + movie.Title,
		Description:  "번역 " + movie.Description,
		SettingsHash: "hash",
	}
	return "", true, &translation.TranslationOutput{Movie: &record, Movies: []models.MovieTranslation{record}}
}

func TestTranslateSourceMetadataPreservesRawValues(t *testing.T) {
	translator := &sourceTranslatorStub{}
	results := []*models.ScraperResult{
		{Source: "dmm", ID: "ABC-1", Title: "原題 A", Description: "説明 A"},
		{Source: "r18dev", ID: "ABC-1", Title: "原題 B", Description: "説明 B"},
		{Source: actressResolverScraperName},
	}

	warning := translateSourceMetadata(context.Background(), translator, results)

	assert.Empty(t, warning)
	require.Len(t, translator.movies, 2)
	assert.Equal(t, "原題 A", results[0].Title)
	assert.Equal(t, "説明 A", results[0].Description)
	require.Len(t, results[0].Translations, 1)
	assert.Equal(t, "ko", results[0].Translations[0].Language)
	assert.Equal(t, "번역 原題 A", results[0].Translations[0].Title)
	assert.Equal(t, "번역 説明 A", results[0].Translations[0].Description)
	assert.Empty(t, results[2].Translations)
}

func TestTranslateSourceMetadataCollectsWarnings(t *testing.T) {
	translator := &warningSourceTranslatorStub{}
	results := []*models.ScraperResult{{Source: "dmm", Title: "原題"}}

	warning := translateSourceMetadata(context.Background(), translator, results)

	assert.Contains(t, warning, "source metadata")
	assert.Contains(t, warning, "dmm")
}

type warningSourceTranslatorStub struct{}

func (*warningSourceTranslatorStub) Translate(_ context.Context, _ *models.Movie) (string, bool, *translation.TranslationOutput) {
	return "partial", true, nil
}
