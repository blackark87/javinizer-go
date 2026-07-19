package paipancon

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const (
	scraperName    = "paipancon"
	defaultBaseURL = "https://paipancon.com"
)

var (
	fc2IDPattern    = regexp.MustCompile(`(?i)FC2[\s_-]*PPV[\s_-]*(\d{5,10})`)
	datePattern     = regexp.MustCompile(`\b(20\d{2})-(\d{2})-(\d{2})\b`)
	durationPattern = regexp.MustCompile(`\b(\d{1,2}):(\d{2}):(\d{2})\b`)
)

type scraper struct {
	client      *resty.Client
	enabled     bool
	baseURL     string
	rateLimiter *ratelimit.Limiter
	settings    models.ScraperSettings
}

func newScraper(settings *models.ScraperSettings, globalProxy *models.ProxyConfig, flaresolverr models.FlareSolverrConfig) *scraper {
	result := httpclient.InitScraperClient(settings, globalProxy, flaresolverr,
		httpclient.WithScraperHeaders(httpclient.CombineHeaders(
			httpclient.StandardHTMLHeaders(), httpclient.UserAgentHeader(settings.UserAgent),
			map[string]string{"Accept-Language": "ja,en-US;q=0.8,en;q=0.6"},
		)),
	)
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &scraper{client: result.Client, enabled: settings.Enabled, baseURL: baseURL, rateLimiter: ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond), settings: *settings}
}

func (s *scraper) Name() string                    { return scraperName }
func (s *scraper) IsEnabled() bool                 { return s.enabled }
func (s *scraper) Config() *models.ScraperSettings { c := s.settings.Clone(); return &c }
func (s *scraper) Close() error                    { return nil }

func canonicalID(input string) string {
	m := fc2IDPattern.FindStringSubmatch(input)
	if len(m) != 2 {
		return ""
	}
	return "FC2-PPV-" + m[1]
}

func (s *scraper) ResolveSearchQuery(input string) (string, bool) {
	id := canonicalID(input)
	return id, id != ""
}

func (s *scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && strings.EqualFold(u.Hostname(), "paipancon.com") && canonicalID(u.Path) != ""
}

func (s *scraper) ExtractIDFromURL(rawURL string) (string, error) {
	if id := canonicalID(rawURL); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("failed to extract FC2 ID from Paipancon URL")
}

func (s *scraper) GetURL(_ context.Context, id string) (string, error) {
	id = canonicalID(id)
	if id == "" {
		return "", models.NewScraperNotFoundError("Paipancon", "invalid FC2 ID")
	}
	return s.baseURL + "/fc2daily/detail/" + id, nil
}

func (s *scraper) ScrapeURL(ctx context.Context, rawURL string) (*models.ScraperResult, error) {
	id, err := s.ExtractIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	return s.scrapeDetail(ctx, id, rawURL)
}

func (s *scraper) Search(ctx context.Context, input string) (*models.ScraperResult, error) {
	id := canonicalID(input)
	if id == "" {
		return nil, models.NewScraperNotFoundError("Paipancon", "invalid FC2 ID")
	}
	return s.scrapeDetail(ctx, id, s.baseURL+"/fc2daily/detail/"+id)
}

func (s *scraper) fetch(ctx context.Context, rawURL string) (*goquery.Document, int, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, 0, err
	}
	resp, err := s.client.R().SetContext(ctx).Get(rawURL)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, resp.StatusCode(), nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	return doc, resp.StatusCode(), err
}

func (s *scraper) scrapeDetail(ctx context.Context, id, detailURL string) (*models.ScraperResult, error) {
	doc, status, err := s.fetch(ctx, detailURL)
	if err != nil {
		return nil, fmt.Errorf("fetch Paipancon detail: %w", err)
	}
	if status == http.StatusNotFound {
		return nil, models.NewScraperNotFoundError("Paipancon", "page not found")
	}
	if status != http.StatusOK {
		return nil, models.NewScraperStatusError("Paipancon", status, "detail request failed")
	}

	heading := strings.TrimSpace(doc.Find("h2.text-center").First().Text())
	prefix := id + " - "
	if !strings.HasPrefix(strings.ToUpper(heading), strings.ToUpper(prefix)) {
		return nil, models.NewScraperNotFoundError("Paipancon", "matching detail not found")
	}
	title := strings.TrimSpace(heading[len(prefix):])
	result := &models.ScraperResult{Source: scraperName, SourceURL: detailURL, Language: "ja", ID: id, ContentID: id, Title: title, OriginalTitle: title}

	doc.Find(`a[href*="/fc2daily/actor/"]`).Each(func(_ int, sel *goquery.Selection) {
		if name := strings.TrimSpace(sel.Text()); name != "" {
			result.Actresses = append(result.Actresses, models.ActressInfo{JapaneseName: name})
		}
	})
	doc.Find("span").EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		if strings.Contains(sel.Text(), "Seller Name:") {
			result.Maker = strings.TrimSpace(sel.Find("a").First().Text())
			return false
		}
		return true
	})

	bodyText := doc.Text()
	if match := datePattern.FindStringSubmatch(bodyText); len(match) == 4 {
		if parsed, parseErr := time.Parse("2006-01-02", strings.Join(match[1:], "-")); parseErr == nil {
			result.ReleaseDate = &parsed
		}
	}
	seconds := 0
	fileSummary := doc.Find("div.container-md.my-container").First().Text()
	for _, match := range durationPattern.FindAllStringSubmatch(fileSummary, -1) {
		hours, _ := strconv.Atoi(match[1])
		minutes, _ := strconv.Atoi(match[2])
		secs, _ := strconv.Atoi(match[3])
		seconds += hours*3600 + minutes*60 + secs
	}
	if seconds > 0 {
		result.Runtime = (seconds + 30) / 60
	}

	doc.Find("img.media-item").Each(func(_ int, sel *goquery.Selection) {
		src, _ := sel.Attr("src")
		resolved := scraperutil.ResolveURL(detailURL, src)
		switch {
		case strings.HasSuffix(strings.ToLower(src), "/cover.jpg"):
			result.CoverURL, result.PosterURL = resolved, resolved
		case strings.HasSuffix(strings.ToLower(src), "/grid.jpg"):
			result.ScreenshotURL = append(result.ScreenshotURL, resolved)
		}
	})
	if len(result.Actresses) == 0 {
		result.Actresses = s.searchActresses(ctx, id)
	}
	return result, nil
}

func (s *scraper) searchActresses(ctx context.Context, id string) []models.ActressInfo {
	doc, status, err := s.fetch(ctx, s.baseURL+"/fc2daily/search/"+id)
	if err != nil || status != http.StatusOK {
		return nil
	}
	var actresses []models.ActressInfo
	doc.Find(".fc2-box").EachWithBreak(func(_ int, card *goquery.Selection) bool {
		href, _ := card.Find(`a[href*="/fc2daily/detail/"]`).First().Attr("href")
		if canonicalID(href) != id {
			return true
		}
		card.Find(".card-body > div a").Each(func(_ int, a *goquery.Selection) {
			if name := strings.TrimSpace(a.Text()); name != "" {
				actresses = append(actresses, models.ActressInfo{JapaneseName: name})
			}
		})
		return false
	})
	if len(actresses) > 1 {
		actresses = actresses[:1]
	}
	return actresses
}

var _ models.Scraper = (*scraper)(nil)
var _ models.URLHandler = (*scraper)(nil)
var _ models.ScraperQueryResolver = (*scraper)(nil)
