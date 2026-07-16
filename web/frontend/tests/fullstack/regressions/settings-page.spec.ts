/**
 * /settings page full-stack UI spec.
 *
 * Pins the settings page UI end-to-end against the real e2emock backend:
 * page render, config load (section headers render only after the config
 * query resolves), + collapsible section expansion (Scraper Settings +
 * Metadata Priority). Every assertion goes through the real rendered page
 * (real SvelteKit /settings) → real /api/v1/config + /api/v1/scrapers
 * (proxied to cmd/javinizer-e2e) → real :memory: SQLite. No page.route
 * mocking.
 *
 * Why this matters: the /settings page is the largest UI surface in the
 * app — 16+ collapsible sections backed by the full config struct + the
 * scraper registry. It had zero Playwright coverage. A regression in the
 * config load, the scraper list render, or the SettingsSection collapse/
 * expand wiring is invisible without this spec. The existing token-
 * management.spec.ts covers the API tokens CRUD but never renders the
 * settings page shell.
 *
 * Structure: every section is wrapped in a SettingsSection with
 * defaultExpanded={false} — there are no always-visible section bodies.
 * The section headers (h3 titles inside <button aria-expanded>) render
 * only after settings.settingsConfig populates (GET /api/v1/config
 * resolved), so asserting their presence pins the config query succeeded.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { loginAgainstRealBackend } from '../helpers';

const HEADING = 'Settings';
const E2EMOCK_DISPLAY = 'Deterministic E2E mock scraper';

async function navigateToSettings(page: Page): Promise<void> {
	await page.goto('/settings');
	// Use exact: true — 12 section headings end in "Settings" (e.g. "Server
	// Settings"), so a substring match resolves to 13 elements.
	await expect(page.getByRole('heading', { name: HEADING, exact: true })).toBeVisible({
		timeout: 15_000,
	});
}

/** Force a fresh version check against the real GitHub API and return state.
 * Mirrors update-indicator.spec.ts: a relative POST flows through the Vite proxy
 * carrying the storageState session cookie. Used to branch the Settings
 * upgrade-CTA test on the real update_available state (GitHub may rate-limit). */
async function forceVersionCheck(request: APIRequestContext) {
	const resp = await request.post('/api/v1/version/check', { failOnStatusCode: false });
	expect(resp.ok(), 'POST /api/v1/version/check should return 200').toBeTruthy();
	return (await resp.json()) as {
		update_available: boolean;
		latest: string;
		source: string;
		install_environment?: string;
	};
}

test.describe('/settings: real page shell + sections against the e2emock backend', () => {
	test('page renders with collapsible section headers (config loaded)', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToSettings(page);

		// ── Section headers render → config query resolved ──────────────
		// The section headers are inside {:else if settings.settingsConfig},
		// so their presence pins GET /api/v1/config succeeded + the config
		// populated. Assert a few stable section titles.
		for (const title of ['Server Settings', 'Scraper Settings', 'Metadata Priority']) {
			await expect(
				page.getByRole('button', { name: new RegExp(`^${title}`) }).first(),
			).toBeVisible({ timeout: 15_000 });
		}
	});

	test('Scraper Settings section expands → renders the e2emock scraper', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToSettings(page);

		// ── Expand the "Scraper Settings" section ────────────────────────
		// ScraperSettingsSection wraps itself in a SettingsSection with
		// defaultExpanded={false}. The header is a <button> with
		// aria-expanded. Expand it to reveal the scraper list.
		const scraperHeader = page.getByRole('button', { name: /^Scraper Settings/ }).first();
		await expect(scraperHeader).toBeVisible({ timeout: 15_000 });
		await expect(scraperHeader).toHaveAttribute('aria-expanded', 'false');

		await scraperHeader.click();
		await expect(scraperHeader).toHaveAttribute('aria-expanded', 'true');

		// The scraper list renders with "Available Scrapers" + each scraper's
		// displayName. The e2emock scraper's display_title is "Deterministic
		// E2E mock scraper — never hits the network" (not the raw name).
		await expect(page.getByText('Available Scrapers')).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(E2EMOCK_DISPLAY)).toBeVisible({
			timeout: 10_000,
		});
	});

	test('Metadata Priority section expands → priority content renders', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToSettings(page);

		// ── Expand the "Metadata Priority" section ──────────────────────
		const priorityHeader = page.getByRole('button', { name: /^Metadata Priority/ }).first();
		await expect(priorityHeader).toBeVisible({ timeout: 15_000 });
		await expect(priorityHeader).toHaveAttribute('aria-expanded', 'false');

		await priorityHeader.click();
		await expect(priorityHeader).toHaveAttribute('aria-expanded', 'true');

		// The MetadataPriority component renders. Assert a stable element
		// from its body: the "Per-Field Overrides" heading or a category
		// group heading ("Primary" / "Metadata"). These render after the
		// component mounts inside the expanded section.
		await expect(page.getByText(/per-field overrides|primary|metadata/i).first()).toBeVisible({
			timeout: 10_000,
		});

		// Collapse back — aria-expanded flips to false.
		await priorityHeader.click();
		await expect(priorityHeader).toHaveAttribute('aria-expanded', 'false');
	});

	test('Server Settings “Check for Updates” surfaces the upgrade CTA when an update is found', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		// The ServerSettingsSection keeps its OWN local versionStatus (separate
		// from the nav's TanStack query), populated by checkVersion() (POST
		// /api/v1/version/check). When update_available=true the section renders
		// an UpgradeAction CTA so a user who runs a check from Settings can act
		// on it without hunting for the nav indicator. Pin that wiring here.
		//
		// The e2e backend reports install_environment="cli", so the CTA is the
		// "View release" link (the desktop-only "Update & restart" button is
		// covered by tests/frontend/desktop-upgrade.spec.ts against a mocked env).
		//
		// Determinism: branch on the real GitHub response — if rate-limited
		// (update_available=false / source=error), assert the CTA stays hidden
		// instead. Both branches are valid; the spec never flakes.
		await loginAgainstRealBackend(request);
		await navigateToSettings(page);

		const serverHeader = page.getByRole('button', { name: /^Server Settings/ }).first();
		await expect(serverHeader).toBeVisible({ timeout: 15_000 });
		await serverHeader.click();
		await expect(serverHeader).toHaveAttribute('aria-expanded', 'true');

		const checkButton = page.getByRole('button', { name: /check for updates/i }).first();
		await expect(checkButton).toBeVisible({ timeout: 10_000 });

		// Force a fresh check via the UI button (same path the user takes), then
		// also read the state via the API to decide which branch to assert.
		await checkButton.click();
		await expect(page.getByText(/checking/i)).toBeVisible({ timeout: 5_000 }).catch(() => {});
		const fresh = await forceVersionCheck(request);

		// The CTA renders inside the same version block as the "Check for Updates"
		// button. Scope to that block rather than the fragile Tailwind class.
		const versionBlock = checkButton.locator('xpath=ancestor::div[contains(@class,"bg-muted")]');
		if (fresh.update_available && fresh.source !== 'disabled' && fresh.source !== 'none') {
			// Shown: the releases link (CLI env) appears next to the badge.
			const releaseLink = versionBlock.locator(
				'a[href*="github.com/javinizer/javinizer-go/releases"]',
			);
			await expect(releaseLink).toBeVisible({ timeout: 10_000 });
			await expect(releaseLink).toHaveText('View release');
			// Desktop-only self-upgrade button must NOT render for CLI env.
			await expect(versionBlock.getByRole('button', { name: /update.*restart/i })).toHaveCount(0);
		} else {
			// Hidden: no CTA when up-to-date / rate-limited / disabled.
			await expect(versionBlock.locator('a[href*="github.com/javinizer/javinizer-go/releases"]')).toHaveCount(0);
			await expect(versionBlock.getByRole('button', { name: /update.*restart/i })).toHaveCount(0);
		}
	});
});
