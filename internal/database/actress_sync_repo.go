package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ActressSyncRepository struct {
	db *DB
}

func NewActressSyncRepository(db *DB) *ActressSyncRepository {
	return &ActressSyncRepository{db: db}
}

func (r *ActressSyncRepository) CreateJob(job *models.ActressSyncJob, tasks []models.ActressSyncTask) error {
	err := retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(job).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("actress sync job %s", job.ID), err)
			}
			for i := range tasks {
				res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&tasks[i])
				if res.Error != nil {
					return wrapDBErr("create", fmt.Sprintf("actress sync task %s", tasks[i].ID), res.Error)
				}
				if res.RowsAffected == 0 {
					now := time.Now().UTC()
					tasks[i].Status = models.ActressSyncTaskSkipped
					tasks[i].Stage = "completed"
					tasks[i].Outcome = "skipped"
					tasks[i].Messages = []string{"An equivalent actress sync item is already pending or running"}
					tasks[i].CompletedAt = &now
					tasks[i].DedupeKey += ":duplicate:" + tasks[i].ID
					if err := tx.Create(&tasks[i]).Error; err != nil {
						return wrapDBErr("create", fmt.Sprintf("duplicate actress sync task %s", tasks[i].ID), err)
					}
				}
			}
			return r.refreshJobTx(tx, job.ID, time.Now().UTC())
		})
	})
	if err != nil {
		return err
	}
	refreshed, err := r.FindJob(job.ID)
	if err == nil {
		*job = *refreshed
	}
	return err
}

func (r *ActressSyncRepository) FindJob(id string) (*models.ActressSyncJob, error) {
	var job models.ActressSyncJob
	if err := r.db.First(&job, "id = ?", id).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress sync job %s", id), err)
	}
	return &job, nil
}

func (r *ActressSyncRepository) ListActiveJobs() ([]models.ActressSyncJob, error) {
	jobs := make([]models.ActressSyncJob, 0)
	if err := r.db.Where("status IN ?", []string{models.ActressSyncJobPending, models.ActressSyncJobRunning}).
		Order("created_at ASC").Find(&jobs).Error; err != nil {
		return nil, wrapDBErr("find", "active actress sync jobs", err)
	}
	return jobs, nil
}

func (r *ActressSyncRepository) ListTasks(jobID string) ([]models.ActressSyncTask, error) {
	tasks := make([]models.ActressSyncTask, 0)
	if err := r.db.Where("job_id = ?", jobID).Order("created_at ASC, id ASC").Find(&tasks).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress sync tasks for %s", jobID), err)
	}
	return tasks, nil
}

func (r *ActressSyncRepository) HasActiveTask(dedupeKey string) (bool, error) {
	var count int64
	err := r.db.Model(&models.ActressSyncTask{}).
		Where("dedupe_key = ? AND status IN ?", dedupeKey, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).
		Count(&count).Error
	return count > 0, err
}

// RecoverExpiredLeases makes abandoned running tasks claimable again.
func (r *ActressSyncRepository) RecoverExpiredLeases(now time.Time) error {
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			expired := "status = ? AND (lease_expires_at IS NULL OR datetime(lease_expires_at) <= datetime(?))"
			var cancelledJobIDs []string
			if err := tx.Model(&models.ActressSyncTask{}).
				Where(expired+" AND job_id IN (SELECT id FROM actress_sync_jobs WHERE cancel_requested = 1)", models.ActressSyncTaskRunning, now.UTC().Format(SqliteTimeFormat)).
				Distinct("job_id").Pluck("job_id", &cancelledJobIDs).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.ActressSyncTask{}).
				Where(expired+" AND job_id IN (SELECT id FROM actress_sync_jobs WHERE cancel_requested = 1)", models.ActressSyncTaskRunning, now.UTC().Format(SqliteTimeFormat)).
				Updates(map[string]interface{}{
					"status": models.ActressSyncTaskCancelled, "stage": "completed", "outcome": "cancelled",
					"completed_at": now, "lease_owner": "", "lease_token": "", "lease_expires_at": nil,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.ActressSyncTask{}).
				Where(expired, models.ActressSyncTaskRunning, now.UTC().Format(SqliteTimeFormat)).
				Updates(map[string]interface{}{
					"status": models.ActressSyncTaskPending, "stage": "queued", "lease_owner": "", "lease_token": "", "lease_expires_at": nil,
				}).Error; err != nil {
				return err
			}
			for _, jobID := range cancelledJobIDs {
				if err := r.refreshJobTx(tx, jobID, now); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func (r *ActressSyncRepository) ReleaseOwnerLeases(owner string) error {
	if owner == "" {
		return nil
	}
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			now := time.Now().UTC()
			var cancelledJobIDs []string
			if err := tx.Model(&models.ActressSyncTask{}).
				Where("status = ? AND lease_owner = ? AND job_id IN (SELECT id FROM actress_sync_jobs WHERE cancel_requested = 1)", models.ActressSyncTaskRunning, owner).
				Distinct("job_id").Pluck("job_id", &cancelledJobIDs).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.ActressSyncTask{}).
				Where("status = ? AND lease_owner = ? AND job_id IN (SELECT id FROM actress_sync_jobs WHERE cancel_requested = 1)", models.ActressSyncTaskRunning, owner).
				Updates(map[string]interface{}{
					"status": models.ActressSyncTaskCancelled, "stage": "completed", "outcome": "cancelled", "completed_at": now,
					"lease_owner": "", "lease_token": "", "lease_expires_at": nil,
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.ActressSyncTask{}).
				Where("status = ? AND lease_owner = ?", models.ActressSyncTaskRunning, owner).
				Updates(map[string]interface{}{
					"status": models.ActressSyncTaskPending, "stage": "queued", "lease_owner": "", "lease_token": "", "lease_expires_at": nil,
				}).Error; err != nil {
				return err
			}
			for _, jobID := range cancelledJobIDs {
				if err := r.refreshJobTx(tx, jobID, now); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// ClaimNext atomically claims the oldest runnable task.
func (r *ActressSyncRepository) ClaimNext(owner string, leaseUntil time.Time) (*models.ActressSyncTask, error) {
	var claimed models.ActressSyncTask
	err := retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			var candidate models.ActressSyncTask
			err := tx.Table("actress_sync_tasks AS task").
				Select("task.*").
				Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").
				Where("task.status = ? AND job.cancel_requested = ?", models.ActressSyncTaskPending, false).
				Order("task.created_at ASC, task.id ASC").
				First(&candidate).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}

			now := time.Now().UTC()
			leaseToken := uuid.NewString()
			res := tx.Model(&models.ActressSyncTask{}).
				Where("id = ? AND status = ?", candidate.ID, models.ActressSyncTaskPending).
				Updates(map[string]interface{}{
					"status":           models.ActressSyncTaskRunning,
					"stage":            "resolving",
					"lease_owner":      owner,
					"lease_token":      leaseToken,
					"heartbeat_at":     now,
					"lease_expires_at": leaseUntil,
					"attempts":         gorm.Expr("attempts + 1"),
					"started_at":       gorm.Expr("COALESCE(started_at, ?)", now),
				})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return nil
			}
			if err := tx.Model(&models.ActressSyncJob{}).
				Where("id = ? AND status = ?", candidate.JobID, models.ActressSyncJobPending).
				Updates(map[string]interface{}{"status": models.ActressSyncJobRunning, "started_at": now}).Error; err != nil {
				return err
			}
			candidate.Status = models.ActressSyncTaskRunning
			candidate.Stage = "resolving"
			candidate.LeaseOwner = owner
			candidate.LeaseToken = leaseToken
			candidate.HeartbeatAt = &now
			candidate.LeaseExpiresAt = &leaseUntil
			candidate.Attempts++
			if candidate.StartedAt == nil {
				candidate.StartedAt = &now
			}
			claimed = candidate
			return nil
		})
	})
	if err != nil {
		return nil, wrapDBErr("claim", "next actress sync task", err)
	}
	if claimed.ID == "" {
		return nil, nil
	}
	return &claimed, nil
}

func (r *ActressSyncRepository) Heartbeat(taskID, leaseToken string, leaseUntil time.Time) error {
	now := time.Now().UTC()
	return r.db.Model(&models.ActressSyncTask{}).
		Where("id = ? AND status = ? AND lease_token = ?", taskID, models.ActressSyncTaskRunning, leaseToken).
		Updates(map[string]interface{}{"heartbeat_at": now, "lease_expires_at": leaseUntil}).Error
}

func (r *ActressSyncRepository) UpdateStage(taskID, leaseToken, stage string, messages []string) error {
	updates := map[string]interface{}{"stage": stage}
	if messages != nil {
		updates["messages"] = serializedStringSlice(messages)
	}
	return r.db.Model(&models.ActressSyncTask{}).
		Where("id = ? AND status = ? AND lease_token = ?", taskID, models.ActressSyncTaskRunning, leaseToken).
		Updates(updates).Error
}

func (r *ActressSyncRepository) CompleteTask(task *models.ActressSyncTask, leaseToken string) error {
	if task == nil {
		return ErrInvalidLookup
	}
	now := time.Now().UTC()
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			updates := map[string]interface{}{
				"status":           task.Status,
				"stage":            "completed",
				"outcome":          task.Outcome,
				"messages":         serializedStringSlice(task.Messages),
				"updated_fields":   serializedStringSlice(task.UpdatedFields),
				"warning":          task.Warning,
				"error_message":    task.ErrorMessage,
				"completed_at":     now,
				"lease_owner":      "",
				"lease_token":      "",
				"lease_expires_at": nil,
			}
			res := tx.Model(&models.ActressSyncTask{}).
				Where("id = ? AND status = ? AND lease_token = ?", task.ID, models.ActressSyncTaskRunning, leaseToken).
				Updates(updates)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return fmt.Errorf("task lease lost")
			}
			return r.refreshJobTx(tx, task.JobID, now)
		})
	})
}

func serializedStringSlice(values []string) string {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func (r *ActressSyncRepository) CancelJob(jobID string) error {
	now := time.Now().UTC()
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			res := tx.Model(&models.ActressSyncJob{}).
				Where("id = ? AND status IN ?", jobID, []string{models.ActressSyncJobPending, models.ActressSyncJobRunning}).
				Update("cancel_requested", true)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				var count int64
				if err := tx.Model(&models.ActressSyncJob{}).Where("id = ?", jobID).Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					return ErrNotFound
				}
			}
			if err := tx.Model(&models.ActressSyncTask{}).
				Where("job_id = ? AND status = ?", jobID, models.ActressSyncTaskPending).
				Updates(map[string]interface{}{
					"status": models.ActressSyncTaskCancelled, "stage": "completed", "outcome": "cancelled", "completed_at": now,
				}).Error; err != nil {
				return err
			}
			return r.refreshJobTx(tx, jobID, now)
		})
	})
}

func (r *ActressSyncRepository) refreshJobTx(tx *gorm.DB, jobID string, now time.Time) error {
	type counts struct {
		Total     int
		Terminal  int
		Updated   int
		Warnings  int
		Skipped   int
		Conflicts int
		Failed    int
		Cancelled int
	}
	var c counts
	if err := tx.Raw(`
		SELECT COUNT(*) AS total,
		SUM(CASE WHEN status IN ('completed','skipped','conflict','failed','cancelled') THEN 1 ELSE 0 END) AS terminal,
		SUM(CASE WHEN outcome IN ('updated','updated_with_warning') THEN 1 ELSE 0 END) AS updated,
		SUM(CASE WHEN outcome = 'updated_with_warning' OR TRIM(COALESCE(warning,'')) <> '' THEN 1 ELSE 0 END) AS warnings,
		SUM(CASE WHEN status = 'skipped' THEN 1 ELSE 0 END) AS skipped,
		SUM(CASE WHEN status = 'conflict' THEN 1 ELSE 0 END) AS conflicts,
		SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed,
		SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) AS cancelled
		FROM actress_sync_tasks WHERE job_id = ?`, jobID).Scan(&c).Error; err != nil {
		return err
	}
	var job models.ActressSyncJob
	if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
		return err
	}
	status := job.Status
	completedAt := job.CompletedAt
	if c.Total == c.Terminal {
		status = models.ActressSyncJobCompleted
		if job.CancelRequested {
			status = models.ActressSyncJobCancelled
		}
		completedAt = &now
	}
	return tx.Model(&models.ActressSyncJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"status": status, "total_tasks": c.Total, "completed": c.Terminal, "updated": c.Updated,
		"warnings": c.Warnings, "skipped": c.Skipped, "conflicts": c.Conflicts,
		"failed": c.Failed, "cancelled": c.Cancelled, "completed_at": completedAt,
	}).Error
}
