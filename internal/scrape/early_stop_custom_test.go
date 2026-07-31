package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestScrapeResultsCoverRequiredFields(t *testing.T) {
	results := []*models.ScraperResult{{ID: "ABC-1", Title: "title", Rating: &models.Rating{Score: 8.5}}}
	tests := []struct {
		name     string
		required []string
		want     bool
	}{
		{name: "empty", want: true},
		{name: "covered", required: []string{"id", "title"}, want: true},
		{name: "rating covered", required: []string{"rating"}, want: true},
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

func TestEarlyStopCoverageFields(t *testing.T) {
	if got := earlyStopCoverageFields(nil); got != nil {
		t.Fatalf("earlyStopCoverageFields(nil) = %v, want nil", got)
	}

	required := []string{"title"}
	if got := earlyStopCoverageFields(&Config{RequiredFields: required}); len(got) != 1 || got[0] != "title" {
		t.Fatalf("legacy required_fields fallback = %v, want %v", got, required)
	}

	earlyStopFields := []string{"description", "poster_url"}
	got := earlyStopCoverageFields(&Config{
		EarlyStopFields: earlyStopFields,
		RequiredFields:  required,
	})
	if len(got) != 2 || got[0] != "description" || got[1] != "poster_url" {
		t.Fatalf("early_stop_fields override = %v, want %v", got, earlyStopFields)
	}
}

func TestQueryUntilCovered_StopsAfterConfiguredFieldCoverage(t *testing.T) {
	first := &mockScraper{
		name:    "first",
		enabled: true,
		result:  &models.ScraperResult{Source: "first", Title: "complete"},
	}
	second := &mockScraper{
		name:    "second",
		enabled: true,
		result:  &models.ScraperResult{Source: "second", Description: "unused"},
	}
	s := &Scraper{cfg: &Config{
		EarlyStopMinResults: 1,
		EarlyStopFields:     []string{"title"},
	}}

	results, failures := s.queryUntilCovered(context.Background(), "ABC-001", []models.Scraper{first, second})

	if len(failures) != 0 {
		t.Fatalf("failures = %v, want none", failures)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if first.callCount != 1 || second.callCount != 0 {
		t.Fatalf("call counts = first:%d second:%d, want 1 and 0", first.callCount, second.callCount)
	}
}

func TestQueryUntilCovered_QueriesFallbackForMissingFields(t *testing.T) {
	first := &mockScraper{
		name:    "first",
		enabled: true,
		result:  &models.ScraperResult{Source: "first", Title: "partial"},
	}
	second := &mockScraper{
		name:    "second",
		enabled: true,
		result:  &models.ScraperResult{Source: "second", Description: "fallback"},
	}
	s := &Scraper{cfg: &Config{
		EarlyStopMinResults: 1,
		EarlyStopFields:     []string{"title", "description"},
	}}

	results, failures := s.queryUntilCovered(context.Background(), "ABC-001", []models.Scraper{first, second})

	if len(failures) != 0 {
		t.Fatalf("failures = %v, want none", failures)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if first.callCount != 1 || second.callCount != 1 {
		t.Fatalf("call counts = first:%d second:%d, want 1 and 1", first.callCount, second.callCount)
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
