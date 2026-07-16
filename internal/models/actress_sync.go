package models

import "time"

// Durable actress-sync job, task, and task-kind values.
const (
	ActressSyncJobPending   = "pending"
	ActressSyncJobRunning   = "running"
	ActressSyncJobCompleted = "completed"
	ActressSyncJobCancelled = "cancelled"

	ActressSyncTaskPending   = "pending"
	ActressSyncTaskRunning   = "running"
	ActressSyncTaskCompleted = "completed"
	ActressSyncTaskSkipped   = "skipped"
	ActressSyncTaskConflict  = "conflict"
	ActressSyncTaskFailed    = "failed"
	ActressSyncTaskCancelled = "cancelled"

	ActressSyncTaskKindActress      = "actress"
	ActressSyncTaskKindUnknownMovie = "unknown_movie"
)

// ActressSyncJob is the durable aggregate for a background actress sync run.
type ActressSyncJob struct {
	ID              string     `json:"id" gorm:"primaryKey;size:36"`
	Status          string     `json:"status" gorm:"index"`
	Scope           string     `json:"scope"`
	TotalTasks      int        `json:"total_tasks"`
	Completed       int        `json:"completed"`
	Updated         int        `json:"updated"`
	Warnings        int        `json:"warnings"`
	Skipped         int        `json:"skipped"`
	Conflicts       int        `json:"conflicts"`
	Failed          int        `json:"failed"`
	Cancelled       int        `json:"cancelled"`
	CancelRequested bool       `json:"cancel_requested"`
	CreatedAt       time.Time  `json:"created_at" gorm:"index"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// TableName returns the durable actress-sync job table name.
func (ActressSyncJob) TableName() string { return "actress_sync_jobs" }

// ActressSyncTask is one independently retryable actress or Unknown/movie unit.
type ActressSyncTask struct {
	ID             string     `json:"id" gorm:"primaryKey;size:36"`
	JobID          string     `json:"job_id" gorm:"index;size:36"`
	Kind           string     `json:"kind"`
	ActressID      *uint      `json:"actress_id,omitempty" gorm:"index"`
	MovieContentID string     `json:"movie_content_id,omitempty" gorm:"index"`
	MovieID        string     `json:"movie_id,omitempty"`
	Label          string     `json:"label"`
	DedupeKey      string     `json:"dedupe_key" gorm:"index"`
	Status         string     `json:"status" gorm:"index"`
	Stage          string     `json:"stage"`
	Outcome        string     `json:"outcome,omitempty"`
	Messages       []string   `json:"messages" gorm:"serializer:json"`
	UpdatedFields  []string   `json:"updated_fields" gorm:"serializer:json"`
	Warning        string     `json:"warning,omitempty" gorm:"type:text"`
	ErrorMessage   string     `json:"error_message,omitempty" gorm:"type:text"`
	LeaseOwner     string     `json:"-" gorm:"index"`
	LeaseToken     string     `json:"-"`
	HeartbeatAt    *time.Time `json:"heartbeat_at,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty" gorm:"index"`
	Attempts       int        `json:"attempts"`
	CreatedAt      time.Time  `json:"created_at" gorm:"index"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

// TableName returns the durable actress-sync task table name.
func (ActressSyncTask) TableName() string { return "actress_sync_tasks" }
