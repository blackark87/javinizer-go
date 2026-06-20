package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputConfig_ActressLanguageJA_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	// config_version: 3 avoids the legacy "wipe + regenerate" migration path,
	// which is intentional (the migration intentionally overwrites fields with
	// defaults for configs that predate v3). We only want to verify parsing.
	path := filepath.Join(dir, "config.yaml")
	yaml := "config_version: 3\noutput:\n  actress_language_ja: true\n  first_name_order: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Output.ActressLanguageJA {
		t.Errorf("ActressLanguageJA = false, want true")
	}
	if !cfg.Output.FirstNameOrder {
		t.Errorf("FirstNameOrder = false, want true (control case)")
	}

	// Default value when unspecified
	path2 := filepath.Join(dir, "config2.yaml")
	_ = os.WriteFile(path2, []byte("config_version: 3\noutput:\n  enabled: false\n"), 0o644)
	cfg2, err := Load(path2)
	if err != nil {
		t.Fatalf("Load path2: %v", err)
	}
	if cfg2.Output.ActressLanguageJA {
		t.Errorf("ActressLanguageJA default = true, want false")
	}
}
