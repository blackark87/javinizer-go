package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/cobra"
)

// NewCommand creates the config CLI command and its subcommands.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management commands",
	}

	cmd.AddCommand(newMigrateCommand())
	return cmd
}

func newMigrateCommand() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate configuration to current version",
		Long: `Migrate configuration file to the current version.

For legacy configs (v0, v1, v2), this will:
  - Create a backup of the existing config
  - Generate a fresh config from defaults
  - NOT preserve user settings

For current configs, this is a no-op.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath := filepath.Join("configs", "config.yaml")
			if envPath := os.Getenv("JAVINIZER_CONFIG"); envPath != "" {
				configPath = envPath
			}

			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				return fmt.Errorf("config file not found: %s", configPath)
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if cfg.ConfigVersion >= config.CurrentConfigVersion {
				logging.Infof("Config is already at current version (%d)", cfg.ConfigVersion)
				return nil
			}

			config.SetMigrationContext(config.MigrationContext{
				ConfigPath: configPath,
				DryRun:     dryRun,
			})

			if dryRun {
				fmt.Printf("\n📋 DRY RUN - No changes will be made\n")
				fmt.Printf("Current config version: %d\n", cfg.ConfigVersion)
				fmt.Printf("Target version: %d\n\n", config.CurrentConfigVersion)
			}

			if cfg.ConfigVersion <= 2 {
				fmt.Printf("\n⚠️  WARNING: Your config (version %d) is outdated.\n", cfg.ConfigVersion)
				fmt.Printf("   A backup will be created at: %s.bak-<timestamp>\n", configPath)
				fmt.Printf("   A fresh configuration will be generated from defaults.\n")
				fmt.Printf("   Your previous settings will NOT be preserved.\n\n")

				if dryRun {
					fmt.Printf("   Run without --dry-run to proceed.\n")
				} else {
					fmt.Printf("   Proceed? [y/N]: ")
					var response string
					if _, err := fmt.Scanln(&response); err != nil {
						fmt.Println("\nMigration cancelled (invalid input).")
						return nil
					}
					if response != "y" && response != "Y" {
						fmt.Println("Migration cancelled.")
						return nil
					}
				}
			}

			if dryRun {
				fmt.Printf("Would migrate config from version %d to %d\n", cfg.ConfigVersion, config.CurrentConfigVersion)
				return nil
			}

			if err := config.MigrateToCurrent(cfg); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}

			ctx := config.GetMigrationContext()
			if ctx.BackupPath != "" {
				fmt.Printf("✓ Backup created: %s\n", ctx.BackupPath)
			}

			if err := config.Save(cfg, configPath); err != nil {
				return fmt.Errorf("failed to save migrated config: %w", err)
			}

			fmt.Printf("✓ Config migrated to version %d\n", cfg.ConfigVersion)
			fmt.Printf("✓ Migration complete. Please review your settings.\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change without modifying")

	return cmd
}
