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
});
