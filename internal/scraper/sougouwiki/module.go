package sougouwiki

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// Register adds SougouWiki as an internal actress resolver. It is called
// automatically for unresolved cast identities and never queried as a normal
// movie metadata provider.
func Register(reg scraperutil.ScraperRegistrar) {
	reg.Register(scraperutil.ScraperRegistration{
		Name:         scraperName,
		Description:  "SougouWiki actress resolver",
		InternalOnly: true,
		Options: []models.ScraperOption{
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "SougouWiki base URL used for actress lookups",
				Type:        "string",
				Default:     defaultBaseURL,
			},
			{
				Key:         "request_delay",
				Label:       "Request Delay",
				Description: "Delay between SougouWiki requests",
				Type:        "number",
				Default:     1000,
				Min:         scraperutil.IntPtr(0),
				Max:         scraperutil.IntPtr(5000),
				Unit:        "ms",
			},
		},
		Defaults: models.ScraperSettings{
			Enabled:   false,
			RateLimit: 1000,
			BaseURL:   defaultBaseURL,
		},
		Priority: 97,
		Constructor: func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
			settings := deps.Settings
			if settings.Timeout <= 0 {
				settings.Timeout = deps.TimeoutSeconds
			}
			return New(settings, deps.GlobalProxy, deps.FlareSolverr), nil
		},
		ValidateFn: validateScraperSettings,
	})
}
