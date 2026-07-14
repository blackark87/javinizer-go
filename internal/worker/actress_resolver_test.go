package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type actressFlowScraper struct {
	name        string
	enabled     bool
	searchCalls int
	result      *models.ScraperResult
	err         error
}

func (s *actressFlowScraper) Name() string { return s.name }
func (s *actressFlowScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	s.searchCalls++
	return s.result, s.err
}
func (s *actressFlowScraper) GetURL(string) (string, error) { return "", nil }
func (s *actressFlowScraper) IsEnabled() bool               { return s.enabled }
func (s *actressFlowScraper) Config() *config.ScraperSettings {
	return &config.ScraperSettings{Enabled: s.enabled}
}
func (s *actressFlowScraper) Close() error { return nil }

type actressFlowResolver struct {
	*actressFlowScraper
	resolveCalls  int
	resolveID     string
	resolveResult *models.ScraperResult
	resolveErr    error
}

func (s *actressFlowResolver) ResolveActresses(_ context.Context, id string) (*models.ScraperResult, error) {
	s.resolveCalls++
	s.resolveID = id
	return s.resolveResult, s.resolveErr
}

type actressFlowThumbnailResolver struct {
	*actressFlowScraper
	calls int
	url   string
}

func (s *actressFlowThumbnailResolver) ResolveActressThumbnail(_ context.Context, _ models.ActressInfo) string {
	s.calls++
	return s.url
}

func TestQueryScrapersRunsActressResolverOnlyWhenAllDMMIDsAreMissing(t *testing.T) {
	tests := []struct {
		name          string
		actresses     []models.ActressInfo
		wantCalls     int
		wantOverride  string
		wantResultLen int
		thumbnailURL  string
	}{
		{
			name:          "no actresses",
			wantCalls:     1,
			wantOverride:  "sougouwiki",
			wantResultLen: 2,
			thumbnailURL:  "https://pics.dmm.co.jp/mono/actjpgs/test.jpg",
		},
		{
			name:          "all DMM IDs missing",
			actresses:     []models.ActressInfo{{JapaneseName: "별명", DMMID: 0}},
			wantCalls:     1,
			wantOverride:  "sougouwiki",
			wantResultLen: 2,
		},
		{
			name:          "regular source positive DMM ID skips fallback verification",
			actresses:     []models.ActressInfo{{JapaneseName: "정식명", DMMID: 123}},
			wantCalls:     0,
			wantOverride:  "",
			wantResultLen: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			regular := &actressFlowScraper{
				name:    "regular",
				enabled: true,
				result: &models.ScraperResult{
					Source:    "regular",
					ID:        "TEST-001",
					Title:     "Existing title",
					Actresses: test.actresses,
				},
			}
			resolver := &actressFlowResolver{
				actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
				resolveResult: &models.ScraperResult{
					Source: "sougouwiki",
					ID:     "TEST-001",
					Actresses: []models.ActressInfo{{
						JapaneseName: "正式名",
						DMMID:        999,
					}},
				},
			}
			thumbnail := &actressFlowThumbnailResolver{
				actressFlowScraper: &actressFlowScraper{name: "dmm", enabled: false},
				url:                test.thumbnailURL,
			}
			registry := models.NewScraperRegistry()
			registry.Register(regular)
			registry.Register(resolver)
			registry.Register(thumbnail)

			results, _, override, cancel, err := queryActressFlow(t, registry, []string{"regular", "sougouwiki"}, &config.Config{})
			if err != nil || cancel != nil {
				t.Fatalf("queryScrapers() err=%v cancel=%+v", err, cancel)
			}
			if resolver.resolveCalls != test.wantCalls {
				t.Errorf("resolver calls = %d, want %d", resolver.resolveCalls, test.wantCalls)
			}
			if override != test.wantOverride {
				t.Errorf("override source = %q, want %q", override, test.wantOverride)
			}
			if len(results) != test.wantResultLen {
				t.Errorf("result count = %d, want %d", len(results), test.wantResultLen)
			}
			if test.wantCalls == 1 {
				if resolver.resolveID != "TEST-001" {
					t.Errorf("resolver ID = %q, want original movie ID", resolver.resolveID)
				}
				if thumbnail.calls != 1 || results[len(results)-1].Actresses[0].ThumbURL != test.thumbnailURL {
					t.Errorf("DMM thumbnail enrichment not applied: calls=%d result=%+v", thumbnail.calls, results[len(results)-1].Actresses[0])
				}
				if results[len(results)-1].Actresses[0].JapaneseName != "正式名" {
					t.Errorf("verified actress name was lost after thumbnail lookup: %+v", results[len(results)-1].Actresses[0])
				}
			} else if thumbnail.calls != 0 {
				t.Errorf("thumbnail resolver calls = %d, want 0", thumbnail.calls)
			}
		})
	}
}

func TestQueryScrapersActressResolverFailureIsOptional(t *testing.T) {
	regular := &actressFlowScraper{
		name:    "regular",
		enabled: true,
		result: &models.ScraperResult{
			Source: "regular",
			Title:  "Existing title",
			Actresses: []models.ActressInfo{{
				JapaneseName: "별명",
			}},
		},
	}
	resolver := &actressFlowResolver{
		actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
		resolveErr:         errors.New("wiki unavailable"),
	}
	registry := models.NewScraperRegistry()
	registry.Register(regular)
	registry.Register(resolver)

	results, failures, override, cancel, err := queryActressFlow(t, registry, []string{"regular", "sougouwiki"}, &config.Config{})
	if err != nil || cancel != nil {
		t.Fatalf("queryScrapers() err=%v cancel=%+v", err, cancel)
	}
	if len(results) != 1 || len(results[0].Actresses) != 1 || results[0].Actresses[0].JapaneseName != "별명" {
		t.Fatalf("existing actress was not preserved: %+v", results)
	}
	if override != "" {
		t.Errorf("override = %q, want empty", override)
	}
	if len(failures) != 1 || failures[0].Scraper != "sougouwiki" {
		t.Errorf("failures = %+v, want SougouWiki failure", failures)
	}
}

func TestQueryScrapersSougouWikiOnlyFailureReturnsNoResults(t *testing.T) {
	resolver := &actressFlowResolver{
		actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
		resolveErr:         errors.New("not found"),
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	results, failures, override, cancel, err := queryActressFlow(t, registry, []string{"sougouwiki"}, &config.Config{})
	if err != nil || cancel != nil {
		t.Fatalf("queryScrapers() err=%v cancel=%+v", err, cancel)
	}
	if len(results) != 0 || len(failures) != 1 || override != "" {
		t.Fatalf("results=%+v failures=%+v override=%q", results, failures, override)
	}
}

func TestActressResolverRunsAfterRegularEarlyStop(t *testing.T) {
	first := &actressFlowScraper{
		name:    "first",
		enabled: true,
		result:  &models.ScraperResult{Source: "first", Title: "Enough metadata"},
	}
	second := &actressFlowScraper{
		name:    "second",
		enabled: true,
		result:  &models.ScraperResult{Source: "second", Title: "Should not run"},
	}
	resolver := &actressFlowResolver{
		actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
		resolveResult: &models.ScraperResult{
			Source:    "sougouwiki",
			Actresses: []models.ActressInfo{{JapaneseName: "正式名", DMMID: 42}},
		},
	}
	registry := models.NewScraperRegistry()
	registry.Register(first)
	registry.Register(second)
	registry.Register(resolver)
	cfg := &config.Config{}
	cfg.Scrapers.EarlyStop = true
	cfg.Scrapers.EarlyStopMinResults = 1

	results, _, override, cancel, err := queryActressFlow(t, registry, []string{"first", "second", "sougouwiki"}, cfg)
	if err != nil || cancel != nil {
		t.Fatalf("queryScrapers() err=%v cancel=%+v", err, cancel)
	}
	if first.searchCalls != 1 || second.searchCalls != 0 || resolver.resolveCalls != 1 {
		t.Errorf("calls: first=%d second=%d resolver=%d", first.searchCalls, second.searchCalls, resolver.resolveCalls)
	}
	if len(results) != 2 || override != "sougouwiki" {
		t.Errorf("results=%d override=%q", len(results), override)
	}
}

func TestBuildActressOverrideResultsPreservesRawResultsAndSources(t *testing.T) {
	raw := []*models.ScraperResult{
		{
			Source:    "regular",
			ID:        "CANONICAL-001",
			Title:     "Existing title",
			PosterURL: "https://example.com/poster.jpg",
			Genres:    []string{"Drama"},
			Actresses: []models.ActressInfo{{JapaneseName: "별명"}},
		},
		{
			Source:    "sougouwiki",
			ID:        "RAW-001",
			Actresses: []models.ActressInfo{{JapaneseName: "正式名", DMMID: 777}},
		},
	}

	aggregationResults := buildActressOverrideResults(raw, "sougouwiki")
	if len(raw[0].Actresses) != 1 {
		t.Fatal("raw diagnostic result was mutated")
	}
	if len(aggregationResults[0].Actresses) != 0 || len(aggregationResults[1].Actresses) != 1 {
		t.Fatalf("override copies have wrong actresses: %+v", aggregationResults)
	}
	if aggregationResults[0].Title != raw[0].Title || aggregationResults[0].PosterURL != raw[0].PosterURL || len(aggregationResults[0].Genres) != 1 {
		t.Fatal("non-actress fields changed in aggregation copy")
	}
	if aggregationResults[0].ID != "CANONICAL-001" || aggregationResults[1].ID != "" {
		t.Fatalf("resolver changed non-actress movie ID: %+v", aggregationResults)
	}

	priorities := map[string][]string{
		"Title":   {"regular", "sougouwiki"},
		"Actress": {"regular", "sougouwiki"},
	}
	fieldSources := buildFieldSourcesFromScrapeResults(aggregationResults, priorities, nil)
	if fieldSources["title"] != "regular" || fieldSources["actresses"] != "sougouwiki" {
		t.Errorf("field sources = %+v", fieldSources)
	}
	actressSources := buildActressSourcesFromScrapeResults(
		aggregationResults,
		priorities,
		nil,
		[]models.Actress{{JapaneseName: "正式名", DMMID: 777}},
	)
	if actressSources["dmmid:777"] != "sougouwiki" {
		t.Errorf("actress sources = %+v", actressSources)
	}

	agg := aggregator.New(&config.Config{})
	movie, _, err := agg.AggregateWithPriority(aggregationResults, []string{"sougouwiki", "regular"})
	if err != nil {
		t.Fatalf("AggregateWithPriority() error = %v", err)
	}
	if movie.ID != "CANONICAL-001" || movie.Title != "Existing title" || movie.PosterURL != "https://example.com/poster.jpg" || len(movie.Genres) != 1 {
		t.Errorf("non-actress fields changed after aggregation: %+v", movie)
	}
	if len(movie.Actresses) != 1 || movie.Actresses[0].DMMID != 777 || movie.Actresses[0].JapaneseName != "正式名" {
		t.Errorf("actress list was not fully replaced: %+v", movie.Actresses)
	}

	resolverOnly := buildActressOverrideResults(raw[1:], "sougouwiki")
	if len(resolverOnly) != 1 || resolverOnly[0].ID != "RAW-001" {
		t.Errorf("resolver-only result lost query ID: %+v", resolverOnly)
	}

	candidateResults := buildMovieCandidateResults(raw, "sougouwiki")
	if len(candidateResults) != 1 || candidateResults[0] != raw[0] || candidateResults[0].Actresses[0].JapaneseName != "별명" {
		t.Errorf("regular diagnostic candidate was not preserved: %+v", candidateResults)
	}
	resolverOnlyCandidates := buildMovieCandidateResults(raw[1:], "sougouwiki")
	if len(resolverOnlyCandidates) != 1 || resolverOnlyCandidates[0] != raw[1] {
		t.Errorf("resolver-only candidate was lost: %+v", resolverOnlyCandidates)
	}
}

func TestReconcileVerifiedMovieActressesKeepsDMMOwnerAndAddsActivityNameAlias(t *testing.T) {
	_, db, _, _, _ := newRunBatchTestEnv(t, "regular")
	repo := database.NewActressRepository(db)
	polluted := &models.Actress{DMMID: 777, JapaneseName: "もな"}
	canonical := &models.Actress{JapaneseName: "弥生みづき"}
	if err := repo.Create(polluted); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(canonical); err != nil {
		t.Fatal(err)
	}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 777, JapaneseName: "弥生みづき"}}}

	if err := reconcileVerifiedMovieActresses(movie, repo); err != nil {
		t.Fatal(err)
	}
	if len(movie.Actresses) != 1 || movie.Actresses[0].ID != polluted.ID {
		t.Fatalf("canonical actresses = %+v, want DMM owner actress #%d", movie.Actresses, polluted.ID)
	}
	stored, err := repo.FindByID(polluted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.JapaneseName != "もな" || stored.Aliases != "弥生みづき" {
		t.Fatalf("DMM owner profile was overwritten instead of aliased: %+v", stored)
	}
	if _, err := repo.FindByID(canonical.ID); !database.IsNotFound(err) {
		t.Fatalf("matching unverified activity-name row was not merged: %v", err)
	}
}

func TestVerifyCachedActressesSkipsResolverWhenAnyPositiveDMMIDExists(t *testing.T) {
	cfg, db, movieRepo, _, _ := newRunBatchTestEnv(t, "sougouwiki")
	actressRepo := database.NewActressRepository(db)
	canonical := &models.Actress{JapaneseName: "弥生みづき"}
	if err := actressRepo.Create(canonical); err != nil {
		t.Fatal(err)
	}
	cached, err := movieRepo.Upsert(&models.Movie{
		ContentID: "jnt051", ID: "JNT-051", Title: "cached",
		Actresses: []models.Actress{{DMMID: 777, JapaneseName: "もな"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &actressFlowResolver{
		actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
		resolveResult: &models.ScraperResult{Source: "sougouwiki", Actresses: []models.ActressInfo{{
			DMMID: 777, JapaneseName: "弥生みづき",
		}}},
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	verified, err := verifyCachedActresses(
		context.Background(), &BatchJob{ID: "cached-verify"}, 0, cached,
		movieRepo, actressRepo, registry, cfg,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.resolveCalls != 0 {
		t.Fatalf("resolver calls=%d id=%q", resolver.resolveCalls, resolver.resolveID)
	}
	if verified != nil {
		t.Fatalf("cached positive-DMM cast should not be replaced: %+v", verified)
	}
}

func TestVerifyCachedActressesRunsResolverWhenAllDMMIDsAreMissing(t *testing.T) {
	cfg, db, movieRepo, _, _ := newRunBatchTestEnv(t, "sougouwiki")
	actressRepo := database.NewActressRepository(db)
	cached, err := movieRepo.Upsert(&models.Movie{
		ContentID: "mium834", ID: "300MIUM-834", Title: "cached",
		Actresses: []models.Actress{{JapaneseName: "新人さん"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &actressFlowResolver{
		actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
		resolveResult: &models.ScraperResult{Source: "sougouwiki", Actresses: []models.ActressInfo{{
			DMMID: 834, JapaneseName: "実名女優",
		}}},
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	verified, err := verifyCachedActresses(
		context.Background(), &BatchJob{ID: "cached-verify"}, 0, cached,
		movieRepo, actressRepo, registry, cfg,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.resolveCalls != 1 || resolver.resolveID != "300MIUM-834" {
		t.Fatalf("resolver calls=%d id=%q", resolver.resolveCalls, resolver.resolveID)
	}
	if verified == nil || len(verified.Actresses) != 1 || verified.Actresses[0].DMMID != 834 || verified.Actresses[0].JapaneseName != "実名女優" {
		t.Fatalf("verified cached movie = %+v", verified)
	}
}

func TestVerifyCachedActressesCleansFallbackAfterSuccessfulEmptyResolution(t *testing.T) {
	cfg, db, movieRepo, _, _ := newRunBatchTestEnv(t, "sougouwiki")
	actressRepo := database.NewActressRepository(db)
	cached, err := movieRepo.Upsert(&models.Movie{
		ContentID: "mium-empty", ID: "300MIUM-834", Title: "cached",
		Actresses: []models.Actress{{JapaneseName: "あいり 21歳 大学3年生"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &actressFlowResolver{
		actressFlowScraper: &actressFlowScraper{name: "sougouwiki", enabled: true},
		resolveResult:      &models.ScraperResult{Source: "sougouwiki"},
	}
	registry := models.NewScraperRegistry()
	registry.Register(resolver)

	cleaned, err := verifyCachedActresses(
		context.Background(), &BatchJob{ID: "cached-empty"}, 0, cached,
		movieRepo, actressRepo, registry, cfg,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.resolveCalls != 1 || cleaned == nil || len(cleaned.Actresses) != 1 {
		t.Fatalf("resolver calls=%d cleaned=%+v", resolver.resolveCalls, cleaned)
	}
	if cleaned.Actresses[0].JapaneseName != "あいり" {
		t.Fatalf("fallback actress was not cleaned: %+v", cleaned.Actresses[0])
	}
}

func queryActressFlow(
	t *testing.T,
	registry *models.ScraperRegistry,
	selected []string,
	cfg *config.Config,
) ([]*models.ScraperResult, []scraperFailure, string, *FileResult, error) {
	t.Helper()
	return queryScrapers(
		context.Background(),
		&BatchJob{ID: "actress-resolver-test"},
		"test.mp4",
		0,
		&scrapeQueryResult{movieID: "TEST-001"},
		"",
		registry,
		selected,
		nil,
		cfg,
		time.Now(),
	)
}
