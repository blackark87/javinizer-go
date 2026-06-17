-- +goose Up
-- +goose StatementBegin
ALTER TABLE movie_translations ADD COLUMN actresses TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE movie_translations DROP COLUMN actresses;
-- +goose StatementEnd
