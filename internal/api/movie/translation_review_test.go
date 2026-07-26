package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewMovieTranslation_PersistsReviewedTitleAndTranslation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Len(t, request.Messages, 2)
		content := "<<<title>>>\n새 번역 제목\n<<<JZ_DONE>>>"
		if strings.Contains(request.Messages[1].Content, "[JAPANESE SOURCE]") {
			content = "<<<quality_review_title>>>\n검토된 번역 제목\n<<<JZ_DONE>>>"
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": content},
				"finish_reason": "stop",
			}},
		}))
	}))
	defer server.Close()

	deps := createTestDeps(t, &config.Config{}, "")
	stored, err := deps.Repos.MovieRepo.Upsert(context.Background(), &models.Movie{
		ContentID:     "test001",
		ID:            "TEST-001",
		Title:         "이전 제목",
		DisplayTitle:  "[2026][4K]이전 제목",
		OriginalTitle: "元の題名",
		Translations: []models.MovieTranslation{{
			Language: "ja",
			Title:    "元の題名",
		}},
	})
	require.NoError(t, err)

	thinking := false
	translationConfig := config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai-compatible",
		SourceLanguage: "ja",
		TargetLanguage: "ko",
		TimeoutSeconds: 5,
		OpenAICompatible: config.OpenAICompatibleTranslationConfig{
			BaseURL:        server.URL,
			Model:          "test-model",
			EnableThinking: &thinking,
		},
	}
	movieDeps := NewMovieDeps(
		deps.Repos.MovieRepo,
		WithTranslationConfig(func() config.TranslationConfig { return translationConfig }),
	)
	router := gin.New()
	router.POST("/movies/:id/translation-review", reviewMovieTranslation(movieDeps))

	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/movies/TEST-001/translation-review",
			bytes.NewBufferString(`{"field":"title"}`),
		),
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body contracts.TranslationReviewResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.True(t, body.Changed)
	assert.Equal(t, "검토된 번역 제목", body.Movie.Title)
	assert.Equal(t, "[2026][4K]검토된 번역 제목", body.Movie.DisplayTitle)
	assert.Equal(t, 2, callCount)

	reloaded, err := deps.Repos.MovieRepo.FindByContentID(context.Background(), stored.ContentID)
	require.NoError(t, err)
	assert.Equal(t, "검토된 번역 제목", reloaded.Title)
	var korean *models.MovieTranslation
	for i := range reloaded.Translations {
		if reloaded.Translations[i].Language == "ko" {
			korean = &reloaded.Translations[i]
			break
		}
	}
	require.NotNil(t, korean)
	assert.Equal(t, "검토된 번역 제목", korean.Title)
	assert.Equal(t, "translation-review", korean.SourceName)
}

func TestRetainedMovieJapaneseField_DescriptionRequiresStoredJapanese(t *testing.T) {
	movie := &models.Movie{
		OriginalTitle: "原題",
		Description:   "현재 설명",
		Translations: []models.MovieTranslation{{
			Language:    "ja",
			Title:       "保存題",
			Description: "保存説明",
		}},
	}
	assert.Equal(t, "保存題", retainedMovieJapaneseField(movie, "title"))
	assert.Equal(t, "保存説明", retainedMovieJapaneseField(movie, "description"))

	movie.Translations = nil
	assert.Equal(t, "原題", retainedMovieJapaneseField(movie, "title"))
	assert.Empty(t, retainedMovieJapaneseField(movie, "description"))
}
