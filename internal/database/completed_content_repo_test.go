package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchFileOperationRepository_ListCompletedContent(t *testing.T) {
	db := setupTestDBV2(t)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	repo := NewBatchFileOperationRepository(db)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Create(&models.Job{
		ID:          "job-new",
		Status:      models.JobStatusOrganized,
		StartedAt:   now.Add(-time.Hour),
		OrganizedAt: &now,
	}).Error)
	require.NoError(t, db.Create(&models.Job{
		ID:        "job-old",
		Status:    models.JobStatusOrganized,
		StartedAt: now.Add(-3 * time.Hour),
	}).Error)

	actress := models.Actress{
		FirstName:    "후유모에",
		LastName:     "시라이와",
		JapaneseName: "白岩冬萌",
		Reading:      "しらいわ とも",
		Aliases:      "Tomo Shiroiwa|Winter",
	}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{
		ActressID:   actress.ID,
		Language:    "ko",
		DisplayName: "시라이와 토모",
	}).Error)

	movie := models.Movie{
		ContentID:     "mium00985",
		ID:            "MIUM-985",
		DisplayTitle:  "정리된 한국어 제목",
		Title:         "한국어 제목",
		OriginalTitle: "綺麗なお姉さん",
		Poster: models.PosterState{
			PosterURL:        "https://example.com/poster.jpg",
			CroppedPosterURL: "/api/v1/temp/posters/job-new/MIUM-985.jpg",
		},
		CreatedAt: now.Add(-24 * time.Hour),
		UpdatedAt: now,
	}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO movie_actresses(movie_content_id, actress_id) VALUES (?, ?)",
		movie.ContentID,
		actress.ID,
	).Error)
	require.NoError(t, db.Create(&models.MovieTranslation{
		MovieID:  movie.ContentID,
		Language: "ko",
		Title:    "번역 테이블 제목",
	}).Error)

	createOperation := func(op models.BatchFileOperation) {
		t.Helper()
		require.NoError(t, db.Create(&op).Error)
	}
	createOperation(models.BatchFileOperation{
		BatchJobID: "job-old", MovieID: movie.ID, OriginalPath: "/src/a.mp4",
		NewPath: "/dest/MIUM-985/MIUM-985.mp4", OperationType: models.OperationTypeMove,
		RevertStatus: models.RevertStatusApplied, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	})
	createOperation(models.BatchFileOperation{
		BatchJobID: "job-new", MovieID: movie.ID, OriginalPath: "/src/a-copy.mp4",
		NewPath: "/dest/MIUM-985/MIUM-985.mp4", OperationType: models.OperationTypeCopy,
		RevertStatus: models.RevertStatusApplied, CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now,
	})
	createOperation(models.BatchFileOperation{
		BatchJobID: "job-new", MovieID: movie.ID, OriginalPath: "/src/b.mp4",
		NewPath: "/dest/MIUM-985/MIUM-985-CD2.mp4", OperationType: models.OperationTypeMove,
		RevertStatus: models.RevertStatusApplied, CreatedAt: now.Add(-10 * time.Minute), UpdatedAt: now,
	})
	createOperation(models.BatchFileOperation{
		BatchJobID: "job-new", MovieID: movie.ID, OriginalPath: "/src/reverted.mp4",
		NewPath: "/dest/MIUM-985/reverted.mp4", OperationType: models.OperationTypeMove,
		RevertStatus: models.RevertStatusReverted, CreatedAt: now.Add(-5 * time.Minute), UpdatedAt: now,
	})
	createOperation(models.BatchFileOperation{
		BatchJobID: "job-old", MovieID: "NO-CACHE-001", OriginalPath: "/src/no-cache.mp4",
		NewPath: "/dest/NO-CACHE-001.mp4", OperationType: models.OperationTypeMove,
		RevertStatus: models.RevertStatusApplied, CreatedAt: now.Add(-90 * time.Minute), UpdatedAt: now,
	})
	createOperation(models.BatchFileOperation{
		BatchJobID: "job-old", MovieID: "FAILED-001", OriginalPath: "/src/failed.mp4",
		NewPath: "/dest/FAILED-001.mp4", OperationType: models.OperationTypeMove,
		RevertStatus: models.RevertStatusFailed, CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now,
	})

	t.Run("groups applied paths and keeps metadata", func(t *testing.T) {
		items, total, err := repo.ListCompletedContent(ctx, "", 20, 0)
		require.NoError(t, err)
		require.Equal(t, int64(2), total)
		require.Len(t, items, 2)

		first := items[0]
		assert.Equal(t, "MIUM-985", first.MovieID)
		assert.Equal(t, "mium00985", first.ContentID)
		assert.Equal(t, "job-new", first.LatestJobID)
		assert.Equal(t, []string{
			"/dest/MIUM-985/MIUM-985-CD2.mp4",
			"/dest/MIUM-985/MIUM-985.mp4",
		}, first.Paths)
		require.Len(t, first.Actresses, 1)
		assert.Equal(t, "白岩冬萌", first.Actresses[0].JapaneseName)
		require.Len(t, first.Actresses[0].Translations, 1)
		assert.Equal(t, "시라이와 토모", first.Actresses[0].Translations[0].DisplayName)

		fallback := items[1]
		assert.Equal(t, "NO-CACHE-001", fallback.MovieID)
		assert.Empty(t, fallback.ContentID)
		assert.Equal(t, []string{"/dest/NO-CACHE-001.mp4"}, fallback.Paths)
		assert.Empty(t, fallback.Actresses)
	})

	t.Run("searches title and actress identity fields", func(t *testing.T) {
		queries := []string{
			"정리된 한국어",
			"綺麗なお姉さん",
			"번역 테이블 제목",
			"白岩冬萌",
			"Tomo Shiroiwa",
			"시라이와 토모",
		}
		for _, query := range queries {
			items, total, err := repo.ListCompletedContent(ctx, query, 20, 0)
			require.NoError(t, err, query)
			assert.Equal(t, int64(1), total, query)
			require.Len(t, items, 1, query)
			assert.Equal(t, "MIUM-985", items[0].MovieID, query)
		}
	})

	t.Run("searches fallback movie ID and path", func(t *testing.T) {
		items, total, err := repo.ListCompletedContent(ctx, "NO-CACHE", 20, 0)
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		require.Len(t, items, 1)
		assert.Equal(t, "NO-CACHE-001", items[0].MovieID)
	})

	t.Run("paginates newest movies", func(t *testing.T) {
		items, total, err := repo.ListCompletedContent(ctx, "", 1, 1)
		require.NoError(t, err)
		assert.Equal(t, int64(2), total)
		require.Len(t, items, 1)
		assert.Equal(t, "NO-CACHE-001", items[0].MovieID)
	})
}
