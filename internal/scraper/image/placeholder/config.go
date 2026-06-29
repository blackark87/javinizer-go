package placeholder

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

const defaultThresholdKB = 10

// Config controls placeholder image detection for scraped posters.
type Config struct {
	Enabled   bool
	Threshold int64
	Hashes    []string
}

// ConfigFromSettings builds a Config from scraper settings and the default placeholder hashes.
func ConfigFromSettings(settings *models.ScraperSettings, defaultHashes []string) Config {
	cfg := Config{
		Enabled:   true,
		Threshold: defaultThresholdKB * 1024,
		Hashes:    make([]string, 0),
	}

	seen := make(map[string]bool)

	for _, h := range defaultHashes {
		if !seen[h] {
			seen[h] = true
			cfg.Hashes = append(cfg.Hashes, h)
		}
	}

	if settings == nil {
		return cfg
	}

	if settings.PlaceholderThresholdKB > 0 {
		cfg.Threshold = int64(settings.PlaceholderThresholdKB * 1024)
	}

	for _, h := range settings.ExtraPlaceholderHashes {
		h = strings.TrimSpace(strings.ToLower(h))
		if len(h) == 64 && !seen[h] {
			seen[h] = true
			cfg.Hashes = append(cfg.Hashes, h)
		}
	}

	return cfg
}
