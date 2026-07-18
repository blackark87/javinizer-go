package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildScrapeCandidates(t *testing.T) {
	t.Run("different titles conflict", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "dmm", ID: "ABC-1", Title: "Title A", Actresses: []models.ActressInfo{{}}},
			{Source: "r18dev", ID: "ABC-1", Title: "Title B"},
		}
		candidates, conflict := buildScrapeCandidates(results)
		require.Len(t, candidates, 2)
		assert.True(t, conflict)
		assert.Equal(t, "Title A", candidates[0].OriginalTitle)
		assert.Equal(t, 1, candidates[0].ActressCount)
	})

	t.Run("normalized titles agree", func(t *testing.T) {
		results := []*models.ScraperResult{
			{Source: "dmm", Title: " Same  Title "},
			{Source: "r18dev", Title: "same title"},
		}
		_, conflict := buildScrapeCandidates(results)
		assert.False(t, conflict)
	})

	t.Run("ID fallback detects conflict", func(t *testing.T) {
		results := []*models.ScraperResult{{Source: "a", ID: "ABC-1"}, {Source: "b", ID: "XYZ-2"}}
		_, conflict := buildScrapeCandidates(results)
		assert.True(t, conflict)
	})
}

type candidateTranslatorStub struct {
	movies []models.Movie
}

func (s *candidateTranslatorStub) Translate(_ context.Context, movie *models.Movie) (string, bool, *translation.TranslationOutput) {
	s.movies = append(s.movies, *movie.Clone())
	movie.Title = "번역 " + movie.Title
	movie.Description = "번역 " + movie.Description
	record := models.MovieTranslation{Language: "ko", Title: movie.Title, Description: movie.Description, SettingsHash: "hash"}
	return "", true, &translation.TranslationOutput{Movie: &record, Movies: []models.MovieTranslation{record}}
}

func TestTranslateCandidateMetadata(t *testing.T) {
	translator := &candidateTranslatorStub{}
	candidates := []models.ScrapeCandidate{
		{Source: "a", OriginalTitle: "原題 A", Title: "原題 A", OriginalDescription: "説明 A", Description: "説明 A"},
		{Source: "b", OriginalTitle: "原題 B", Title: "原題 B", OriginalDescription: "説明 B", Description: "説明 B"},
	}
	warning := translateCandidateMetadata(context.Background(), translator, candidates)
	assert.Empty(t, warning)
	require.Len(t, translator.movies, 2)
	assert.Equal(t, "原題 A", translator.movies[0].Title)
	assert.Equal(t, "説明 A", translator.movies[0].Description)
	assert.Equal(t, "번역 原題 A", candidates[0].Title)
	assert.Equal(t, "번역 説明 A", candidates[0].Description)
	require.Len(t, candidates[0].Translations, 1)
	assert.Equal(t, "ko", candidates[0].Translations[0].Language)
}

func TestMovieCandidateResults_ExcludesActressResolver(t *testing.T) {
	regular := &models.ScraperResult{Source: "dmm", Title: "movie"}
	resolver := &models.ScraperResult{Source: actressResolverScraperName, Title: "actress-only"}
	assert.Equal(t, []*models.ScraperResult{regular}, movieCandidateResults([]*models.ScraperResult{regular, resolver}, actressResolverScraperName))
	assert.Equal(t, []*models.ScraperResult{resolver}, movieCandidateResults([]*models.ScraperResult{resolver}, actressResolverScraperName))
}
