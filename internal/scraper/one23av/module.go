package one23av

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// Register adds 123AV's server-rendered movie metadata pages to the catalog.
func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        scraperName,
		Description: "123AV",
		Options: []models.ScraperOption{
			{Key: "rate_limit", Label: "Rate Limit", Description: "Delay between requests", Type: "number", Min: scraperutil.IntPtr(0), Max: scraperutil.IntPtr(5000), Unit: "ms"},
			{Key: "base_url", Label: "Base URL", Description: "123AV base URL", Type: "string"},
		},
		Defaults: models.ScraperSettings{
			Enabled:   true,
			Language:  "ja",
			RateLimit: 1000,
			BaseURL:   defaultBaseURL,
		},
		Priority: 33,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: func(settings *models.ScraperSettings) error { return settings.Validate(scraperName) },
	})
}
