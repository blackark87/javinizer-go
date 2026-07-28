package fanzamcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch_MapsMetadataAndMedia(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/metadata/ABC-001", r.URL.EscapedPath())
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"source":"fanza-mcp",
			"source_url":"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc00001/",
			"language":"ja",
			"id":"ABC-001",
			"content_id":"abc00001",
			"title":"API title",
			"original_title":"ignored provider original title",
			"description":"API description",
			"release_date":"2026-07-07",
			"actresses":[
				{"dmm_id":123456,"first_name":"","last_name":"","japanese_name":"宮下玲奈","reading":"","thumb_url":""},
				{"dmm_id":789012,"first_name":"","last_name":"","japanese_name":"森日向子","reading":"","thumb_url":""}
			],
			"poster_url":%q,
			"cover_url":%q,
			"screenshot_urls":[%q,%q],
			"trailer_url":%q,
			"should_crop_poster":false,
			"providers":["fanza"],
			"expires_at":123
		}`,
			server.URL+"/api/v1/media/ABC-001/poster",
			server.URL+"/api/v1/media/ABC-001/cover",
			server.URL+"/api/v1/media/ABC-001/screenshots/1",
			server.URL+"/api/v1/media/ABC-001/screenshots/2",
			server.URL+"/api/v1/media/ABC-001/trailer",
		)
	}))
	t.Cleanup(server.Close)

	s := newScraper(&models.ScraperSettings{
		Enabled: true,
		BaseURL: server.URL,
		Timeout: 1,
	})
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	result, err := s.Search(context.Background(), "abc_1")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "fanzamcp", result.Source)
	assert.Equal(t, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=abc00001/", result.SourceURL)
	assert.Equal(t, "ja", result.Language)
	assert.Equal(t, "ABC-001", result.ID)
	assert.Equal(t, "abc00001", result.ContentID)
	assert.Equal(t, "API title", result.Title)
	assert.Equal(t, "API title", result.OriginalTitle)
	assert.Equal(t, "API description", result.Description)
	require.NotNil(t, result.ReleaseDate)
	assert.Equal(t, "2026-07-07", result.ReleaseDate.Format("2006-01-02"))
	require.Len(t, result.Actresses, 2)
	assert.Equal(t, 123456, result.Actresses[0].DMMID)
	assert.Equal(t, "宮下玲奈", result.Actresses[0].JapaneseName)
	assert.Equal(t, 789012, result.Actresses[1].DMMID)
	assert.Equal(t, "森日向子", result.Actresses[1].JapaneseName)
	assert.Equal(t, server.URL+"/api/v1/media/ABC-001/poster", result.PosterURL)
	assert.Equal(t, server.URL+"/api/v1/media/ABC-001/cover", result.CoverURL)
	assert.Equal(t, []string{
		server.URL + "/api/v1/media/ABC-001/screenshots/1",
		server.URL + "/api/v1/media/ABC-001/screenshots/2",
	}, result.ScreenshotURL)
	assert.Equal(t, server.URL+"/api/v1/media/ABC-001/trailer", result.TrailerURL)
	assert.False(t, result.ShouldCropPoster)

	agg := aggregator.New(&aggregator.Config{
		ScrapersPriority: []string{scraperName},
		Metadata:         &aggregator.MetadataConfig{},
	}, nil, nil, nil)
	movie, _, err := agg.AggregateWithPriority(
		[]*models.ScraperResult{result},
		[]string{scraperName},
	)
	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Equal(t, "ABC-001", movie.ID)
	assert.Equal(t, "abc00001", movie.ContentID)
	assert.Equal(t, "API title", movie.Title)
	assert.Equal(t, "API description", movie.Description)
	require.Len(t, movie.Actresses, 2)
	assert.Equal(t, 123456, movie.Actresses[0].DMMID)
	assert.Equal(t, "宮下玲奈", movie.Actresses[0].JapaneseName)
	assert.Equal(t, server.URL+"/api/v1/media/ABC-001/poster", movie.Poster.PosterURL)
}

func TestSearch_AllowsEmptyOptionalFields(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{
			"source":"fanza-mcp",
			"source_url":"",
			"language":"ja",
			"id":"MDVR-428",
			"content_id":"mdvr00428",
			"title":"VR title",
			"description":"",
			"release_date":null,
			"actresses":[],
			"poster_url":%q,
			"cover_url":"",
			"screenshot_urls":[],
			"trailer_url":"",
			"should_crop_poster":false
		}`, server.URL+"/api/v1/media/MDVR-428/poster")
	}))
	t.Cleanup(server.Close)

	s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: server.URL, Timeout: 1})
	result, err := s.Search(context.Background(), "mdvr428")

	require.NoError(t, err)
	assert.Empty(t, result.Description)
	assert.Nil(t, result.ReleaseDate)
	assert.Empty(t, result.Actresses)
	assert.Empty(t, result.CoverURL)
	assert.Empty(t, result.ScreenshotURL)
	assert.Empty(t, result.TrailerURL)
}

func TestSearch_ClassifiesHTTPAndPayloadFailures(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantKind    models.ScraperErrorKind
		wantContain string
	}{
		{name: "not found", status: http.StatusNotFound, body: `{"error":"missing"}`, wantKind: models.ScraperErrorKindNotFound},
		{name: "server error", status: http.StatusBadGateway, body: `{"error":"upstream"}`, wantKind: models.ScraperErrorKindUnavailable},
		{name: "invalid JSON", status: http.StatusOK, body: `{"source":`, wantContain: "parse FANZA MCP metadata response"},
		{
			name:   "unexpected source",
			status: http.StatusOK,
			body: `{
				"source":"other","id":"ABC-001","title":"title",
				"poster_url":"POSTER_URL"
			}`,
			wantContain: "unexpected source",
		},
		{
			name:   "invalid date",
			status: http.StatusOK,
			body: `{
				"source":"fanza-mcp","id":"ABC-001","title":"title",
				"release_date":"07/07/2026",
				"poster_url":"POSTER_URL"
			}`,
			wantContain: "release_date",
		},
		{
			name:   "mismatched product",
			status: http.StatusOK,
			body: `{
				"source":"fanza-mcp","id":"ABC-002","title":"title",
				"poster_url":"http://fanza-mcp:8000/api/v1/media/ABC-002/poster"
			}`,
			wantContain: "mismatched product code",
		},
		{
			name:   "external media URL",
			status: http.StatusOK,
			body: `{
				"source":"fanza-mcp","id":"ABC-001","title":"title",
				"poster_url":"http://example.com/api/v1/media/ABC-001/poster"
			}`,
			wantContain: "does not match metadata service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				body := strings.ReplaceAll(
					tt.body,
					"POSTER_URL",
					"http://"+r.Host+"/api/v1/media/ABC-001/poster",
				)
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)

			s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: server.URL, Timeout: 1})
			_, err := s.Search(context.Background(), "ABC-001")

			require.Error(t, err)
			if tt.wantKind != "" {
				scraperErr, ok := models.AsScraperError(err)
				require.True(t, ok)
				assert.Equal(t, tt.wantKind, scraperErr.Kind)
			}
			if tt.wantContain != "" {
				assert.ErrorContains(t, err, tt.wantContain)
			}
		})
	}
}

func TestSearch_RespectsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	s := newScraper(&models.ScraperSettings{Enabled: true, BaseURL: server.URL, Timeout: 1})
	s.client.SetTimeout(20 * time.Millisecond)

	_, err := s.Search(context.Background(), "ABC-001")

	require.Error(t, err)
	assert.ErrorContains(t, err, "fetch FANZA MCP metadata")
}

func TestGetURL_NormalizesProductCodesAndRejectsTraversal(t *testing.T) {
	s := newScraper(&models.ScraperSettings{BaseURL: defaultBaseURL, Timeout: 1})
	tests := []struct {
		input string
		code  string
		ok    bool
	}{
		{input: "abc-1", code: "ABC-001", ok: true},
		{input: "ABC 00042", code: "ABC-042", ok: true},
		{input: "118abf00042", code: "ABF-042", ok: true},
		{input: "h_1234abc00123", code: "ABC-123", ok: true},
		{input: "../../etc/passwd", ok: false},
		{input: "x", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := s.GetURL(context.Background(), tt.input)
			if !tt.ok {
				require.Error(t, err)
				scraperErr, typed := models.AsScraperError(err)
				require.True(t, typed)
				assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, defaultBaseURL+"/api/v1/metadata/"+tt.code, got)
		})
	}
}

func TestValidateScraperSettings_RestrictsInternalServiceURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "default fallback", baseURL: ""},
		{name: "default", baseURL: defaultBaseURL},
		{name: "alternate port", baseURL: "http://fanza-mcp:9000"},
		{name: "HTTPS rejected", baseURL: "https://fanza-mcp:8000", wantErr: true},
		{name: "external host rejected", baseURL: "http://example.com:8000", wantErr: true},
		{name: "loopback rejected", baseURL: "http://127.0.0.1:8000", wantErr: true},
		{name: "userinfo rejected", baseURL: "http://user:pass@fanza-mcp:8000", wantErr: true},
		{name: "path rejected", baseURL: "http://fanza-mcp:8000/internal", wantErr: true},
		{name: "query rejected", baseURL: "http://fanza-mcp:8000?target=other", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScraperSettings(&models.ScraperSettings{BaseURL: tt.baseURL})
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewScraper_DisablesEnvironmentProxyAndMediaProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:9999")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:9999")

	s := newScraper(&models.ScraperSettings{BaseURL: defaultBaseURL, Timeout: 1})
	transport, ok := s.client.GetClient().Transport.(*http.Transport)
	require.True(t, ok)
	assert.Nil(t, transport.Proxy)

	downloadOverride, scraperProxy, handled := s.ResolveDownloadProxyForHost("FANZA-MCP")
	require.True(t, handled)
	require.NotNil(t, downloadOverride)
	assert.False(t, downloadOverride.Enabled)
	assert.Nil(t, scraperProxy)

	downloadOverride, scraperProxy, handled = s.ResolveDownloadProxyForHost("pics.dmm.co.jp")
	assert.False(t, handled)
	assert.Nil(t, downloadOverride)
	assert.Nil(t, scraperProxy)
}
