package sougouwiki

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"golang.org/x/net/html/charset"
)

const (
	scraperName    = "sougouwiki"
	displayName    = "SougouWiki"
	defaultBaseURL = "https://seesaawiki.jp/w/sougouwiki/"
)

var (
	dmmActressIDPattern  = regexp.MustCompile(`/article=actress/id=(\d+)`)
	movieTokenPattern    = regexp.MustCompile(`(?i)[0-9]*[a-z][a-z0-9]*[\s_-]*\d+`)
	readingSuffixPattern = regexp.MustCompile(`\s*[（(][^）)]*[）)]\s*$`)
)

// Scraper resolves verified actress identities from SougouWiki. It deliberately
// returns no movie metadata beyond the query ID and actress list.
type Scraper struct {
	client      *resty.Client
	enabled     bool
	baseURL     string
	rateLimiter *ratelimit.Limiter
	settings    config.ScraperSettings
}

func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, flareSolverr config.FlareSolverrConfig) *Scraper {
	base := strings.TrimSpace(settings.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	base = strings.TrimRight(base, "/") + "/"

	clientResult := httpclient.InitScraperClient(&settings, globalProxy, flareSolverr,
		httpclient.WithScraperHeaders(httpclient.CombineHeaders(
			httpclient.JapaneseLanguageHeaders(),
			httpclient.UserAgentHeader(settings.UserAgent),
		)),
	)
	if parsedBase, err := url.Parse(base); err == nil && parsedBase.Hostname() != "" {
		clientResult.Client.SetRedirectPolicy(resty.DomainCheckRedirectPolicy(parsedBase.Hostname()))
	}

	return &Scraper{
		client:      clientResult.Client,
		enabled:     settings.Enabled,
		baseURL:     base,
		rateLimiter: ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:    settings,
	}
}

func (s *Scraper) Name() string { return scraperName }

func (s *Scraper) IsEnabled() bool { return s.enabled }

func (s *Scraper) Config() *config.ScraperSettings { return s.settings.DeepCopy() }

func (s *Scraper) Close() error { return nil }

func (s *Scraper) GetURL(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("%s: movie ID cannot be empty", scraperName)
	}
	searchURL, err := url.Parse(s.baseURL + "search")
	if err != nil {
		return "", fmt.Errorf("%s: invalid base URL: %w", scraperName, err)
	}
	query := searchURL.Query()
	query.Set("keywords", id)
	query.Set("search_target", "all")
	searchURL.RawQuery = query.Encode()
	return searchURL.String(), nil
}

func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	return s.ResolveActresses(ctx, id)
}

// ResolveActresses implements models.ActressResolver.
func (s *Scraper) ResolveActresses(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("%s scraper is disabled", displayName)
	}

	id = strings.TrimSpace(id)
	searchURL, err := s.GetURL(id)
	if err != nil {
		return nil, err
	}

	searchDoc, err := s.fetchDocument(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	candidates := s.extractCandidateURLs(searchDoc)
	actresses := make([]models.ActressInfo, 0)
	seenDMMIDs := make(map[int]struct{})
	for _, candidateURL := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		candidateDoc, fetchErr := s.fetchDocument(ctx, candidateURL)
		if fetchErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			logging.Debugf("SougouWiki: candidate %s could not be read: %v", candidateURL, fetchErr)
			continue
		}

		actress, ok := parseVerifiedActressPage(candidateDoc, id)
		if !ok {
			continue
		}
		if _, exists := seenDMMIDs[actress.DMMID]; exists {
			continue
		}
		seenDMMIDs[actress.DMMID] = struct{}{}
		actresses = append(actresses, actress)
	}

	if len(actresses) == 0 {
		return nil, models.NewScraperNotFoundError(displayName, fmt.Sprintf("no verified actress page found for %s", id))
	}

	return &models.ScraperResult{
		Source:    scraperName,
		SourceURL: searchURL,
		Language:  "ja",
		ID:        id,
		Actresses: actresses,
	}, nil
}

func (s *Scraper) fetchDocument(ctx context.Context, targetURL string) (*goquery.Document, error) {
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	resp, err := s.client.R().SetContext(ctx).Get(targetURL)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", displayName, err)
	}
	if resp.StatusCode() != 200 {
		return nil, models.NewScraperStatusError(displayName, resp.StatusCode(), fmt.Sprintf("request returned HTTP %d", resp.StatusCode()))
	}

	reader, err := charset.NewReader(bytes.NewReader(resp.Body()), resp.Header().Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("%s response decoding failed: %w", displayName, err)
	}
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, fmt.Errorf("%s HTML parsing failed: %w", displayName, err)
	}
	return doc, nil
}

func (s *Scraper) extractCandidateURLs(doc *goquery.Document) []string {
	base, err := url.Parse(s.baseURL)
	if err != nil || doc == nil {
		return nil
	}
	allowedPrefix := strings.TrimRight(base.Path, "/") + "/d/"

	seen := make(map[string]struct{})
	candidates := make([]string, 0)
	doc.Find("div.result-box div.body h3.keyword a").Each(func(_ int, link *goquery.Selection) {
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		candidate, parseErr := base.Parse(strings.TrimSpace(href))
		if parseErr != nil || !strings.EqualFold(candidate.Hostname(), base.Hostname()) {
			return
		}
		if !strings.HasPrefix(candidate.Path, allowedPrefix) {
			return
		}
		candidate.Fragment = ""
		candidate.RawQuery = ""
		normalized := candidate.String()
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, normalized)
	})
	return candidates
}

func parseVerifiedActressPage(doc *goquery.Document, movieID string) (models.ActressInfo, bool) {
	if doc == nil || !pageContainsMovieID(doc.Text(), movieID) {
		return models.ActressInfo{}, false
	}

	heading := doc.Find("#content_block_1 h3#content_1").First()
	if heading.Length() == 0 {
		return models.ActressInfo{}, false
	}

	var result models.ActressInfo
	heading.Find("a").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		href, ok := link.Attr("href")
		if !ok || !isDMMActressURL(href) {
			return true
		}
		match := dmmActressIDPattern.FindStringSubmatch(href)
		if len(match) < 2 {
			return true
		}
		dmmID, err := strconv.Atoi(match[1])
		if err != nil || dmmID <= 0 {
			return true
		}
		name := cleanCanonicalActressName(link.Text())
		if name == "" {
			return true
		}
		result = models.ActressInfo{DMMID: dmmID, JapaneseName: name}
		return false
	})
	return result, result.DMMID > 0 && result.JapaneseName != ""
}

func isDMMActressURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "dmm.co.jp" && !strings.HasSuffix(host, ".dmm.co.jp") &&
		host != "dmm.com" && !strings.HasSuffix(host, ".dmm.com") {
		return false
	}
	return dmmActressIDPattern.MatchString(parsed.Path)
}

func cleanCanonicalActressName(name string) string {
	name = scraperutil.CleanString(name)
	name = readingSuffixPattern.ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

func pageContainsMovieID(text, movieID string) bool {
	want := normalizeMovieID(movieID)
	if want == "" {
		return false
	}
	for _, token := range movieTokenPattern.FindAllString(text, -1) {
		if equivalentMovieID(normalizeMovieID(token), want) {
			return true
		}
	}
	return false
}

func normalizeMovieID(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func equivalentMovieID(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	return stripNumericDistributorPrefix(left) == stripNumericDistributorPrefix(right)
}

func stripNumericDistributorPrefix(value string) string {
	index := 0
	for index < len(value) && value[index] >= '0' && value[index] <= '9' {
		index++
	}
	if index > 0 && index < len(value) && value[index] >= 'A' && value[index] <= 'Z' {
		return value[index:]
	}
	return value
}

var (
	_ models.Scraper         = (*Scraper)(nil)
	_ models.ActressResolver = (*Scraper)(nil)
)
