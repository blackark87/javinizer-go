package one23av

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
	scraperName    = "123av"
	defaultBaseURL = "https://123av.com"
)

var (
	movieIDPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9]{1,20})[\s_-]+(\d{2,10})(?:[\s_-]+(uncensored(?:[\s_-]+leaked)?))?`)
	datePattern    = regexp.MustCompile(`\b(20\d{2})[-/.](\d{1,2})[-/.](\d{1,2})\b`)
	clockPattern   = regexp.MustCompile(`\b(?:(\d{1,2}):)?(\d{1,2}):(\d{2})\b`)
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
			httpclient.StandardHTMLHeaders(),
			httpclient.UserAgentHeader(settings.UserAgent),
			map[string]string{"Accept-Language": "ja,en-US;q=0.8,en;q=0.6"},
		)),
	)
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &scraper{
		client:      result.Client,
		enabled:     settings.Enabled,
		baseURL:     baseURL,
		rateLimiter: ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:    *settings,
	}
}

func (s *scraper) Name() string                    { return scraperName }
func (s *scraper) IsEnabled() bool                 { return s.enabled }
func (s *scraper) Config() *models.ScraperSettings { c := s.settings.Clone(); return &c }
func (s *scraper) Close() error                    { return nil }

func canonicalID(input string) string {
	match := movieIDPattern.FindStringSubmatch(input)
	if len(match) < 3 {
		return ""
	}
	id := strings.ToUpper(match[1]) + "-" + match[2]
	if len(match) > 3 && strings.TrimSpace(match[3]) != "" {
		suffix := strings.ToUpper(strings.NewReplacer("_", "-", " ", "-").Replace(match[3]))
		id += "-" + suffix
	}
	return id
}

func (s *scraper) ResolveSearchQuery(input string) (string, bool) {
	id := canonicalID(input)
	return id, id != ""
}

func (s *scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "123av.com" || host == "www.123av.com" || host == "njav.tv" || host == "www.njav.tv") && canonicalID(u.Path) != ""
}

func (s *scraper) ExtractIDFromURL(rawURL string) (string, error) {
	if id := canonicalID(rawURL); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("failed to extract movie ID from 123AV URL")
}

func (s *scraper) GetURL(_ context.Context, id string) (string, error) {
	id = canonicalID(id)
	if id == "" {
		return "", models.NewScraperNotFoundError("123AV", "invalid movie ID")
	}
	return s.baseURL + "/ja/v/" + strings.ToLower(id), nil
}

func (s *scraper) Search(ctx context.Context, input string) (*models.ScraperResult, error) {
	id := canonicalID(input)
	if id == "" {
		return nil, models.NewScraperNotFoundError("123AV", "invalid movie ID")
	}
	pageURL, _ := s.GetURL(ctx, id)
	return s.scrapePage(ctx, id, pageURL, "ja")
}

func (s *scraper) ScrapeURL(ctx context.Context, rawURL string) (*models.ScraperResult, error) {
	id, err := s.ExtractIDFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	language := "ja"
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil && strings.Contains(strings.ToLower(parsed.Path), "/en/") {
		language = "en"
	}
	return s.scrapePage(ctx, id, rawURL, language)
}

func (s *scraper) scrapePage(ctx context.Context, requestedID, pageURL, language string) (*models.ScraperResult, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}
	resp, err := s.client.R().SetContext(ctx).Get(pageURL)
	if err != nil {
		return nil, fmt.Errorf("fetch 123AV detail: %w", err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, models.NewScraperNotFoundError("123AV", "page not found")
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, models.NewScraperStatusError("123AV", resp.StatusCode(), "detail request failed")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return nil, fmt.Errorf("parse 123AV detail: %w", err)
	}
	return parseDetail(doc, requestedID, resp.Request.URL, language)
}

func parseDetail(doc *goquery.Document, requestedID, sourceURL, language string) (*models.ScraperResult, error) {
	heading := strings.TrimSpace(doc.Find("h1").First().Text())
	pageID := canonicalID(heading)
	if pageID == "" || !strings.EqualFold(pageID, requestedID) {
		return nil, models.NewScraperNotFoundError("123AV", "matching detail not found")
	}
	title := strings.TrimSpace(heading)
	for _, separator := range []string{" — ", " – ", " - ", "：", ":"} {
		prefix := pageID + separator
		if strings.HasPrefix(strings.ToUpper(title), strings.ToUpper(prefix)) {
			title = strings.TrimSpace(title[len(prefix):])
			break
		}
	}

	result := &models.ScraperResult{
		Source:        scraperName,
		SourceURL:     sourceURL,
		Language:      scraperutil.NormalizeLanguage(language),
		ID:            pageID,
		ContentID:     pageID,
		Title:         title,
		OriginalTitle: title,
	}

	if value := detailText(doc, "Release date", "発売日", "リリース日"); value != "" {
		if match := datePattern.FindStringSubmatch(value); len(match) == 4 {
			if parsed, err := time.Parse("2006-1-2", strings.Join(match[1:], "-")); err == nil {
				result.ReleaseDate = &parsed
			}
		}
	}
	if value := detailText(doc, "Duration", "収録時間", "再生時間", "時間"); value != "" {
		result.Runtime = parseRuntime(value)
	}

	result.Actresses = actressLinks(detailLinks(doc, []string{"Cast", "出演者", "女優", "キャスト"}, "/actresses/"))
	result.Maker = firstLinkText(detailLinks(doc, []string{"Maker", "メーカー", "制作会社"}, "/makers/"))
	result.Series = firstLinkText(detailLinks(doc, []string{"Series", "シリーズ"}, "/series/"))
	for _, value := range detailLinks(doc, []string{"Tags", "タグ"}, "/tags/") {
		if value != "" && !contains(result.Genres, value) {
			result.Genres = append(result.Genres, value)
		}
	}

	imageURL := firstAttribute(doc, `meta[property="og:image"]`, "content")
	if imageURL == "" {
		imageURL = firstAttribute(doc, "video[poster]", "poster")
	}
	if imageURL != "" {
		result.CoverURL = scraperutil.ResolveURL(sourceURL, imageURL)
		result.PosterURL = result.CoverURL
	}
	return result, nil
}

func detailText(doc *goquery.Document, labels ...string) string {
	label := findLabel(doc, labels...)
	if label == nil {
		return ""
	}
	if next := label.Next(); next.Length() > 0 {
		if value := strings.TrimSpace(next.Text()); value != "" {
			return value
		}
	}
	if parent := label.Parent(); parent.Length() > 0 {
		text := strings.TrimSpace(parent.Text())
		own := strings.TrimSpace(label.Text())
		return strings.TrimSpace(strings.TrimPrefix(text, own))
	}
	return ""
}

func detailLinks(doc *goquery.Document, labels []string, hrefPart string) []string {
	label := findLabel(doc, labels...)
	if label == nil {
		return nil
	}
	selectors := []*goquery.Selection{label.Next(), label.Parent()}
	for _, scope := range selectors {
		if scope == nil || scope.Length() == 0 {
			continue
		}
		values := make([]string, 0)
		scope.Find("a").AddBackFiltered("a").Each(func(_ int, link *goquery.Selection) {
			href, _ := link.Attr("href")
			if strings.Contains(strings.ToLower(href), hrefPart) {
				if value := strings.TrimSpace(link.Text()); value != "" && !contains(values, value) {
					values = append(values, value)
				}
			}
		})
		if len(values) > 0 {
			return values
		}
	}
	return nil
}

func findLabel(doc *goquery.Document, labels ...string) *goquery.Selection {
	wanted := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		wanted[normalizeLabel(label)] = struct{}{}
	}
	var found *goquery.Selection
	doc.Find("dt, th, div, span, p").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		if _, ok := wanted[normalizeLabel(ownText(selection))]; ok {
			found = selection
			return false
		}
		return true
	})
	return found
}

func ownText(selection *goquery.Selection) string {
	clone := selection.Clone()
	clone.Children().Remove()
	return strings.TrimSpace(clone.Text())
}

func normalizeLabel(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimRight(value, ":：")))
}

func parseRuntime(value string) int {
	match := clockPattern.FindStringSubmatch(value)
	if len(match) != 4 {
		return 0
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	seconds, _ := strconv.Atoi(match[3])
	return hours*60 + minutes + (seconds+30)/60
}

func actressLinks(names []string) []models.ActressInfo {
	result := make([]models.ActressInfo, 0, len(names))
	for _, name := range names {
		result = append(result, models.ActressInfo{JapaneseName: name})
	}
	return result
}

func firstLinkText(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstAttribute(doc *goquery.Document, selector, attribute string) string {
	value, _ := doc.Find(selector).First().Attr(attribute)
	return strings.TrimSpace(value)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var _ models.Scraper = (*scraper)(nil)
var _ models.URLHandler = (*scraper)(nil)
var _ models.ScraperQueryResolver = (*scraper)(nil)
