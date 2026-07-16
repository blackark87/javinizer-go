package database

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMigrationsOnStartup_UpgradesLegacyFeatureV12(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "javinizer.db")
	db, err := New(&Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	legacyProvider, err := goose.NewProvider(
		goose.DialectSQLite3,
		sqlDB,
		legacyFeatureV12Filesystem(t),
		goose.WithTableName(schemaMigrationsTable),
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)
	_, err = legacyProvider.Up(context.Background())
	require.NoError(t, err)

	legacyVersion, err := legacyProvider.GetDBVersion(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(12), legacyVersion)

	require.NoError(t, db.Exec(`INSERT INTO actresses
        (id, dmm_id, japanese_name, created_at, updated_at)
        VALUES (1, 1001, '波多野結衣', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO actress_translations
        (id, actress_id, language, name, source_name, settings_hash, created_at, updated_at)
        VALUES (7, 1, 'ko', '하타노 유이', '波多野結衣', 'abc123', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO actress_sync_jobs
        (id, status, scope, total_tasks, created_at)
        VALUES ('legacy-job', 'running', 'all', 1, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO actress_sync_tasks
        (id, job_id, kind, actress_id, dedupe_key, status, created_at)
        VALUES ('legacy-task', 'legacy-job', 'actress', 1, 'actress:1', 'pending', CURRENT_TIMESTAMP)`).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	currentProvider := newMigrationProvider(t, sqlDB)
	currentVersion, err := currentProvider.GetDBVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(14), currentVersion)

	assertSchemaColumn(t, db, "jobs", "operation_mode_override", true)
	assertSchemaColumn(t, db, "movies", "original_cover_url", true)
	assertSchemaColumn(t, db, "movie_translations", "actresses", true)
	assertSchemaColumn(t, db, "actress_translations", "name", false)
	assertSchemaColumn(t, db, "actress_translations", "display_name", true)
	assertSchemaColumn(t, db, "actress_translations", "settings_hash", true)
	assertSchemaTable(t, db, "genre_translations", true)
	assertSchemaTable(t, db, "actress_sync_jobs", true)
	assertSchemaTable(t, db, "actress_sync_tasks", true)

	var translation struct {
		ID           uint
		DisplayName  string
		SourceName   string
		SettingsHash string
	}
	require.NoError(t, db.Raw(`SELECT id, display_name, source_name, settings_hash
        FROM actress_translations WHERE actress_id = 1 AND language = 'ko'`).Scan(&translation).Error)
	assert.Equal(t, uint(7), translation.ID)
	assert.Equal(t, "하타노 유이", translation.DisplayName)
	assert.Equal(t, "波多野結衣", translation.SourceName)
	assert.Equal(t, "abc123", translation.SettingsHash)

	var jobCount, taskCount int64
	require.NoError(t, db.Table("actress_sync_jobs").Where("id = ?", "legacy-job").Count(&jobCount).Error)
	require.NoError(t, db.Table("actress_sync_tasks").Where("id = ?", "legacy-task").Count(&taskCount).Error)
	assert.Equal(t, int64(1), jobCount)
	assert.Equal(t, int64(1), taskCount)

	initialBackups, err := filepath.Glob(dbPath + ".*.backup")
	require.NoError(t, err)
	require.Len(t, initialBackups, 1)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	finalBackups, err := filepath.Glob(dbPath + ".*.backup")
	require.NoError(t, err)
	assert.Equal(t, initialBackups, finalBackups)
}

func TestRunMigrationsOnStartup_UpgradesUpstreamV11(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.UpTo(context.Background(), 11)
	require.NoError(t, err)
	assertSchemaColumn(t, db, "movie_translations", "actresses", false)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	version, err := provider.GetDBVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(14), version)
	assertSchemaColumn(t, db, "movie_translations", "actresses", true)
	assertSchemaColumn(t, db, "actress_translations", "settings_hash", true)
	assertSchemaTable(t, db, "actress_sync_jobs", true)
}

func legacyFeatureV12Filesystem(t *testing.T) fs.FS {
	t.Helper()
	result := make(fstest.MapFS)
	commonMigrations := []string{
		"000001_baseline.sql",
		"000002_add_job_temp_dir.sql",
		"000003_rename_display_name_to_display_title.sql",
		"000004_history_and_events.sql",
		"000005_original_poster_fields.sql",
		"000006_word_replacements.sql",
		"000007_api_tokens.sql",
		"000008_jobs_update_column.sql",
	}
	for _, name := range commonMigrations {
		content, err := fs.ReadFile(dbmigrations.Filesystem(), name)
		require.NoError(t, err)
		result[name] = &fstest.MapFile{Data: content}
	}

	entries, err := os.ReadDir("testdata/legacy_feature_v12")
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join("testdata/legacy_feature_v12", entry.Name()))
		require.NoError(t, err)
		result[entry.Name()] = &fstest.MapFile{Data: content}
	}
	return result
}

func assertSchemaColumn(t *testing.T, db *DB, table, column string, want bool) {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count).Error)
	assert.Equal(t, want, count == 1, "%s.%s presence", table, column)
}

func assertSchemaTable(t *testing.T, db *DB, table string, want bool) {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count).Error)
	assert.Equal(t, want, count == 1, "%s presence", table)
}
