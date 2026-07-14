package database

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActressSyncRepositoryTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(&config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "sync-queue.db")}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return db
}

func actressSyncTestJob(id string, taskCount int) (*models.ActressSyncJob, []models.ActressSyncTask) {
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: id, Scope: "selected", Status: models.ActressSyncJobPending, TotalTasks: taskCount, CreatedAt: now}
	tasks := make([]models.ActressSyncTask, taskCount)
	for index := range tasks {
		actressID := uint(index + 1)
		tasks[index] = models.ActressSyncTask{
			ID: id + "-task-" + string(rune('a'+index)), JobID: id, Kind: models.ActressSyncTaskKindActress,
			ActressID: &actressID, DedupeKey: id + ":actress:" + string(rune('a'+index)),
			Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now,
		}
	}
	return job, tasks
}

func TestActressSyncRepositoryClaimsEachTaskOnceWithUniqueLeaseToken(t *testing.T) {
	db := newActressSyncRepositoryTestDB(t)
	repo := NewActressSyncRepository(db)
	job, tasks := actressSyncTestJob("claim-job", 5)
	require.NoError(t, repo.CreateJob(job, tasks))

	var wg sync.WaitGroup
	claimed := make(chan *models.ActressSyncTask, 10)
	for index := 0; index < 10; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := repo.ClaimNext("worker", time.Now().UTC().Add(time.Minute))
			require.NoError(t, err)
			if task != nil {
				claimed <- task
			}
		}()
	}
	wg.Wait()
	close(claimed)

	ids := map[string]struct{}{}
	tokens := map[string]struct{}{}
	for task := range claimed {
		ids[task.ID] = struct{}{}
		require.NotEmpty(t, task.LeaseToken)
		tokens[task.LeaseToken] = struct{}{}
	}
	assert.Len(t, ids, 5)
	assert.Len(t, tokens, 5)

	stored, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	for _, task := range stored {
		assert.Equal(t, models.ActressSyncTaskRunning, task.Status)
		assert.Equal(t, 1, task.Attempts)
		assert.NotNil(t, task.HeartbeatAt)
	}
}

func TestActressSyncRepositoryRejectsStaleLeaseAndPersistsDetails(t *testing.T) {
	db := newActressSyncRepositoryTestDB(t)
	repo := NewActressSyncRepository(db)
	job, tasks := actressSyncTestJob("complete-job", 1)
	require.NoError(t, repo.CreateJob(job, tasks))
	claimed, err := repo.ClaimNext("worker", time.Now().UTC().Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	claimed.Status = models.ActressSyncTaskCompleted
	claimed.Outcome = "updated_with_warning"
	claimed.Messages = []string{"resolved", "mapped"}
	claimed.UpdatedFields = []string{"dmm_id", "nfo"}
	claimed.Warning = "NFO was unavailable"
	require.Error(t, repo.CompleteTask(claimed, "stale-token"))
	require.NoError(t, repo.CompleteTask(claimed, claimed.LeaseToken))
	require.NoError(t, repo.RecoverExpiredLeases(time.Now().UTC().Add(time.Hour)))

	stored, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, claimed.Messages, stored[0].Messages)
	assert.Equal(t, claimed.UpdatedFields, stored[0].UpdatedFields)
	assert.Equal(t, "updated_with_warning", stored[0].Outcome)
	assert.Equal(t, 1, stored[0].Attempts, "completed tasks must never be replayed during recovery")

	completedJob, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ActressSyncJobCompleted, completedJob.Status)
	assert.Equal(t, 1, completedJob.Completed)
	assert.Equal(t, 1, completedJob.Updated)
	assert.Equal(t, 1, completedJob.Warnings)
}

func TestActressSyncRepositoryRecoversExpiredLeasesAndFinalizesCancelledJobs(t *testing.T) {
	db := newActressSyncRepositoryTestDB(t)
	repo := NewActressSyncRepository(db)

	recoverJob, recoverTasks := actressSyncTestJob("recover-job", 1)
	require.NoError(t, repo.CreateJob(recoverJob, recoverTasks))
	recoveredTask, err := repo.ClaimNext("old-worker", time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)
	require.NotNil(t, recoveredTask)
	require.NoError(t, repo.RecoverExpiredLeases(time.Now().UTC()))
	recoveredRows, err := repo.ListTasks(recoverJob.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ActressSyncTaskPending, recoveredRows[0].Status)
	assert.Empty(t, recoveredRows[0].LeaseToken)
	require.NoError(t, repo.CancelJob(recoverJob.ID))

	cancelJob, cancelTasks := actressSyncTestJob("cancel-job", 1)
	require.NoError(t, repo.CreateJob(cancelJob, cancelTasks))
	cancelledRunning, err := repo.ClaimNext("old-worker", time.Now().UTC().Add(-time.Minute))
	require.NoError(t, err)
	require.NotNil(t, cancelledRunning)
	require.NoError(t, repo.CancelJob(cancelJob.ID))
	require.NoError(t, repo.RecoverExpiredLeases(time.Now().UTC()))

	cancelledRows, err := repo.ListTasks(cancelJob.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ActressSyncTaskCancelled, cancelledRows[0].Status)
	cancelled, err := repo.FindJob(cancelJob.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ActressSyncJobCancelled, cancelled.Status)
}

func TestActressSyncRepositoryMakesConcurrentDuplicateACompletedSkip(t *testing.T) {
	db := newActressSyncRepositoryTestDB(t)
	repo := NewActressSyncRepository(db)
	first, firstTasks := actressSyncTestJob("first-job", 1)
	firstTasks[0].DedupeKey = "actress:42"
	require.NoError(t, repo.CreateJob(first, firstTasks))

	second, secondTasks := actressSyncTestJob("second-job", 1)
	secondTasks[0].DedupeKey = "actress:42"
	require.NoError(t, repo.CreateJob(second, secondTasks))
	assert.Equal(t, models.ActressSyncJobCompleted, second.Status)
	assert.Equal(t, 1, second.Skipped)

	rows, err := repo.ListTasks(second.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, models.ActressSyncTaskSkipped, rows[0].Status)
	assert.Contains(t, rows[0].Messages[0], "already pending or running")
}

func TestActressSyncRepositoryNormalizesLegacyMovieTasksToOneActiveItem(t *testing.T) {
	db := newActressSyncRepositoryTestDB(t)
	repo := NewActressSyncRepository(db)

	first, firstTasks := actressSyncTestJob("legacy-first", 1)
	firstTasks[0].Kind = models.ActressSyncTaskKindUnknownMovie
	firstTasks[0].MovieContentID = "mium921"
	firstTasks[0].MovieID = "300MIUM-921"
	firstTasks[0].DedupeKey = "movie:mium921:placeholder:1"
	require.NoError(t, repo.CreateJob(first, firstTasks))

	second, secondTasks := actressSyncTestJob("legacy-second", 1)
	secondTasks[0].Kind = models.ActressSyncTaskKindUnknownMovie
	secondTasks[0].MovieContentID = "mium921"
	secondTasks[0].MovieID = "300MIUM-921"
	secondTasks[0].DedupeKey = "movie:mium921:placeholder:2"
	require.NoError(t, repo.CreateJob(second, secondTasks))

	require.NoError(t, repo.NormalizeActiveMovieTasks(time.Now().UTC()))
	firstRows, err := repo.ListTasks(first.ID)
	require.NoError(t, err)
	secondRows, err := repo.ListTasks(second.ID)
	require.NoError(t, err)
	require.Len(t, firstRows, 1)
	require.Len(t, secondRows, 1)
	statuses := []string{firstRows[0].Status, secondRows[0].Status}
	assert.ElementsMatch(t, []string{models.ActressSyncTaskPending, models.ActressSyncTaskSkipped}, statuses)
	if firstRows[0].Status == models.ActressSyncTaskPending {
		assert.Equal(t, "movie:mium921:missing-dmm", firstRows[0].DedupeKey)
	} else {
		assert.Equal(t, "movie:mium921:missing-dmm", secondRows[0].DedupeKey)
	}
}
