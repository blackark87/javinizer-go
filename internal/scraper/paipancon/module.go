package paipancon

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// Register adds Paipancon's FC2 Daily archive to the scraper catalog.
func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:        scraperName,
		Description: "Paipancon FC2 Daily",
		Options: []models.ScraperOption{
			{Key: "rate_limit", Label: "Rate Limit", Description: "Delay between requests", Type: "number", Min: scraperutil.IntPtr(0), Max: scraperutil.IntPtr(5000), Unit: "ms"},
			{Key: "base_url", Label: "Base URL", Description: "Paipancon base URL", Type: "string"},
		},
		Defaults: models.ScraperSettings{Enabled: true, RateLimit: 1000, BaseURL: defaultBaseURL},
		Priority: 34,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			return newScraper(&deps.Settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: func(settings *models.ScraperSettings) error { return settings.Validate(scraperName) },
	})
}
