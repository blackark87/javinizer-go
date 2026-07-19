package database

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressFindOrCreateDoesNotReuseVerifiedProfilesByNameOnly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	aliasMatch := &models.Actress{DMMID: 100, JapaneseName: "波多野結衣", Aliases: "はたの ゆい|結衣"}
	require.NoError(t, repo.Create(context.Background(), aliasMatch))
	incomingAlias := &models.Actress{JapaneseName: "はたのゆい", ThumbURL: "alias.jpg"}
	require.NoError(t, repo.FindOrCreate(context.Background(), incomingAlias))
	assert.NotEqual(t, aliasMatch.ID, incomingAlias.ID)
	assert.Zero(t, incomingAlias.DMMID)
	assert.Equal(t, "alias.jpg", incomingAlias.ThumbURL)

	primaryMatch := &models.Actress{DMMID: 200, FirstName: "마유키", LastName: "이토"}
	require.NoError(t, repo.Create(context.Background(), primaryMatch))
	incomingPrimary := &models.Actress{FirstName: "마유키", LastName: "이 토"}
	require.NoError(t, repo.FindOrCreate(context.Background(), incomingPrimary))
	assert.NotEqual(t, primaryMatch.ID, incomingPrimary.ID)
	assert.Zero(t, incomingPrimary.DMMID)
}

func TestActressFindOrCreateKeepsDifferentPositiveDMMIdentities(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	existing := &models.Actress{DMMID: 100, JapaneseName: "波多野結衣"}
	require.NoError(t, repo.Create(context.Background(), existing))
	incoming := &models.Actress{DMMID: 200, JapaneseName: "波多野 結衣"}
	require.NoError(t, repo.FindOrCreate(context.Background(), incoming))
	assert.NotEqual(t, existing.ID, incoming.ID)
	assert.Equal(t, 200, incoming.DMMID)

	count, countErr := repo.Count(context.Background())
	require.NoError(t, countErr)
	assert.EqualValues(t, 2, count)
}

func TestActressFindOrCreateConcurrentVerifiedIdentityIsIdempotent(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	var wg sync.WaitGroup
	ids := make(chan uint, 8)
	errs := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			actress := &models.Actress{DMMID: 777, JapaneseName: "同時女優", ThumbURL: "thumb.jpg"}
			if createErr := repo.FindOrCreate(context.Background(), actress); createErr != nil {
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
	count, countErr := repo.Count(context.Background())
	require.NoError(t, countErr)
	assert.EqualValues(t, 1, count)
}

func TestActressTranslationUpsertIsAtomic(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 900, JapaneseName: "翻訳女優"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	repo := NewActressTranslationRepository(db)

	const workers = 128
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(value int) {
			defer wg.Done()
			errs <- repo.Upsert(context.Background(), &models.ActressTranslation{ActressID: actress.ID, Language: "ko", DisplayName: fmt.Sprintf("이름%d", value)})
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

func TestMovieActressTranslationsStayMappedToOriginalActressIndex(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	movie := &models.Movie{
		ID:        "MAP-001",
		ContentID: "map001",
		Actresses: []models.Actress{
			{DMMID: 2002, JapaneseName: "二人目"},
			{DMMID: 1001, JapaneseName: "一人目"},
		},
	}
	translations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "ko", DisplayName: "두 번째"},
		{ActressIndex: 1, Language: "ko", DisplayName: "첫 번째"},
	}

	_, err := repo.UpsertWithTranslations(context.Background(), movie, nil, translations)
	require.NoError(t, err)

	var actresses []models.Actress
	require.NoError(t, db.Order("dmm_id ASC").Find(&actresses).Error)
	require.Len(t, actresses, 2)
	var first, second models.ActressTranslation
	require.NoError(t, db.First(&first, "actress_id = ? AND language = ?", actresses[0].ID, "ko").Error)
	require.NoError(t, db.First(&second, "actress_id = ? AND language = ?", actresses[1].ID, "ko").Error)
	assert.Equal(t, "첫 번째", first.DisplayName)
	assert.Equal(t, "두 번째", second.DisplayName)
}
