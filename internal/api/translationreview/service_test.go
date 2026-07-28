package translationreview

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReviewMovie_ReviewsTitleAndDescriptionTogether(t *testing.T) {
	var requestBodies []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestBody := string(body)
		requestBodies = append(requestBodies, requestBody)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(requestBody, "mandatory second-pass quality reviewer") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<quality_review_title>>>\n교정 제목\n<<<quality_review_description>>>\n교정 설명\n<<<JZ_DONE>>>"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<title>>>\n새 제목\n<<<description>>>\n새 설명\n<<<JZ_DONE>>>"}}]}`))
	}))
	defer llm.Close()

	tc := config.DefaultConfig(nil, nil).Metadata.Translation
	tc.Enabled = true
	tc.Provider = "openai-compatible"
	tc.TargetLanguage = "ko"
	tc.TimeoutSeconds = 10
	tc.OpenAICompatible.BaseURL = llm.URL
	tc.OpenAICompatible.Model = "test-model"

	result, err := ReviewMovie(context.Background(), tc, "日本語題名", "日本語説明", nil)

	require.NoError(t, err)
	assert.Equal(t, "교정 제목", result.Title.Value)
	require.NotNil(t, result.Description)
	assert.Equal(t, "교정 설명", result.Description.Value)
	assert.Equal(t, "ko", result.Title.TargetLanguage)
	require.Len(t, requestBodies, 2)
	assert.Contains(t, requestBodies[0], "日本語題名")
	assert.Contains(t, requestBodies[0], "日本語説明")
	assert.Contains(t, requestBodies[1], "새 제목")
	assert.Contains(t, requestBodies[1], "새 설명")
}

func TestReviewMovie_TitleOnly(t *testing.T) {
	var requests int
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "mandatory second-pass quality reviewer") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<quality_review_title>>>\n교정 제목\n<<<JZ_DONE>>>"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<<<title>>>\n새 제목\n<<<JZ_DONE>>>"}}]}`))
	}))
	defer llm.Close()

	tc := config.DefaultConfig(nil, nil).Metadata.Translation
	tc.Enabled = true
	tc.Provider = "openai-compatible"
	tc.TargetLanguage = "ko"
	tc.TimeoutSeconds = 10
	tc.OpenAICompatible.BaseURL = llm.URL
	tc.OpenAICompatible.Model = "test-model"

	result, err := ReviewMovie(context.Background(), tc, "日本語題名", "", nil)

	require.NoError(t, err)
	assert.Nil(t, result.Description)
	assert.Equal(t, "교정 제목", result.Title.Value)
	assert.Equal(t, 2, requests)
}
