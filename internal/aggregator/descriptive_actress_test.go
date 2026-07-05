package aggregator

import (
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
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	agg := NewWithDatabase(cfg, db)

	results := map[string]*models.ScraperResult{
		"libredmm": {Actresses: []models.ActressInfo{{JapaneseName: "あいり 21歳 大学3年生"}}},
		"javbus":   {Actresses: []models.ActressInfo{{JapaneseName: "あいり"}}},
	}

	got := agg.getActressesByPriority(results, []string{"libredmm", "javbus"})

	require.Len(t, got, 1, "decorated and plain forms should merge into one actress")
	assert.Equal(t, "あいり", got[0].JapaneseName)
	assert.False(t, models.IsUnknownActressFields(got[0].LastName, got[0].FirstName, got[0].JapaneseName))
}
