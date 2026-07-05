package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillActressThumb(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: "file::memory:?cache=shared"},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	repo := database.NewActressRepository(db)
	require.NoError(t, repo.Create(&models.Actress{
		JapaneseName: "あいり",
		ThumbURL:     "https://pics.dmm.co.jp/mono/actjpgs/airi.jpg",
	}))

	t.Run("empty thumb is backfilled from DB by Japanese name", func(t *testing.T) {
		a := &models.Actress{JapaneseName: "あいり"}
		backfillActressThumb(a, repo)
		assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/airi.jpg", a.ThumbURL)
	})

	t.Run("existing thumb is never overwritten", func(t *testing.T) {
		a := &models.Actress{JapaneseName: "あいり", ThumbURL: "https://scraped/x.jpg"}
		backfillActressThumb(a, repo)
		assert.Equal(t, "https://scraped/x.jpg", a.ThumbURL)
	})

	t.Run("unknown actress is skipped", func(t *testing.T) {
		a := &models.Actress{FirstName: models.UnknownActressName, JapaneseName: models.UnknownActressName}
		backfillActressThumb(a, repo)
		assert.Empty(t, a.ThumbURL)
	})

	t.Run("no DB match leaves thumb empty", func(t *testing.T) {
		a := &models.Actress{JapaneseName: "存在しない"}
		backfillActressThumb(a, repo)
		assert.Empty(t, a.ThumbURL)
	})
}
