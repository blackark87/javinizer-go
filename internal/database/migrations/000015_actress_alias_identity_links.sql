-- +goose Up
-- +goose StatementBegin
ALTER TABLE actress_aliases ADD COLUMN alias_actress_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE actress_aliases ADD COLUMN canonical_actress_id INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_actress_aliases_alias_actress_id ON actress_aliases(alias_actress_id);
CREATE INDEX IF NOT EXISTS idx_actress_aliases_canonical_actress_id ON actress_aliases(canonical_actress_id);

UPDATE actress_aliases
SET alias_actress_id = COALESCE((
    SELECT actresses.id FROM actresses
    WHERE TRIM(actresses.japanese_name) = TRIM(actress_aliases.alias_name)
    ORDER BY CASE WHEN actresses.dmm_id > 0 THEN 0 ELSE 1 END, actresses.id
    LIMIT 1
), 0),
canonical_actress_id = COALESCE((
    SELECT actresses.id FROM actresses
    WHERE TRIM(actresses.japanese_name) = TRIM(actress_aliases.canonical_name)
    ORDER BY CASE WHEN actresses.dmm_id > 0 THEN 0 ELSE 1 END, actresses.id
    LIMIT 1
), 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_actress_aliases_alias_actress_id;
DROP INDEX IF EXISTS idx_actress_aliases_canonical_actress_id;
ALTER TABLE actress_aliases DROP COLUMN alias_actress_id;
ALTER TABLE actress_aliases DROP COLUMN canonical_actress_id;
-- +goose StatementEnd
