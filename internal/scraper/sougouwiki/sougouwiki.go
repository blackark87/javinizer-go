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
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	scraperName    = "sougouwiki"
	displayName    = "SougouWiki"
	defaultBaseURL = "https://seesaawiki.jp/w/sougouwiki/"
)

var (
	dmmActressIDPattern  = regexp.MustCompile(`/article=actress/id=(\d+)`)
	movieTokenPattern    = regexp.MustCompile(`(?i)[0-9]*[a-z][a-z0-9]*(?:[\s_-]+[a-z][a-z0-9]*)*[\s_-]*\d+`)
	readingSuffixPattern = regexp.MustCompile(`\s*[（(][^）)]*[）)]\s*$`)
)

// Scraper resolves verified actress identities from SougouWiki. It deliberately
// returns no movie metadata beyond the query ID and actress list.
type Scraper struct {
	client      *resty.Client
	enabled     bool
	baseURL     string
	rateLimiter *ratelimit.Limiter
	settings    models.ScraperSettings
}

// New constructs a SougouWiki actress resolver from scraper settings.
func New(settings models.ScraperSettings, globalProxy *models.ProxyConfig, flareSolverr models.FlareSolverrConfig) *Scraper {
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

// Name returns the registry name for this scraper.
func (s *Scraper) Name() string { return scraperName }

// IsEnabled reports whether the resolver is enabled.
func (s *Scraper) IsEnabled() bool { return s.enabled }

// Config returns a copy of the scraper settings.
func (s *Scraper) Config() *models.ScraperSettings {
	cloned := s.settings.Clone()
	return &cloned
}

// Close releases scraper resources.
func (s *Scraper) Close() error { return nil }

// GetURL returns the SougouWiki search URL for a movie ID.
func (s *Scraper) GetURL(_ context.Context, id string) (string, error) {
	return s.getSearchURL(id, "all")
}

func (s *Scraper) getSearchURL(keyword, target string) (string, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", fmt.Errorf("%s: search keyword cannot be empty", scraperName)
	}
	searchURL, err := url.Parse(s.baseURL + "search")
	if err != nil {
		return "", fmt.Errorf("%s: invalid base URL: %w", scraperName, err)
	}
	encodedKeyword, _, err := transform.String(japanese.EUCJP.NewEncoder(), keyword)
	if err != nil {
		return "", fmt.Errorf("%s: encode search keyword: %w", scraperName, err)
	}
	searchURL.RawQuery = "keywords=" + url.QueryEscape(encodedKeyword) + "&search_target=" + url.QueryEscape(target)
	return searchURL.String(), nil
}

// Search resolves the verified actresses associated with a movie ID.
func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("%s scraper is disabled", displayName)
	}
	return s.ResolveActresses(ctx, id)
}

// ResolveActresses implements models.ActressResolver. The dedicated fallback
// remains callable even when the scraper is disabled for ordinary metadata
// searches; missing-DMM actress verification is an automatic safety step.
func (s *Scraper) ResolveActresses(ctx context.Context, id string) (*models.ScraperResult, error) {
	id = strings.TrimSpace(id)
	searchURL, err := s.GetURL(ctx, id)
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

// ResolveActressIdentity searches actress page names directly. It never
// fetches or re-scrapes linked movie metadata.
func (s *Scraper) ResolveActressIdentity(ctx context.Context, query models.ActressIdentityQuery) (*models.ScraperResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("%s scraper is disabled", displayName)
	}

	names := uniqueIdentityNames(query.Names)
	if len(names) == 0 {
		return nil, models.NewScraperNotFoundError(displayName, "no actress name is available")
	}

	var lastErr error

	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		searchURL, err := s.getSearchURL(name, "page_name")
		if err != nil {
			lastErr = err
			continue
		}
		searchDoc, err := s.fetchDocument(ctx, searchURL)
		if err != nil {
			lastErr = err
			continue
		}

		actresses := make([]models.ActressInfo, 0)
		seenDMMIDs := make(map[int]struct{})
		var sourceURL string
		for _, candidateURL := range s.extractExactCandidateURLs(searchDoc, name) {
			candidateDoc, fetchErr := s.fetchDocument(ctx, candidateURL)
			if fetchErr != nil {
				lastErr = fetchErr
				continue
			}
			actress, ok := parseActressIdentityPage(candidateDoc, name)
			if !ok {
				continue
			}
			if _, exists := seenDMMIDs[actress.DMMID]; exists {
				continue
			}
			seenDMMIDs[actress.DMMID] = struct{}{}
			actresses = append(actresses, actress)
			if sourceURL == "" {
				sourceURL = candidateURL
			}
		}
		if len(actresses) > 0 {
			return &models.ScraperResult{
				Source:    scraperName,
				SourceURL: sourceURL,
				Language:  "ja",
				ID:        name,
				Actresses: actresses,
			}, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, models.NewScraperNotFoundError(displayName, "no exact actress page match was found")
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

func (s *Scraper) extractExactCandidateURLs(doc *goquery.Document, name string) []string {
	if doc == nil {
		return nil
	}
	wanted := normalizeIdentityName(name)
	if wanted == "" {
		return nil
	}
	base, err := url.Parse(s.baseURL)
	if err != nil {
		return nil
	}
	allowedPrefix := strings.TrimRight(base.Path, "/") + "/d/"
	seen := make(map[string]struct{})
	var candidates []string
	doc.Find("div.result-box div.body h3.keyword a").Each(func(_ int, link *goquery.Selection) {
		if normalizeIdentityName(link.Text()) != wanted {
			return
		}
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		candidate, parseErr := base.Parse(strings.TrimSpace(href))
		if parseErr != nil || !strings.EqualFold(candidate.Hostname(), base.Hostname()) || !strings.HasPrefix(candidate.Path, allowedPrefix) {
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

func parseActressIdentityPage(doc *goquery.Document, matchedName string) (models.ActressInfo, bool) {
	if doc == nil || normalizeIdentityName(matchedName) == "" {
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
		result = models.ActressInfo{DMMID: dmmID, JapaneseName: strings.TrimSpace(matchedName)}
		return false
	})
	return result, result.DMMID > 0
}

func uniqueIdentityNames(names []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := normalizeIdentityName(name)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	return result
}

func normalizeIdentityName(name string) string {
	return strings.ToLower(scraperutil.CleanActressName(cleanCanonicalActressName(name)))
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

var _ models.ActressIdentityResolver = (*Scraper)(nil)

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
