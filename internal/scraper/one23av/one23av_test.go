package one23av

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchParsesJapaneseDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/ja/v/cwpbd-119", r.URL.Path)
		fmt.Fprint(w, `<html><head><meta property="og:image" content="/images/cwpbd-119.jpg"></head><body>
		<h1>CWPBD-119 — キャットウォーク ポイズン 119 デビュー 無修正版 瀬名まお（ブルーレイ）</h1>
		<h2>詳細</h2>
		<dl>
		<dt>コード</dt><dd>CWPBD-119</dd>
		<dt>発売日</dt><dd>2015-01-15</dd>
		<dt>再生時間</dt><dd>1:50:45</dd>
		<dt>出演者</dt><dd><a href="/ja/actresses/mao-sena">瀬名まお</a></dd>
		<dt>メーカー</dt><dd><a href="/ja/makers/catwalk">キャットウォーク</a></dd>
		<dt>シリーズ</dt><dd><a href="/ja/series/blu-ray">キャットウォーク ポイズン（ブルーレイ）</a></dd>
		<dt>タグ</dt><dd><a href="/ja/tags/cwpbd">CWPBD</a></dd>
		</dl></body></html>`)
	}))
	defer server.Close()

	s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: server.URL}, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "CWPBD-119")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "123av", result.Source)
	assert.Equal(t, "ja", result.Language)
	assert.Equal(t, "CWPBD-119", result.ID)
	assert.Equal(t, "キャットウォーク ポイズン 119 デビュー 無修正版 瀬名まお（ブルーレイ）", result.Title)
	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, "2015-01-15", result.ReleaseDate.Format("2006-01-02"))
	assert.Equal(t, 111, result.Runtime)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "瀬名まお", result.Actresses[0].JapaneseName)
	assert.Equal(t, "キャットウォーク", result.Maker)
	assert.Equal(t, "キャットウォーク ポイズン（ブルーレイ）", result.Series)
	assert.Equal(t, []string{"CWPBD"}, result.Genres)
	assert.Equal(t, server.URL+"/images/cwpbd-119.jpg", result.CoverURL)
	assert.Equal(t, result.CoverURL, result.PosterURL)
}

func TestURLHandlingAndMismatchedDetail(t *testing.T) {
	assert.Equal(t, "CWPBD-119", canonicalID("cwpbd_119"))
	s := &scraper{baseURL: defaultBaseURL}
	assert.True(t, s.CanHandleURL("https://123av.com/ja/v/cwpbd-119"))
	assert.True(t, s.CanHandleURL("https://njav.tv/en/v/cwpbd-119"))
	assert.False(t, s.CanHandleURL("https://example.com/ja/v/cwpbd-119"))
	url, err := s.GetURL(context.Background(), "CWPBD-119")
	require.NoError(t, err)
	assert.Equal(t, "https://123av.com/ja/v/cwpbd-119", url)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<h1>OTHER-100 — Wrong</h1>`))
	require.NoError(t, err)
	_, err = parseDetail(doc, "CWPBD-119", url, "ja")
	require.Error(t, err)
}
