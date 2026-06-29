-- +goose Up
-- +goose StatementBegin
ALTER TABLE movies ADD COLUMN original_cover_url TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE movies DROP COLUMN original_cover_url;
-- +goose StatementEnd
