package fanzamcp

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// Register adds the internal FANZA MCP metadata provider to the scraper catalog.
func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        scraperName,
		Description: "FANZA MCP",
		Options: []models.ScraperOption{
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "Internal FANZA MCP service URL",
				Type:        "string",
				Default:     defaultBaseURL,
			},
			{
				Key:         "rate_limit",
				Label:       "Rate Limit",
				Description: "Delay between metadata requests",
				Type:        "number",
				Min:         scraperutil.IntPtr(0),
				Max:         scraperutil.IntPtr(5000),
				Unit:        "ms",
			},
			{
				Key:         "timeout",
				Label:       "Timeout",
				Description: "Metadata request timeout",
				Type:        "number",
				Default:     defaultTimeoutSeconds,
				Min:         scraperutil.IntPtr(1),
				Max:         scraperutil.IntPtr(300),
				Unit:        "seconds",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			RateLimit: 0,
			Timeout:   defaultTimeoutSeconds,
			BaseURL:   defaultBaseURL,
		},
		Priority: 98,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
