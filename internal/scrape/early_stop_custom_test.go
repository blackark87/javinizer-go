package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestScrapeResultsCoverRequiredFields(t *testing.T) {
	results := []*models.ScraperResult{{ID: "ABC-1", Title: "title"}}
	tests := []struct {
		name     string
		required []string
		want     bool
	}{
		{name: "empty", want: true},
		{name: "covered", required: []string{"id", "title"}, want: true},
		{name: "missing", required: []string{"poster_url"}, want: false},
		{name: "unknown ignored", required: []string{"future_field"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scrapeResultsCoverRequiredFields(results, test.required); got != test.want {
				t.Fatalf("scrapeResultsCoverRequiredFields() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEarlyStopMinimum(t *testing.T) {
	if got := earlyStopMinimum(nil); got != 2 {
		t.Fatalf("earlyStopMinimum(nil) = %d, want 2", got)
	}
	if got := earlyStopMinimum(&Config{EarlyStopMinResults: 3}); got != 3 {
		t.Fatalf("earlyStopMinimum(config) = %d, want 3", got)
	}
}
