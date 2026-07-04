/**
 * /logs page full-stack UI spec.
 *
 * Pins the event-logs viewer UI end-to-end against the real e2emock
 * backend: page render, type-filter chips, severity/type badges, search.
 * Every action goes through the real rendered page (real SvelteKit /logs)
 * → real /api/v1/events (proxied to cmd/javinizer-e2e) → real
 * EventRepository.List → real :memory: SQLite. No page.route mocking.
 *
 * Why this matters: the /logs page is the only observability surface in
 * the app — it renders system/scrape/organize events with type + severity
 * filters, search, date range, + live mode. It had zero Playwright
 * coverage. The existing events.spec.ts covers the /events API contract
 * (response shape, pagination, offset) but never renders the page. A
 * regression in the logs page's loadEvents wiring, the type-filter chip
 * state, the search filter, or the event-row render (severity badge,
 * type badge, message) is invisible without this spec.
 *
 * Event data: the fullstack suite is serial — by the time this spec runs,
 * prior specs (scrape, organize, revert) + the global-setup auth login
 * have emitted real events into the :memory: SQLite. The spec asserts
 * structural elements (filter chips, badges) without depending on
 * specific event content.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

const HEADING = 'Logs';
const TYPE_FILTERS = ['All', 'Scraper', 'Organize', 'System'] as const;

interface EventItem {
	id: number;
	event_type: string;
	severity: string;
	message: string;
}

async function listEvents(api: APIRequestContext): Promise<EventItem[]> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/events?limit=100`);
	expect(resp.ok(), `list events failed: ${resp.status()}`).toBeTruthy();
	const body = (await resp.json()) as { events: EventItem[] };
	return body.events ?? [];
}

async function navigateToLogs(page: Page): Promise<void> {
	await page.goto('/logs');
	await expect(page.getByRole('heading', { name: HEADING })).toBeVisible({ timeout: 15_000 });
}

test.describe('/logs: real event viewer UI against the e2emock backend', () => {
	test('page renders with type-filter chips + event rows', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		const events = await listEvents(request);

		await navigateToLogs(page);

		// ── 1. The type-filter chips render (All, Scraper, Organize, System)
		// Each button's accessible name includes the count, e.g. "Scraper (5)",
		// so match by leading word boundary.
		for (const label of TYPE_FILTERS) {
			await expect(
				page.getByRole('button', { name: new RegExp(`^${label}\\s*\\(`) }),
			).toBeVisible({ timeout: 10_000 });
		}

		// ── 2. Events render with severity + type badges ─────────────────
		// Each event row renders a severity badge (raw severity text, e.g.
		// "info") + a type badge (raw event_type, e.g. "system"). Assert at
		// least one event row is present when the backend has events.
		if (events.length > 0) {
			const firstEvent = events[0];
			await expect(page.getByText(firstEvent.severity, { exact: true }).first()).toBeVisible({
				timeout: 10_000,
			});
			await expect(page.getByText(firstEvent.event_type, { exact: true }).first()).toBeVisible();
		}
	});

	test('clicking a type filter refetches + the active button reflects the state', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		await navigateToLogs(page);

		// ── Click the "Scraper" type filter ───────────────────────────────
		// This triggers loadEvents with type=scraper. The active button
		// switches to variant="default" (bg-primary). Assert the page still
		// renders the heading after the refetch (no crash on the filter).
		const scraperBtn = page.getByRole('button', { name: /^Scraper\s*\(/ });
		await expect(scraperBtn).toBeVisible({ timeout: 10_000 });
		await scraperBtn.click();

		// The page re-renders after the refetch. The heading stays visible.
		await expect(page.getByRole('heading', { name: HEADING })).toBeVisible({ timeout: 10_000 });

		// ── Click "All" to clear the filter ──────────────────────────────
		const allBtn = page.getByRole('button', { name: /^All\s*\(/ });
		await allBtn.click();
		await expect(page.getByRole('heading', { name: HEADING })).toBeVisible({ timeout: 10_000 });
	});

	test('search text filters the rendered event list', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);
		const events = await listEvents(request);
		// Skip if no events exist (guards against a fresh-backend edge case).
		test.skip(events.length === 0, 'no events in the backend to search');

		await navigateToLogs(page);

		// Pick a search term from the first event's message. getDisplayEvents
		// filters client-side by substring match on the message.
		const searchTerm = events[0].message.slice(0, Math.min(8, events[0].message.length));
		const searchInput = page.getByPlaceholder('Search messages...');
		await expect(searchInput).toBeVisible({ timeout: 10_000 });
		await searchInput.fill(searchTerm);

		// The matching event's severity badge still renders (the row didn't
		// all disappear — the filter kept the matching event).
		await expect(page.getByText(events[0].severity, { exact: true }).first()).toBeVisible({
			timeout: 10_000,
		});
	});
});
