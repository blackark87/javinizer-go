-- +goose Up
-- +goose StatementBegin
ALTER TABLE jobs ADD COLUMN cancelled INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE jobs DROP COLUMN cancelled;
-- +goose StatementEnd
