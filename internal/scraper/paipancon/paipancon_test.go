package paipancon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchParsesFC2DailyDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fc2daily/detail/FC2-PPV-3061625":
			fmt.Fprint(w, `<html><body>
			<h2 class="text-center">FC2-PPV-3061625 - テスト作品</h2>
			<div class="text-center"><span>Actress: <a href="/fc2daily/actor/1">あすか</a></span>
			<span>Seller Name: <a href="/fc2daily/search/seller">販売者</a></span></div>
			<div class="text-center">2022-08-01</div>
			<div class="container-md my-container">
			<div>FC2-PPV-3061625_1.mp4 1:22:53 1920x1080</div>
			<div>FC2-PPV-3061625_2.mp4 0:12:25 1920x1080</div></div>
			<img class="media-item" src="/fc2daily/data/FC2-PPV-3061625/cover.jpg">
			<img class="media-item" src="/fc2daily/data/FC2-PPV-3061625/grid.jpg">
			<video src="/preview.mp4"></video></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: server.URL}, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "FC2-PPV-3061625")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "テスト作品", result.Title)
	assert.Equal(t, "販売者", result.Maker)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "あすか", result.Actresses[0].JapaneseName)
	assert.Equal(t, 95, result.Runtime)
	assert.Contains(t, result.CoverURL, "/cover.jpg")
	assert.Contains(t, result.PosterURL, "/cover.jpg")
	assert.Equal(t, []string{server.URL + "/fc2daily/data/FC2-PPV-3061625/grid.jpg"}, result.ScreenshotURL)
	assert.Empty(t, result.TrailerURL)
}

func TestSearchFallsBackToPublicSearchForActress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fc2daily/detail/FC2-PPV-4451504" {
			fmt.Fprint(w, `<h2 class="text-center">FC2-PPV-4451504 - 作品</h2>`)
			return
		}
		if r.URL.Path == "/fc2daily/search/FC2-PPV-4451504" {
			fmt.Fprint(w, `<div class="fc2-box"><div class="card-body"><a href="/fc2daily/detail/FC2-PPV-4451504">作品</a><div><a>しずく</a></div></div></div>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: server.URL}, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "FC2-PPV-4451504")
	require.NoError(t, err)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "しずく", result.Actresses[0].JapaneseName)
}
