-- +goose Up
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_history_status ON history(status);
CREATE INDEX IF NOT EXISTS idx_history_operation ON history(operation);
CREATE INDEX IF NOT EXISTS idx_history_created_at_status ON history(created_at, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_history_created_at_status;
DROP INDEX IF EXISTS idx_history_operation;
DROP INDEX IF EXISTS idx_history_status;
-- +goose StatementEnd
