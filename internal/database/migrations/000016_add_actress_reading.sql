-- +goose Up
ALTER TABLE actresses ADD COLUMN reading TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE actresses DROP COLUMN reading;
