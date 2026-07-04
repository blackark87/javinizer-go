/**
 * /words page full-stack UI spec.
 *
 * Pins the word-replacement CRUD UI end-to-end against the real e2emock
 * backend: list render, add, edit, search, sort, delete. Every action
 * goes through the real rendered page (real SvelteKit /words) → real
 * /api/v1/words/replacements (proxied to cmd/javinizer-e2e) → real
 * :memory: SQLite. No page.route mocking.
 *
 * Why this matters: the /words page is structurally identical to /genres
 * (same CRUD table, same search/sort, same import/export), but it hits a
 * distinct API surface (word replacements, not genre replacements) with
 * its own backend handler + repo. A regression specific to the word-
 * replacement wiring (e.g., a renamed query key, a swapped endpoint) is
 * invisible to the genres spec. This spec mirrors the genres coverage
 * against the word-replacement contract.
 *
 * Uniqueness: each test uses a timestamp-suffixed `original` to avoid
 * collisions with prior runs (the suite is serial against one backend).
 * afterEach cleans up any leftover test replacements via the API.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

const TEST_PREFIX = 'E2E-WORD-';
const HEADING = 'Word Replacements';

interface WordReplacement {
	id: number;
	original: string;
	replacement: string;
}

async function listWordReplacements(api: APIRequestContext): Promise<WordReplacement[]> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/words/replacements?limit=1000`);
	expect(resp.ok(), `list words failed: ${resp.status()}`).toBeTruthy();
	const body = (await resp.json()) as { replacements: WordReplacement[] };
	return body.replacements ?? [];
}

async function createWordReplacement(
	api: APIRequestContext,
	original: string,
	replacement: string,
): Promise<WordReplacement> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/words/replacements`, {
		data: { original, replacement },
	});
	expect(resp.ok(), `create word failed: ${resp.status()} ${await resp.text()}`).toBeTruthy();
	return (await resp.json()) as WordReplacement;
}

async function deleteWordReplacement(api: APIRequestContext, id: number): Promise<void> {
	const resp = await api.delete(`${BACKEND_BASE}/api/v1/words/replacements?id=${id}`);
	expect(resp.ok(), `delete word ${id} failed: ${resp.status()}`).toBeTruthy();
}

async function cleanupTestReplacements(api: APIRequestContext): Promise<void> {
	const all = await listWordReplacements(api);
	await Promise.all(
		all
			.filter((r) => r.original.startsWith(TEST_PREFIX))
			.map((r) => deleteWordReplacement(api, r.id).catch(() => {})),
	);
}

async function navigateToWords(page: Page): Promise<void> {
	await page.goto('/words');
	await expect(page.getByRole('heading', { name: HEADING })).toBeVisible({ timeout: 15_000 });
	await expect(page.getByPlaceholder('e.g., R**e')).toBeVisible({ timeout: 10_000 });
}

test.describe('/words: real CRUD UI against the e2emock backend', () => {
	test.afterEach(async ({ request }: { request: APIRequestContext }) => {
		await cleanupTestReplacements(request);
	});

	test('list renders a pre-created replacement row', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		const original = `${TEST_PREFIX}LIST-${Date.now()}`;
		const replacement = 'Uncensored';
		await createWordReplacement(request, original, replacement);

		await navigateToWords(page);

		await expect(page.getByText(original, { exact: true })).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(replacement, { exact: true })).toBeVisible();
	});

	test('add → row renders → edit → replacement updates → delete → row gone', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToWords(page);

		// ── 1. Add a replacement via the UI ──────────────────────────────
		const original = `${TEST_PREFIX}CRUD-${Date.now()}`;
		const replacement = 'First Replacement';
		await page.getByPlaceholder('e.g., R**e').fill(original);
		await page.getByPlaceholder('e.g., Rape').fill(replacement);
		await page.getByRole('button', { name: /^Add$/ }).click();

		await expect(page.getByText(original, { exact: true })).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(replacement, { exact: true })).toBeVisible();

		// ── 2. Edit the replacement inline ───────────────────────────────
		// The original-text cell's following-sibling div holds the Actions
		// cell (Edit + Delete buttons). Scoping via xpath avoids matching
		// ancestor container divs that would resolve to multiple buttons.
		const originalCell = page.getByText(original, { exact: true });
		await originalCell.locator('xpath=following-sibling::div//button[@title="Edit"]').click();

		const editInput = page.locator('input.font-mono:not([disabled])').last();
		await editInput.fill('Edited Replacement');
		await page.getByRole('button', { name: /^Save$/ }).click();

		await expect(page.getByText('Edited Replacement', { exact: true })).toBeVisible({
			timeout: 10_000,
		});

		// ── 3. Delete the row ────────────────────────────────────────────
		const originalCellAfter = page.getByText(original, { exact: true });
		await originalCellAfter.locator('xpath=following-sibling::div//button[@title="Delete"]').click();

		await expect(page.getByText(original, { exact: true })).toHaveCount(0, { timeout: 10_000 });
	});

	test('search filters the list + sort toggles the order', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		const a = await createWordReplacement(request, `${TEST_PREFIX}ALPHA-${Date.now()}`, 'A-Rep');
		const b = await createWordReplacement(request, `${TEST_PREFIX}BETA-${Date.now()}`, 'B-Rep');

		await navigateToWords(page);

		await expect(page.getByText(a.original, { exact: true })).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText(b.original, { exact: true })).toBeVisible();

		// ── 1. Search filters to the matching row ────────────────────────
		await page.getByPlaceholder('Search by original or replacement...').fill('ALPHA');
		await expect(page.getByText(a.original, { exact: true })).toBeVisible();
		await expect(page.getByText(b.original, { exact: true })).toHaveCount(0);

		await page.getByTitle('Clear search').click();
		await expect(page.getByText(b.original, { exact: true })).toBeVisible({ timeout: 10_000 });

		// ── 2. Sort toggle flips A-Z ↔ Z-A ───────────────────────────────
		const sortBtn = page.getByTitle('Toggle sort order');
		await expect(sortBtn).toContainText('A-Z');
		await sortBtn.click();
		await expect(sortBtn).toContainText('Z-A');
		await expect(page.getByText(a.original, { exact: true })).toBeVisible();
		await expect(page.getByText(b.original, { exact: true })).toBeVisible();
	});
});
