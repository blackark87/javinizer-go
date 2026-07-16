package config

import (
	"fmt"
	"os"
	"time"
)

// Migration transforms a Config from one schema version to the next.
type Migration interface {
	FromVersions() []int
	ToVersion() int
	Description() string
	Migrate(cfg *Config) error
}

// MigrationContext carries environment settings shared across migration runs.
type MigrationContext struct {
	ConfigPath string
	DryRun     bool
	BackupPath string
}

var migrationContext MigrationContext

// SetMigrationContext stores the context used by subsequent migrations.
func SetMigrationContext(ctx MigrationContext) {
	migrationContext = ctx
}

// GetMigrationContext returns the currently active migration context.
func GetMigrationContext() MigrationContext {
	return migrationContext
}

var migrations = make(map[int]Migration)

// RegisterMigration registers a migration as the handler for each of its source versions.
func RegisterMigration(m Migration) {
	for _, v := range m.FromVersions() {
		migrations[v] = m
	}
}

// MigrateToCurrent applies registered migrations until cfg reaches CurrentConfigVersion.
func MigrateToCurrent(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if cfg.ConfigVersion > CurrentConfigVersion {
		return fmt.Errorf("no migration from version %d. This version is not supported", cfg.ConfigVersion)
	}

	for cfg.ConfigVersion < CurrentConfigVersion {
		m, ok := migrations[cfg.ConfigVersion]
		if !ok {
			return fmt.Errorf("no migration from version %d. This version is not supported", cfg.ConfigVersion)
		}
		if err := m.Migrate(cfg); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		cfg.ConfigVersion = m.ToVersion()
	}

	return nil
}

// LegacyMigration migrates configs from versions 0, 1, and 2 to version 3 by
// backing up the existing file and replacing its contents with the embedded default.
type LegacyMigration struct{}

// NewLegacyMigration returns a new LegacyMigration.
func NewLegacyMigration() *LegacyMigration {
	return &LegacyMigration{}
}

// FromVersions returns the config versions this migration upgrades from.
func (m *LegacyMigration) FromVersions() []int { return []int{0, 1, 2} }

// ToVersion returns the config version produced by this migration.
func (m *LegacyMigration) ToVersion() int { return 3 }

// Description returns a human-readable summary of the migration.
func (m *LegacyMigration) Description() string {
	return "Backup + recreate from embedded example (v0/v1/v2 → v3)"
}

// Migrate backs up the existing config file and resets cfg to the embedded defaults.
func (m *LegacyMigration) Migrate(cfg *Config) error {
	ctx := GetMigrationContext()

	if ctx.ConfigPath != "" && !ctx.DryRun {
		if _, err := os.Stat(ctx.ConfigPath); err == nil {
			backupPath := fmt.Sprintf("%s.bak-%s", ctx.ConfigPath, time.Now().Format("20060102-150405"))
			data, err := os.ReadFile(ctx.ConfigPath)
			if err != nil {
				return fmt.Errorf("failed to read config for backup: %w", err)
			}
			if err := os.WriteFile(backupPath, data, FilePerm); err != nil {
				return fmt.Errorf("failed to create backup: %w", err)
			}
			ctx.BackupPath = backupPath
			SetMigrationContext(ctx)
		}
	}

	newCfg := DefaultConfig(nil, nil)
	*cfg = *newCfg

	return nil
}
