-- +goose Up
-- +goose StatementBegin
ALTER TABLE movie_translations ADD COLUMN actresses TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE movie_translations_backup (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id TEXT,
    language TEXT,
    title TEXT,
    original_title TEXT,
    description TEXT,
    director TEXT,
    maker TEXT,
    label TEXT,
    series TEXT,
    source_name TEXT,
    settings_hash VARCHAR(16),
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_movies_translations FOREIGN KEY (movie_id) REFERENCES movies(content_id)
);

INSERT INTO movie_translations_backup (id, movie_id, language, title, original_title, description, director, maker, label, series, source_name, settings_hash, created_at, updated_at)
SELECT id, movie_id, language, title, original_title, description, director, maker, label, series, source_name, settings_hash, created_at, updated_at FROM movie_translations;

DROP TABLE movie_translations;
ALTER TABLE movie_translations_backup RENAME TO movie_translations;
CREATE UNIQUE INDEX IF NOT EXISTS idx_movie_language ON movie_translations(movie_id, language);
-- +goose StatementEnd
