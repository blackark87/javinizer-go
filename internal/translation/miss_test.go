package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Google Paid translation with httptest.NewServer ---

func TestGooglePaid_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/language/translate/v2")
		assert.NotEmpty(t, r.URL.Query().Get("key"))

		var req googlePaidTranslateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "en", req.Target)
		assert.Equal(t, "ja", req.Source)
		assert.Equal(t, "text", req.Format)

		resp := googlePaidTranslateResponse{}
		resp.Data.Translations = []struct {
			TranslatedText string `json:"translatedText"`
		}{
			{TranslatedText: "Hello"},
			{TranslatedText: "World"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModePaid,
			APIKey:  "test-key",
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	result, err := p.Translate(context.Background(), "ja", "en", []string{"こんにちは", "世界"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello", "World"}, result.Texts)
}

func TestGooglePaid_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModePaid,
			APIKey:  "test-key",
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
	var te *translationError
	assert.ErrorAs(t, err, &te)
	assert.Equal(t, TranslationErrorHTTPStatus, te.Kind)
	assert.Equal(t, http.StatusForbidden, te.StatusCode)
}

func TestGooglePaid_EmptyTexts(t *testing.T) {
	cfg := Config{
		Google: googleConfig{
			Mode:   models.GoogleModePaid,
			APIKey: "test-key",
		},
	}
	p := NewGoogleProvider(cfg, nil)
	result, err := p.Translate(context.Background(), "ja", "en", []string{})
	require.NoError(t, err)
	assert.Empty(t, result.Texts)
}

func TestGooglePaid_MissingAPIKey(t *testing.T) {
	cfg := Config{
		Google: googleConfig{
			Mode:   models.GoogleModePaid,
			APIKey: "",
		},
	}
	p := NewGoogleProvider(cfg, nil)
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

// --- Google Free translation with httptest.NewServer ---

func TestGoogleFree_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "gtx", r.URL.Query().Get("client"))
		assert.Equal(t, "ja", r.URL.Query().Get("sl"))
		assert.Equal(t, "en", r.URL.Query().Get("tl"))

		// Google free API returns: [[segment1, segment2, ...], "source", "target", ...]
		// where each segment is ["translated_text", "original_text", ...]
		// Since translateWithGoogleFree sends each text individually, we return
		// a single translation per request
		resp := []any{
			[]any{
				[]any{"Hello", "こんにちは", "translit"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModeFree,
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	result, err := p.Translate(context.Background(), "ja", "en", []string{"こんにちは"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello"}, result.Texts)
}

func TestGoogleFree_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, "Rate Limited")
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModeFree,
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
	var te *translationError
	assert.ErrorAs(t, err, &te)
	assert.Equal(t, TranslationErrorHTTPStatus, te.Kind)
}

func TestGoogleFree_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `not valid json`)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModeFree,
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
}

func TestGoogleFree_EmptyTexts(t *testing.T) {
	cfg := Config{
		Google: googleConfig{
			Mode: models.GoogleModeFree,
		},
	}
	p := NewGoogleProvider(cfg, nil)
	result, err := p.Translate(context.Background(), "ja", "en", []string{})
	require.NoError(t, err)
	assert.Empty(t, result.Texts)
}

func TestGoogleFree_UnexpectedResponseShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return an object instead of the expected array
		_, _ = fmt.Fprint(w, `{"key": "value"}`)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModeFree,
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected")
}

func TestGoogleFree_EmptyTranslationResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return array with empty segments
		_, _ = fmt.Fprint(w, `[[[]]]`)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModeFree,
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())
	_, err := p.Translate(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty text")
}

// --- translateWithProvider retry logic ---

func TestTranslateWithProvider_RetryableError(t *testing.T) {
	callCount := 0
	provider := &mockProvider{
		translateFunc: func(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
			callCount++
			if callCount < 3 {
				return &translationResult{RawLLM: "some output", Texts: []string{}}, &translationError{
					Kind:    TranslationErrorCountMismatch,
					Message: "count mismatch",
				}
			}
			return &translationResult{Texts: texts}, nil
		},
	}

	svc := New(Config{
		Provider:       "mock",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		Fields: fieldsConfig{
			Title: true,
		},
	}, provider)

	result, err := svc.translateTexts(context.Background(), "ja", "en", []string{"test"})
	require.NoError(t, err)
	assert.Equal(t, []string{"test"}, result)
	assert.True(t, callCount >= 2, "expected retries, got %d calls", callCount)
}

func TestTranslateWithProvider_NonRetryableError(t *testing.T) {
	provider := &mockProvider{
		translateFunc: func(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
			return nil, &translationError{
				Kind:       TranslationErrorHTTPStatus,
				StatusCode: 403,
				Message:    "forbidden",
			}
		},
	}

	svc := New(Config{
		Provider:       "mock",
		SourceLanguage: "ja",
		TargetLanguage: "en",
	}, provider)

	_, err := svc.translateTexts(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
}

func TestTranslateWithProvider_UnsupportedProvider(t *testing.T) {
	svc := New(Config{
		Provider:       "nonexistent",
		SourceLanguage: "ja",
		TargetLanguage: "en",
	})

	_, err := svc.translateTexts(context.Background(), "ja", "en", []string{"test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// --- mockProvider for testing ---

type mockProvider struct {
	name          string
	translateFunc func(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error)
}

func (m *mockProvider) Name() string {
	if m.name != "" {
		return m.name
	}
	return "mock"
}

func (m *mockProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if m.translateFunc != nil {
		return m.translateFunc(ctx, sourceLang, targetLang, texts)
	}
	return &translationResult{Texts: texts}, nil
}

// --- TranslateMovie with actual provider and warning on empty ---

func TestTranslateMovie_EmptyTranslationFails(t *testing.T) {
	provider := &mockProvider{
		translateFunc: func(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
			// Return empty strings for translations
			return &translationResult{Texts: make([]string, len(texts))}, nil
		},
	}

	svc := New(Config{
		Enabled:        true,
		Provider:       "mock",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		Fields: fieldsConfig{
			Title: true,
		},
	}, provider)

	movie := &models.Movie{Title: "テスト"}
	out, warning, err := svc.TranslateMovie(context.Background(), movie, "hash123")
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, warning, "invalid model output")
	assert.Contains(t, err.Error(), "empty translation after retry")
	assert.Equal(t, "テスト", movie.Title)
}

func TestTranslateMovie_LowQualityRetryReceivesValidationFailure(t *testing.T) {
	calls := 0
	provider := &mockProvider{
		translateFunc: func(ctx context.Context, _, _ string, _ []string) (*translationResult, error) {
			calls++
			if calls == 1 {
				return &translationResult{Texts: []string{"컵 수: 치っぱい"}}, nil
			}
			correction, ok := translationCorrectionFromContext(ctx)
			require.True(t, ok)
			assert.Contains(t, correction.Issue, `untranslated Japanese remains: "っぱい"`)
			assert.Equal(t, "컵 수: 치っぱい", correction.Candidate)
			return &translationResult{Texts: []string{"컵 수: 빈유"}}, nil
		},
	}

	svc := New(Config{
		Enabled:        true,
		Provider:       "mock",
		SourceLanguage: "ja",
		TargetLanguage: "ko",
		Fields: fieldsConfig{
			Description: true,
		},
	}, provider)

	movie := &models.Movie{Description: "カップ数：ちっぱい"}
	out, _, err := svc.TranslateMovie(context.Background(), movie, "hash123")
	require.NoError(t, err)
	require.NotNil(t, out)
	require.NotNil(t, out.Movie)
	assert.Equal(t, "컵 수: 빈유", out.Movie.Description)
	assert.Equal(t, 2, calls)
}

func TestTranslateMovie_DictionaryLowQualityRetryFallsBackToFullPrompt(t *testing.T) {
	calls := 0
	provider := &mockProvider{
		name: "openai-compatible",
		translateFunc: func(ctx context.Context, _, _ string, _ []string) (*translationResult, error) {
			calls++
			switch calls {
			case 1:
				return &translationResult{Texts: []string{"円光"}}, nil
			case 2:
				correction, ok := translationCorrectionFromContext(ctx)
				require.True(t, ok)
				assert.Contains(t, correction.Issue, `untranslated Japanese remains: "円光"`)
				_, overridden := dictionaryPromptModeFromContext(ctx)
				assert.False(t, overridden)
				return &translationResult{Texts: []string{"交縁"}}, nil
			default:
				dictionaryEnabled, overridden := dictionaryPromptModeFromContext(ctx)
				require.True(t, overridden)
				assert.False(t, dictionaryEnabled)
				correction, ok := translationCorrectionFromContext(ctx)
				require.True(t, ok)
				assert.Contains(t, correction.Issue, `untranslated Japanese remains: "交縁"`)
				return &translationResult{Texts: []string{"조건만남"}}, nil
			}
		},
	}

	svc := New(Config{
		Enabled:           true,
		Provider:          "openai-compatible",
		SourceLanguage:    "ja",
		TargetLanguage:    "ko",
		DictionaryEnabled: true,
		Fields: fieldsConfig{
			Description: true,
		},
	}, provider)
	svc.acquireProviderCall = nil
	svc.releaseProviderCall = nil

	movie := &models.Movie{Description: "円光"}
	out, _, err := svc.TranslateMovie(context.Background(), movie, "hash123")
	require.NoError(t, err)
	require.NotNil(t, out)
	require.NotNil(t, out.Movie)
	assert.Equal(t, "조건만남", out.Movie.Description)
	assert.Equal(t, 3, calls)
}

func TestTranslateWithProvider_ParseRetryReceivesProtocolFailure(t *testing.T) {
	calls := 0
	provider := &mockProvider{
		translateFunc: func(ctx context.Context, _, _ string, _ []string) (*translationResult, error) {
			calls++
			if calls == 1 {
				return &translationResult{RawLLM: "<<<title>>>\n번역\n<<<description>>>\n불필요"}, &translationError{
					Kind:    TranslationErrorParse,
					Message: "unexpected embedded output marker in slot 0",
				}
			}
			correction, ok := translationCorrectionFromContext(ctx)
			require.True(t, ok)
			assert.Contains(t, correction.Issue, "unexpected embedded output marker")
			assert.Empty(t, correction.Candidate, "marker-bearing malformed output must not be echoed back")
			return &translationResult{Texts: []string{"정상 제목"}}, nil
		},
	}
	svc := New(Config{Provider: "mock"}, provider)
	svc.acquireProviderCall = nil
	svc.releaseProviderCall = nil

	translated, err := svc.translateWithProvider(context.Background(), provider, "ja", "ko", []string{"作品名"})
	require.NoError(t, err)
	assert.Equal(t, []string{"정상 제목"}, translated)
	assert.Equal(t, 2, calls)
}

func TestTranslateWithProvider_DictionaryFailureFallsBackToFullPrompt(t *testing.T) {
	calls := 0
	provider := &mockProvider{
		name: "openai-compatible",
		translateFunc: func(ctx context.Context, _, _ string, _ []string) (*translationResult, error) {
			calls++
			if calls <= maxTranslationRetries {
				dictionaryEnabled, overridden := dictionaryPromptModeFromContext(ctx)
				assert.False(t, overridden)
				assert.False(t, dictionaryEnabled)
				return &translationResult{RawLLM: "broken output"}, &translationError{
					Kind:    TranslationErrorParse,
					Message: "protected metadata token count changed",
				}
			}

			dictionaryEnabled, overridden := dictionaryPromptModeFromContext(ctx)
			require.True(t, overridden)
			assert.False(t, dictionaryEnabled)
			correction, ok := translationCorrectionFromContext(ctx)
			require.True(t, ok)
			assert.Contains(t, correction.Issue, "compact dictionary prompt failed")
			return &translationResult{Texts: []string{"정상 번역"}}, nil
		},
	}
	svc := New(Config{Provider: "openai-compatible", DictionaryEnabled: true}, provider)
	svc.acquireProviderCall = nil
	svc.releaseProviderCall = nil

	translated, err := svc.translateWithProvider(context.Background(), provider, "ja", "ko", []string{"原文"})
	require.NoError(t, err)
	assert.Equal(t, []string{"정상 번역"}, translated)
	assert.Equal(t, maxTranslationRetries+1, calls)
}

// --- TranslateMovie count mismatch ---

func TestTranslateMovie_CountMismatch_MissTest(t *testing.T) {
	provider := &mockProvider{
		translateFunc: func(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
			// Return wrong number of results
			return &translationResult{Texts: []string{"only one"}}, nil
		},
	}

	svc := New(Config{
		Enabled:        true,
		Provider:       "mock",
		SourceLanguage: "ja",
		TargetLanguage: "en",
		Fields: fieldsConfig{
			Title: true,
			Maker: true,
		},
	}, provider)

	movie := &models.Movie{Title: "テスト", Maker: "メーカー"}
	_, _, err := svc.TranslateMovie(context.Background(), movie, "hash123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned 1 items for 2 inputs")
}

// --- Google Paid: context cancellation ---

func TestGooglePaid_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow response that outlasts the client timeout. Use a
		// FINITE sleep (not a bare select{}): a select{} handler would never
		// return, deadlocking httptest.Server.Close() (which waits for
		// in-flight handlers). The client cancels well before this sleep
		// finishes, so Close() only waits out the remainder.
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	cfg := Config{
		Google: googleConfig{
			Mode:    models.GoogleModePaid,
			APIKey:  "test-key",
			BaseURL: server.URL,
		},
	}
	p := NewGoogleProvider(cfg, server.Client())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := p.Translate(ctx, "ja", "en", []string{"test"})
	require.Error(t, err)
}
