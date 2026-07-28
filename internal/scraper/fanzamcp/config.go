package fanzamcp

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

var allowedServiceHosts = []string{"fanza-mcp"}

func validateScraperSettings(settings *models.ScraperSettings) error {
	if settings == nil {
		return fmt.Errorf("%s: config is nil", scraperName)
	}

	raw := strings.TrimSpace(settings.BaseURL)
	if raw == "" {
		raw = defaultBaseURL
	}
	if err := config.ValidateScraperBaseURL(scraperName+".base_url", raw, allowedServiceHosts); err != nil {
		return err
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s.base_url must be a valid URL: %w", scraperName, err)
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("%s.base_url must use the http scheme", scraperName)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s.base_url must not contain user information", scraperName)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s.base_url must not contain a query or fragment", scraperName)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("%s.base_url must not contain a path", scraperName)
	}
	return nil
}
