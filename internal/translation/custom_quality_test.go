package translation

import (
	"context"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateMovie_DMMReadingProtectsActressName(t *testing.T) {
	var inputs [][]string
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		inputs = append(inputs, append([]string(nil), texts...))
		return &translationResult{Texts: []string{"⟦0⟧의 유혹", "⟦0⟧가 등장하는 작품"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true, Description: true, Actresses: true},
	}, provider)
	movie := &models.Movie{
		Title:       "響蓮の誘惑",
		Description: "本編に響蓮が登場する",
		Actresses: []models.Actress{{
			JapaneseName: "響蓮",
			ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hibiki_ren.jpg",
		}},
	}

	output, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	assert.Empty(t, warning)
	require.Len(t, inputs, 1)
	assert.NotContains(t, strings.Join(inputs[0], "\n"), "響蓮")
	assert.Equal(t, "히비키 렌의 유혹", movie.Title)
	assert.Equal(t, "히비키 렌이 등장하는 작품", movie.Description)
	assert.Equal(t, "響蓮", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "히비키", movie.Actresses[0].LastName)
	assert.Equal(t, "렌", movie.Actresses[0].FirstName)
	require.NotNil(t, output)
	require.Len(t, output.Movie.Actresses, 1)
	assert.Equal(t, "히비키 렌", output.Movie.Actresses[0])
}

func TestTranslateMovie_RetriesNonHangulPersonSlot(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls++
		if len(texts) == 2 {
			return &translationResult{Texts: []string{"멋진 작품", "Hibiki Ren"}}, nil
		}
		return &translationResult{Texts: []string{"히비키 렌"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true, Actresses: true},
	}, provider)
	movie := &models.Movie{Title: "素敵な作品", Actresses: []models.Actress{{JapaneseName: "響蓮"}}}

	output, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	assert.Empty(t, warning)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "히비키 렌", output.Movie.Actresses[0])
	assert.Equal(t, "響蓮", movie.Actresses[0].JapaneseName)
	assert.Equal(t, "히비키", movie.Actresses[0].LastName)
	assert.Equal(t, "렌", movie.Actresses[0].FirstName)
}

func TestTranslateMovie_RetriesResidualJapaneseSlot(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, _ []string) (*translationResult, error) {
		calls++
		if calls == 1 {
			return &translationResult{Texts: []string{"격차가 최고すぎる"}}, nil
		}
		return &translationResult{Texts: []string{"격차가 너무 좋다"}}, nil
	}}
	service := New(Config{
		Enabled: true, Provider: "mock", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: fieldsConfig{Title: true},
	}, provider)
	movie := &models.Movie{Title: "格差が最高すぎる"}

	_, warning, err := service.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	assert.Empty(t, warning)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "격차가 너무 좋다", movie.Title)
}

func TestTranslateTexts_FallsBackOnMergedSlotAnomaly(t *testing.T) {
	calls := 0
	provider := &mockProvider{translateFunc: func(_ context.Context, _, _ string, texts []string) (*translationResult, error) {
		calls++
		if len(texts) == 2 {
			return &translationResult{Texts: []string{strings.Repeat("merged ", 30), "second"}}, nil
		}
		return &translationResult{Texts: []string{"single"}}, nil
	}}
	service := New(Config{Provider: "mock"}, provider)

	translated, err := service.translateTexts(context.Background(), "ja", "ko", []string{"短い", "説明"}, []string{"title", "description"})
	require.NoError(t, err)
	assert.Equal(t, []string{"single", "single"}, translated)
	assert.Equal(t, 3, calls)
}
