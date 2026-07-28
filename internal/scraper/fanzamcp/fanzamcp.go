package fanzamcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

const (
	scraperName           = "fanzamcp"
	apiSourceName         = "fanza-mcp"
	displayName           = "FANZA MCP"
	defaultBaseURL        = "http://fanza-mcp:8000"
	defaultTimeoutSeconds = 30
)

var (
	displayCodePattern  = regexp.MustCompile(`(?i)^([a-z]+)[\s_-]*0*(\d+)$`)
	contentIDPattern    = regexp.MustCompile(`(?i)^([a-z]+)(\d+)([a-z0-9]*)$`)
	standardCodePattern = regexp.MustCompile(`^[A-Z]{2,10}-\d{2,5}$`)
)

type scraper struct {
	client         *resty.Client
	enabled        bool
	baseURL        string
	mediaAuthority string
	rateLimiter    *ratelimit.Limiter
	settings       models.ScraperSettings
}

type metadataResponse struct {
	Source           string               `json:"source"`
	SourceURL        string               `json:"source_url"`
	Language         string               `json:"language"`
	ID               string               `json:"id"`
	ContentID        string               `json:"content_id"`
	Title            string               `json:"title"`
	Description      string               `json:"description"`
	ReleaseDate      *string              `json:"release_date"`
	Actresses        []models.ActressInfo `json:"actresses"`
	PosterURL        string               `json:"poster_url"`
	CoverURL         string               `json:"cover_url"`
	ScreenshotURLs   []string             `json:"screenshot_urls"`
	TrailerURL       string               `json:"trailer_url"`
	ShouldCropPoster bool                 `json:"should_crop_poster"`
}

func newScraper(settings *models.ScraperSettings) *scraper {
	if settings == nil {
		settings = &models.ScraperSettings{}
	}
	copied := settings.Clone()
	baseURL := strings.TrimRight(strings.TrimSpace(copied.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	timeoutSeconds := copied.Timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultTimeoutSeconds
	}
	parsed, _ := url.Parse(baseURL)
	authority := ""
	if parsed != nil {
		authority = strings.ToLower(parsed.Host)
	}

	return &scraper{
		client:         httpclient.NewRestyClientNoProxy(time.Duration(timeoutSeconds)*time.Second, copied.RetryCount),
		enabled:        copied.Enabled,
		baseURL:        baseURL,
		mediaAuthority: authority,
		rateLimiter:    ratelimit.NewLimiter(time.Duration(copied.RateLimit) * time.Millisecond),
		settings:       copied,
	}
}

func (s *scraper) Name() string                    { return scraperName }
func (s *scraper) IsEnabled() bool                 { return s.enabled }
func (s *scraper) Config() *models.ScraperSettings { c := s.settings.Clone(); return &c }

func (s *scraper) Close() error {
	if s != nil && s.client != nil && s.client.GetClient() != nil {
		s.client.GetClient().CloseIdleConnections()
	}
	return nil
}

func (s *scraper) GetURL(_ context.Context, id string) (string, error) {
	code, ok := normalizeProductCode(id)
	if !ok {
		return "", models.NewScraperNotFoundError(displayName, "invalid product code")
	}
	return s.baseURL + "/api/v1/metadata/" + url.PathEscape(code), nil
}

func (s *scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	code, ok := normalizeProductCode(id)
	if !ok {
		return nil, models.NewScraperNotFoundError(displayName, "invalid product code")
	}
	endpoint, _ := s.GetURL(ctx, code)
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Accept", "application/json").
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetch FANZA MCP metadata: %w", err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, models.NewScraperNotFoundError(displayName, "metadata not found")
	}
	if resp.StatusCode() < http.StatusOK || resp.StatusCode() >= http.StatusMultipleChoices {
		return nil, models.NewScraperStatusError(displayName, resp.StatusCode(), "metadata request failed")
	}

	var payload metadataResponse
	if err := json.Unmarshal(resp.Body(), &payload); err != nil {
		return nil, fmt.Errorf("parse FANZA MCP metadata response: %w", err)
	}
	return s.mapResponse(code, payload)
}

func (s *scraper) mapResponse(requestedCode string, payload metadataResponse) (*models.ScraperResult, error) {
	responseCode, ok := normalizeProductCode(payload.ID)
	if !ok || responseCode != requestedCode {
		return nil, fmt.Errorf("FANZA MCP returned mismatched product code %q for %s", payload.ID, requestedCode)
	}
	source := strings.TrimSpace(payload.Source)
	if source == "" {
		return nil, fmt.Errorf("FANZA MCP response is missing source")
	}
	if source != apiSourceName {
		return nil, fmt.Errorf("FANZA MCP returned unexpected source %q", payload.Source)
	}
	if strings.TrimSpace(payload.Title) == "" {
		return nil, fmt.Errorf("FANZA MCP response is missing title")
	}

	posterURL, err := s.validateMediaURL(requestedCode, "poster_url", payload.PosterURL, true)
	if err != nil {
		return nil, err
	}
	coverURL, err := s.validateMediaURL(requestedCode, "cover_url", payload.CoverURL, false)
	if err != nil {
		return nil, err
	}
	trailerURL, err := s.validateMediaURL(requestedCode, "trailer_url", payload.TrailerURL, false)
	if err != nil {
		return nil, err
	}
	screenshots := make([]string, 0, len(payload.ScreenshotURLs))
	for index, raw := range payload.ScreenshotURLs {
		resolved, err := s.validateMediaURL(
			requestedCode,
			fmt.Sprintf("screenshot_urls[%d]", index),
			raw,
			false,
		)
		if err != nil {
			return nil, err
		}
		if resolved != "" {
			screenshots = append(screenshots, resolved)
		}
	}

	var releaseDate *time.Time
	if payload.ReleaseDate != nil && strings.TrimSpace(*payload.ReleaseDate) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*payload.ReleaseDate))
		if err != nil {
			return nil, fmt.Errorf("parse FANZA MCP release_date %q: %w", *payload.ReleaseDate, err)
		}
		releaseDate = &parsed
	}

	title := strings.TrimSpace(payload.Title)
	return &models.ScraperResult{
		// The API contract uses "fanza-mcp", while scraper priorities and
		// configuration use "fanzamcp". Normalize at the adapter boundary so
		// the aggregator can match this result to the selected scraper.
		Source:           scraperName,
		SourceURL:        strings.TrimSpace(payload.SourceURL),
		Language:         strings.TrimSpace(payload.Language),
		ID:               strings.TrimSpace(payload.ID),
		ContentID:        strings.TrimSpace(payload.ContentID),
		Title:            title,
		OriginalTitle:    title,
		Description:      strings.TrimSpace(payload.Description),
		ReleaseDate:      releaseDate,
		Actresses:        payload.Actresses,
		PosterURL:        posterURL,
		CoverURL:         coverURL,
		ShouldCropPoster: payload.ShouldCropPoster,
		ScreenshotURL:    screenshots,
		TrailerURL:       trailerURL,
	}, nil
}

func (s *scraper) validateMediaURL(productCode, field, raw string, required bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return "", fmt.Errorf("FANZA MCP response is missing %s", field)
		}
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return "", fmt.Errorf("FANZA MCP %s must be an absolute internal HTTP URL", field)
	}
	if parsed.User != nil || !strings.EqualFold(parsed.Host, s.mediaAuthority) {
		return "", fmt.Errorf("FANZA MCP %s host %q does not match metadata service", field, parsed.Host)
	}
	expectedPrefix := "/api/v1/media/" + productCode + "/"
	if !strings.HasPrefix(parsed.Path, expectedPrefix) {
		return "", fmt.Errorf("FANZA MCP %s path is outside the product media endpoint", field)
	}
	return parsed.String(), nil
}

func (s *scraper) ResolveDownloadProxyForHost(host string) (*models.ProxyConfig, *models.ProxyConfig, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	serviceHost := s.mediaAuthority
	if parsed, err := url.Parse("http://" + serviceHost); err == nil {
		serviceHost = strings.ToLower(parsed.Hostname())
	}
	if host != serviceHost {
		return nil, nil, false
	}
	return &models.ProxyConfig{Enabled: false}, nil, true
}

func normalizeProductCode(input string) (string, bool) {
	original := strings.TrimSpace(input)
	if original == "" {
		return "", false
	}

	if match := displayCodePattern.FindStringSubmatch(original); len(match) == 3 {
		return buildStandardCode(match[1], match[2])
	}

	cleaned := strings.ToLower(original)
	cleaned = strings.TrimPrefix(cleaned, "h_")
	cleaned = strings.TrimLeft(cleaned, "0123456789")
	if match := contentIDPattern.FindStringSubmatch(cleaned); len(match) == 4 {
		return buildStandardCode(match[1], match[2])
	}
	return "", false
}

func buildStandardCode(label, digits string) (string, bool) {
	label = strings.ToUpper(strings.TrimSpace(label))
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}
	if len(digits) < 3 {
		digits = strings.Repeat("0", 3-len(digits)) + digits
	}
	code := label + "-" + digits
	return code, standardCodePattern.MatchString(code)
}

var (
	_ models.Scraper               = (*scraper)(nil)
	_ models.DownloadProxyResolver = (*scraper)(nil)
)
