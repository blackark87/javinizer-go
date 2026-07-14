package translation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderLimiterWrapsActualTranslationRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"translations": []map[string]string{{"text": "translated"}},
		})
	}))
	defer server.Close()

	var acquired atomic.Int32
	var released atomic.Int32
	service := NewWithProviderLimiter(config.TranslationConfig{
		Provider: "deepl", SourceLanguage: "ja", TargetLanguage: "en",
		DeepL: config.DeepLTranslationConfig{Mode: "free", BaseURL: server.URL, APIKey: "test"},
	}, func(context.Context) error {
		acquired.Add(1)
		return nil
	}, func() { released.Add(1) })

	result, err := service.translateTexts(context.Background(), "ja", "en", []string{"原文"}, []string{"title"})
	require.NoError(t, err)
	assert.Equal(t, []string{"translated"}, result)
	assert.EqualValues(t, 1, acquired.Load())
	assert.EqualValues(t, 1, released.Load())
}

func TestReplaceActressNameDoesNotDowngradeHangulPrimary(t *testing.T) {
	actress := &models.Actress{FirstName: "마유키", LastName: "이토", JapaneseName: "伊藤舞雪"}
	replaceActressName(actress, "Ito Mayuki")
	assert.Equal(t, "마유키", actress.FirstName)
	assert.Equal(t, "이토", actress.LastName)
	assert.Equal(t, "伊藤舞雪", actress.JapaneseName)
}

func TestSharedProviderLimiterCapsConcurrentRequestsAtThree(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 5)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"translations": []map[string]string{{"text": "translated"}}})
	}))
	defer server.Close()

	service := New(config.TranslationConfig{
		Provider: "deepl", SourceLanguage: "ja", TargetLanguage: "en", MaxConcurrency: 3,
		DeepL: config.DeepLTranslationConfig{Mode: "free", BaseURL: server.URL, APIKey: "test"},
	})
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for index := 0; index < 5; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.translateTexts(context.Background(), "ja", "en", []string{"原文"}, []string{"title"})
			errs <- err
		}()
	}
	for index := 0; index < 3; index++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("three provider requests did not start")
		}
	}
	select {
	case <-entered:
		t.Fatal("a fourth provider request bypassed max_concurrency")
	case <-time.After(75 * time.Millisecond):
	}
	assert.EqualValues(t, 3, maximum.Load())
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
