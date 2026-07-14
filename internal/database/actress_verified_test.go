package database

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newVerifiedActressTestRepos(t *testing.T) (*DB, *ActressRepository, *MovieRepository) {
	t.Helper()
	db, err := New(&config.Config{Database: config.DatabaseConfig{
		Type: "sqlite",
		DSN:  filepath.Join(t.TempDir(), "verified-actress.db"),
	}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return db, NewActressRepository(db), NewMovieRepository(db)
}

func TestResolveVerifiedIdentityMergesNicknameIntoExistingCanonicalActress(t *testing.T) {
	_, actressRepo, movieRepo := newVerifiedActressTestRepos(t)
	nickname := &models.Actress{JapaneseName: "もな", ThumbURL: "nickname.jpg"}
	canonical := &models.Actress{JapaneseName: "弥生みづき", FirstName: "Mizuki", LastName: "Yayoi"}
	require.NoError(t, actressRepo.Create(nickname))
	require.NoError(t, actressRepo.Create(canonical))
	require.NoError(t, movieRepo.Create(&models.Movie{
		ContentID: "jnt051", ID: "JNT-051", Actresses: []models.Actress{*nickname},
	}))

	resolution, err := actressRepo.ResolveVerifiedIdentity(nickname.ID, models.Actress{
		DMMID: 777, JapaneseName: "弥生みづき", ThumbURL: "canonical.jpg",
	}, false)
	require.NoError(t, err)
	assert.Equal(t, canonical.ID, resolution.Actress.ID)
	assert.Equal(t, 777, resolution.Actress.DMMID)
	assert.Equal(t, "弥生みづき", resolution.Actress.JapaneseName)
	assert.Contains(t, strings.Split(resolution.Actress.Aliases, "|"), "もな")
	assert.Equal(t, []uint{nickname.ID}, resolution.MergedFromIDs)
	assert.Equal(t, 1, resolution.UpdatedMovies)

	_, err = actressRepo.FindByID(nickname.ID)
	assert.True(t, IsNotFound(err), "nickname row must be deleted after its mappings move")
	movie, err := movieRepo.FindByContentID("jnt051")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, canonical.ID, movie.Actresses[0].ID)
	count, err := actressRepo.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestResolveVerifiedIdentityRepairsPollutedDMMOwnerInsteadOfCreatingAnotherRow(t *testing.T) {
	_, actressRepo, movieRepo := newVerifiedActressTestRepos(t)
	polluted := &models.Actress{DMMID: 777, JapaneseName: "もな", FirstName: "Mona", Aliases: "弥生みづき"}
	canonical := &models.Actress{JapaneseName: "弥生みづき"}
	require.NoError(t, actressRepo.Create(polluted))
	require.NoError(t, actressRepo.Create(canonical))
	require.NoError(t, movieRepo.Create(&models.Movie{
		ContentID: "jnt051", ID: "JNT-051", Actresses: []models.Actress{*polluted},
	}))

	resolution, err := actressRepo.ResolveVerifiedIdentity(0, models.Actress{
		DMMID: 777, JapaneseName: "弥生みづき", FirstName: "Mizuki", LastName: "Yayoi",
	}, true)
	require.NoError(t, err)
	assert.Equal(t, canonical.ID, resolution.Actress.ID)
	assert.Equal(t, 777, resolution.Actress.DMMID)
	assert.Contains(t, strings.Split(resolution.Actress.Aliases, "|"), "もな")
	assert.Equal(t, []uint{polluted.ID}, resolution.MergedFromIDs)

	_, err = actressRepo.FindByID(polluted.ID)
	assert.True(t, IsNotFound(err))
	movie, err := movieRepo.FindByContentID("jnt051")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, canonical.ID, movie.Actresses[0].ID)
}

func TestResolveVerifiedIdentityCanonicalizesSourceInPlaceWhenNoReusableRowExists(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	nickname := &models.Actress{JapaneseName: "新人ちゃん", FirstName: "Nickname"}
	require.NoError(t, actressRepo.Create(nickname))

	resolution, err := actressRepo.ResolveVerifiedIdentity(nickname.ID, models.Actress{
		DMMID: 888, JapaneseName: "実名女優", FirstName: "Jitsumei", LastName: "Joyu",
	}, false)
	require.NoError(t, err)
	assert.Equal(t, nickname.ID, resolution.Actress.ID)
	assert.Equal(t, "実名女優", resolution.Actress.JapaneseName)
	assert.Equal(t, "Jitsumei", resolution.Actress.FirstName)
	assert.Equal(t, "Joyu", resolution.Actress.LastName)
	assert.Contains(t, strings.Split(resolution.Actress.Aliases, "|"), "新人ちゃん")
	assert.Contains(t, strings.Split(resolution.Actress.Aliases, "|"), "Nickname")
	count, err := actressRepo.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestResolveVerifiedIdentityRejectsCanonicalNameOwnedByDifferentDMMIdentity(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	source := &models.Actress{JapaneseName: "별명"}
	canonical := &models.Actress{DMMID: 111, JapaneseName: "実名女優"}
	require.NoError(t, actressRepo.Create(source))
	require.NoError(t, actressRepo.Create(canonical))

	_, err := actressRepo.ResolveVerifiedIdentity(source.ID, models.Actress{
		DMMID: 222, JapaneseName: "実名女優",
	}, false)
	conflict, ok := AsActressDMMIDConflict(err)
	require.True(t, ok)
	assert.Equal(t, canonical.ID, conflict.ExistingID)
	assert.Equal(t, 111, conflict.ExistingDMMID)
	assert.Equal(t, 222, conflict.IncomingDMMID)

	savedSource, findErr := actressRepo.FindByID(source.ID)
	require.NoError(t, findErr)
	assert.Equal(t, 0, savedSource.DMMID)
	count, countErr := actressRepo.Count()
	require.NoError(t, countErr)
	assert.EqualValues(t, 2, count)
}

func TestResolveVerifiedIdentityConcurrentCreateIsIdempotent(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	var wg sync.WaitGroup
	ids := make(chan uint, 8)
	errs := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolution, err := actressRepo.ResolveVerifiedIdentity(0, models.Actress{
				DMMID: 999, JapaneseName: "同時女優",
			}, true)
			if err != nil {
				errs <- err
				return
			}
			ids <- resolution.Actress.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	unique := make(map[uint]struct{})
	for id := range ids {
		unique[id] = struct{}{}
	}
	assert.Len(t, unique, 1)
	count, err := actressRepo.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}
