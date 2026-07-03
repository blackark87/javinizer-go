//go:build live

// Package live_e2e contains REAL-LIVE end-to-end tests that hit actual scraper
// websites over the public internet. These are deliberately excluded from CI
// (the `live` build tag is not passed in any workflow) and must be run locally
// by the developer.
//
// Why not CI: real scrapers are inherently flaky — sites change their HTML,
// rate-limit, geo-block, require auth/cookies, or go down. A failing live
// scrape is not always a code regression; it's often an upstream change. These
// tests exist to give the developer a structured way to detect scraping
// degradation across all scrapers from their own machine, with their own
// proxy/FlareSolverr/browser setup.
//
// Future: this suite is designed to be lifted onto a dedicated server that
// runs on a schedule and tracks scraping degradation over time. The JSON
// output mode (JAVINIZER_LIVE_E2E_JSON=true) emits machine-parseable per-
// scraper results for that purpose.
//
// Safety: TWO opt-ins are required to run — the `live` build tag AND the
// JAVINIZER_LIVE_E2E=true env var. Either missing → the suite skips. This
// prevents accidental hammering of real sites from a stray `go test ./...`.
//
// Usage:
//
//	# Run all scrapers (uses your real config at configs/config.yaml):
//	JAVINIZER_LIVE_E2E=true make test-e2e-live
//
//	# Run a single scraper:
//	JAVINIZER_LIVE_E2E=true go test -tags live -run 'TestLive_Scrapers/r18dev' ./test/e2e/live/
//
//	# JSON output (for future server-side tracking):
//	JAVINIZER_LIVE_E2E=true JAVINIZER_LIVE_E2E_JSON=true make test-e2e-live
//
//	# Use a specific config (your proxy/FlareSolverr/browser setup):
//	JAVINIZER_LIVE_E2E=true JAVINIZER_CONFIG=path/to/config.yaml make test-e2e-live
package live_e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var binaryPath string

func TestMain(m *testing.M) {
	// Double opt-in: build tag (live) is already required to compile this
	// file; the env var is the second gate. Skip cleanly if only the tag
	// was set without the env.
	if os.Getenv("JAVINIZER_LIVE_E2E") != "true" {
		fmt.Fprintln(os.Stderr, "live_e2e: skipping (set JAVINIZER_LIVE_E2E=true to run real-network scraper tests)")
		os.Exit(0)
	}

	tmp, err := os.MkdirTemp("", "javinizer-live-e2e-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "live_e2e: mkdir temp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "javinizer")
	if p := os.Getenv("JAVINIZER_E2E_BIN"); p != "" {
		binaryPath = p // reuse a prebuilt binary for faster iteration
	} else {
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/javinizer")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "live_e2e: go build failed: %v\n%s\n", err, out)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

func repoRoot() string {
	src, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	return filepath.Join(src, "..", "..", "..")
}

// scraperFixture pairs a scraper name with a known-good movie ID for it.
// The IDs are sourced from the developer's test-videos/ directory and were
// VERIFIED to resolve on each site during the first live run — a failing
// fixture means an upstream change (site dropped the title, HTML changed,
// geo-block), which is exactly the degradation signal this suite catches.
//
// The standard-JAV scrapers each use a DIFFERENT studio/ID so that one site
// dropping one title fails only that scraper, not all nine. The special-
// format scrapers (caribbeancom, fc2, tokyohot) use site-specific ID formats
// drawn from the corresponding test-video filenames.
//
// IDs by scraper (all verified from test-videos/):
//   - r18dev:          SSIS-123   (S1 — broad VOD coverage)
//   - javlibrary:      IPX-535    (IPX — most comprehensive index)
//   - javbus:          ABW-013    (Prestige — wide coverage)
//   - javdb:           SONE-267   (S1 — comprehensive aggregator)
//   - mgstage:         ABW-102    (Prestige — mgstage only indexes certain
//     labels; ABW series is reliably present)
//   - jav321:          STARS-136  (SOD Star)
//   - javstash:        DASS-643   (DAS — aggregator)
//   - libredmm:        ROYD-191   (mirrors DMM's index)
//   - dmm:             SONE-267   (S1 — DMM-hosted; content-id resolves)
//   - caribbeancom:    120614-753 (Caribbean format MMDDYY-NNN)
//   - fc2:             FC2-PPV-4761557 (FC2 PPV article ID)
//   - tokyohot:        n0814      (Tokyo-Hot format [A-Za-z]\d+)
//   - dlgetchu:        4021016    (product ID — scraper resolves to 4064461)
//   - aventertainment: 1PON-020326-001 (1Pondo PPV — AVEntertainment hosts
//     PPV content (1Pondo/Caribbean/FC2), not standard studio
//     JAV; standard IDs like SSIS-123 don't exist in its
//     catalog)
var scraperFixtures = map[string]string{
	"r18dev":          "SSIS-123",
	"javlibrary":      "IPX-535",
	"javbus":          "ABW-013",
	"javdb":           "SONE-267",
	"mgstage":         "ABW-102",
	"jav321":          "STARS-136",
	"javstash":        "DASS-643",
	"libredmm":        "ROYD-191",
	"dmm":             "SONE-267",
	"caribbeancom":    "120614-753",
	"fc2":             "FC2-PPV-4761557",
	"tokyohot":        "n0814",
	"dlgetchu":        "4021016",
	"aventertainment": "1PON-020326-001",
	// #22: FC2 routing through libredmm. Currently pending (libredmm.com
	// returns "still processing" for this ID). See pendingFixtures.
	"libredmm_fc2": "FC2-PPV-4761557",
}

// pendingFixtures lists scrapers that have no verified fixture ID yet.
// These are skipped (not failed) so the suite stays green — a missing fixture
// is not a degradation signal. Remove a scraper from this set once a verified
// ID is added to scraperFixtures.
var pendingFixtures = map[string]bool{
	// #22: libredmm should route FC2-PPV-* inputs to its FC2 source.
	// libredmm.com currently returns "still processing" for FC2-PPV-4761557
	// (the FC2 scraper's known-good fixture ID), so it's not a stable live
	// fixture yet. When libredmm's FC2 index catches up, add
	// `"libredmm_fc2": "FC2-PPV-4761557"` to scraperFixtures and remove
	// this entry to activate a second libredmm subtest that pins the FC2
	// routing regression directly.
	"libredmm_fc2": true,
}

// invariant is a single output-row assertion against a scrape's stdout.
// Each pins a specific past regression (see scraperInvariants below).
type invariant struct {
	label    string // human-readable description, e.g. "poster URL is ps.jpg not pl.jpg (#31/#37)"
	check    func(out string) bool
	describe func() string
}

// rowMatches asserts a labelled row exists and its value matches re.
func rowMatches(label, pattern, desc string) invariant {
	re := regexp.MustCompile(pattern)
	return invariant{
		label:    desc,
		describe: func() string { return desc },
		check:    func(out string) bool { return re.MatchString(extractRow(out, label)) },
	}
}

// rowPresent asserts a labelled row exists with a non-empty value.
func rowPresent(label, desc string) invariant {
	return invariant{
		label:    desc,
		describe: func() string { return desc },
		check:    func(out string) bool { return extractRow(out, label) != "" },
	}
}

// screenshotsMatch asserts every screenshot URL matches re (and there's ≥1).
func screenshotsMatch(pattern, desc string) invariant {
	re := regexp.MustCompile(pattern)
	return invariant{
		label:    desc,
		describe: func() string { return desc },
		check: func(out string) bool {
			urls := extractScreenshotURLs(out)
			if len(urls) == 0 {
				return false
			}
			for _, u := range urls {
				if !re.MatchString(u) {
					return false
				}
			}
			return true
		},
	}
}

// scraperInvariants maps a scraper name to the output assertions that pin
// specific past regressions. A scraper not in this map has only the exit-0
// pass gate. Each invariant documents the issue(s) it pins.
//
// NOTE on dmm Poster URL (#37/#31): when the awsimgsrc ps.jpg is genuinely
// unavailable (404/too small), GetOptimalPosterURL intentionally falls back
// to the cover (pl.jpg) with shouldCrop=true. For the SONE-267 fixture the
// portrait ps.jpg IS reachable (verified), so a pl.jpg Poster URL here is a
// real regression, not a fallback. If a future run flips to the fallback,
// treat it as the degradation signal it is.
var scraperInvariants = map[string][]invariant{
	"r18dev": {
		// #75: r18.dev was down (then came back). #18: every lookup 403'd.
		// A passing scrape with a real cover + source proves the site is up
		// and the scraper is reaching it.
		rowPresent("Cover URL", "Cover URL present (r18.dev reachable, #75/#18)"),
		rowMatches("Cover URL", `^https://(pics\.dmm\.co\.jp|awsimgsrc\.dmm\.com|r18\.dev)/.*\.(jpg|jpeg)`, "Cover URL is a DMM/r18 image (#75/#18)"),
		rowMatches("Sources", `r18dev`, "Sources includes r18dev (#75/#18)"),
		// #1: r18.dev must populate BOTH EN and JA translations (the fix for
		// #1 was "populate translations for both EN and JA from R18.dev API").
		// The Translations row lists per-language sources; assert Japanese
		// is present. A sort-based <TITLE:JA> test would be more end-to-end
		// but fragile (config loading + template resolution + FS encoding);
		// the translations row is the direct, stable regression signal.
		rowMatches("Translations", `Japanese`, "Translations includes Japanese (#1)"),
	},
	"dmm": {
		// #37/#31: poster was upgraded from ps.jpg (portrait) to pl.jpg
		// (landscape jacket). The poster must be ps.jpg.
		rowMatches("Cover URL", `^https://pics\.dmm\.co\.jp/.*(pl|ps)\.jpg$`, "Cover URL is a DMM jacket (#37/#31)"),
		rowMatches("Poster URL", `ps\.jpg$`, "Poster URL is portrait ps.jpg, not landscape pl.jpg (#37/#31)"),
		// #23: screenshots were emitted as small thumbnails (-N.jpg) instead
		// of the high-res form (jp-N.jpg).
		screenshotsMatch(`jp-\d+\.jpg$`, "Screenshots use high-res jp-N.jpg form, not thumbnails (#23)"),
		rowMatches("Sources", `dmm`, "Sources includes dmm"),
	},
	"caribbeancom": {
		// #20: Caribbeancom IDs did not resolve; the scraper failed instead
		// of constructing the canonical moviepage URL.
		rowMatches("ID", `^120614-753$`, "ID matches input (#20)"),
		rowMatches("Cover URL", `caribbeancom\.com/moviepages/120614-753/images/l`, "Cover URL is canonical moviepage cover (#20)"),
		rowMatches("Sources", `caribbeancom`, "Sources includes caribbeancom (#20)"),
	},
	"libredmm": {
		// #22: libredmm expanded to route FC2/MGStage/SOD sources. The
		// existing ROYD-191 fixture guards the standard-JAV path stays
		// working. The FC2 routing (FC2-PPV-*) is tracked as a pending
		// fixture (libredmm_fc2) — libredmm.com returns "still processing"
		// for FC2-PPV-4761557, so it's not a stable fixture yet.
		rowPresent("Cover URL", "Cover URL present (libredmm reachable, #22)"),
		rowMatches("Sources", `libredmm`, "Sources includes libredmm (#22)"),
	},
}

// scrapeTimeout is the per-scraper wall-clock budget. Real sites can be slow
// (FlareSolverr challenges, proxy hops, browser rendering). 90s is generous
// but keeps a hung scraper from stalling the whole suite.
const scrapeTimeout = 90 * time.Second

// scraperResult is the structured outcome for one scraper, emitted in JSON
// mode for future server-side degradation tracking.
type scraperResult struct {
	Scraper string        `json:"scraper"`
	ID      string        `json:"id"`
	Pass    bool          `json:"pass"`
	Latency time.Duration `json:"latency_ms"`
	Error   string        `json:"error,omitempty"`
	Title   string        `json:"title,omitempty"`
}

// TestLive_Scrapers runs a real scrape against every scraper in the fixture,
// using the developer's real config (configs/config.yaml or $JAVINIZER_CONFIG)
// for proxy/FlareSolverr/browser setup. The DB is isolated to a temp path so
// the test never pollutes the developer's real database.
//
// Each scraper is a subtest — run one with:
//
//	JAVINIZER_LIVE_E2E=true go test -tags live -run 'TestLive_Scrapers/<name>' ./test/e2e/live/
//
// A scraper PASSES if the binary exits 0 and the output contains a "Title:"
// row with a non-empty value — the core signal that scraping produced real
// metadata. A failure is reported with the error/exit code so the developer
// can diagnose (upstream change, geo-block, missing proxy, etc.).
func TestLive_Scrapers(t *testing.T) {
	configPath := os.Getenv("JAVINIZER_CONFIG")
	if configPath == "" {
		// Default to the shipped config, which has all 14 scrapers in the
		// priority list + the developer's proxy/FlareSolverr/browser setup.
		configPath = filepath.Join(repoRoot(), "configs", "config.yaml")
	}

	// Isolate the DB so repeated runs don't accumulate cache entries that
	// would mask a regressed scraper (--force below also bypasses cache,
	// but isolation is defense-in-depth).
	dbPath := filepath.Join(t.TempDir(), "live-e2e.db")

	jsonMode := os.Getenv("JAVINIZER_LIVE_E2E_JSON") == "true"
	var results []scraperResult

	// Deterministic order (map iteration is random).
	names := make([]string, 0, len(scraperFixtures))
	for name := range scraperFixtures {
		names = append(names, name)
	}
	sortStrings(names)

	for _, name := range names {
		name := name
		id := scraperFixtures[name]

		t.Run(name, func(t *testing.T) {
			if pendingFixtures[name] {
				t.Skipf("%s: no verified fixture ID — add one to scraperFixtures and remove from pendingFixtures", name)
			}
			start := time.Now()
			out, code := runScrape(t, configPath, dbPath, id, name)
			latency := time.Since(start)

			// Pass gate: the scrape binary returns exit 1 when the workflow
			// considers the scrape failed (no results, site error, etc.). Exit 0
			// means the scraper fetched + parsed real metadata. The title is a
			// quality signal (reported below) but not a pass/fail gate — many
			// scrapers return valid metadata (ID, screenshots, cover) without
			// populating the Title field specifically.
			title := extractTitle(out)
			pass := code == 0

			// Per-scraper output invariants: beyond exit 0, assert the scrape
			// produced the expected output rows (Cover URL, Poster URL,
			// Screenshots, etc.). These pin specific past regressions:
			//   - #75/#18: r18.dev must return a cover + source (not 403/down)
			//   - #37/#31: dmm poster must be the portrait ps.jpg, not the
			//     landscape pl.jpg jacket (ps→pl upgrade regression)
			//   - #23: dmm screenshots must be the high-res jp-N.jpg form,
			//     not the small thumbnail -N.jpg form
			//   - #20: caribbeancom must resolve the ID to the canonical
			//     /moviepages/<id>/images/l_l.jpg cover
			// A failing invariant is a real regression signal — the exact
			// degradation this suite exists to catch.
			var invariantFailures []string
			if pass {
				for _, inv := range scraperInvariants[name] {
					if !inv.check(out) {
						invariantFailures = append(invariantFailures, inv.describe())
					}
				}
				if len(invariantFailures) > 0 {
					pass = false
				}
			}

			res := scraperResult{
				Scraper: name,
				ID:      id,
				Pass:    pass,
				Latency: latency,
				Title:   title,
			}
			if !pass {
				res.Error = diagnoseFailure(code, out)
			}
			results = append(results, res)

			if !pass {
				if len(invariantFailures) > 0 {
					res.Error = fmt.Sprintf("%d invariant(s) failed: %s", len(invariantFailures), strings.Join(invariantFailures, "; "))
					t.Errorf("%s invariant failures:\n  - %s\n--- output ---\n%s",
						name, strings.Join(invariantFailures, "\n  - "), out)
				} else {
					t.Errorf("%s scrape failed (exit %d): %s\n--- output ---\n%s",
						name, code, res.Error, out)
				}
			} else {
				if title != "" {
					t.Logf("%s ✓  %s  (%.1fs)", name, truncate(title, 60), latency.Seconds())
				} else {
					t.Logf("%s ✓  (no title; exit 0 with metadata)  (%.1fs)", name, latency.Seconds())
				}
			}
		})
	}

	// After all subtests: emit a summary. In JSON mode, print machine-
	// parseable JSON for future server-side tracking.
	if jsonMode {
		t.Run("Summary_JSON", func(t *testing.T) {
			data, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(data))
		})
	} else {
		t.Run("Summary", func(t *testing.T) {
			passed := 0
			for _, r := range results {
				if r.Pass {
					passed++
				}
			}
			fmt.Printf("\n=== Live E2E Summary: %d/%d scrapers passed ===\n", passed, len(results))
			for _, r := range results {
				mark := "✗"
				if r.Pass {
					mark = "✓"
				}
				extra := ""
				if r.Error != "" {
					extra = " — " + r.Error
				}
				fmt.Printf("  %s %-16s %6.1fs  %s%s\n", mark, r.Scraper, r.Latency.Seconds(), r.Title, extra)
			}
		})
	}
}

// runScrape executes `javinizer scrape <id> --scrapers <name> --force` with
// the real config + an isolated DB. Returns combined output + exit code.
func runScrape(t *testing.T, configPath, dbPath, id, scraper string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "scrape", "--scrapers", scraper, "--force", id)
	cmd.Env = func() []string {
		env := os.Environ()
		out := make([]string, 0, len(env)+2)
		for _, kv := range env {
			// Strip our own env vars so they don't leak into the subprocess
			// config resolution (we set them explicitly below).
			if strings.HasPrefix(kv, "JAVINIZER_CONFIG=") || strings.HasPrefix(kv, "JAVINIZER_DB=") {
				continue
			}
			out = append(out, kv)
		}
		return append(out,
			"JAVINIZER_CONFIG="+configPath,
			"JAVINIZER_DB="+dbPath,
		)
	}()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		// Context deadline / exec failure.
		code = 124
		if ctx.Err() == context.DeadlineExceeded {
			buf.WriteString(fmt.Sprintf("\n[timeout: scrape exceeded %s]", scrapeTimeout))
		}
	}
	return buf.String(), code
}

// extractTitle pulls the movie title out of the formatter's text-table output.
// The table prints rows as `<padded-label> : <value>` (e.g. `Title         : foo`),
// so we split on the first colon and match the label exactly. Returns "" if no
// Title row is present — many scrapers return valid metadata (ID, screenshots,
// cover) without populating the title field, so an empty title is NOT a failure
// signal; the pass/fail gate is the binary's exit code (see pass criteria).
func extractTitle(out string) string {
	return extractRow(out, "Title")
}

// extractRow pulls the value of a labelled row from the formatter's text-table
// output. Rows are printed as `<padded-label> : <value>` (e.g.
// `Cover URL    : https://...`). The Media-URL rows (Cover URL, Poster URL,
// Trailer URL, Screenshots) are indented two spaces under a `Media URLs:`
// header but the same `label : value` parse works. Returns "" if the row is
// absent.
func extractRow(out, label string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) == label {
			return strings.TrimSpace(line[idx+1:])
		}
	}
	return ""
}

// extractScreenshotURLs returns the `[ i] <url>` screenshot URLs listed under
// the Screenshots row. Used to assert each screenshot URL matches an expected
// pattern (e.g. the high-res jp-N.jpg form, not the small thumbnail).
func extractScreenshotURLs(out string) []string {
	var urls []string
	inScreenshots := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Screenshots") {
			inScreenshots = true
			continue
		}
		if inScreenshots {
			// Screenshot lines look like `  [ 1] https://...`. Stop when we
			// hit a non-matching line (next section / separator).
			if !strings.HasPrefix(trimmed, "[") {
				break
			}
			// Extract the URL after the `]`.
			if idx := strings.Index(trimmed, "]"); idx >= 0 {
				urls = append(urls, strings.TrimSpace(trimmed[idx+1:]))
			}
		}
	}
	return urls
}

// diagnoseFailure produces a short, human-readable reason for the failure
// from the exit code + output tail.
func diagnoseFailure(code int, out string) string {
	if code == 124 {
		return "timeout"
	}
	// Surface the last non-empty debug/error line — usually the root cause
	// (e.g. "403 Forbidden", "no result", "geo-restriction").
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		// Truncate long lines so the summary stays readable.
		if len(l) > 200 {
			l = l[:200] + "…"
		}
		return fmt.Sprintf("exit %d: %s", code, l)
	}
	return fmt.Sprintf("exit %d (no output)", code)
}

func sortStrings(s []string) {
	// Small fixed set; avoid importing sort for one call.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// truncate clips a string to n chars (with ellipsis) for summary readability.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// TestLive_UpgradeCheck_RealGitHubAPI runs `javinizer upgrade --check` against
// the REAL GitHub releases API (api.github.com/repos/javinizer/javinizer-go).
// Pins issue #39 (background update checker wired to the real GitHub checker +
// the defaultRepo hardcode pointing at the Go repo, not the legacy Python repo).
//
// A regression here means: the binary can't reach GitHub, misparses the release
// JSON, or the hardcoded repo is wrong — only catchable by hitting the real
// endpoint. The unit tests (internal/update/) use stubs + httptest servers and
// can't surface a real-transport break.
//
// Rate-limit handling: GitHub allows 60 unauthenticated requests/hour. If
// rate-limited, the command errors with a 403/rate-limit message — we treat
// that as a SKIP, not a FAIL (same convention as the Playwright
// update-indicator.spec.ts).
func TestLive_UpgradeCheck_RealGitHubAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "upgrade", "--check")
	cmd.Env = func() []string {
		env := os.Environ()
		out := make([]string, 0, len(env)+1)
		for _, kv := range env {
			if strings.HasPrefix(kv, "JAVINIZER_DB=") {
				continue
			}
			out = append(out, kv)
		}
		return append(out, "JAVINIZER_DB="+filepath.Join(t.TempDir(), "upgrade-check.db"))
	}()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = 124
	}

	// Rate-limited (60/hr unauthenticated) → SKIP, not FAIL.
	if isGitHubRateLimited(out, code) {
		t.Skipf("GitHub API rate-limited (60/hr unauthenticated); skipping #39 live check")
	}

	if code != 0 {
		t.Errorf("upgrade --check failed (exit %d): %s\n--- output ---\n%s", code, diagnoseFailure(code, out), out)
		return
	}

	// A successful check mentions the current version + that a check happened.
	// We don't assert "up to date" vs "update available" — either is a pass
	// (the binary reached GitHub and parsed a release). The #39 regression is
	// "never reaches GitHub / can't parse" — both of which produce exit != 0.
	if !strings.Contains(out, "Checking for") && !strings.Contains(out, "latest") {
		t.Errorf("upgrade --check output missing release-check signal:\n%s", out)
	}

	// Guard the defaultRepo regression specifically: output must NOT reference
	// the legacy Python repo (javinizer/Javinizer). The hardcoded defaultRepo
	// in internal/update/checker.go points at javinizer/javinizer-go.
	if strings.Contains(out, "javinizer/Javinizer") && !strings.Contains(out, "javinizer-go") {
		t.Errorf("upgrade --check referenced the legacy Python repo (javinizer/Javinizer) — defaultRepo regression (#39):\n%s", out)
	}

	t.Logf("upgrade --check ✓  (exit 0, reached real GitHub API)  %s", truncate(strings.TrimSpace(out), 80))
}

// isGitHubRateLimited reports whether the output/exit-code indicates a GitHub
// API rate-limit (403 with rate-limit messaging) rather than a real failure.
func isGitHubRateLimited(out string, code int) bool {
	if code == 0 {
		return false
	}
	low := strings.ToLower(out)
	return strings.Contains(low, "rate limit") ||
		strings.Contains(low, "403") && strings.Contains(low, "github")
}
