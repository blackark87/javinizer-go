package sougouwiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func TestModuleDefaultsAndFlattening(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	Register(registry)
	registered, ok := registry.Get(scraperName)
	if !ok {
		t.Fatalf("%s defaults are not registered", scraperName)
	}
	settings := registered.Defaults
	if settings.Enabled || settings.BaseURL != defaultBaseURL || settings.RateLimit != 1000 {
		t.Errorf("unexpected defaults: %+v", settings)
	}
	if registered.Constructor == nil || registered.ValidateFn == nil {
		t.Fatal("constructor and validator must be registered")
	}
}

func TestConfigValidation(t *testing.T) {
	validator := &Config{}
	for _, test := range []struct {
		name     string
		settings *models.ScraperSettings
		wantErr  bool
	}{
		{name: "valid", settings: &models.ScraperSettings{Enabled: true, BaseURL: defaultBaseURL, RateLimit: 1000, Timeout: 30}},
		{name: "invalid URL", settings: &models.ScraperSettings{Enabled: true, BaseURL: "seesaawiki.jp"}, wantErr: true},
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
	registry := scraperutil.NewScraperRegistry()
	Register(registry)
	constructor, ok := registry.GetScraperConstructor(scraperName)
	if !ok {
		t.Fatal("constructor is not registered")
	}
	created, err := constructor(scraperutil.ScraperDeps{
		Settings:       models.ScraperSettings{Enabled: true, UserAgent: "Global-Wiki-UA/1.0"},
		TimeoutSeconds: 44,
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
