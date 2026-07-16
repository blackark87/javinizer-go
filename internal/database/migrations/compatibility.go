package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const createActressTranslationsTable = `
CREATE TABLE actress_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actress_id INTEGER NOT NULL,
    language TEXT NOT NULL,
    first_name TEXT,
    last_name TEXT,
    japanese_name TEXT,
    display_name TEXT,
    source_name TEXT,
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_actress_translations_actress
        FOREIGN KEY (actress_id) REFERENCES actresses(id) ON DELETE CASCADE
)`

const createActressTranslationsTableWithHash = `
CREATE TABLE actress_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actress_id INTEGER NOT NULL,
    language TEXT NOT NULL,
    first_name TEXT,
    last_name TEXT,
    japanese_name TEXT,
    display_name TEXT,
    source_name TEXT,
    settings_hash VARCHAR(16),
    created_at DATETIME,
    updated_at DATETIME,
    CONSTRAINT fk_actress_translations_actress
        FOREIGN KEY (actress_id) REFERENCES actresses(id) ON DELETE CASCADE
)`

func upMigration013(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS genre_translations (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            genre_id INTEGER NOT NULL,
            language TEXT NOT NULL,
            name TEXT NOT NULL,
            source_name TEXT,
            created_at DATETIME,
            updated_at DATETIME,
            CONSTRAINT fk_genre_translations_genre
                FOREIGN KEY (genre_id) REFERENCES genres(id) ON DELETE CASCADE
        )`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_genre_translations_genre_language
            ON genre_translations(genre_id, language)`,
	}
	if err := execStatements(ctx, tx, statements...); err != nil {
		return fmt.Errorf("ensure translation tables: %w", err)
	}

	if err := normalizeActressTranslations(ctx, tx); err != nil {
		return fmt.Errorf("normalize actress translations: %w", err)
	}

	columns := []struct {
		table      string
		column     string
		definition string
	}{
		{"jobs", "operation_mode_override", "TEXT NOT NULL DEFAULT ''"},
		{"movies", "original_cover_url", "TEXT"},
		{"movie_translations", "actresses", "TEXT"},
	}
	for _, column := range columns {
		if err := addColumnIfMissing(ctx, tx, column.table, column.column, column.definition); err != nil {
			return err
		}
	}

	if err := execStatements(ctx, tx,
		`CREATE INDEX IF NOT EXISTS idx_history_status ON history(status)`,
		`CREATE INDEX IF NOT EXISTS idx_history_operation ON history(operation)`,
		`CREATE INDEX IF NOT EXISTS idx_history_created_at_status ON history(created_at, status)`,
	); err != nil {
		return fmt.Errorf("ensure history indexes: %w", err)
	}

	return nil
}

func downMigration013(ctx context.Context, tx *sql.Tx) error {
	return dropColumnIfPresent(ctx, tx, "movie_translations", "actresses")
}

func upMigration014(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfMissing(ctx, tx, "actress_translations", "settings_hash", "VARCHAR(16)"); err != nil {
		return err
	}

	return execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS actress_sync_jobs (
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
        )`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_jobs_status ON actress_sync_jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_jobs_created_at ON actress_sync_jobs(created_at)`,
		`CREATE TABLE IF NOT EXISTS actress_sync_tasks (
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
            CONSTRAINT fk_actress_sync_tasks_job
                FOREIGN KEY (job_id) REFERENCES actress_sync_jobs(id)
        )`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_job_id ON actress_sync_tasks(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_status ON actress_sync_tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_actress_id ON actress_sync_tasks(actress_id)`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_movie_content_id ON actress_sync_tasks(movie_content_id)`,
		`CREATE INDEX IF NOT EXISTS idx_actress_sync_tasks_lease_expires_at ON actress_sync_tasks(lease_expires_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_sync_tasks_active_key
            ON actress_sync_tasks(dedupe_key) WHERE status IN ('pending', 'running')`,
	)
}

func downMigration014(ctx context.Context, tx *sql.Tx) error {
	if err := execStatements(ctx, tx,
		`DROP INDEX IF EXISTS idx_actress_sync_tasks_active_key`,
		`DROP TABLE IF EXISTS actress_sync_tasks`,
		`DROP TABLE IF EXISTS actress_sync_jobs`,
	); err != nil {
		return err
	}
	return dropColumnIfPresent(ctx, tx, "actress_translations", "settings_hash")
}

func normalizeActressTranslations(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, "actress_translations")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, createActressTranslationsTable); err != nil {
			return err
		}
		return ensureActressTranslationIndex(ctx, tx)
	}

	columns, err := tableColumns(ctx, tx, "actress_translations")
	if err != nil {
		return err
	}
	if columns["name"] {
		if err := rebuildLegacyActressTranslations(ctx, tx, columns); err != nil {
			return err
		}
		return ensureActressTranslationIndex(ctx, tx)
	}

	for _, column := range []string{"first_name", "last_name", "japanese_name", "display_name", "source_name"} {
		if !columns[column] {
			if err := addColumnIfMissing(ctx, tx, "actress_translations", column, "TEXT"); err != nil {
				return err
			}
		}
	}
	return ensureActressTranslationIndex(ctx, tx)
}

func rebuildLegacyActressTranslations(ctx context.Context, tx *sql.Tx, columns map[string]bool) error {
	if err := execStatements(ctx, tx,
		`DROP TABLE IF EXISTS actress_translations_legacy_013`,
		`ALTER TABLE actress_translations RENAME TO actress_translations_legacy_013`,
		`DROP INDEX IF EXISTS idx_actress_translations_actress_language`,
		`DROP INDEX IF EXISTS idx_actress_translation_actress_language`,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, createActressTranslationsTableWithHash); err != nil {
		return err
	}

	displayName := "name"
	if columns["display_name"] {
		displayName = "COALESCE(NULLIF(TRIM(display_name), ''), name)"
	}
	selectExpression := func(column string) string {
		if columns[column] {
			return column
		}
		return "NULL"
	}

	// Every interpolated expression is selected from the fixed column names
	// above; no database value or external input becomes SQL syntax.
	// #nosec G201 -- schema-driven selection uses only hard-coded expressions.
	copyQuery := fmt.Sprintf(`
        INSERT INTO actress_translations (
            id, actress_id, language, first_name, last_name, japanese_name,
            display_name, source_name, settings_hash, created_at, updated_at
        )
        SELECT id, actress_id, language, %s, %s, %s, %s, %s, %s, %s, %s
        FROM actress_translations_legacy_013`,
		selectExpression("first_name"),
		selectExpression("last_name"),
		selectExpression("japanese_name"),
		displayName,
		selectExpression("source_name"),
		selectExpression("settings_hash"),
		selectExpression("created_at"),
		selectExpression("updated_at"),
	)
	if _, err := tx.ExecContext(ctx, copyQuery); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DROP TABLE actress_translations_legacy_013`)
	return err
}

func ensureActressTranslationIndex(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
        CREATE UNIQUE INDEX IF NOT EXISTS idx_actress_translations_actress_language
        ON actress_translations(actress_id, language)`)
	return err
}

func tableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&count)
	return count > 0, err
}

func tableColumns(ctx context.Context, tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}

func addColumnIfMissing(ctx context.Context, tx *sql.Tx, table, column, definition string) error {
	columns, err := tableColumns(ctx, tx, table)
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	if columns[strings.ToLower(column)] {
		return nil
	}
	query := fmt.Sprintf(`ALTER TABLE %q ADD COLUMN %q %s`, table, column, definition)
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func dropColumnIfPresent(ctx context.Context, tx *sql.Tx, table, column string) error {
	columns, err := tableColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	if !columns[strings.ToLower(column)] {
		return nil
	}
	query := fmt.Sprintf(`ALTER TABLE %q DROP COLUMN %q`, table, column)
	_, err = tx.ExecContext(ctx, query)
	return err
}

func execStatements(ctx context.Context, tx *sql.Tx, statements ...string) error {
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
