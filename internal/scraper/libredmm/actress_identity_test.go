package libredmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveActressIdentityUsesLibreDMMActressIndexWithoutMovieScrape(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, "/actresses", r.URL.Path)
		assert.Equal(t, "Name", r.URL.Query().Get("order"))
		assert.Equal(t, "波多野結衣", r.URL.Query().Get("fuzzy"))
		_, _ = w.Write([]byte(`<html><body>
<div class="card actress">
  <a href="/actresses/26225"><img src="http://pics.dmm.co.jp/mono/actjpgs/hatano_yui.jpg"></a>
  <div class="card-body"><h6 class="card-title"><a href="/actresses/26225">波多野結衣</a></h6></div>
</div>
</body></html>`))
	}))
	defer server.Close()

	settings := models.ScraperSettings{Enabled: true, BaseURL: server.URL}
	scraper := newScraper(&settings, nil, models.FlareSolverrConfig{})
	result, err := scraper.ResolveActressIdentity(context.Background(), models.ActressIdentityQuery{Names: []string{"波多野結衣"}})
	require.NoError(t, err)
	assert.Equal(t, 1, requests)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, 0, result.Actresses[0].DMMID)
	assert.Equal(t, "波多野結衣", result.Actresses[0].JapaneseName)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/hatano_yui.jpg", result.Actresses[0].ThumbURL)
}
