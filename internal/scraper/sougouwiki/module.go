package sougouwiki

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func init() {
	m := &scraperModule{}
	m.StandardModule = scraperutil.StandardModule{
		ScraperName:        scraperName,
		ScraperDescription: "SougouWiki actress resolver",
		ScraperOptions: []any{
			models.ScraperOption{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "SougouWiki base URL used for actress lookups",
				Type:        "string",
				Default:     defaultBaseURL,
			},
			models.ScraperOption{
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
		ScraperDefaults: config.ScraperSettings{
			Enabled:   false,
			RateLimit: 1000,
			BaseURL:   defaultBaseURL,
		},
		ScraperPriority: 97,
		ConfigType:      func() scraperutil.ScraperConfigInterface { return &Config{} },
		NewScraperFunc: func(settings config.ScraperSettings, _ *database.DB, globalConfig *config.ScrapersConfig) (models.Scraper, error) {
			var globalProxy *config.ProxyConfig
			var flareSolverr config.FlareSolverrConfig
			if globalConfig != nil {
				if settings.Timeout <= 0 {
					settings.Timeout = globalConfig.TimeoutSeconds
				}
				if settings.UserAgent == "" {
					settings.UserAgent = globalConfig.UserAgent
				}
				globalProxy = &globalConfig.Proxy
				flareSolverr = globalConfig.FlareSolverr
			}
			return New(settings, globalProxy, flareSolverr), nil
		},
		FlatOverrides: scraperutil.FlattenOverrides{BaseURL: defaultBaseURL},
		FlatBuilder: func(fc *scraperutil.FlattenedConfig, overrides scraperutil.FlattenOverrides) any {
			return &config.ScraperSettings{
				Enabled:       fc.Enabled,
				RateLimit:     fc.RateLimit,
				BaseURL:       overrides.BaseURL,
				Proxy:         config.ProxyAsConfig(fc.Proxy),
				DownloadProxy: config.ProxyAsConfig(fc.DownloadProxy),
			}
		},
	}
	scraperutil.RegisterModule(m)
}

type scraperModule struct {
	scraperutil.StandardModule
}

var _ scraperutil.ScraperModule = (*scraperModule)(nil)
