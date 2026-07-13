package sougouwiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func TestModuleDefaultsAndFlattening(t *testing.T) {
	defaults := scraperutil.GetDefaultScraperSettings()
	raw, ok := defaults[scraperName]
	if !ok {
		t.Fatalf("%s defaults are not registered", scraperName)
	}
	settings, ok := raw.(config.ScraperSettings)
	if !ok {
		t.Fatalf("defaults type = %T, want config.ScraperSettings", raw)
	}
	if settings.Enabled || settings.BaseURL != defaultBaseURL || settings.RateLimit != 1000 {
		t.Errorf("unexpected defaults: %+v", settings)
	}

	flatten := scraperutil.GetFlattenFunc(scraperName)
	if flatten == nil {
		t.Fatal("flatten function is not registered")
	}
	proxy := &config.ProxyConfig{Enabled: true, Profile: "wiki"}
	flattened, ok := flatten(&Config{BaseScraperConfig: config.BaseScraperConfig{
		Enabled:      true,
		RequestDelay: 250,
		Proxy:        proxy,
	}}).(*config.ScraperSettings)
	if !ok {
		t.Fatal("flatten did not return ScraperSettings")
	}
	if !flattened.Enabled || flattened.RateLimit != 250 || flattened.BaseURL != defaultBaseURL || flattened.Proxy != proxy {
		t.Errorf("unexpected flattened settings: %+v", flattened)
	}
}

func TestConfigValidation(t *testing.T) {
	validator := &Config{}
	for _, test := range []struct {
		name     string
		settings *config.ScraperSettings
		wantErr  bool
	}{
		{name: "valid", settings: &config.ScraperSettings{Enabled: true, BaseURL: defaultBaseURL, RateLimit: 1000, Timeout: 30}},
		{name: "invalid URL", settings: &config.ScraperSettings{Enabled: true, BaseURL: "seesaawiki.jp"}, wantErr: true},
		{name: "negative delay", settings: &config.ScraperSettings{Enabled: true, BaseURL: defaultBaseURL, RateLimit: -1}, wantErr: true},
		{name: "negative timeout", settings: &config.ScraperSettings{Enabled: true, BaseURL: defaultBaseURL, Timeout: -1}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validator.ValidateConfig(test.settings)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateConfig() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestConstructorInheritsCommonHTTPSettings(t *testing.T) {
	rawConstructor, ok := scraperutil.GetScraperConstructor(scraperName)
	if !ok {
		t.Fatal("constructor is not registered")
	}
	constructor, ok := rawConstructor.(func(config.ScraperSettings, *database.DB, *config.ScrapersConfig) (models.Scraper, error))
	if !ok {
		t.Fatalf("constructor type = %T", rawConstructor)
	}

	created, err := constructor(config.ScraperSettings{Enabled: true}, nil, &config.ScrapersConfig{
		TimeoutSeconds: 44,
		UserAgent:      "Global-Wiki-UA/1.0",
	})
	if err != nil {
		t.Fatalf("constructor error = %v", err)
	}
	settings := created.Config()
	if settings.Timeout != 44 || settings.UserAgent != "Global-Wiki-UA/1.0" {
		t.Errorf("common settings not inherited: %+v", settings)
	}
}

func TestConfigYAMLRoundTrip(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	input := []byte(`
scrapers:
  priority:
    - sougouwiki
  sougouwiki:
    enabled: true
    base_url: https://example.com/wiki/
    request_delay: 321
    timeout: 12
    user_agent: Wiki-Test/1.0
`)
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	settings := loaded.Scrapers.Overrides[scraperName]
	if settings == nil {
		t.Fatalf("%s settings missing after load", scraperName)
	}
	if !settings.Enabled || settings.BaseURL != "https://example.com/wiki/" || settings.RateLimit != 321 || settings.Timeout != 12 || settings.UserAgent != "Wiki-Test/1.0" {
		t.Errorf("unexpected loaded settings: %+v", settings)
	}

	if err := config.Save(loaded, path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload error = %v", err)
	}
	settings = reloaded.Scrapers.Overrides[scraperName]
	if settings == nil || !settings.Enabled || settings.BaseURL != "https://example.com/wiki/" || settings.RateLimit != 321 {
		t.Errorf("unexpected reloaded settings: %+v", settings)
	}
}
