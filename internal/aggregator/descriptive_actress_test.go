package aggregator

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanActressInfoName(t *testing.T) {
	blurb := "【あいちゃん/24歳/173cm！！超美巨Iカップのガチ美女OL！！】【のんちゃん/22歳/Gカップの美爆乳OL！！】神スタイル美女2人の大乱れ！！一挙配信SP！！"

	t.Run("strips age/occupation modifier to bare name", func(t *testing.T) {
		info := models.ActressInfo{JapaneseName: "あいり 21歳 大学3年生"}
		cleanActressInfoName(&info)
		assert.Equal(t, "あいり", info.JapaneseName)
		assert.False(t, models.IsUnknownActressFields(info.LastName, info.FirstName, info.JapaneseName))
	})

	t.Run("nameless promo blurb becomes Unknown", func(t *testing.T) {
		info := models.ActressInfo{JapaneseName: blurb}
		cleanActressInfoName(&info)
		assert.True(t, models.IsUnknownActressFields(info.LastName, info.FirstName, info.JapaneseName))
	})

	t.Run("real name unchanged", func(t *testing.T) {
		info := models.ActressInfo{JapaneseName: "波多野結衣"}
		cleanActressInfoName(&info)
		assert.Equal(t, "波多野結衣", info.JapaneseName)
	})
}

// TestGetActressesByPriority_MergesDecoratedName verifies that a decorated form of a
// name ("あいり 21歳 大学3年生") and its plain form ("あいり") collapse into a single
// actress instead of producing [Unknown, あいり].
func TestGetActressesByPriority_MergesDecoratedName(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: "file::memory:?cache=shared"},
		Metadata: config.MetadataConfig{
			Priority:        config.PriorityConfig{Priority: []string{"libredmm", "javbus"}},
			ActressDatabase: config.ActressDatabaseConfig{Enabled: true},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"libredmm", "javbus"}},
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: "file::memory:?cache=shared"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	agg := NewWithDatabase(cfg, db)

	results := map[string]*models.ScraperResult{
		"libredmm": {Actresses: []models.ActressInfo{{JapaneseName: "あいり 21歳 大学3年生"}}},
		"javbus":   {Actresses: []models.ActressInfo{{JapaneseName: "あいり"}}},
	}

	got := agg.getActressesByPriorityWithSource(results, []string{"libredmm", "javbus"}, nil)

	require.Len(t, got, 1, "decorated and plain forms should merge into one actress")
	assert.Equal(t, "あいり", got[0].JapaneseName)
	assert.False(t, models.IsUnknownActressFields(got[0].LastName, got[0].FirstName, got[0].JapaneseName))
}

func newDescriptiveTestAggregator(t *testing.T) *Aggregator {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: "file::memory:?cache=shared"},
		Metadata: config.MetadataConfig{
			Priority:        config.PriorityConfig{Priority: []string{"libredmm", "javbus"}},
			ActressDatabase: config.ActressDatabaseConfig{Enabled: true},
		},
		Scrapers: config.ScrapersConfig{Priority: []string{"libredmm", "javbus"}},
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: "file::memory:?cache=shared"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return NewWithDatabase(cfg, db)
}

// TestGetActressesByPriority_HonorificAndDescription covers JAC-140: an honorific name
// ("ありささん") and a name-less description ("欲求不満セレブ妻") should reduce to a single
// real actress "ありさ" with the description dropped.
func TestGetActressesByPriority_HonorificAndDescription(t *testing.T) {
	agg := newDescriptiveTestAggregator(t)
	results := map[string]*models.ScraperResult{
		"libredmm": {Actresses: []models.ActressInfo{
			{JapaneseName: "ありささん"},
			{JapaneseName: "欲求不満セレブ妻"},
		}},
	}

	got := agg.getActressesByPriorityWithSource(results, []string{"libredmm", "javbus"}, nil)

	require.Len(t, got, 1, "honorific name kept, description dropped")
	assert.Equal(t, "ありさ", got[0].JapaneseName)
}

func TestGetActressesByPriority_HonorificBeforeOccupationDescription(t *testing.T) {
	agg := newDescriptiveTestAggregator(t)
	results := map[string]*models.ScraperResult{
		"libredmm": {Actresses: []models.ActressInfo{
			{JapaneseName: "マヒロさん マッチョバー経営の女社長"},
			{JapaneseName: "マヒロ"},
		}},
	}

	got := agg.getActressesByPriorityWithSource(results, []string{"libredmm"}, nil)

	require.Len(t, got, 1)
	assert.Equal(t, "マヒロ", got[0].JapaneseName)
}

// TestGetActressesByPriority_OccupationSuffixMerges covers MIUM-1256: "愛梨沙 西麻布ラウンジ
// 勤務" and plain "愛梨沙" collapse into a single "愛梨沙".
func TestGetActressesByPriority_OccupationSuffixMerges(t *testing.T) {
	agg := newDescriptiveTestAggregator(t)
	results := map[string]*models.ScraperResult{
		"libredmm": {Actresses: []models.ActressInfo{{JapaneseName: "愛梨沙 西麻布ラウンジ勤務"}}},
		"javbus":   {Actresses: []models.ActressInfo{{JapaneseName: "愛梨沙"}}},
	}

	got := agg.getActressesByPriorityWithSource(results, []string{"libredmm", "javbus"}, nil)

	require.Len(t, got, 1, "occupation-decorated and plain forms should merge")
	assert.Equal(t, "愛梨沙", got[0].JapaneseName)
	assert.False(t, models.IsUnknownActressFields(got[0].LastName, got[0].FirstName, got[0].JapaneseName))
}
