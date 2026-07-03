package translation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// fieldNamesFor generates one generic marker label per text for tests.
func fieldNamesFor(texts []string) []string {
	names := make([]string, len(texts))
	for i := range texts {
		names[i] = fmt.Sprintf("field_%d", i)
	}
	return names
}

// markerContent builds a named-marker LLM response payload for tests.
func markerContent(fieldNames, texts []string) string {
	var b strings.Builder
	for i, fn := range fieldNames {
		b.WriteString("<<<" + fn + ">>>\n")
		if i < len(texts) {
			b.WriteString(texts[i])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// =============================================================================
// New tests
// =============================================================================

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.TranslationConfig
	}{
		{
			name: "returns non-nil service",
			cfg:  config.TranslationConfig{Enabled: true},
		},
		{
			name: "service has http client",
			cfg:  config.TranslationConfig{},
		},
		{
			name: "service preserves config",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg)
			assert.NotNil(t, s)
			assert.NotNil(t, s.httpClient)
			assert.Equal(t, tt.cfg.Enabled, s.cfg.Enabled)
		})
	}
}

// =============================================================================
// TranslateMovie tests - early returns and validation
// =============================================================================

func TestTranslateMovie_EarlyReturns(t *testing.T) {
	tests := []struct {
		name    string
		service *Service
		movie   *models.Movie
		wantErr bool
		wantNil bool
	}{
		{
			name:    "nil service returns nil",
			service: nil,
			movie:   &models.Movie{Title: "Test"},
			wantNil: true,
		},
		{
			name: "nil movie returns nil",
			service: New(config.TranslationConfig{
				Enabled: true,
			}),
			movie:   nil,
			wantNil: true,
		},
		{
			name: "disabled service returns nil",
			service: New(config.TranslationConfig{
				Enabled: false,
			}),
			movie:   &models.Movie{Title: "Test"},
			wantNil: true,
		},
		{
			name: "missing target language returns error",
			service: New(config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "",
			}),
			movie:   &models.Movie{Title: "Test"},
			wantErr: true,
		},
		{
			name: "same source and target language returns nil",
			service: New(config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				SourceLanguage: "en",
				TargetLanguage: "en",
			}),
			movie:   &models.Movie{Title: "Test"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, err := tt.service.TranslateMovie(context.Background(), tt.movie, "")
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.Nil(t, result)
			}
		})
	}
}

// =============================================================================
// TranslateMovie tests - ApplyToPrimary flag
// =============================================================================

func TestTranslateMovie_ApplyToPrimary(t *testing.T) {
	t.Run("apply to primary enabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<title>>>\nTranslated Title",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{Title: "テスト"}
		result, _, err := s.TranslateMovie(context.Background(), movie, "")

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Translated Title", result[0].Title)
		// Verify movie was mutated when ApplyToPrimary is enabled
		assert.Equal(t, "Translated Title", movie.Title)
	})

	t.Run("apply to primary disabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<title>>>\nTranslated Title",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: false,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{Title: "テスト"}
		originalTitle := movie.Title
		result, _, err := s.TranslateMovie(context.Background(), movie, "")

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Translated Title", result[0].Title)
		// Verify movie was NOT mutated when ApplyToPrimary is disabled
		assert.Equal(t, originalTitle, movie.Title)
	})
}

// =============================================================================
// TranslateMovie tests - VR marker stripping from title
// =============================================================================

func TestTranslateMovie_StripsVRMarkerFromTitle(t *testing.T) {
	t.Run("bracketed VR tag removed before sending to LLM", func(t *testing.T) {
		var requestBody string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			requestBody = string(body)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<title>>>\nTranslated Title",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{Title: "【8K VR】素敵なタイトル"}
		result, _, err := s.TranslateMovie(context.Background(), movie, "")

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(t, requestBody, "素敵なタイトル")
		assert.NotContains(t, requestBody, "8K VR")
		assert.Equal(t, "Translated Title", movie.Title)
	})

	t.Run("title that is only a VR tag skips translation", func(t *testing.T) {
		llmCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			llmCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{Title: "[VR]"}
		_, _, err := s.TranslateMovie(context.Background(), movie, "")

		require.NoError(t, err)
		assert.False(t, llmCalled)
		assert.Equal(t, "[VR]", movie.Title)
	})
}

// =============================================================================
// TranslateMovie tests - title == actress name → title_as_name transliteration
// =============================================================================

func TestTranslateMovie_TitleIsActressName(t *testing.T) {
	newTitleAsNameServer := func(t *testing.T, capture *string) *httptest.Server {
		t.Helper()
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if capture != nil {
				*capture = string(body)
			}
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "<<<title_as_name>>>\n마히루"}},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
	}

	t.Run("title matches actress JapaneseName - queued as title_as_name with romaji input", func(t *testing.T) {
		var requestBody string
		server := newTitleAsNameServer(t, &requestBody)
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Title: "まひる",
			Actresses: []models.Actress{
				{
					JapaneseName: "まひる",
					FirstName:    "Mahiru", // provided by r18dev name_romaji
				},
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)

		// Romaji (correct reading) is sent under the title_as_name label
		assert.Contains(t, requestBody, "title_as_name")
		assert.Contains(t, requestBody, "Mahiru")

		assert.Equal(t, "마히루", result[0].Title, "record title should be the transliterated result")
		assert.Equal(t, "마히루", movie.Title, "primary title should be updated via ApplyToPrimary")
	})

	t.Run("title matches actress JapaneseName via ThumbURL - romaji from URL used as input", func(t *testing.T) {
		var requestBody string
		server := newTitleAsNameServer(t, &requestBody)
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Title: "双葉れぇな",
			Actresses: []models.Actress{
				{
					JapaneseName: "双葉れぇな",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hutaba_reena.jpg",
				},
			},
		}

		_, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)

		assert.Contains(t, requestBody, "title_as_name")
		assert.Contains(t, requestBody, "Futaba Reena")
	})

	t.Run("title matches actress without romaji - Japanese name sent as title_as_name", func(t *testing.T) {
		var requestBody string
		server := newTitleAsNameServer(t, &requestBody)
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Title: "なつ",
			Actresses: []models.Actress{
				{JapaneseName: "なつ"}, // no romaji available anywhere
			},
		}

		_, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)

		assert.Contains(t, requestBody, "title_as_name")
		assert.Contains(t, requestBody, "なつ")
	})

	t.Run("title does not match any actress - normal title label", func(t *testing.T) {
		var capturedRequest struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedRequest)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "<<<title>>>\n번역된 제목"}},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Title: "素晴らしい夜",
			Actresses: []models.Actress{
				{JapaneseName: "田中香", FirstName: "Yui", LastName: "Tanaka"},
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)

		require.Len(t, capturedRequest.Messages, 2)
		userPrompt := capturedRequest.Messages[1].Content
		assert.Contains(t, userPrompt, "<<<title>>>\n素晴らしい夜")
		assert.NotContains(t, userPrompt, "<<<title_as_name>>>")
		assert.Equal(t, "번역된 제목", result[0].Title, "non-name title should go through the normal title label")
	})
}

func TestBuildLLMTranslationPrompts_Rules(t *testing.T) {
	t.Run("prompt includes person-name transliteration rule", func(t *testing.T) {
		systemPrompt, _, _, err := buildLLMTranslationPrompts("ja", "ko", []string{"なつ"}, []string{"title"})
		require.NoError(t, err)

		assert.Contains(t, systemPrompt, "Person-name rule")
		assert.Contains(t, systemPrompt, "<<<actress[N]>>>")
		assert.Contains(t, systemPrompt, "<<<title_as_name>>>")
		assert.Contains(t, systemPrompt, "Transliterate it phonetically")
		assert.Contains(t, systemPrompt, "never translate its meaning")
		assert.Contains(t, systemPrompt, "なつ → 나츠")
		assert.Contains(t, systemPrompt, "夏 → 나츠")
		assert.Contains(t, systemPrompt, "NOT 여름")
		assert.Contains(t, systemPrompt, "FamilyName GivenName")
		assert.Contains(t, systemPrompt, "short personal-name-like Japanese string")
		// Romaji is the authoritative reading — no "correcting" to a known reading
		assert.Contains(t, systemPrompt, "AUTHORITATIVE reading")
		assert.Contains(t, systemPrompt, "NEVER substitute a different reading")
		assert.Contains(t, systemPrompt, "Rena → 레나 (NOT 레이나)")
		// Output script rule: echoing the romaji input back is an error
		assert.Contains(t, systemPrompt, "returning the romaji/Latin input unchanged is an ERROR")
		assert.Contains(t, systemPrompt, "Miyashita Rena → 미야시타 레나")
	})

	t.Run("prompt includes proper-noun rule for maker/label/director", func(t *testing.T) {
		systemPrompt, _, _, err := buildLLMTranslationPrompts("ja", "ko", []string{"アタッカーズ"}, []string{"maker"})
		require.NoError(t, err)

		assert.Contains(t, systemPrompt, "Proper-noun rule")
		assert.Contains(t, systemPrompt, "<<<maker>>>")
		assert.Contains(t, systemPrompt, "<<<label>>>")
		assert.Contains(t, systemPrompt, "<<<director>>>")
		assert.Contains(t, systemPrompt, "do NOT embellish them into marketing copy")
	})

	t.Run("errors when field names are missing", func(t *testing.T) {
		_, _, _, err := buildLLMTranslationPrompts("ja", "ko", []string{"夏"}, nil)
		require.Error(t, err)
	})

	t.Run("errors when texts are empty", func(t *testing.T) {
		_, _, _, err := buildLLMTranslationPrompts("ja", "ko", nil, nil)
		require.Error(t, err)
	})
}

// =============================================================================
// TranslateMovie tests - actress names translated in the main batch
// =============================================================================

func TestTranslateMovie_ActressInMainBatch(t *testing.T) {
	t.Run("URL-extracted romaji is sent to LLM and Hangul result applied to primary", func(t *testing.T) {
		var requestBody string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			requestBody = string(body)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<actress[0]>>>\n후타바 레에나",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Actresses: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Actresses: []models.Actress{
				{
					JapaneseName: "双葉れぇな",
					FirstName:    "Futabarena", // wrong single-word value from scraper
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hutaba_reena.jpg",
				},
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)

		// Romaji from the URL (correct reading) is the LLM input, not the Japanese name
		assert.Contains(t, requestBody, "Futaba Reena")
		assert.Contains(t, requestBody, "actress[0]")

		// Hangul LLM result is applied to the primary actress
		assert.Equal(t, "후타바", movie.Actresses[0].LastName)
		assert.Equal(t, "레에나", movie.Actresses[0].FirstName)
		assert.Equal(t, "双葉れぇな", movie.Actresses[0].JapaneseName)

		// Record carries the Hangul name under the metadata target language
		require.Len(t, result[0].Actresses, 1)
		assert.Equal(t, "ko", result[0].Language)
		assert.Equal(t, "후타바 레에나", result[0].Actresses[0])
	})

	t.Run("primary not modified when ApplyToPrimary is false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<actress[0]>>>\n후타바 레에나",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			SourceLanguage: "ja",
			ApplyToPrimary: false,
			Fields: config.TranslationFieldsConfig{
				Actresses: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Actresses: []models.Actress{
				{
					JapaneseName: "双葉れぇな",
					FirstName:    "Futabarena",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hutaba_reena.jpg",
				},
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)

		// Primary must NOT be modified when ApplyToPrimary is false
		assert.Equal(t, "Futabarena", movie.Actresses[0].FirstName)

		// Record still carries the translated name
		require.Len(t, result[0].Actresses, 1)
		assert.Equal(t, "후타바 레에나", result[0].Actresses[0])
	})

	t.Run("actress without any source name is skipped", func(t *testing.T) {
		llmCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			llmCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			SourceLanguage: "ja",
			Fields: config.TranslationFieldsConfig{
				Actresses: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Actresses: []models.Actress{
				{FirstName: "Already", LastName: "Latin"}, // no JapaneseName, no usable ThumbURL
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		assert.False(t, llmCalled)
		assert.Nil(t, result)
	})

	t.Run("unknown actress placeholder is not sent to LLM", func(t *testing.T) {
		llmCalled := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			llmCalled = true
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Actresses: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Actresses: []models.Actress{
				{FirstName: "Unknown", JapaneseName: "Unknown"},
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		assert.False(t, llmCalled)
		assert.Equal(t, "Unknown", movie.Actresses[0].FirstName)
		assert.Empty(t, movie.Actresses[0].LastName)
		assert.Equal(t, "Unknown", movie.Actresses[0].JapaneseName)
		require.Len(t, result, 1)
		require.Len(t, result[0].Actresses, 1)
		assert.Equal(t, "Unknown", result[0].Actresses[0])
	})

	t.Run("translated unknown actress result is canonicalized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<actress[0]>>>\n미상",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		cfg := config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Actresses: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "key",
			},
		}

		s := New(cfg)
		movie := &models.Movie{
			Actresses: []models.Actress{
				{
					JapaneseName: "双葉れぇな",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hutaba_reena.jpg",
				},
			},
		}

		result, _, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		assert.Equal(t, "Unknown", movie.Actresses[0].FirstName)
		assert.Empty(t, movie.Actresses[0].LastName)
		assert.Equal(t, "Unknown", movie.Actresses[0].JapaneseName)
		require.Len(t, result, 1)
		require.Len(t, result[0].Actresses, 1)
		assert.Equal(t, "Unknown", result[0].Actresses[0])
	})
}

// =============================================================================
// TranslateMovie tests - Hangul validation of person-name slots (ko target)
// =============================================================================

func TestTranslateMovie_KoreanPersonNameHangulValidation(t *testing.T) {
	markerRE := regexp.MustCompile(`<<<[\w\[\]]+>>>`)

	// requestMarkers returns the unique markers of the request's user prompt, in order.
	requestMarkers := func(r *http.Request) []string {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) == 0 {
			return nil
		}
		seen := map[string]bool{}
		var out []string
		for _, m := range markerRE.FindAllString(req.Messages[len(req.Messages)-1].Content, -1) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
		return out
	}

	respond := func(w http.ResponseWriter, content string) {
		response := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": content}},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}

	newConfig := func(baseURL string) config.TranslationConfig {
		return config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "ko",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title:     true,
				Actresses: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: baseURL,
				APIKey:  "key",
			},
		}
	}

	newMovie := func(title string) *models.Movie {
		return &models.Movie{
			Title: title,
			Actresses: []models.Actress{
				{
					JapaneseName: "双葉れぇな",
					ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/hutaba_reena.jpg",
				},
			},
		}
	}

	t.Run("romaji echo is retried per slot and Hangul applied", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var b strings.Builder
			for _, m := range requestMarkers(r) {
				switch {
				case m == "<<<title>>>":
					b.WriteString(m + "\n멋진 작품\n")
				case strings.HasPrefix(m, "<<<actress["):
					if calls == 1 {
						b.WriteString(m + "\nFutaba Reena\n") // batch echoes the romaji input
					} else {
						b.WriteString(m + "\n후타바 레에나\n")
					}
				}
			}
			respond(w, b.String())
		}))
		defer server.Close()

		s := New(newConfig(server.URL))
		movie := newMovie("素敵な作品")

		result, warning, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, warning)
		assert.Equal(t, 2, calls) // batch + one per-slot retry

		assert.Equal(t, "멋진 작품", movie.Title)
		assert.Equal(t, "후타바", movie.Actresses[0].LastName)
		assert.Equal(t, "레에나", movie.Actresses[0].FirstName)
		require.Len(t, result[0].Actresses, 1)
		assert.Equal(t, "후타바 레에나", result[0].Actresses[0])
	})

	t.Run("persistent romaji echo keeps source name and warns", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var b strings.Builder
			for _, m := range requestMarkers(r) {
				switch {
				case m == "<<<title>>>":
					b.WriteString(m + "\n멋진 작품\n")
				case strings.HasPrefix(m, "<<<actress["):
					b.WriteString(m + "\nFutaba Reena\n") // always echoes romaji
				}
			}
			respond(w, b.String())
		}))
		defer server.Close()

		s := New(newConfig(server.URL))
		movie := newMovie("素敵な作品")

		result, warning, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 2, calls) // batch + one per-slot retry
		assert.Contains(t, warning, "actress[0]: LLM returned non-Hangul, kept romaji")

		// The title still translates; the actress falls back to the URL romaji.
		assert.Equal(t, "멋진 작품", movie.Title)
		assert.Equal(t, "Futaba", movie.Actresses[0].LastName)
		assert.Equal(t, "Reena", movie.Actresses[0].FirstName)
		require.Len(t, result[0].Actresses, 1)
		assert.Equal(t, "Futaba Reena", result[0].Actresses[0])
	})

	t.Run("title_as_name slot is validated for Hangul too", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var b strings.Builder
			for _, m := range requestMarkers(r) {
				if m == "<<<title_as_name>>>" || strings.HasPrefix(m, "<<<actress[") {
					if calls == 1 {
						b.WriteString(m + "\nFutaba Reena\n")
					} else {
						b.WriteString(m + "\n후타바 레에나\n")
					}
				}
			}
			respond(w, b.String())
		}))
		defer server.Close()

		s := New(newConfig(server.URL))
		movie := newMovie("双葉れぇな") // title equals the actress JapaneseName

		result, warning, err := s.TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, warning)
		assert.Equal(t, 3, calls) // batch + one retry per echoed slot

		assert.Equal(t, "후타바 레에나", movie.Title)
		assert.Equal(t, "후타바", movie.Actresses[0].LastName)
		assert.Equal(t, "레에나", movie.Actresses[0].FirstName)
	})
}

// =============================================================================
// translateTexts tests - connection failure handling
// =============================================================================

func TestTranslateTexts_ConnectionFailure(t *testing.T) {
	t.Run("openai connection refused", func(t *testing.T) {
		// Create a test server that immediately closes to force connection refused
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.Close() // Close immediately to make connection fail

		cfg := config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

		require.Error(t, err)
		// Verify it's a connection-related error (not HTTP status error)
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "dial tcp") ||
			strings.Contains(errMsg, "connection reset"),
			"expected connection error, got: %v", errMsg)
	})

	t.Run("deepl connection refused", func(t *testing.T) {
		// Create a test server that immediately closes to force connection refused
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		server.Close() // Close immediately to make connection fail

		cfg := config.TranslationConfig{
			Provider:       "deepl",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			DeepL: config.DeepLTranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		}

		s := New(cfg)
		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

		require.Error(t, err)
		// Verify it's a connection-related error
		errMsg := err.Error()
		assert.True(t, strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "dial tcp") ||
			strings.Contains(errMsg, "connection reset"),
			"expected connection error, got: %v", errMsg)
	})
}

// =============================================================================
// translateTexts tests - provider dispatch
// =============================================================================

func TestTranslateTexts_Dispatch(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name:     "openai provider",
			provider: "openai",
			cfg: config.TranslationConfig{
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: func() string {
						// Create a test server that immediately closes to force connection refused
						s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusOK)
						}))
						// Close the server immediately so the URL becomes unreachable
						s.Close()
						return s.URL
					}(),
					APIKey: "test-key",
				},
			},
			wantErr: true, // Error due to connection failure
		},
		{
			name:     "deepl provider",
			provider: "deepl",
			cfg: config.TranslationConfig{
				Provider:       "deepl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					BaseURL: func() string {
						// Create a test server that immediately closes to force connection refused
						s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							w.WriteHeader(http.StatusOK)
						}))
						// Close the server immediately so the URL becomes unreachable
						s.Close()
						return s.URL
					}(),
					APIKey: "test-key",
				},
			},
			wantErr: true, // Error due to connection failure
		},
		{
			name:     "google paid mode requires api key",
			provider: "google",
			cfg: config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:   "paid",
					APIKey: "",
				},
			},
			wantErr:     true,
			errContains: "google api_key is required for paid mode",
		},
		{
			name:     "unsupported provider",
			provider: "custom",
			cfg: config.TranslationConfig{
				Provider:       "custom",
				TargetLanguage: "en",
				SourceLanguage: "ja",
			},
			wantErr:     true,
			errContains: "unsupported translation provider",
		},
		{
			name:     "uppercase provider",
			provider: "OPENAI",
			cfg: config.TranslationConfig{
				Provider:       "OPENAI",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true,
		},
		{
			name:     "mixed case provider",
			provider: "DeePl",
			cfg: config.TranslationConfig{
				Provider:       "DeePl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg)

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// translateWithDeepL tests
// =============================================================================

func TestTranslateWithDeepL(t *testing.T) {
	tests := []struct {
		name            string
		mode            string // "free" or "pro"
		handler         func(http.ResponseWriter, *http.Request)
		wantErr         bool
		errContains     string
		expectCount     int // Expected number of results (default 1)
		validateRequest func(t *testing.T, r *http.Request)
	}{
		{
			name: "free mode success",
			mode: "free",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"translations": []map[string]string{
						{"text": "translated text"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
			validateRequest: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "DeepL-Auth-Key test-key", r.Header.Get("Authorization"))

				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "EN", body["target_lang"])
				assert.Equal(t, "JA", body["source_lang"])
			},
		},
		{
			name: "pro mode success",
			mode: "pro",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"translations": []map[string]string{
						{"text": "translated text"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
			validateRequest: func(t *testing.T, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "DeepL-Auth-Key test-key", r.Header.Get("Authorization"))

				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "EN", body["target_lang"])
			},
		},
		{
			name: "API returns error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Forbidden"))
			},
			wantErr:     true,
			errContains: "deepl translation failed",
		},
		{
			name: "malformed JSON response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			wantErr:     true,
			errContains: "failed to decode deepl response",
		},
		{
			name: "multiple texts translated",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"translations": []map[string]string{
						{"text": "first"},
						{"text": "second"},
						{"text": "third"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     false,
			expectCount: 3,
			validateRequest: func(t *testing.T, r *http.Request) {
				var body map[string]interface{}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				texts, ok := body["text"].([]interface{})
				require.True(t, ok)
				assert.Len(t, texts, 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v2/translate", func(w http.ResponseWriter, r *http.Request) {
				if tt.validateRequest != nil {
					tt.validateRequest(t, r)
				}
				tt.handler(w, r)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			mode := tt.mode
			if mode == "" {
				mode = "free"
			}
			cfg := config.TranslationConfig{
				Provider:       "deepl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					Mode:    mode,
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			}

			s := New(cfg)
			inputTexts := []string{"test"}
			if tt.name == "multiple texts translated" {
				inputTexts = []string{"test1", "test2", "test3"}
			}
			result, err := s.translateTexts(context.Background(), "ja", "en", inputTexts, fieldNamesFor(inputTexts))

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				expectedCount := 1
				if tt.expectCount > 0 {
					expectedCount = tt.expectCount
				}
				assert.Len(t, result, expectedCount)
				if expectedCount == 1 {
					assert.Equal(t, "translated text", result[0])
				}
			}
		})
	}
}

func TestTranslateWithDeepL_MissingAPIKey(t *testing.T) {
	s := New(config.TranslationConfig{
		Provider:       "deepl",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		DeepL: config.DeepLTranslationConfig{
			Mode:    "free",
			BaseURL: "http://example.com",
			APIKey:  "",
		},
	})

	_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deepl api_key is required")
}

func TestTranslateWithDeepL_SourceLanguage(t *testing.T) {
	var capturedSourceLang string

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/translate", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if sl, ok := body["source_lang"].(string); ok {
			capturedSourceLang = sl
		}
		response := map[string]interface{}{
			"translations": []map[string]string{
				{"text": "translated"},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	s := New(config.TranslationConfig{
		Provider:       "deepl",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		DeepL: config.DeepLTranslationConfig{
			Mode:    "free",
			BaseURL: server.URL,
			APIKey:  "test-key",
		},
	})

	result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
	require.NoError(t, err)
	assert.Equal(t, "JA", capturedSourceLang)
	assert.Len(t, result, 1)
}

// =============================================================================
// translateWithGoogle tests
// =============================================================================

func TestTranslateWithGoogle(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		handler     func(http.ResponseWriter, *http.Request)
		wantErr     bool
		errContains string
	}{
		{
			name: "free mode success",
			mode: "free",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Google free API returns nested array format: [[[translated_text, null, ...]]]
				response := []any{
					[]any{
						[]any{"translated text", nil, "en", nil, nil, nil, "gtx"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
		},
		{
			name: "paid mode success",
			mode: "paid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"translations": []map[string]string{
							{"translatedText": "translated text"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr: false,
		},
		{
			name: "missing API key for paid mode",
			mode: "paid",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Server won't be called
			},
			wantErr:     true,
			errContains: "google api_key is required for paid mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			if tt.handler != nil {
				// Google free uses /translate_a/single, paid uses /language/translate/v2
				if tt.mode == "free" {
					mux.HandleFunc("/translate_a/single", tt.handler)
				} else {
					mux.HandleFunc("/language/translate/v2", tt.handler)
				}
			}

			server := httptest.NewServer(mux)
			defer server.Close()

			cfg := config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:    tt.mode,
					BaseURL: server.URL,
					APIKey: func() string {
						if tt.name == "missing API key for paid mode" {
							return ""
						}
						return "test-key"
					}(),
				},
			}

			s := New(cfg)
			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
				assert.Equal(t, "translated text", result[0])
			}
		})
	}
}

// =============================================================================
// translateWithOpenAI tests
// =============================================================================

func TestTranslateWithOpenAI(t *testing.T) {
	t.Run("successful translation", func(t *testing.T) {
		var capturedBody map[string]interface{}
		var capturedAuthHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify endpoint
			assert.Equal(t, "/chat/completions", r.URL.Path)
			// Verify auth header
			capturedAuthHeader = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<title>>>\ntranslated text",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "translated text", result[0])
		assert.Equal(t, "gpt-4o-mini", capturedBody["model"])
		// Verify request structure
		messages := capturedBody["messages"].([]interface{})
		assert.Len(t, messages, 2)
		assert.Equal(t, "system", messages[0].(map[string]interface{})["role"])
		assert.Equal(t, "user", messages[1].(map[string]interface{})["role"])
		// Verify auth header format
		assert.Equal(t, "Bearer test-key", capturedAuthHeader)
	})

	t.Run("API returns error status", func(t *testing.T) {
		var capturedAuthHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify endpoint
			assert.Equal(t, "/chat/completions", r.URL.Path)
			// Verify auth header
			capturedAuthHeader = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Invalid API key"))
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "openai translation failed")
		assert.Contains(t, err.Error(), "Invalid API key")
		// Verify auth header format
		assert.Equal(t, "Bearer test-key", capturedAuthHeader)
	})

	t.Run("malformed JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not valid json"))
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode openai response")
	})

	t.Run("empty choices in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "openai response contained no choices")
	})

	t.Run("invalid JSON in content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "not a json array",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse translated output payload")
	})
}

func TestTranslateWithOpenAI_DefaultValues(t *testing.T) {
	t.Run("default model when not specified", func(t *testing.T) {
		var capturedBody map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedBody)
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "<<<title>>>\ntranslated",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
				Model:   "",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "gpt-4o-mini", capturedBody["model"])
	})
}

func TestTranslateWithGooglePaid(t *testing.T) {
	tests := []struct {
		name            string
		handler         func(http.ResponseWriter, *http.Request)
		validateRequest func(t *testing.T, r *http.Request)
		wantErr         bool
		errContains     string
	}{
		{
			name: "successful paid translation",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"translations": []map[string]string{
							{"translatedText": "translated text"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
				// Verify key query parameter
				assert.Contains(t, r.URL.RawQuery, "key=")
			},
			wantErr: false,
		},
		{
			name: "API returns error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Unauthorized"))
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
			},
			wantErr:     true,
			errContains: "google paid translation failed",
		},
		{
			name: "malformed JSON response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
			},
			wantErr:     true,
			errContains: "failed to decode google paid response",
		},
		{
			name: "HTML entity unescaping",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"data": map[string]interface{}{
						"translations": []map[string]string{
							{"translatedText": "&lt;hello&gt;"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			validateRequest: func(t *testing.T, r *http.Request) {
				// Verify endpoint
				assert.Equal(t, "/language/translate/v2", r.URL.Path)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/language/translate/v2", func(w http.ResponseWriter, r *http.Request) {
				if tt.validateRequest != nil {
					tt.validateRequest(t, r)
				}
				tt.handler(w, r)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:    "paid",
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
			}
		})
	}
}

func TestTranslateWithGooglePaid_EndpointAndAuth(t *testing.T) {
	var capturedKey string
	mux := http.NewServeMux()
	mux.HandleFunc("/language/translate/v2", func(w http.ResponseWriter, r *http.Request) {
		// Extract key from query string
		capturedKey = r.URL.Query().Get("key")
		// Verify endpoint path
		assert.Equal(t, "/language/translate/v2", r.URL.Path)
		response := map[string]interface{}{
			"data": map[string]interface{}{
				"translations": []map[string]string{
					{"translatedText": "translated text"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	s := New(config.TranslationConfig{
		Provider:       "google",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		Google: config.GoogleTranslationConfig{
			Mode:    "paid",
			BaseURL: server.URL,
			APIKey:  "test-key",
		},
	})

	result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "translated text", result[0])
	// Verify key query parameter contains the API key
	assert.Equal(t, "test-key", capturedKey)
}

func TestTranslateWithGoogleFree(t *testing.T) {
	t.Run("successful free translation", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			// Verify query parameters directly in handler
			assert.Equal(t, "gtx", r.URL.Query().Get("client"))
			assert.Equal(t, "ja", r.URL.Query().Get("sl"))
			assert.Equal(t, "en", r.URL.Query().Get("tl"))
			// Google free API returns nested array: [[[translated_text, null, "en", ...]]]
			response := []any{
				[]any{
					[]any{"translated text", nil, "en", nil, nil, nil, "gtx"},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "translated text", result[0])
	})

	t.Run("API returns error status", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google free translation failed")
		assert.Contains(t, err.Error(), "Internal Server Error")
	})

	t.Run("multiple texts translated sequentially", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{
				[]any{
					[]any{"translated", nil, "en", nil, nil, nil, "gtx"},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test1", "test2"}, []string{"title", "description"})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("source language auto when empty", func(t *testing.T) {
		var capturedSL string
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			capturedSL = r.URL.Query().Get("sl")
			response := []any{
				[]any{
					[]any{"translated", nil, "en", nil, nil, nil, "gtx"},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "", // Empty should become "auto"
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Equal(t, "auto", capturedSL)
	})

	t.Run("malformed response - empty array", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected google free response shape")
	})

	t.Run("malformed response - nested array without segments", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{
				[]any{},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google free translation returned empty text")
	})

	t.Run("malformed response - non-string segment", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/translate_a/single", func(w http.ResponseWriter, r *http.Request) {
			response := []any{
				[]any{
					[]any{123, nil, "en"}, // Number instead of string
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider:       "google",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			Google: config.GoogleTranslationConfig{
				Mode:    "free",
				BaseURL: server.URL,
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "google free translation returned empty text")
	})
}

// =============================================================================
// TranslateMovie full flow tests
// =============================================================================

func TestTranslateMovie_FullFlow(t *testing.T) {
	tests := []struct {
		name                string
		cfg                 config.TranslationConfig
		movie               *models.Movie
		mockFieldNames      []string
		mockResponse        []string
		wantErr             bool
		wantTitle           string
		wantPrimarySet      bool
		wantTranslatedCount int
	}{
		{
			name: "happy_path_translates_and_updates_fields",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "テスト",
			},
			mockFieldNames:      []string{"title"},
			mockResponse:        []string{"Translated Title"},
			wantErr:             false,
			wantTitle:           "Translated Title",
			wantPrimarySet:      true,
			wantTranslatedCount: 1,
		},
		{
			name: "apply_to_primary_false_does_not_modify_movie",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: false,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "テスト",
			},
			mockFieldNames:      []string{"title"},
			mockResponse:        []string{"Translated Title"},
			wantErr:             false,
			wantTitle:           "Translated Title",
			wantPrimarySet:      false,
			wantTranslatedCount: 1,
		},
		{
			name: "translates_multiple_fields",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title:       true,
					Description: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title:       "テスト",
				Description: "説明",
			},
			mockFieldNames:      []string{"title", "description"},
			mockResponse:        []string{"Translated Title", "Translated Description"},
			wantErr:             false,
			wantTitle:           "Translated Title",
			wantPrimarySet:      true,
			wantTranslatedCount: 2,
		},
		{
			name: "translates_actresses",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Actresses: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "Test",
				Actresses: []models.Actress{
					{JapaneseName: "田中香", FirstName: "Yui", LastName: "Tanaka"},
				},
			},
			mockFieldNames:      []string{"actress[0]"},
			mockResponse:        []string{"Yui Tanaka"},
			wantErr:             false,
			wantPrimarySet:      true,
			wantTranslatedCount: 1,
		},
		{
			name: "translates_genres",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Genres: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "Test",
				Genres: []models.Genre{
					{Name: "ジャンル 1"},
					{Name: "ジャンル 2"},
				},
			},
			mockFieldNames:      []string{"genre[0]", "genre[1]"},
			mockResponse:        []string{"Genre 1", "Genre 2"},
			wantErr:             false,
			wantPrimarySet:      true,
			wantTranslatedCount: 2,
		},
		{
			name: "empty_fields_are_skipped",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "",
			},
			wantErr:        false,
			wantPrimarySet: false,
		},
		{
			name: "provider_error_returns_error",
			cfg: config.TranslationConfig{
				Enabled:        true,
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				ApplyToPrimary: true,
				Fields: config.TranslationFieldsConfig{
					Title: true,
				},
				OpenAI: config.OpenAITranslationConfig{
					APIKey: "key",
				},
			},
			movie: &models.Movie{
				Title: "テスト",
			},
			mockResponse:   nil,
			wantErr:        true,
			wantPrimarySet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var server *httptest.Server

			if tt.mockResponse == nil {
				// Error case - return 500
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte("Internal Server Error"))
				}))
			} else {
				// Success case - return mock response
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					response := map[string]interface{}{
						"choices": []map[string]interface{}{
							{
								"message": map[string]string{
									"content": markerContent(tt.mockFieldNames, tt.mockResponse),
								},
							},
						},
					}
					_ = json.NewEncoder(w).Encode(response)
				}))
			}
			defer server.Close()

			tt.cfg.OpenAI.BaseURL = server.URL
			s := New(tt.cfg)

			movieCopy := &models.Movie{}
			*movieCopy = *tt.movie
			if len(tt.movie.Actresses) > 0 {
				movieCopy.Actresses = make([]models.Actress, len(tt.movie.Actresses))
				copy(movieCopy.Actresses, tt.movie.Actresses)
			}
			if len(tt.movie.Genres) > 0 {
				movieCopy.Genres = make([]models.Genre, len(tt.movie.Genres))
				copy(movieCopy.Genres, tt.movie.Genres)
			}

			originalTitle := movieCopy.Title

			result, _, err := s.TranslateMovie(context.Background(), movieCopy, "")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.wantTranslatedCount > 0 {
				require.NotNil(t, result)
			}

			if tt.wantTitle != "" {
				assert.Equal(t, tt.wantTitle, result[0].Title)
			}

			// Verify that the appropriate fields were translated based on config
			if tt.cfg.Fields.Title {
				if tt.wantPrimarySet {
					assert.NotEqual(t, originalTitle, movieCopy.Title)
				} else {
					assert.Equal(t, originalTitle, movieCopy.Title)
				}
			}

			// Verify actresses translation if configured.
			// replaceActressName splits "FamilyName GivenName" into LastName + FirstName;
			// JapaneseName is preserved.
			if tt.cfg.Fields.Actresses && len(tt.movie.Actresses) > 0 && tt.wantPrimarySet {
				a := movieCopy.Actresses[0]
				assert.NotEmpty(t, a.FirstName, "translated actress given name should be in FirstName")
				assert.NotEmpty(t, a.LastName, "translated actress family name should be in LastName")
				assert.Equal(t, tt.movie.Actresses[0].JapaneseName, a.JapaneseName, "JapaneseName should be preserved after translation")
			}

			// Verify genres translation if configured
			if tt.cfg.Fields.Genres && len(tt.movie.Genres) > 0 {
				for i := range tt.movie.Genres {
					assert.NotEqual(t, tt.movie.Genres[i].Name, movieCopy.Genres[i].Name)
				}
			}
		})
	}
}

// =============================================================================
// Translation count mismatch tests
// =============================================================================

func TestTranslateMovie_MissingMarkerFallback(t *testing.T) {
	// The mock echoes back every marker requested in the user prompt, except the
	// ones listed in omit. A missing marker causes a parse error on the batch,
	// then the one-by-one fallback retries each field individually.
	newEchoServer := func(omit map[string]bool) *httptest.Server {
		markerRE := regexp.MustCompile(`<<<[\w\[\]]+>>>`)
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			var b strings.Builder
			seen := map[string]bool{}
			if len(req.Messages) > 0 {
				user := req.Messages[len(req.Messages)-1].Content
				for _, m := range markerRE.FindAllString(user, -1) {
					if seen[m] || omit[m] {
						continue
					}
					seen[m] = true
					b.WriteString(m + "\nTranslated\n")
				}
			}
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": b.String()}},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
	}

	newService := func(baseURL string) *Service {
		return New(config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title:       true,
				Description: true,
				Director:    true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: baseURL,
				APIKey:  "test-key",
			},
		})
	}

	t.Run("all markers present succeeds", func(t *testing.T) {
		server := newEchoServer(nil)
		defer server.Close()

		movie := &models.Movie{Title: "Test Title", Description: "Test Description", Director: "Test Director"}
		_, _, err := newService(server.URL).TranslateMovie(context.Background(), movie, "")
		require.NoError(t, err)
		assert.Equal(t, "Translated", movie.Title)
		assert.Equal(t, "Translated", movie.Director)
	})

	t.Run("persistently missing marker returns parse error", func(t *testing.T) {
		server := newEchoServer(map[string]bool{"<<<director>>>": true})
		defer server.Close()

		movie := &models.Movie{Title: "Test Title", Description: "Test Description", Director: "Test Director"}
		_, _, err := newService(server.URL).TranslateMovie(context.Background(), movie, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse translated output payload")
	})
}

func TestTranslateTexts_RetryOnLLMParseFailure(t *testing.T) {
	t.Run("openai-compatible retries and succeeds after unparseable response", func(t *testing.T) {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			content := "no markers here"
			if requestCount == 2 {
				content = "<<<title>>>\ntranslated"
			}
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": content,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider: "openai-compatible",
			OpenAICompatible: config.OpenAICompatibleTranslationConfig{
				BaseURL: server.URL,
				Model:   "test-model",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Equal(t, []string{"translated"}, result)
		assert.Equal(t, 2, requestCount)
	})

	t.Run("openai-compatible stops after max retries", func(t *testing.T) {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": "no markers here",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider: "openai-compatible",
			OpenAICompatible: config.OpenAICompatibleTranslationConfig{
				BaseURL: server.URL,
				Model:   "test-model",
			},
		})

		_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse translated output payload")
		assert.Equal(t, maxTranslationRetries, requestCount)
	})
}

// =============================================================================
// Malformed response tests
// =============================================================================

func TestTranslateWithOpenAI_MalformedResponses(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(http.ResponseWriter, *http.Request)
		wantErr     bool
		errContains string
	}{
		{
			name: "returns_error_for_missing_choices_field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"not_choices": []interface{}{},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "openai response contained no choices",
		},
		{
			name: "returns_error_for_null_choices",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": nil,
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "openai response contained no choices",
		},
		{
			name: "returns_error_for_missing_message_field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"not_message": "data"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "failed to parse translated output payload",
		},
		{
			name: "returns_error_for_empty_content",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": "",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			wantErr:     true,
			errContains: "failed to parse translated output payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// HTTP error response tests
// =============================================================================

func TestTranslateWithOpenAI_HTTPErrorResponses(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		wantErr      bool
		errContains  string
		bodyContains string
	}{
		{
			name:         "returns_error_for_401_unauthorized",
			statusCode:   http.StatusUnauthorized,
			body:         "Invalid API key",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Invalid API key",
		},
		{
			name:         "returns_error_for_429_rate_limit",
			statusCode:   http.StatusTooManyRequests,
			body:         "Rate limit exceeded",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Rate limit exceeded",
		},
		{
			name:         "returns_error_for_500_internal_server_error",
			statusCode:   http.StatusInternalServerError,
			body:         "Internal server error",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Internal server error",
		},
		{
			name:         "returns_error_for_503_service_unavailable",
			statusCode:   http.StatusServiceUnavailable,
			body:         "Service temporarily unavailable",
			wantErr:      true,
			errContains:  "openai translation failed",
			bodyContains: "Service temporarily unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "openai",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAI: config.OpenAITranslationConfig{
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
			if tt.bodyContains != "" {
				assert.Contains(t, err.Error(), tt.bodyContains)
			}
		})
	}
}

// =============================================================================
// Context cancellation tests
// =============================================================================

func TestTranslateMovie_ContextCancellation(t *testing.T) {
	t.Run("cancels_during_request", func(t *testing.T) {
		started := make(chan struct{})
		done := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			// Wait for either context cancellation or done signal
			select {
			case <-r.Context().Done():
				// Client disconnected or context canceled
				return
			case <-done:
			}
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		ctx, cancel := context.WithCancel(context.Background())

		// Start a goroutine to cancel after the request has started
		go func() {
			<-started
			cancel()
		}()

		movie := &models.Movie{Title: "テスト"}
		_, _, err := s.TranslateMovie(ctx, movie, "")

		close(done) // Unblock the server handler
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled) ||
			strings.Contains(err.Error(), "canceled") ||
			strings.Contains(err.Error(), "context"),
			"expected context cancellation error, got: %v", err)
	})

	t.Run("deadline_exceeded", func(t *testing.T) {
		started := make(chan struct{})
		done := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			// Wait for either context cancellation or done signal
			select {
			case <-r.Context().Done():
				// Client disconnected or context canceled
				return
			case <-done:
			}
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Enabled:        true,
			Provider:       "openai",
			TargetLanguage: "en",
			SourceLanguage: "ja",
			ApplyToPrimary: true,
			Fields: config.TranslationFieldsConfig{
				Title: true,
			},
			OpenAI: config.OpenAITranslationConfig{
				BaseURL: server.URL,
				APIKey:  "test-key",
			},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		movie := &models.Movie{Title: "テスト"}
		_, _, err := s.TranslateMovie(ctx, movie, "")

		close(done) // Unblock the server handler
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded) ||
			strings.Contains(err.Error(), "deadline exceeded"),
			"expected deadline exceeded error, got: %v", err)
	})
}

// =============================================================================
// DeepL specific error tests
// =============================================================================

func TestTranslateWithDeepL_ErrorResponses(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "returns_error_for_403_forbidden",
			statusCode:  http.StatusForbidden,
			body:        "Invalid auth key",
			wantErr:     true,
			errContains: "deepl translation failed",
		},
		{
			name:        "returns_error_for_400_bad_request",
			statusCode:  http.StatusBadRequest,
			body:        "Invalid text parameter",
			wantErr:     true,
			errContains: "deepl translation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "deepl",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				DeepL: config.DeepLTranslationConfig{
					Mode:    "free",
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

// =============================================================================
// Google specific error tests
// =============================================================================

func TestTranslateWithGooglePaid_ErrorResponses(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "returns_error_for_401_unauthorized",
			statusCode:  http.StatusUnauthorized,
			body:        `{"error":{"message":"Invalid API key"}}`,
			wantErr:     true,
			errContains: "google paid translation failed",
		},
		{
			name:        "returns_error_for_403_quota_exceeded",
			statusCode:  http.StatusForbidden,
			body:        `{"error":{"message":"Quota exceeded"}}`,
			wantErr:     true,
			errContains: "google paid translation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			s := New(config.TranslationConfig{
				Provider:       "google",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Google: config.GoogleTranslationConfig{
					Mode:    "paid",
					BaseURL: server.URL,
					APIKey:  "test-key",
				},
			})

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			require.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

// =============================================================================
// parseCompactTranslationPayload embedded-marker stripping
// =============================================================================

func TestParseCompactTranslationPayload_EmbeddedMarkers(t *testing.T) {
	t.Run("strips extra markers echoed by LLM from last slot", func(t *testing.T) {
		// LLM outputs description swap + echoes extra markers after last slot
		payload := "<<<JZ_0>>>\n[Korean description - very long]\n<<<JZ_1>>>\n메이\n<<<JZ_2>>>\n<<<JZ_3>>>\n<<<JZ_4>>>"
		got, err := parseCompactTranslationPayload(payload, []string{"<<<JZ_0>>>", "<<<JZ_1>>>", "<<<JZ_2>>>"})
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "[Korean description - very long]", got[0])
		assert.Equal(t, "메이", got[1])
		// slot 2 (last) should be empty after stripping embedded markers
		assert.Equal(t, "", got[2])
	})

	t.Run("strips embedded marker inside slot content", func(t *testing.T) {
		payload := "<<<JZ_0>>>\nhello <<<JZ_0>>> world\n<<<JZ_1>>>\nbye"
		got, err := parseCompactTranslationPayload(payload, []string{"<<<JZ_0>>>", "<<<JZ_1>>>"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "hello  world", got[0])
		assert.Equal(t, "bye", got[1])
	})

	t.Run("clean output is unchanged", func(t *testing.T) {
		payload := "<<<JZ_0>>>\ntitle translation\n<<<JZ_1>>>\ndescription translation"
		got, err := parseCompactTranslationPayload(payload, []string{"<<<JZ_0>>>", "<<<JZ_1>>>"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "title translation", got[0])
		assert.Equal(t, "description translation", got[1])
	})
}

// =============================================================================
// translateWithOpenAICompatible tests
// =============================================================================

func TestTranslateWithOpenAICompatible(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(http.ResponseWriter, *http.Request)
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "success with api key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/chat/completions", r.URL.Path)
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": "<<<title>>>\ntranslated text",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr: false,
		},
		{
			name: "success without api key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "", r.Header.Get("Authorization"))
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": "<<<title>>>\ntranslated",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: false,
		},
		{
			name: "missing model returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Should not be called
				t.Error("handler should not be called")
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "openai-compatible model is required",
		},
		{
			name: "upstream error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Invalid key"))
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr:     true,
			errContains: "openai-compatible translation failed",
		},
		{
			name: "malformed json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr:     true,
			errContains: "failed to decode openai-compatible response",
		},
		{
			name: "empty choices in response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					APIKey: "test-key",
					Model:  "llama3",
				},
			},
			wantErr:     true,
			errContains: "openai-compatible response contained no choices",
		},
		{
			name: "uses default base url when empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": "<<<title>>>\ntranslated",
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr && tt.errContains == "openai-compatible model is required" {
				s := New(tt.cfg)
				_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			tt.cfg.OpenAICompatible.BaseURL = server.URL
			s := New(tt.cfg)

			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
			}
		})
	}
}

func TestTranslateWithOpenAICompatible_UsesCompactMarkerPromptAndResponse(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		response := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `<<<title>>>
Karen
<<<description>>>
She says "It's forceful..." but looks happy while being teased.
`,
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	s := New(config.TranslationConfig{
		Provider:       "openai-compatible",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		OpenAICompatible: config.OpenAICompatibleTranslationConfig{
			BaseURL: server.URL,
			Model:   "llama3",
		},
	})

	result, err := s.translateTexts(context.Background(), "ja", "en", []string{"かれん", "強引って言いながら嬉しそう"}, []string{"title", "description"})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"Karen",
		`She says "It's forceful..." but looks happy while being teased.`,
	}, result)

	messages := capturedBody["messages"].([]interface{})
	require.Len(t, messages, 2)

	systemPrompt := messages[0].(map[string]interface{})["content"].(string)
	userPrompt := messages[1].(map[string]interface{})["content"].(string)

	assert.Contains(t, systemPrompt, "Do not use JSON")
	assert.Contains(t, userPrompt, "Translate each labeled section below:")
	assert.Contains(t, userPrompt, "<<<title>>>")
	assert.Contains(t, userPrompt, "<<<description>>>")
	assert.NotContains(t, userPrompt, "<<<JZ_0>>>")
}

func TestTranslateWithOpenAICompatible_ThinkingControls(t *testing.T) {
	t.Run("vllm uses chat_template_kwargs", func(t *testing.T) {
		var capturedBody map[string]interface{}
		thinkingEnabled := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `<<<title>>>
translated
`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider: "openai-compatible",
			OpenAICompatible: config.OpenAICompatibleTranslationConfig{
				BaseURL:        server.URL,
				Model:          "test-model",
				EnableThinking: &thinkingEnabled,
				BackendType:    "vllm",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Equal(t, []string{"translated"}, result)

		assert.NotContains(t, capturedBody, "reasoning_effort")
		assert.NotContains(t, capturedBody, "enable_thinking")
		kwargs := capturedBody["chat_template_kwargs"].(map[string]interface{})
		assert.Equal(t, false, kwargs["enable_thinking"])
		assert.Equal(t, false, kwargs["thinking"])
	})

	t.Run("ollama uses reasoning_effort", func(t *testing.T) {
		var capturedBody map[string]interface{}
		thinkingEnabled := true

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `<<<title>>>
translated
`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider: "openai-compatible",
			OpenAICompatible: config.OpenAICompatibleTranslationConfig{
				BaseURL:        server.URL,
				Model:          "test-model",
				EnableThinking: &thinkingEnabled,
				BackendType:    "ollama",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Equal(t, []string{"translated"}, result)

		assert.Equal(t, "medium", capturedBody["reasoning_effort"])
		assert.NotContains(t, capturedBody, "chat_template_kwargs")
		assert.NotContains(t, capturedBody, "enable_thinking")
	})

	t.Run("llama.cpp uses enable_thinking field", func(t *testing.T) {
		var capturedBody map[string]interface{}
		thinkingEnabled := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
			response := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{
							"content": `<<<title>>>
translated
`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider: "openai-compatible",
			OpenAICompatible: config.OpenAICompatibleTranslationConfig{
				BaseURL:        server.URL,
				Model:          "test-model.gguf",
				EnableThinking: &thinkingEnabled,
				BackendType:    "llama.cpp",
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Equal(t, []string{"translated"}, result)

		assert.Equal(t, false, capturedBody["enable_thinking"])
		assert.NotContains(t, capturedBody, "chat_template_kwargs")
		assert.NotContains(t, capturedBody, "reasoning_effort")
	})

	t.Run("auto fallback tries another backend control", func(t *testing.T) {
		requestKinds := make([]string, 0, 2)
		thinkingEnabled := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]interface{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

			switch {
			case body["chat_template_kwargs"] != nil:
				requestKinds = append(requestKinds, "chat_template_kwargs")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"unknown field chat_template_kwargs"}`))
			case body["reasoning_effort"] != nil:
				requestKinds = append(requestKinds, "reasoning_effort")
				response := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content": `<<<title>>>
translated
`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			default:
				t.Fatalf("unexpected request body: %#v", body)
			}
		}))
		defer server.Close()

		s := New(config.TranslationConfig{
			Provider: "openai-compatible",
			OpenAICompatible: config.OpenAICompatibleTranslationConfig{
				BaseURL:        server.URL,
				Model:          "test-model",
				EnableThinking: &thinkingEnabled,
			},
		})

		result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
		require.NoError(t, err)
		assert.Equal(t, []string{"translated"}, result)
		assert.Equal(t, []string{"chat_template_kwargs", "reasoning_effort"}, requestKinds)
	})
}

// =============================================================================
// translateWithAnthropic tests
// =============================================================================

func TestTranslateWithAnthropic(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(http.ResponseWriter, *http.Request)
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "success with valid response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/messages", r.URL.Path)
				assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
				assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
				response := map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": "<<<title>>>\ntranslated text"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "missing api key returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler should not be called")
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "",
				},
			},
			wantErr:     true,
			errContains: "anthropic api_key is required",
		},
		{
			name: "upstream error status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Invalid API key"))
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "anthropic translation failed",
		},
		{
			name: "malformed json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not valid json"))
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "failed to decode anthropic response",
		},
		{
			name: "empty content blocks",
			handler: func(w http.ResponseWriter, r *http.Request) {
				response := map[string]interface{}{
					"content": []map[string]string{},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr:     true,
			errContains: "anthropic response contained no content blocks",
		},
		{
			name: "uses default model when not specified",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.Equal(t, "claude-sonnet-4-20250514", body["model"])
				response := map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": "<<<title>>>\ntranslated"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "uses custom model when specified",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.Equal(t, "claude-3-5-sonnet-20241022", body["model"])
				response := map[string]interface{}{
					"content": []map[string]string{
						{"type": "text", "text": "<<<title>>>\ntranslated"},
					},
				}
				_ = json.NewEncoder(w).Encode(response)
			},
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
					Model:  "claude-3-5-sonnet-20241022",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr && tt.errContains == "anthropic api_key is required" {
				s := New(tt.cfg)
				_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			server := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer server.Close()

			tt.cfg.Anthropic.BaseURL = server.URL
			s := New(tt.cfg)

			result, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, result, 1)
			}
		})
	}
}

// =============================================================================
// Dispatch tests for new providers
// =============================================================================

func TestTranslateTexts_Dispatch_NewProviders(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		cfg         config.TranslationConfig
		wantErr     bool
		errContains string
	}{
		{
			name:     "openai-compatible provider dispatch",
			provider: "openai-compatible",
			cfg: config.TranslationConfig{
				Provider:       "openai-compatible",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: true, // Error due to connection failure (no server)
		},
		{
			name:     "anthropic provider dispatch",
			provider: "anthropic",
			cfg: config.TranslationConfig{
				Provider:       "anthropic",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true, // Error due to connection failure (no server)
		},
		{
			name:     "uppercase openai-compatible provider",
			provider: "OPENAI-COMPATIBLE",
			cfg: config.TranslationConfig{
				Provider:       "OPENAI-COMPATIBLE",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				OpenAICompatible: config.OpenAICompatibleTranslationConfig{
					Model: "llama3",
				},
			},
			wantErr: true,
		},
		{
			name:     "uppercase anthropic provider",
			provider: "ANTHROPIC",
			cfg: config.TranslationConfig{
				Provider:       "ANTHROPIC",
				TargetLanguage: "en",
				SourceLanguage: "ja",
				Anthropic: config.AnthropicTranslationConfig{
					APIKey: "test-key",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.cfg)

			_, err := s.translateTexts(context.Background(), "ja", "en", []string{"test"}, []string{"title"})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// SettingsHash storage tests
// =============================================================================

func TestService_TranslateMovie_StoresHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": "<<<title>>>\nTranslated Title",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := &config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		Fields: config.TranslationFieldsConfig{
			Title: true,
		},
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: server.URL,
			Model:   "gpt-4",
			APIKey:  "test-key",
		},
	}

	movie := &models.Movie{
		ContentID:   "test001",
		Title:       "テストタイトル",
		Description: "テスト説明",
	}

	service := New(*cfg)
	translation, _, err := service.TranslateMovie(context.Background(), movie, "abc123def456")

	require.NoError(t, err)
	require.NotNil(t, translation)
	assert.Equal(t, "abc123def456", translation[0].SettingsHash, "hash should be stored in translation")
}

func TestSanitizeTranslationWarning(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		err          error
		wantContains string
	}{
		{
			name:         "HTTP 429",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429, Message: "too many requests"},
			wantContains: "rate limited",
		},
		{
			name:         "HTTP 403",
			provider:     "deepl",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 403, Message: "forbidden"},
			wantContains: "access denied",
		},
		{
			name:         "HTTP 500",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 500, Message: "internal"},
			wantContains: "external service error",
		},
		{
			name:         "HTTP 502",
			provider:     "anthropic",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 502, Message: "bad gateway"},
			wantContains: "external service error",
		},
		{
			name:         "HTTP 400",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 400, Message: "bad request"},
			wantContains: "request error",
		},
		{
			name:         "HTTP 422",
			provider:     "deepl",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 422, Message: "unprocessable"},
			wantContains: "request error",
		},
		{
			name:         "parse error",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorParse, Message: "bad json"},
			wantContains: "service unavailable",
		},
		{
			name:         "count mismatch",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorCountMismatch, Message: "3 vs 5"},
			wantContains: "service unavailable",
		},
		{
			name:         "provider error",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorProvider, Message: "failed after 3 attempts"},
			wantContains: "service unavailable",
		},
		{
			name:         "plain error",
			provider:     "google",
			err:          fmt.Errorf("connection refused"),
			wantContains: "internal error",
		},
		{
			name:         "wrapped TranslationError",
			provider:     "google",
			err:          fmt.Errorf("wrapper: %w", &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429, Message: "rate limited"}),
			wantContains: "rate limited",
		},
		{
			name:         "HTTP status 0 falls through",
			provider:     "google",
			err:          &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 0, Message: "unknown"},
			wantContains: "Translation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTranslationWarning(tt.provider, tt.err)
			assert.Contains(t, got, tt.wantContains)
		})
	}
}

func TestTranslateMovie_ReturnsWarningOnEmptyFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{[]interface{}{[]interface{}{"translated text"}}})
	}))
	defer server.Close()

	cfg := config.TranslationConfig{
		Enabled:        true,
		Provider:       "google",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{
			Title: true,
		},
		Google: config.GoogleTranslationConfig{
			Mode:    "free",
			BaseURL: server.URL,
		},
	}

	s := New(cfg)
	movie := &models.Movie{
		Title: "テストタイトル",
	}
	translation, warning, err := s.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	require.NotNil(t, translation)
	assert.Empty(t, warning, "no warning when all translations succeed")
	assert.Equal(t, "translated text", movie.Title)
}

func TestTranslateMovie_ReturnsWarningOnProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	cfg := config.TranslationConfig{
		Enabled:        true,
		Provider:       "google",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		Fields: config.TranslationFieldsConfig{
			Title: true,
		},
		Google: config.GoogleTranslationConfig{
			Mode:    "free",
			BaseURL: server.URL,
		},
	}

	s := New(cfg)
	movie := &models.Movie{Title: "テスト"}
	_, warning, err := s.TranslateMovie(context.Background(), movie, "")
	require.Error(t, err)
	assert.Contains(t, warning, "rate limited")
}

func TestTranslationError_Error(t *testing.T) {
	t.Run("with status code", func(t *testing.T) {
		e := &TranslationError{Kind: TranslationErrorHTTPStatus, StatusCode: 429, Message: "too many requests"}
		assert.Equal(t, "too many requests (status 429)", e.Error())
	})
	t.Run("without status code", func(t *testing.T) {
		e := &TranslationError{Kind: TranslationErrorParse, Message: "bad json"}
		assert.Equal(t, "bad json", e.Error())
	})
	t.Run("nil message falls back to kind", func(t *testing.T) {
		e := &TranslationError{Kind: TranslationErrorCountMismatch}
		assert.Equal(t, "count_mismatch", e.Error())
	})
	t.Run("nil error", func(t *testing.T) {
		var e *TranslationError
		assert.Equal(t, "", e.Error())
	})
}

func TestTranslationError_Unwrap(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := fmt.Errorf("root cause")
		e := &TranslationError{Kind: TranslationErrorProvider, Cause: cause}
		assert.Equal(t, cause, e.Unwrap())
	})
	t.Run("nil error", func(t *testing.T) {
		var e *TranslationError
		assert.Nil(t, e.Unwrap())
	})
}

func TestTranslateMovie_EmptyTranslationWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{[]interface{}{[]interface{}{""}}})
	}))
	defer server.Close()

	cfg := config.TranslationConfig{
		Enabled:        true,
		Provider:       "google",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{
			Title: true,
		},
		Google: config.GoogleTranslationConfig{
			Mode:    "free",
			BaseURL: server.URL,
		},
	}

	s := New(cfg)
	movie := &models.Movie{Title: "テスト"}
	translation, warning, err := s.TranslateMovie(context.Background(), movie, "")
	require.NoError(t, err)
	require.NotNil(t, translation)
	assert.Contains(t, warning, "title: empty translation, kept original")
	assert.Equal(t, "テスト", movie.Title, "original text preserved on empty translation")
}

func TestTranslateMovie_CountMismatchWarning(t *testing.T) {
	// Batch requests (title+description) get a response missing the description
	// marker → parse error → one-by-one fallback, where each single request gets
	// a valid response for its own marker.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		content := "<<<title>>>\nonly_one"
		if len(req.Messages) > 0 && !strings.Contains(req.Messages[len(req.Messages)-1].Content, "<<<title>>>") {
			content = "<<<description>>>\nonly_one"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": content}},
			},
		})
	}))
	defer server.Close()

	cfg := config.TranslationConfig{
		Enabled:        true,
		Provider:       "openai",
		TargetLanguage: "en",
		SourceLanguage: "ja",
		Fields: config.TranslationFieldsConfig{
			Title:       true,
			Description: true,
		},
		OpenAI: config.OpenAITranslationConfig{
			BaseURL: server.URL,
			APIKey:  "test-key",
		},
	}

	s := New(cfg)
	movie := &models.Movie{Title: "テスト", Description: "説明"}
	results, _, err := s.TranslateMovie(context.Background(), movie, "")
	// With one-by-one fallback, count mismatch on the batch succeeds per-item
	// (mock returns 1 item for each single-item call → correct count).
	require.NoError(t, err)
	require.NotEmpty(t, results)
}
