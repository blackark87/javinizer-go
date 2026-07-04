/**
 * /actresses page full-stack UI spec.
 *
 * Pins the actress CRUD UI end-to-end against the real e2emock backend:
 * create via the form, list render, search filter, delete via the confirm
 * dialog. Every action goes through the real rendered page (real SvelteKit
 * /actresses) → real /api/v1/actresses (proxied to cmd/javinizer-e2e) →
 * real :memory: SQLite. No page.route mocking.
 *
 * Why this matters: the /actresses page is the most complex CRUD surface
 * in the app (store-backed, three view modes, merge modal, pagination,
 * search with apply/clear). It had zero Playwright coverage. A regression
 * in the actress store's create/delete mutation wiring, the form binding,
 * the search activeQuery flow, or the confirm-dialog dismissal is
 * invisible without this spec.
 *
 * Uniqueness: each test uses a timestamp-suffixed first_name to avoid
 * collisions with prior runs (the suite is serial against one backend).
 * afterEach cleans up any leftover test actresses via the API.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

const TEST_PREFIX = 'E2E-Actress-';
const HEADING = 'Actress Database';

interface Actress {
	id: number;
	first_name: string;
	last_name: string;
	japanese_name: string;
}

async function listActresses(api: APIRequestContext): Promise<Actress[]> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/actresses?limit=1000`);
	expect(resp.ok(), `list actresses failed: ${resp.status()}`).toBeTruthy();
	const body = (await resp.json()) as { actresses: Actress[] };
	return body.actresses ?? [];
}

async function deleteActress(api: APIRequestContext, id: number): Promise<void> {
	const resp = await api.delete(`${BACKEND_BASE}/api/v1/actresses/${id}`);
	expect(resp.ok(), `delete actress ${id} failed: ${resp.status()}`).toBeTruthy();
}

async function cleanupTestActresses(api: APIRequestContext): Promise<void> {
	const all = await listActresses(api);
	await Promise.all(
		all
			.filter((a) => a.first_name.startsWith(TEST_PREFIX))
			.map((a) => deleteActress(api, a.id).catch(() => {})),
	);
}

async function navigateToActresses(page: Page): Promise<void> {
	await page.goto('/actresses');
	await expect(page.getByRole('heading', { name: HEADING })).toBeVisible({ timeout: 15_000 });
	// The form renders (the "New Actress" button + the form card).
	await expect(page.getByPlaceholder('Yui')).toBeVisible({ timeout: 10_000 });
}

test.describe('/actresses: real CRUD UI against the e2emock backend', () => {
	test.afterEach(async ({ request }: { request: APIRequestContext }) => {
		await cleanupTestActresses(request);
	});

	test('create → list renders the actress → search filters → delete removes the row', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToActresses(page);

		// ── 1. Create an actress via the form ────────────────────────────
		// The form is always visible. Fill first_name (unique per run) +
		// japanese_name, then click "Create" (the Save button's label when
		// not editing). The createMutation invalidates the list query.
		const firstName = `${TEST_PREFIX}${Date.now()}`;
		const japaneseName = 'テスト女優';
		await page.getByPlaceholder('Yui').fill(firstName);
		await page.getByPlaceholder('波多野結衣').fill(japaneseName);
		await page.getByRole('button', { name: /^Create$/ }).click();

		// The actress card renders with getDisplayName. With only first_name
		// set, getDisplayName returns the first_name. The card's <h3> holds it.
		await expect(page.getByRole('heading', { name: firstName })).toBeVisible({
			timeout: 10_000,
		});

		// ── 2. Search filters to the actress ─────────────────────────────
		// The toolbar's search uses queryInput (bound) + activeQuery (set on
		// Apply). Typing + Enter triggers onApplySearch. The list query
		// re-fetches with q=firstName → only the matching actress renders.
		const searchInput = page.getByPlaceholder('Search by English or Japanese name');
		await searchInput.fill(firstName);
		await searchInput.press('Enter');

		await expect(page.getByRole('heading', { name: firstName })).toBeVisible({
			timeout: 10_000,
		});

		// Clear search → the full list re-renders (our actress still present).
		// Scope to the toolbar's Clear button (the form also has a Clear). The
		// input is nested input → div.relative → div.flex-1; the Search/Clear
		// buttons are siblings of div.flex-1, so go up two levels.
		await searchInput.locator('xpath=../../following-sibling::button[contains(.,"Clear")]').click();
		await expect(page.getByRole('heading', { name: firstName })).toBeVisible({
			timeout: 10_000,
		});

		// ── 3. Delete the actress via the confirm dialog ─────────────────
		// The card's delete button has accessible name "Delete" (text + icon).
	// It opens a confirmDialog (role="alertdialog") with a "Delete" confirm
		// button. Scope to the card's Delete button via the heading's ancestor
		// flex-1 container (holds both the name row + the action row).
		const heading = page.getByRole('heading', { name: firstName });
		await heading.locator('xpath=ancestor::div[contains(@class,"flex-1")]//button[contains(., "Delete")]').click();

		// The dialog renders; click its "Delete" confirm button.
		const dialog = page.getByRole('alertdialog');
		await expect(dialog).toBeVisible({ timeout: 5_000 });
		await dialog.getByRole('button', { name: /^Delete$/ }).click();

		// The deleteMutation invalidates → the card disappears.
		await expect(page.getByRole('heading', { name: firstName })).toHaveCount(0, {
			timeout: 10_000,
		});
	});
});
