/**
 * /review/[jobId] organize full-stack UI spec.
 *
 * Pins the organize-from-review-page flow end-to-end: scrape via API →
 * navigate browser to /review/[jobId] → fill the destination → click
 * "Organize N Files" → the real apply phase runs → OrganizeStatusCard
 * renders "Organization Complete!" → real files appear on disk.
 *
 * Real stack: browser → Vite dev server (real SvelteKit /review render)
 * → real /api/v1/batch/:id/organize (proxied to cmd/javinizer-e2e) →
 * real apply phase + organizer workflow → real filesystem write to the
 * destination directory → real :memory: SQLite.
 *
 * Why this matters: the existing organize.spec.ts is API-only — it POSTs
 * to /batch/:id/organize via the `request` fixture + asserts the folder
 * exists on disk, but never renders the review page. The mocked
 * review-page.spec.ts renders the review page but against a mocked API
 * (page.route), so it can't verify the real apply phase writes files.
 * This spec bridges the two: real browser render of /review/[jobId] +
 * real backend apply phase + real disk assertions. A regression in the
 * review page's organize wiring (ReviewActionBar → organizeController →
 * api.organizeBatchJob), the destination-path binding, or the
 * OrganizeStatusCard success render is invisible to both existing specs.
 *
 * Setup via API (submitScrape): the scrape setup uses the `request`
 * fixture directly because the browse→scrape UI path is already covered
 * by browse-launch-scrape.spec.ts. This spec's focus is the review page's
 * organize UI, so setup is a fast API call; the assertions are all
 * against the rendered page + real disk state.
 *
 * Dedicated fixture (GOOD-904.mp4): organize with `move` relocates the
 * file out of the input dir — a dedicated fixture keeps the spec
 * self-contained + avoids touching canonical fixtures shared across specs.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

import {
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	loginAgainstRealBackend,
	seedInputFiles,
	submitScrape,
	waitForJobCompletion,
	navigateToReviewPage,
} from '../helpers';

/** Dedicated fixture — organize (move) relocates it, so don't touch shared fixtures. */
const ORGANIZE_FIXTURE = 'GOOD-904.mp4';
const ORGANIZE_MOVIE_ID = 'GOOD-904';
const ORGANIZE_SOURCE = `${DEFAULT_INPUT_DIR}/${ORGANIZE_FIXTURE}`;

/** Placeholder on the DestinationSettingsCard's path input. */
const DEST_PLACEHOLDER = 'Enter destination path (e.g., /path/to/output)';

test.describe('/review/[jobId] organize: real apply phase creates files on disk', () => {
	test.beforeEach(async () => {
		await seedInputFiles([ORGANIZE_FIXTURE]);
	});

	test.afterEach(async () => {
		// Safety net: re-seed in case a failed test left the fixture moved
		// out of the input dir. Cheap (1-byte write) + idempotent.
		await seedInputFiles([ORGANIZE_FIXTURE]);
	});

	test('scrape → /review/[id] → Organize → OrganizeStatusCard shows complete + files exist on disk', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Setup: scrape the fixture (reaches `completed`, not organized) ─
		// The review page's organize button is the entry point to the apply
		// phase — scrape-only setup means the job is at `completed` + the
		// files are still in the input dir, the exact pre-organize state.
		const job_id = await submitScrape(request, { files: [ORGANIZE_SOURCE] });
		await waitForJobCompletion(request, job_id);

		const destination = `${DEFAULT_OUTPUT_DIR}/review-organize-${Date.now()}`;
		expect(existsSync(destination), 'precondition: destination must not pre-exist').toBeFalsy();

		// ── 2. Navigate browser to /review/[jobId] ──────────────────────────
		await navigateToReviewPage(page, job_id);

		// The review page restores viewMode from localStorage — a prior spec
		// (or this one's previous run) may have left it on 'grid-poster',
		// where the DestinationSettingsCard + ReviewActionBar don't render.
		// Force the detail view: it's the only view that surfaces the organize
		// UI. The Detail button is in the ReviewHeader view toggle.
		await page.getByRole('button', { name: /^Detail$/i }).click();

		// The review page renders the scraped movie's data. GOOD-904's ID
		// (from e2emock) appears in the detail-view navigation card
		// ("Movie 1 of 1" + the ID badge) + the Source File card. Asserting
		// the ID visible pins the page mounted + fetched the real job results.
		// (The title "E2E Movie GOOD-904" shows in grid-poster view's card,
		// but the detail view's Title field is an editable input whose value
		// isn't in textContent — the ID is the reliable mount signal here.)
		await expect(page.locator('body')).toContainText('GOOD-904', { timeout: 15_000 });

		// ── 3. Fill the destination path ───────────────────────────────────
		// The DestinationSettingsCard renders when canOrganize (= operation
		// mode is `organize`, the config default). The Organize button is
		// disabled until destinationPath is non-empty, so filling it is a
		// precondition for the click below.
		const destInput = page.getByPlaceholder(DEST_PLACEHOLDER).first();
		await expect(destInput, 'destination input must render in organize mode').toBeVisible({
			timeout: 10_000,
		});
		await destInput.fill(destination);

		// ── 4. Click "Organize 1 File" ──────────────────────────────────────
		// The button label is "Organize {N} File{s}" — for one file,
		// "Organize 1 File". The ReviewActionBar renders twice (main column +
		// a sticky bar), so target the first. Clicking fires
		// organizeController.organizeAll → api.organizeBatchJob (real POST
		// /batch/:id/organize) + starts the completion polling.
		const organizeBtn = page.getByRole('button', { name: /^Organize 1 File$/i }).first();
		await expect(organizeBtn).toBeEnabled({ timeout: 5_000 });
		await organizeBtn.click();

		// ── 5. Wait for OrganizeStatusCard "Organization Complete!" ─────────
		// The organize-controller polls the job; on terminal organize status
		// it sets organizeStatus='completed', which renders the green success
		// card with "Organization Complete!" + "Redirecting to browse page...".
		// This is the UI-side signal the apply phase finished. 30s timeout
		// is generous — the apply phase for one 1-byte file is near-instant,
		// but the polling cadence + WS round-trip add latency.
		await expect(page.getByText('Organization Complete!')).toBeVisible({ timeout: 30_000 });

		// ── 6. Assert real files exist on disk ─────────────────────────────
		// The e2e config's FolderFormat="<ID>" + default SubfolderFormat=
		// ["<ID>"] produce destination/<ID>/<ID>/<ID>.mp4. The apply phase
		// ran for real (not just enqueued) — the file exists on disk.
		expect(existsSync(destination), 'destination directory must exist after organize').toBeTruthy();
		const entries = readdirSync(destination);
		expect(entries, 'destination must contain a per-movie subfolder').toContain(ORGANIZE_MOVIE_ID);

		const organized_file = join(
			destination,
			ORGANIZE_MOVIE_ID,
			ORGANIZE_MOVIE_ID,
			`${ORGANIZE_MOVIE_ID}.mp4`,
		);
		expect(
			existsSync(organized_file),
			'organized video file must exist at the template-resolved path',
		).toBeTruthy();

		// ── 7. Cross-check: the job reaches `organized` via the API ─────────
		// Ties the UI success render to the backend job state. The
		// organize-controller's success render fires on the WS/REST poll
		// detecting terminal status — asserting the API job status is
		// `organized` confirms the success card wasn't a false positive.
		const organized_job = await waitForJobCompletion(request, job_id, {
			expectStatus: 'organized',
			timeoutMs: 15_000,
		});
		expect(organized_job.status, 'job status must be organized after the UI organize').toBe(
			'organized',
		);
	});
});
