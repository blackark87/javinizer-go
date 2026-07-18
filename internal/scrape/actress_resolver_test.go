package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type actressResolverScraper struct {
	name         string
	enabled      bool
	result       *models.ScraperResult
	err          error
	calls        int
	thumbnail    string
	thumbCalls   int
	resolveID    string
	profile      models.ActressInfo
	profileErr   error
	profileCalls int
}

func (s *actressResolverScraper) Name() string { return s.name }
func (s *actressResolverScraper) Search(context.Context, string) (*models.ScraperResult, error) {
	return s.result, s.err
}
func (s *actressResolverScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *actressResolverScraper) IsEnabled() bool                                { return s.enabled }
func (s *actressResolverScraper) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: s.enabled}
}
func (s *actressResolverScraper) Close() error { return nil }
func (s *actressResolverScraper) ResolveActresses(_ context.Context, id string) (*models.ScraperResult, error) {
	s.calls++
	s.resolveID = id
	return s.result, s.err
}
func (s *actressResolverScraper) ResolveActressThumbnail(context.Context, models.ActressInfo) string {
	s.thumbCalls++
	return s.thumbnail
}
func (s *actressResolverScraper) ResolveActressProfile(context.Context, models.ActressInfo) (models.ActressInfo, error) {
	s.profileCalls++
	return s.profile, s.profileErr
}

func TestResolveMissingActressesRunsWhenAnyActressLacksVerifiedDMMIdentity(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "正式名"}},
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "TEST-001", []*models.ScraperResult{{
		Source: "regular", Actresses: []models.ActressInfo{{JapaneseName: "별명"}},
	}})
	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, 1, resolver.calls)
	assert.Equal(t, "TEST-001", result.ID)

	result, failure = s.resolveMissingActresses(context.Background(), "TEST-002", []*models.ScraperResult{{
		Source: "regular", Actresses: []models.ActressInfo{
			{DMMID: 1, JapaneseName: "정식명"},
			{JapaneseName: "미검증명"},
		},
	}})
	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, 2, resolver.calls)

	result, failure = s.resolveMissingActresses(context.Background(), "TEST-003", []*models.ScraperResult{{
		Source: "regular", Actresses: []models.ActressInfo{
			{DMMID: 1, JapaneseName: "정식명"},
			{DMMID: 2, JapaneseName: "다른 정식명"},
		},
	}})
	assert.Nil(t, result)
	assert.Nil(t, failure)
	assert.Equal(t, 2, resolver.calls)
}

func TestScrapeEnrichesRegularDMMActressesBeforeTranslationAfterDatabaseReset(t *testing.T) {
	fixture := newFixture(t).withScraper("regular", &models.ScraperResult{
		Source: "regular", ID: "TEST-001", Title: "Test",
		Actresses: []models.ActressInfo{{DMMID: 1077521, JapaneseName: "櫻茉日"}},
	}, nil)
	profile := &actressResolverScraper{
		name: "dmm-profile", enabled: false,
		profile: models.ActressInfo{
			DMMID: 1077521, JapaneseName: "櫻茉日",
			ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/sakura_mahiru.jpg",
		},
	}
	fixture.registry.RegisterInstance(profile)
	s := fixture.build()
	translationCfg := config.TranslationConfig{
		Enabled: true, Provider: "openai", SourceLanguage: "ja", TargetLanguage: "ko", ApplyToPrimary: true,
		Fields: config.TranslationFieldsConfig{Actresses: true},
	}
	s.cfg.TranslationEnabled = true
	s.translator = NewTranslatorFromApp(&translationCfg)

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "TEST-001"}, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Movie)
	require.Len(t, result.Movie.Actresses, 1)
	assert.Equal(t, 1, profile.profileCalls)
	assert.Equal(t, 1077521, result.Movie.Actresses[0].DMMID)
	assert.Equal(t, "사쿠라 마히루", result.Movie.Actresses[0].JapaneseName)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/sakura_mahiru.jpg", result.Movie.Actresses[0].ThumbURL)
}

func TestScrapeReplacesMixedVerifiedAndUnverifiedCastWithResolverCast(t *testing.T) {
	fixture := newFixture(t).withScraper("regular", &models.ScraperResult{
		Source: "regular", ID: "MIXED-001", Title: "Mixed cast",
		Actresses: []models.ActressInfo{
			{DMMID: 100, JapaneseName: "검증 배우"},
			{JapaneseName: "남아 있으면 안 되는 이름"},
		},
	}, nil)
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: false, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{
			{DMMID: 100, JapaneseName: "검증 배우"},
			{DMMID: 200, JapaneseName: "보정 배우"},
		},
	}}
	fixture.registry.RegisterInstance(resolver)
	s := fixture.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "MIXED-001"}, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Movie)
	assert.Equal(t, 1, resolver.calls)
	require.Len(t, result.Movie.Actresses, 2)
	assert.Equal(t, []int{100, 200}, []int{result.Movie.Actresses[0].DMMID, result.Movie.Actresses[1].DMMID})
	assert.NotContains(t, []string{result.Movie.Actresses[0].JapaneseName, result.Movie.Actresses[1].JapaneseName}, "남아 있으면 안 되는 이름")
}

func TestResolveMissingActressesUsesDedicatedResolverWhenDisabled(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: false, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 1054165, JapaneseName: "夏希まろん"}},
	}}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "JNT-042", []*models.ScraperResult{{
		Source: "regular", Actresses: []models.ActressInfo{
			{JapaneseName: "マヒロさん マッチョバー経営の女社長"},
			{JapaneseName: "マヒロ"},
		},
	}})

	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, 1, resolver.calls)
	assert.Equal(t, "JNT-042", result.ID)
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, 1054165, result.Actresses[0].DMMID)
	assert.Equal(t, "夏希まろん", result.Actresses[0].JapaneseName)
}

func TestScrapeJNT042ReplacesDecoratedUnverifiedCastWithDisabledResolver(t *testing.T) {
	fixture := newFixture(t).withScraper("regular", &models.ScraperResult{
		Source: "regular", ID: "JNT-042", Title: "JNT test",
		Actresses: []models.ActressInfo{
			{JapaneseName: "マヒロさん マッチョバー経営の女社長"},
			{JapaneseName: "マヒロ"},
		},
	}, nil)
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: false, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 1054165, JapaneseName: "夏希まろん"}},
	}}
	fixture.registry.RegisterInstance(resolver)
	s := fixture.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "JNT-042"}, nil)

	require.NoError(t, err)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "JNT-042", resolver.resolveID)
	assert.Equal(t, 1, resolver.calls)
	require.Len(t, result.Movie.Actresses, 1)
	assert.Equal(t, 1054165, result.Movie.Actresses[0].DMMID)
	assert.Equal(t, "夏希まろん", result.Movie.Actresses[0].JapaneseName)
	require.Len(t, result.ScraperResults, 2)
	require.Len(t, result.ScraperResults[0].Actresses, 2, "raw regular source must remain available for review")
}

func TestCachedJNT042AutomaticallyRepairsAndRequestsPersistence(t *testing.T) {
	fixture := newFixture(t)
	_, err := fixture.movieRepo.Upsert(context.Background(), &models.Movie{
		ID: "JNT-042", Title: "cached", SourceName: "regular",
		Actresses: []models.Actress{
			{JapaneseName: "マヒロさん マッチョバー経営の女社長"},
			{JapaneseName: "マヒロ"},
		},
	})
	require.NoError(t, err)
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: false, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 1054165, JapaneseName: "夏希まろん"}},
	}}
	fixture.registry.RegisterInstance(resolver)
	s := fixture.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "JNT-042"}, nil)

	require.NoError(t, err)
	require.True(t, result.Cached)
	assert.True(t, result.NeedsPersistence)
	assert.Equal(t, "JNT-042", resolver.resolveID)
	assert.Equal(t, 1, resolver.calls)
	require.Len(t, result.Movie.Actresses, 1)
	assert.Equal(t, 1054165, result.Movie.Actresses[0].DMMID)
	assert.Equal(t, "夏希まろん", result.Movie.Actresses[0].JapaneseName)
	require.Len(t, result.ScraperResults, 2)
	require.Len(t, result.ScraperResults[0].Actresses, 2, "pre-repair cache source must remain visible")
	assert.Equal(t, actressResolverScraperName, result.ScraperResults[1].Source)
	assert.Equal(t, actressResolverScraperName, result.ActressSources[ActressSourceKey(result.Movie.Actresses[0])])

	_, err = fixture.movieRepo.UpsertWithTranslations(context.Background(), result.Movie, nil, nil)
	require.NoError(t, err)
	resolver.calls = 0
	resolver.resolveID = ""

	second, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "JNT-042"}, nil)
	require.NoError(t, err)
	require.True(t, second.Cached)
	assert.False(t, second.NeedsPersistence)
	assert.Zero(t, resolver.calls, "persisted DMM identity must prevent repeated Wiki lookups")
	require.Len(t, second.Movie.Actresses, 1)
	assert.Equal(t, 1054165, second.Movie.Actresses[0].DMMID)
	assert.Equal(t, "夏希まろん", second.Movie.Actresses[0].JapaneseName)
}

func TestCachedJNT042CleansAndDeduplicatesWhenResolverFails(t *testing.T) {
	fixture := newFixture(t)
	_, err := fixture.movieRepo.Upsert(context.Background(), &models.Movie{
		ID: "JNT-042", Title: "cached", SourceName: "regular",
		Actresses: []models.Actress{
			{JapaneseName: "マヒロさん マッチョバー経営の女社長"},
			{JapaneseName: "マヒロ"},
		},
	})
	require.NoError(t, err)
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: false, err: errors.New("lookup failed")}
	fixture.registry.RegisterInstance(resolver)
	s := fixture.build()

	result, err := s.Scrape(context.Background(), ScrapeCmd{MovieID: "JNT-042"}, nil)

	require.NoError(t, err)
	assert.True(t, result.NeedsPersistence)
	assert.Equal(t, 1, resolver.calls)
	require.Len(t, result.Movie.Actresses, 1)
	assert.Equal(t, "マヒロ", result.Movie.Actresses[0].JapaneseName)
	assert.Zero(t, result.Movie.Actresses[0].DMMID)
}

func TestResolveMissingActressesFailureIsOptionalAndAttributed(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, err: errors.New("lookup failed")}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "TEST-001", []*models.ScraperResult{{Source: "regular", Title: "keep"}})
	assert.Nil(t, result)
	require.NotNil(t, failure)
	assert.Equal(t, actressResolverScraperName, failure.Scraper)
	assert.ErrorContains(t, failure.Cause, "lookup failed")
}

func TestResolveMissingActressesEnrichesThumbnail(t *testing.T) {
	resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, result: &models.ScraperResult{
		Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "正式名"}},
	}}
	thumbnail := &actressResolverScraper{name: "dmm", enabled: true, thumbnail: "https://example.com/actress.jpg"}
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(resolver)
	registry.RegisterInstance(thumbnail)
	s := &Scraper{registry: registry}

	result, failure := s.resolveMissingActresses(context.Background(), "TEST-001", nil)
	require.Nil(t, failure)
	require.NotNil(t, result)
	assert.Equal(t, "https://example.com/actress.jpg", result.Actresses[0].ThumbURL)
	assert.Equal(t, 1, thumbnail.thumbCalls)
}

func TestResolveMissingActressesPrefersDMMProfileNameAndFallsBackToSougouWiki(t *testing.T) {
	for _, test := range []struct {
		name       string
		profile    models.ActressInfo
		profileErr error
		want       string
	}{
		{name: "DMM name", profile: models.ActressInfo{DMMID: 1077521, JapaneseName: "櫻茉日"}, want: "櫻茉日"},
		{name: "SougouWiki fallback", profileErr: errors.New("DMM unavailable"), want: "SougouWiki名"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &actressResolverScraper{name: actressResolverScraperName, enabled: true, result: &models.ScraperResult{
				Actresses: []models.ActressInfo{{DMMID: 1077521, JapaneseName: "SougouWiki名"}},
			}}
			dmm := &actressResolverScraper{name: "dmm", enabled: true, profile: test.profile, profileErr: test.profileErr}
			registry := scraperutil.NewScraperRegistry()
			registry.RegisterInstance(resolver)
			registry.RegisterInstance(dmm)
			s := &Scraper{registry: registry}

			result, failure := s.resolveMissingActresses(context.Background(), "MIUM-951", nil)
			require.Nil(t, failure)
			require.NotNil(t, result)
			assert.Equal(t, test.want, result.Actresses[0].JapaneseName)
			assert.Equal(t, 1, dmm.profileCalls)
		})
	}
}

func TestActressOverrideResultsPreservesRawMetadata(t *testing.T) {
	raw := []*models.ScraperResult{
		{Source: "regular", ID: "CANONICAL-001", Title: "Existing", Actresses: []models.ActressInfo{{JapaneseName: "별명"}}},
		{Source: actressResolverScraperName, ID: "RAW-001", Actresses: []models.ActressInfo{{DMMID: 777, JapaneseName: "正式名"}}},
	}
	overrides := actressOverrideResults(raw, actressResolverScraperName)
	require.Len(t, overrides, 2)
	assert.Len(t, raw[0].Actresses, 1)
	assert.Empty(t, overrides[0].Actresses)
	assert.Equal(t, "Existing", overrides[0].Title)
	assert.Empty(t, overrides[1].ID)
	assert.Empty(t, overrides[1].Title)
	assert.Equal(t, 777, overrides[1].Actresses[0].DMMID)

	resolverOnly := actressOverrideResults(raw[1:], actressResolverScraperName)
	assert.Same(t, raw[1], resolverOnly[0])
	assert.Equal(t, "RAW-001", resolverOnly[0].ID)
}
