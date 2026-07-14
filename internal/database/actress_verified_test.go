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

func TestResolveVerifiedIdentityPromotesAndMergesExactUnverifiedJapaneseName(t *testing.T) {
	_, actressRepo, movieRepo := newVerifiedActressTestRepos(t)
	canonical := &models.Actress{JapaneseName: "弥生みづき", FirstName: "Mizuki", LastName: "Yayoi"}
	duplicate := &models.Actress{JapaneseName: "弥生 みづき", ThumbURL: "duplicate.jpg"}
	require.NoError(t, actressRepo.Create(canonical))
	require.NoError(t, actressRepo.Create(duplicate))
	require.NoError(t, movieRepo.Create(&models.Movie{
		ContentID: "jnt051", ID: "JNT-051", Actresses: []models.Actress{*duplicate},
	}))

	resolution, err := actressRepo.ResolveVerifiedIdentity(duplicate.ID, models.Actress{
		DMMID: 777, JapaneseName: "弥生みづき", ThumbURL: "canonical.jpg",
	}, false)
	require.NoError(t, err)
	assert.Equal(t, canonical.ID, resolution.Actress.ID)
	assert.Equal(t, 777, resolution.Actress.DMMID)
	assert.Equal(t, "弥生みづき", resolution.Actress.JapaneseName)
	assert.Equal(t, "Mizuki", resolution.Actress.FirstName)
	assert.Equal(t, "canonical.jpg", resolution.Actress.ThumbURL)
	assert.Equal(t, []uint{duplicate.ID}, resolution.MergedFromIDs)
	assert.Equal(t, 1, resolution.UpdatedMovies)

	_, err = actressRepo.FindByID(duplicate.ID)
	assert.True(t, IsNotFound(err), "duplicate row must be deleted after its mappings move")
	movie, err := movieRepo.FindByContentID("jnt051")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, canonical.ID, movie.Actresses[0].ID)
	count, err := actressRepo.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestResolveVerifiedIdentityKeepsDMMOwnerProfileAndAddsVerifiedActivityNameAlias(t *testing.T) {
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
	assert.Equal(t, polluted.ID, resolution.Actress.ID)
	assert.Equal(t, 777, resolution.Actress.DMMID)
	assert.Equal(t, "もな", resolution.Actress.JapaneseName)
	assert.Equal(t, "Mona", resolution.Actress.FirstName)
	assert.Contains(t, strings.Split(resolution.Actress.Aliases, "|"), "弥生みづき")
	assert.Equal(t, []uint{canonical.ID}, resolution.MergedFromIDs)

	_, err = actressRepo.FindByID(canonical.ID)
	assert.True(t, IsNotFound(err))
	movie, err := movieRepo.FindByContentID("jnt051")
	require.NoError(t, err)
	require.Len(t, movie.Actresses, 1)
	assert.Equal(t, polluted.ID, movie.Actresses[0].ID)
}

func TestResolveVerifiedIdentityDeduplicatesNormalizedActivityNameAlias(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	owner := &models.Actress{DMMID: 778, JapaneseName: "既存正名", Aliases: "弥生 みづき"}
	require.NoError(t, actressRepo.Create(owner))

	resolution, err := actressRepo.ResolveVerifiedIdentity(0, models.Actress{
		DMMID: 778, JapaneseName: "弥生みづき",
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "弥生 みづき", resolution.Actress.Aliases)
	assert.Empty(t, resolution.AliasesAdded)
}

func TestResolveVerifiedIdentityDoesNotPromoteUnverifiedNicknameBySourceID(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	nickname := &models.Actress{JapaneseName: "新人ちゃん", FirstName: "Nickname"}
	require.NoError(t, actressRepo.Create(nickname))

	resolution, err := actressRepo.ResolveVerifiedIdentity(nickname.ID, models.Actress{
		DMMID: 888, JapaneseName: "実名女優", FirstName: "Jitsumei", LastName: "Joyu",
	}, true)
	require.NoError(t, err)
	assert.NotEqual(t, nickname.ID, resolution.Actress.ID)
	assert.Equal(t, "実名女優", resolution.Actress.JapaneseName)
	assert.Equal(t, "Jitsumei", resolution.Actress.FirstName)
	assert.Equal(t, "Joyu", resolution.Actress.LastName)
	assert.Empty(t, resolution.Actress.Aliases)
	savedNickname, err := actressRepo.FindByID(nickname.ID)
	require.NoError(t, err)
	assert.Zero(t, savedNickname.DMMID)
	count, err := actressRepo.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestResolveVerifiedIdentityKeepsSameNameDifferentPositiveDMMIdentity(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	canonical := &models.Actress{DMMID: 111, JapaneseName: "実名女優"}
	require.NoError(t, actressRepo.Create(canonical))

	resolution, err := actressRepo.ResolveVerifiedIdentity(0, models.Actress{
		DMMID: 222, JapaneseName: "実名女優",
	}, true)
	require.NoError(t, err)
	assert.NotEqual(t, canonical.ID, resolution.Actress.ID)
	assert.Equal(t, 222, resolution.Actress.DMMID)
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

func TestResolveVerifiedIdentityConcurrentAliasesAreAccumulatedWithoutOverwrite(t *testing.T) {
	_, actressRepo, _ := newVerifiedActressTestRepos(t)
	owner := &models.Actress{DMMID: 7001, JapaneseName: "既存正名", FirstName: "기존", Aliases: "既存別名"}
	require.NoError(t, actressRepo.Create(owner))

	activityNames := []string{"活動名一", "活動名二", "活動名三", "活動名四", "活動名五"}
	var wg sync.WaitGroup
	errs := make(chan error, len(activityNames))
	for _, activityName := range activityNames {
		activityName := activityName
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := actressRepo.ResolveVerifiedIdentity(0, models.Actress{DMMID: 7001, JapaneseName: activityName}, true)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	stored, err := actressRepo.FindByDMMID(7001)
	require.NoError(t, err)
	assert.Equal(t, "既存正名", stored.JapaneseName)
	assert.Equal(t, "기존", stored.FirstName)
	wantAliases := append([]string{"既存別名"}, activityNames...)
	assert.ElementsMatch(t, wantAliases, strings.Split(stored.Aliases, "|"))
}
