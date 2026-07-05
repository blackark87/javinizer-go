package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func earlyStopCfg(enabled bool, min int, required ...string) *config.Config {
	cfg := &config.Config{}
	cfg.Scrapers.EarlyStop = enabled
	cfg.Scrapers.EarlyStopMinResults = min
	cfg.Metadata.RequiredFields = required
	return cfg
}

func TestResultsCoverRequiredFields(t *testing.T) {
	results := []*models.ScraperResult{
		{Source: "a", Title: "T"},
		{Source: "b", PosterURL: "http://x/p.jpg"},
	}
	tests := []struct {
		name     string
		required []string
		want     bool
	}{
		{"empty required is covered", nil, true},
		{"title covered by first", []string{"title"}, true},
		{"poster covered by second", []string{"poster_url"}, true},
		{"title+poster both covered", []string{"title", "poster"}, true},
		{"description not covered", []string{"description"}, false},
		{"unknown field ignored", []string{"nonsense"}, true},
		{"one missing fails", []string{"title", "description"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resultsCoverRequiredFields(results, tt.required); got != tt.want {
				t.Errorf("resultsCoverRequiredFields(%v) = %v, want %v", tt.required, got, tt.want)
			}
		})
	}
}

func TestShouldEarlyStop(t *testing.T) {
	two := []*models.ScraperResult{{Source: "a", Title: "T"}, {Source: "b", Title: "T"}}
	one := []*models.ScraperResult{{Source: "a", Title: "T"}}

	t.Run("disabled never stops", func(t *testing.T) {
		if shouldEarlyStop(earlyStopCfg(false, 2), two) {
			t.Fatal("expected false when disabled")
		}
	})
	t.Run("below min does not stop", func(t *testing.T) {
		if shouldEarlyStop(earlyStopCfg(true, 2), one) {
			t.Fatal("expected false with 1 result and min 2")
		}
	})
	t.Run("min reached with no required stops", func(t *testing.T) {
		if !shouldEarlyStop(earlyStopCfg(true, 2), two) {
			t.Fatal("expected true with 2 results, min 2, no required")
		}
	})
	t.Run("min reached but required missing continues", func(t *testing.T) {
		if shouldEarlyStop(earlyStopCfg(true, 2, "poster_url"), two) {
			t.Fatal("expected false when required poster missing")
		}
	})
	t.Run("min reached and required covered stops", func(t *testing.T) {
		covered := []*models.ScraperResult{{Source: "a", Title: "T"}, {Source: "b", PosterURL: "p"}}
		if !shouldEarlyStop(earlyStopCfg(true, 2, "poster_url"), covered) {
			t.Fatal("expected true when required poster covered")
		}
	})
	t.Run("min defaults to 2 when unset", func(t *testing.T) {
		if earlyStopMin(earlyStopCfg(true, 0)) != 2 {
			t.Fatal("expected default min 2")
		}
	})
}
