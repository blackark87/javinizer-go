package sougouwiki

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type Config struct {
	config.BaseScraperConfig `yaml:",inline"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *Config) ValidateConfig(settings *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings(scraperName, settings); err != nil {
		return err
	}
	return configutil.ValidateHTTPBaseURL(scraperName+".base_url", settings.BaseURL)
}
