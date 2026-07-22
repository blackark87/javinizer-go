package maintenance

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressIndexResolveCastUsesDMMIdentityAndTranslation(t *testing.T) {
	actress := models.Actress{ID: 7, DMMID: 1071639, JapaneseName: "天然美月", FirstName: "wrong", LastName: "wrong"}
	unknown := models.Actress{ID: 8, FirstName: models.UnknownActressName, JapaneseName: models.UnknownActressName}
	index := &actressIndex{
		byDMM: map[int]models.Actress{1071639: actress}, byJapanese: map[string][]models.Actress{}, byAlias: map[string]models.Actress{}, unknown: unknown,
		targetNames: map[uint]models.ActressTranslation{7: {ActressID: 7, Language: "ko", FirstName: "미즈키", LastName: "아마네"}},
	}
	cast, fallback := index.resolveCast(&models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 1071639, JapaneseName: "天然美月"}}})
	require.Len(t, cast, 1)
	assert.False(t, fallback)
	assert.Equal(t, "아마네", cast[0].LastName)
	assert.Equal(t, "미즈키", cast[0].FirstName)
}

func TestReprocessTranslationCheckpointRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	titleSource := &models.ScraperResult{Translations: []models.MovieTranslation{{
		Language: "ko", Title: "검수 제목", SourceName: "translation:openai-compatible", SettingsHash: "hash",
	}}}
	descriptionSource := &models.ScraperResult{Translations: []models.MovieTranslation{{
		Language: "ko", Description: "검수 설명", SourceName: "translation:openai-compatible", SettingsHash: "hash",
	}}}
	items := []reprocessTranslationItem{{groupKey: "group", titleSource: titleSource, descriptionSource: descriptionSource}}
	checkpoint, err := loadReprocessTranslationCheckpoint(path)
	require.NoError(t, err)
	require.NoError(t, checkpoint.store(items, "ko"))

	loaded, err := loadReprocessTranslationCheckpoint(path)
	require.NoError(t, err)
	entry, ok := loaded.entries["group"]
	require.True(t, ok)
	assert.Equal(t, "검수 제목", entry.Title)
	assert.Equal(t, "검수 설명", entry.Description)

	newTitle := &models.ScraperResult{}
	newDescription := &models.ScraperResult{}
	applyCheckpointEntry([]reprocessTranslationItem{{titleSource: newTitle, descriptionSource: newDescription}}, entry, "ko")
	assert.Equal(t, "검수 제목", translatedSourceField(newTitle, "ko", "title"))
	assert.Equal(t, "검수 설명", translatedSourceField(newDescription, "ko", "description"))
}

func TestReprocessMovieIDSelection(t *testing.T) {
	selected := normalizeReprocessMovieIDs([]string{" MIAA-811 ", "drpt-050"})
	result := &worker.MovieResult{
		Status:        models.JobStatusCompleted,
		FileMatchInfo: models.FileMatchInfo{MovieID: "MIAA-811"},
		Movie:         &models.Movie{ID: "MIAA-811", ContentID: "miaa811"},
	}

	assert.True(t, shouldReprocessResult(result, selected))
	assert.False(t, shouldReprocessResult(&worker.MovieResult{
		Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "OTHER"},
	}, selected))
	assert.True(t, shouldReprocessResult(result, nil))
}

func TestReprocessCheckpointPathSeparatesSelections(t *testing.T) {
	jobID := "job-id"
	full := reprocessCheckpointPath(jobID, nil, false)
	first := reprocessCheckpointPath(jobID, normalizeReprocessMovieIDs([]string{"MIAA-811"}), false)
	second := reprocessCheckpointPath(jobID, normalizeReprocessMovieIDs([]string{"DRPT-050"}), false)
	titleOnly := reprocessCheckpointPath(jobID, normalizeReprocessMovieIDs([]string{"MIAA-811"}), true)

	assert.NotEqual(t, full, first)
	assert.NotEqual(t, first, second)
	assert.NotEqual(t, first, titleOnly)
	assert.Equal(t, first, reprocessCheckpointPath(jobID, normalizeReprocessMovieIDs([]string{"miaa-811"}), false))
}

func TestTranslationModelConfigsAddsUniqueOpenAICompatibleModels(t *testing.T) {
	tc := config.TranslationConfig{Provider: "openai-compatible"}
	tc.OpenAICompatible.Model = "primary"

	configs := translationModelConfigs(tc, []string{"primary", "secondary", "secondary", ""})

	require.Len(t, configs, 2)
	assert.Equal(t, "primary", configs[0].OpenAICompatible.Model)
	assert.Equal(t, "secondary", configs[1].OpenAICompatible.Model)
}

func TestActressIndexResolveCastFallsBackForUnverifiedMultiCast(t *testing.T) {
	unknown := models.Actress{ID: 8, FirstName: models.UnknownActressName, JapaneseName: models.UnknownActressName}
	index := &actressIndex{byDMM: map[int]models.Actress{}, byJapanese: map[string][]models.Actress{}, byAlias: map[string]models.Actress{}, unknown: unknown, targetNames: map[uint]models.ActressTranslation{}}
	cast, fallback := index.resolveCast(&models.ScraperResult{Actresses: []models.ActressInfo{{JapaneseName: "仮名A"}, {JapaneseName: "仮名B"}}})
	require.Len(t, cast, 1)
	assert.True(t, fallback)
	assert.Equal(t, models.UnknownActressName, cast[0].JapaneseName)
}

func TestActressIndexTranslationVariantsPreserveCanonicalAndAliasTranslations(t *testing.T) {
	canonical := models.Actress{ID: 2400, DMMID: 1071639, JapaneseName: "天然美月"}
	alias := models.Actress{ID: 2674, DMMID: 1063307, JapaneseName: "天然かのん"}
	index := &actressIndex{
		byID:      map[uint]models.Actress{2400: canonical, 2674: alias},
		linkedIDs: map[uint][]uint{2400: {2674}, 2674: {2400}},
		targetNames: map[uint]models.ActressTranslation{
			2400: {ActressID: 2400, Language: "ko", LastName: "아마네", FirstName: "미즈키"},
			2674: {ActressID: 2674, Language: "ko", LastName: "아마네", FirstName: "카논"},
		},
	}

	variants := index.translationVariants([]models.Actress{canonical})

	require.Len(t, variants, 2)
	assert.Equal(t, "天然美月", variants[0].JapaneseName)
	assert.Equal(t, "아마네 미즈키", variants[0].LastName+" "+variants[0].FirstName)
	assert.Equal(t, "天然かのん", variants[1].JapaneseName)
	assert.Equal(t, "아마네 카논", variants[1].LastName+" "+variants[1].FirstName)
}

func TestRefreshDisplayTitleWithoutMediaPreservesRenderedPrefix(t *testing.T) {
	movie := &models.Movie{Title: "새 제목", DisplayTitle: "[2026][1080p][VR]이전 제목"}
	refreshDisplayTitleWithoutMedia(movie, "이전 제목")
	assert.Equal(t, "[2026][1080p][VR]새 제목", movie.DisplayTitle)
}

func TestMergeSourceTranslationFieldPreservesUnselectedField(t *testing.T) {
	source := &models.ScraperResult{Translations: []models.MovieTranslation{{
		Language: "ko", Title: "기존 제목", Description: "기존 설명", SourceName: "old", SettingsHash: "old-hash",
	}}}
	incoming := models.MovieTranslation{
		Language: "ko", Title: "새 제목", Description: "새 설명", SourceName: "translation:openai-compatible", SettingsHash: "new-hash",
	}

	mergeSourceTranslationField(source, incoming, "title")

	require.Len(t, source.Translations, 1)
	assert.Equal(t, "새 제목", source.Translations[0].Title)
	assert.Equal(t, "기존 설명", source.Translations[0].Description)
	assert.Equal(t, "translation:openai-compatible", source.Translations[0].SourceName)
	assert.Equal(t, "new-hash", source.Translations[0].SettingsHash)
}

func TestProtectReviewActressNamesRestoresKnownKoreanName(t *testing.T) {
	actresses := []models.Actress{{JapaneseName: "円井萌華", LastName: "마루이", FirstName: "모에카"}}
	protected := protectReviewActressNames(
		"イイナリドM 円井萌華",
		"이이나리 도M 마루이 모에카",
		actresses,
	)

	assert.Equal(t, "イイナリドM ⟦7000⟧", protected.source)
	assert.Equal(t, "이이나리 도M ⟦7000⟧", protected.candidate)
	restored, ok := protected.restore("복종하는 극M ⟦7000⟧")
	assert.True(t, ok)
	assert.Equal(t, "복종하는 극M 마루이 모에카", restored)
}

func TestProtectReviewActressNamesRejectsReviewerThatDropsToken(t *testing.T) {
	actresses := []models.Actress{{JapaneseName: "円井萌華", LastName: "마루이", FirstName: "모에카"}}
	protected := protectReviewActressNames("제목 円井萌華", "제목 마루이 모에카", actresses)

	restored, ok := protected.restore("제목 에누이 모에카")
	assert.False(t, ok)
	assert.Equal(t, "제목 마루이 모에카", restored)
}
