package database

import (
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressFindOrCreateDoesNotReuseVerifiedProfilesByNameOnly(t *testing.T) {
	db, err := New(&config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())
	repo := NewActressRepository(db)

	aliasMatch := &models.Actress{DMMID: 100, JapaneseName: "波多野結衣", Aliases: "はたの ゆい|結衣"}
	require.NoError(t, repo.Create(aliasMatch))
	incomingAlias := &models.Actress{JapaneseName: "はたのゆい", ThumbURL: "alias.jpg"}
	require.NoError(t, repo.FindOrCreate(incomingAlias))
	assert.NotEqual(t, aliasMatch.ID, incomingAlias.ID)
	assert.Zero(t, incomingAlias.DMMID)
	assert.Equal(t, "alias.jpg", incomingAlias.ThumbURL)

	primaryMatch := &models.Actress{DMMID: 200, FirstName: "마유키", LastName: "이토"}
	require.NoError(t, repo.Create(primaryMatch))
	incomingPrimary := &models.Actress{FirstName: "마유키", LastName: "이 토"}
	require.NoError(t, repo.FindOrCreate(incomingPrimary))
	assert.NotEqual(t, primaryMatch.ID, incomingPrimary.ID)
	assert.Zero(t, incomingPrimary.DMMID)
}

func TestActressFindOrCreateKeepsDifferentPositiveDMMIdentities(t *testing.T) {
	db, err := New(&config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())
	repo := NewActressRepository(db)

	existing := &models.Actress{DMMID: 100, JapaneseName: "波多野結衣"}
	require.NoError(t, repo.Create(existing))
	incoming := &models.Actress{DMMID: 200, JapaneseName: "波多野 結衣"}
	require.NoError(t, repo.FindOrCreate(incoming))
	assert.NotEqual(t, existing.ID, incoming.ID)
	assert.Equal(t, 200, incoming.DMMID)

	count, countErr := repo.Count()
	require.NoError(t, countErr)
	assert.EqualValues(t, 2, count)
}

func TestActressFindOrCreateConcurrentVerifiedIdentityIsIdempotent(t *testing.T) {
	db, err := New(&config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())
	repo := NewActressRepository(db)

	var wg sync.WaitGroup
	ids := make(chan uint, 8)
	errs := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actress := &models.Actress{DMMID: 777, JapaneseName: "同時女優", ThumbURL: "thumb.jpg"}
			if createErr := repo.FindOrCreate(actress); createErr != nil {
				errs <- createErr
				return
			}
			ids <- actress.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for createErr := range errs {
		require.NoError(t, createErr)
	}
	uniqueIDs := map[uint]struct{}{}
	for id := range ids {
		uniqueIDs[id] = struct{}{}
	}
	assert.Len(t, uniqueIDs, 1)
	count, countErr := repo.Count()
	require.NoError(t, countErr)
	assert.EqualValues(t, 1, count)
}

func TestActressTranslationUpsertIsAtomic(t *testing.T) {
	db, err := New(&config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"}})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 900, JapaneseName: "翻訳女優"}
	require.NoError(t, actressRepo.Create(actress))
	repo := NewActressTranslationRepository(db)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func(value int) {
			defer wg.Done()
			errs <- repo.Upsert(&models.ActressTranslation{ActressID: actress.ID, Language: "ko", Name: fmt.Sprintf("이름%d", value)})
		}(index)
	}
	wg.Wait()
	close(errs)
	for upsertErr := range errs {
		require.NoError(t, upsertErr)
	}
	var count int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
