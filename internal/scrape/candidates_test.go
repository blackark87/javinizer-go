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
	titles []string
}

func (s *candidateTranslatorStub) Translate(_ context.Context, _ *models.Movie) (string, bool, *translation.TranslationOutput) {
	return "", false, nil
}

func (s *candidateTranslatorStub) TranslateTitles(_ context.Context, titles []string) ([]string, error) {
	s.titles = append([]string(nil), titles...)
	return []string{"번역 A", "번역 B"}, nil
}

func TestTranslateCandidateTitles(t *testing.T) {
	translator := &candidateTranslatorStub{}
	candidates := []models.ScrapeCandidate{{OriginalTitle: "原題 A", Title: "原題 A"}, {OriginalTitle: "原題 B", Title: "原題 B"}}
	translateCandidateTitles(context.Background(), translator, candidates)
	assert.Equal(t, []string{"原題 A", "原題 B"}, translator.titles)
	assert.Equal(t, "번역 A", candidates[0].Title)
	assert.Equal(t, "번역 B", candidates[1].Title)
}

func TestMovieCandidateResults_ExcludesActressResolver(t *testing.T) {
	regular := &models.ScraperResult{Source: "dmm", Title: "movie"}
	resolver := &models.ScraperResult{Source: actressResolverScraperName, Title: "actress-only"}
	assert.Equal(t, []*models.ScraperResult{regular}, movieCandidateResults([]*models.ScraperResult{regular, resolver}, actressResolverScraperName))
	assert.Equal(t, []*models.ScraperResult{resolver}, movieCandidateResults([]*models.ScraperResult{resolver}, actressResolverScraperName))
}
