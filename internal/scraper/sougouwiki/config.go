package sougouwiki

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Config contains SougouWiki-specific scraper settings.
type Config struct {
	BaseURL string `yaml:"base_url" json:"base_url"`
}

// ValidateConfig validates the configured SougouWiki base URL.
func (c *Config) ValidateConfig(settings *models.ScraperSettings) error {
	return config.ValidateHTTPBaseURL(scraperName+".base_url", settings.BaseURL)
}

func validateScraperSettings(settings *models.ScraperSettings) error {
	return (&Config{}).ValidateConfig(settings)
}
