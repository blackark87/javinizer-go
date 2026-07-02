//go:build e2e

package cli_e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfigWith is like writeConfig but lets a test override specific config
// defaults — used to set up the "config says X" half of a supersede test. The
// extra string is appended to the output block, so callers pass e.g.
// "download_extrafanart: false" to flip the config default. To avoid YAML
// duplicate-key errors, callers that override a field set in the base block
// must pass the FULL set of output download keys (the base block below omits
// download_extrafanart so it can be set cleanly).
func writeConfigWith(t *testing.T, dir string, outputExtras string) string {
	t.Helper()
	cfg := `config_version: 3

file_matching:
    extensions:
        - .mp4
        - .mkv
    regex_enabled: true
    regex_pattern: '([A-Z]{2,10}-\d{2,5}[A-Z]?)(?:-pt(\d{1,2}))?'

output:
    folder_format: "<ID>"
    subfolder_format: []
    file_format: "<ID>"
    rename_file: true
    download_cover: false
    download_poster: false
    download_trailer: false
    download_actress: false
    download_timeout: 5
` + outputExtras + `

metadata:
    nfo:
        feature:
            enabled: true
        format:
            filename_template: <ID>.nfo

database:
    type: sqlite
    dsn: ` + filepath.Join(dir, "javinizer.db") + `
    log_level: silent

logging:
    level: error
`
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cfg), 0o600))
	return p
}

// TestCLI_Flag_ExtrafanartSupersedesConfig is the e2e complement to the unit
// fix for issue #79. Config disables extrafanart (download_extrafanart: false);
// the --extrafanart flag must be accepted and routed through the full binary
// pipeline without error, proving the flag→config override at
// batch_command.go:176-179 survives end-to-end.
//
// We assert the flag is accepted + sort completes (exit 0), NOT that the
// extrafanart folder is created: the e2emock scraper does not populate
// movie.Screenshots, so downloadExtrafanart returns early regardless of the
// flag. The actual override→download behavior is pinned by the unit test
// TestDownload_ExtrafanartNilOverrideRespectsConfig at the downloader seam,
// which uses a movie WITH screenshots. Here we pin only the CLI plumbing:
// the flag parses, flows through BatchCommandOptions, and doesn't crash.
func TestCLI_Flag_ExtrafanartSupersedesConfig(t *testing.T) {
	dir := t.TempDir()
	// Config says false; the flag must win (be accepted + routed).
	cfgPath := writeConfigWith(t, dir, "    download_extrafanart: false\n")
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	out, code := run(t, cfgPath, "sort", "--extrafanart", src)
	require.Equal(t, 0, code, "--extrafanart over config:false must not error\n%s", out)
	assert.Contains(t, out, "Sort complete!")
	// NFO still generates (organize + NFO are independent of extrafanart).
	assert.FileExists(t, filepath.Join(src, "GOOD-001", "GOOD-001.nfo"),
		"NFO must still generate\n%s", out)
}

// TestCLI_Flag_NoExtrafanartRespectsConfigDisabled confirms the other
// direction: config:false and NO --extrafanart flag → no extrafanart folder.
// Guards against a regression where the #79 fix accidentally always-enabled.
func TestCLI_Flag_NoExtrafanartRespectsConfigDisabled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigWith(t, dir, "    download_extrafanart: false\n")
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	out, code := run(t, cfgPath, "sort", src)
	require.Equal(t, 0, code, "sort exited %d\n%s", code, out)

	folder := filepath.Join(src, "GOOD-001")
	require.FileExists(t, filepath.Join(folder, "GOOD-001.nfo"))
	extraDir := filepath.Join(folder, "extrafanart")
	_, err := os.Stat(extraDir)
	assert.True(t, os.IsNotExist(err),
		"extrafanart folder must NOT be created when config is false and flag is absent\n%s", out)
}

// TestCLI_Flag_NfoFalseDisablesGeneration proves --nfo=false supersedes a
// config that enables NFO (metadata.nfo.feature.enabled: true). The sort must
// complete without writing an .nfo file.
//
// Note the one-directional semantics at apply_phase.go:182
// (applyCmd.GenerateNFO = cfg.GenerateNFO && inputs.NFOEnabled): the flag
// flows through as cfg.GenerateNFO, so --nfo=false can DISABLE over a
// config-enabled NFO, but --nfo=true cannot force-enable over a
// config-disabled NFO. This test pins the disable direction.
func TestCLI_Flag_NfoFalseDisablesGeneration(t *testing.T) {
	dir := t.TempDir()
	// Config has nfo.feature.enabled: true (the writeConfig default).
	cfgPath := writeConfigWith(t, dir, "")
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	out, code := run(t, cfgPath, "sort", "--nfo=false", src)
	require.Equal(t, 0, code, "sort exited %d\n%s", code, out)
	assert.Contains(t, out, "Sort complete!")

	// Video still organized; NFO must NOT exist.
	assert.FileExists(t, filepath.Join(src, "GOOD-001", "GOOD-001.mp4"),
		"organize is independent of NFO generation\n%s", out)
	_, err := os.Stat(filepath.Join(src, "GOOD-001", "GOOD-001.nfo"))
	assert.True(t, os.IsNotExist(err),
		"--nfo=false must suppress NFO generation over config:true\n%s", out)
}

// TestCLI_Flag_DryRunSupersedesConfig is the negative control for the
// supersede contract: --dry-run is a flow-only flag (not a config override),
// but it must still take precedence over the default live mode. No file may
// move and no NFO may be written.
func TestCLI_Flag_DryRunSupersedesConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigWith(t, dir, "")
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	orig := filepath.Join(src, "GOOD-001.mp4")
	writeFile(t, orig)

	out, code := run(t, cfgPath, "sort", "--dry-run", src)
	require.Equal(t, 0, code, "dry-run exited %d\n%s", code, out)

	assert.FileExists(t, orig, "dry-run must not move the source\n%s", out)
	_, err := os.Stat(filepath.Join(src, "GOOD-001"))
	assert.True(t, os.IsNotExist(err), "dry-run must not create the org folder\n%s", out)
}

// TestCLI_Flag_MoveMovesSource proves --move takes effect: the source file is
// removed from its original location after organizing (vs --copy default which
// leaves it). This is the behavioral pin for the --move flow-only flag.
func TestCLI_Flag_MoveMovesSource(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigWith(t, dir, "")
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	orig := filepath.Join(src, "GOOD-001.mp4")
	writeFile(t, orig)

	out, code := run(t, cfgPath, "sort", "--move", src)
	require.Equal(t, 0, code, "sort --move exited %d\n%s", code, out)

	// Moved: original gone, organized copy present.
	assert.False(t, fileExists(orig), "--move must remove the source file\n%s", out)
	assert.FileExists(t, filepath.Join(src, "GOOD-001", "GOOD-001.mp4"),
		"--move must place the file in the org folder\n%s", out)
}

// TestCLI_Flag_DestRedirectsOutput proves --dest supersedes the default
// (destination = source): organized output lands in the --dest directory,
// not alongside the source.
func TestCLI_Flag_DestRedirectsOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigWith(t, dir, "")
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "dest")
	require.NoError(t, os.MkdirAll(src, 0o700))
	require.NoError(t, os.MkdirAll(dest, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	out, code := run(t, cfgPath, "sort", "--dest", dest, src)
	require.Equal(t, 0, code, "sort --dest exited %d\n%s", code, out)

	// Organized into --dest, NOT src.
	assert.FileExists(t, filepath.Join(dest, "GOOD-001", "GOOD-001.mp4"),
		"--dest must place output in the destination dir\n%s", out)
	_, err := os.Stat(filepath.Join(src, "GOOD-001"))
	assert.True(t, os.IsNotExist(err),
		"--dest must NOT place output in the source dir\n%s", out)
}

// TestCLI_Flag_ScrapersAccepted proves --scrapers is accepted and routed:
// `--scrapers e2emock` succeeds (the mock is the only registered scraper in
// e2e mode). Pins the scraper-priority-override flag's plumbing end-to-end.
//
// We do NOT assert the negative (`--scrapers nosuchscraper` fails): in e2e
// mode e2emock.ApplyToConfig forces cfg.Scrapers.Priority=["e2emock"], and an
// unknown --scrapers name flows through CalculateOptimalScrapers whose
// fallback behavior is non-deterministic for our purposes. The unit suites
// cover the scraper-filter semantics; here we only pin that the flag parses
// and routes to a successful scrape.
func TestCLI_Flag_ScrapersAccepted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfigWith(t, dir, "")
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	out, code := run(t, cfgPath, "sort", "--scrapers", "e2emock", src)
	assert.Equal(t, 0, code, "--scrapers e2emock should succeed\n%s", out)
	assert.Contains(t, out, "Scraped GOOD-001 successfully")
	assert.FileExists(t, filepath.Join(src, "GOOD-001", "GOOD-001.nfo"),
		"--scrapers e2emock must organize + generate NFO\n%s", out)
}

// fileExists is a small helper; os.Stat + IsNotExist is noisy inline.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
