-- +goose Up
CREATE INDEX IF NOT EXISTS idx_bfo_revert_movie_created
    ON batch_file_operations(revert_status, movie_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_bfo_revert_movie_created;
