package sougouwiki

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func TestResolveActressesKnownMappingsEUCJP(t *testing.T) {
	tests := []struct {
		movieID string
		pageID  string
		name    string
		dmmID   int
	}{
		{movieID: "JNT-051", pageID: "JNT-051", name: "弥生みづき", dmmID: 1054168},
		{movieID: "300MIUM-834", pageID: "300MIUM-834", name: "天然美月", dmmID: 1071639},
		{movieID: "300MIUM-921", pageID: "300MIUM-921", name: "有栖舞衣", dmmID: 1082672},
	}

	for _, test := range tests {
		t.Run(test.movieID, func(t *testing.T) {
			server := newFixtureServer(t, func(baseURL string, request *http.Request) (string, int) {
				switch request.URL.Path {
				case "/search":
					if got := request.URL.Query().Get("keywords"); got != test.movieID {
						t.Errorf("search keywords = %q, want %q", got, test.movieID)
					}
					if got := request.URL.Query().Get("search_target"); got != "all" {
						t.Errorf("search_target = %q, want all", got)
					}
					return searchFixture(
						"/d/Janet",
						"/d/300MIUM_other",
						"/d/no_dmm",
						"https://example.com/d/external",
						"/d/actress",
					), http.StatusOK
				case "/d/Janet":
					return invalidFirstBlockFixture("Janet", test.pageID), http.StatusOK
				case "/d/300MIUM_other":
					return laterDMMLinkFixture("300MIUMその他", test.pageID), http.StatusOK
				case "/d/no_dmm":
					return invalidFirstBlockFixture("情報", test.pageID), http.StatusOK
				case "/d/actress":
					return actressFixture(test.pageID, test.name, test.dmmID), http.StatusOK
				default:
					t.Errorf("unexpected request to %s%s", baseURL, request.URL.Path)
					return "not found", http.StatusNotFound
				}
			})
			defer server.Close()

			scraper := New(config.ScraperSettings{
				Enabled:   true,
				BaseURL:   server.URL + "/",
				RateLimit: 0,
				Timeout:   2,
			}, nil, config.FlareSolverrConfig{})

			result, err := scraper.ResolveActresses(context.Background(), test.movieID)
			if err != nil {
				t.Fatalf("ResolveActresses() error = %v", err)
			}
			if len(result.Actresses) != 1 {
				t.Fatalf("actress count = %d, want 1: %+v", len(result.Actresses), result.Actresses)
			}
			if got := result.Actresses[0].JapaneseName; got != test.name {
				t.Errorf("JapaneseName = %q, want %q", got, test.name)
			}
			if got := result.Actresses[0].DMMID; got != test.dmmID {
				t.Errorf("DMMID = %d, want %d", got, test.dmmID)
			}
			if result.Source != scraperName {
				t.Errorf("Source = %q, want %q", result.Source, scraperName)
			}
		})
	}
}

func TestResolveActressesMultipleAndDuplicateDMMIDs(t *testing.T) {
	server := newFixtureServer(t, func(_ string, request *http.Request) (string, int) {
		switch request.URL.Path {
		case "/search":
			return searchFixture("/d/one", "/d/duplicate", "/d/two"), http.StatusOK
		case "/d/one":
			return actressFixture("ABC-123", "女優一", 100), http.StatusOK
		case "/d/duplicate":
			return actressFixture("ABC-123", "別名", 100), http.StatusOK
		case "/d/two":
			return actressFixture("ABC-123", "女優二", 200), http.StatusOK
		default:
			return "not found", http.StatusNotFound
		}
	})
	defer server.Close()

	scraper := New(config.ScraperSettings{Enabled: true, BaseURL: server.URL + "/"}, nil, config.FlareSolverrConfig{})
	result, err := scraper.ResolveActresses(context.Background(), "ABC-123")
	if err != nil {
		t.Fatalf("ResolveActresses() error = %v", err)
	}
	if len(result.Actresses) != 2 {
		t.Fatalf("actress count = %d, want 2: %+v", len(result.Actresses), result.Actresses)
	}
	if result.Actresses[0].DMMID != 100 || result.Actresses[1].DMMID != 200 {
		t.Errorf("DMM IDs = [%d %d], want [100 200]", result.Actresses[0].DMMID, result.Actresses[1].DMMID)
	}
}

func TestResolveActressesNoVerifiedPage(t *testing.T) {
	server := newFixtureServer(t, func(_ string, request *http.Request) (string, int) {
		if request.URL.Path == "/search" {
			return searchFixture("/d/not_actress"), http.StatusOK
		}
		return invalidFirstBlockFixture("Janet", "JNT-051"), http.StatusOK
	})
	defer server.Close()

	scraper := New(config.ScraperSettings{Enabled: true, BaseURL: server.URL + "/"}, nil, config.FlareSolverrConfig{})
	_, err := scraper.ResolveActresses(context.Background(), "JNT-051")
	if err == nil {
		t.Fatal("ResolveActresses() error = nil, want not found")
	}
	scraperErr, ok := models.AsScraperError(err)
	if !ok || scraperErr.Kind != models.ScraperErrorKindNotFound {
		t.Fatalf("error = %v, want typed not-found error", err)
	}
}

func TestMovieIDEquivalence(t *testing.T) {
	for _, test := range []struct {
		pageID string
		query  string
		want   bool
	}{
		{pageID: "300MIUM-834", query: "MIUM-834", want: true},
		{pageID: "MIUM-834", query: "300MIUM-834", want: true},
		{pageID: "300MIUM-835", query: "MIUM-834", want: false},
		{pageID: "JNT-051", query: "JNT-051", want: true},
	} {
		if got := pageContainsMovieID(test.pageID, test.query); got != test.want {
			t.Errorf("pageContainsMovieID(%q, %q) = %v, want %v", test.pageID, test.query, got, test.want)
		}
	}
}

func TestParseVerifiedActressPageRequiresFirstInformationBlock(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(laterDMMLinkFixture("300MIUMその他", "300MIUM-834")))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if actress, ok := parseVerifiedActressPage(doc, "300MIUM-834"); ok {
		t.Fatalf("non-actress page accepted: %+v", actress)
	}
}

func newFixtureServer(t *testing.T, fixture func(baseURL string, request *http.Request) (string, int)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, status := fixture(server.URL, request)
		writer.Header().Set("Content-Type", "text/html; charset=EUC-JP")
		writer.WriteHeader(status)
		encoded, _, err := transform.String(japanese.EUCJP.NewEncoder(), body)
		if err != nil {
			t.Errorf("encode EUC-JP fixture: %v", err)
			return
		}
		if _, err := writer.Write([]byte(encoded)); err != nil {
			t.Errorf("write fixture: %v", err)
		}
	}))
	return server
}

func searchFixture(paths ...string) string {
	body := "<html><body>"
	for _, path := range paths {
		body += fmt.Sprintf(`<div class="result-box"><div class="body"><h3 class="keyword"><a href="%s">result</a></h3></div></div>`, path)
	}
	return body + "</body></html>"
}

func actressFixture(movieID, name string, dmmID int) string {
	dmmURL := fmt.Sprintf("https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=%d/sort=date/", dmmID)
	return fmt.Sprintf(`<html><body><div id="content_block_1"><h3 id="content_1"><a href="%s">%s</a></h3><p>%s</p></div></body></html>`, dmmURL, name, movieID)
}

func invalidFirstBlockFixture(title, movieID string) string {
	return fmt.Sprintf(`<html><body><div id="content_block_1"><h3 id="content_1">%s</h3><p>%s</p></div></body></html>`, title, movieID)
}

func laterDMMLinkFixture(title, movieID string) string {
	return fmt.Sprintf(`<html><body><div id="content_block_1"><h3 id="content_1">%s</h3><p>%s</p></div><div id="content_block_2"><h3><a href="https://www.dmm.co.jp/mono/dvd/-/list/=/article=actress/id=999/sort=date/">誤検出</a></h3></div></body></html>`, title, movieID)
}

func TestGetURL(t *testing.T) {
	scraper := New(config.ScraperSettings{BaseURL: "https://example.com/wiki/"}, nil, config.FlareSolverrConfig{})
	raw, err := scraper.GetURL("300MIUM-834")
	if err != nil {
		t.Fatalf("GetURL() error = %v", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if parsed.Path != "/wiki/search" || parsed.Query().Get("keywords") != "300MIUM-834" || parsed.Query().Get("search_target") != "all" {
		t.Errorf("unexpected search URL: %s", raw)
	}
}
