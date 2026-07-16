-- +goose Up
-- +goose StatementBegin
-- actress_translations is created by migration 000009.  The sync workflow
-- adds only the settings fingerprint used to invalidate stale translations.
ALTER TABLE actress_translations ADD COLUMN settings_hash VARCHAR(16);

CREATE TABLE IF NOT EXISTS actress_sync_jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    scope TEXT NOT NULL,
    total_tasks INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    updated INTEGER NOT NULL DEFAULT 0,
    warnings INTEGER NOT NULL DEFAULT 0,
    skipped INTEGER NOT NULL DEFAULT 0,
    conflicts INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    cancelled INTEGER NOT NULL DEFAULT 0,
    cancel_requested NUMERIC NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_actress_sync_jobs_status ON actress_sync_jobs(status);
CREATE INDEX IF NOT EXISTS idx_actress_sync_jobs_created_at ON actress_sync_jobs(created_at);

CREATE TABLE IF NOT EXISTS actress_sync_tasks (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    actress_id INTEGER,
    movie_content_id TEXT,
    movie_id TEXT,
    label TEXT,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL,
    stage TEXT,
    outcome TEXT,
    messages TEXT,
    updated_fields TEXT,
    warning TEXT,
    error_message TEXT,
    lease_owner TEXT,
    lease_token TEXT,
    heartbeat_at DATETIME,
    lease_expires_at DATETIME,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    completed_at DATETIME,
    CONSTRAINT fk_actress_sync_tasks_job FOREIGN KEY (job_id) REFERENCES actress_sync_jobs(id)
);

CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_job_id ON actress_sync_tasks(job_id);
CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_status ON actress_sync_tasks(status);
CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_actress_id ON actress_sync_tasks(actress_id);
CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_movie_content_id ON actress_sync_tasks(movie_content_id);
CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_lease_expires_at ON actress_sync_tasks(lease_expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_sync_tasks_active_key
    ON actress_sync_tasks(dedupe_key) WHERE status IN ('pending', 'running');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actress_sync_tasks_active_key;
DROP TABLE IF EXISTS actress_sync_tasks;
DROP TABLE IF EXISTS actress_sync_jobs;
ALTER TABLE actress_translations DROP COLUMN settings_hash;
-- +goose StatementEnd
