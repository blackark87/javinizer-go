/**
 * /jobs/[jobId] revert full-stack UI spec.
 *
 * Pins the revert flow end-to-end: organize a job (real apply phase moves
 * files on disk) → navigate to /jobs/[jobId] → click "Revert Batch" →
 * confirm in RevertConfirmationModal → assert the job reaches `reverted`
 * status + the organized files are removed from disk + the original file
 * is restored to its source path.
 *
 * Real stack: browser → Vite dev server (real SvelteKit /jobs/[jobId]
 * render) → real /api/v1/jobs/:id/operations + /api/v1/jobs/:id/revert
 * (proxied to cmd/javinizer-e2e) → real history.Reverter → real
 * filesystem rename (move back) → real :memory: SQLite.
 *
 * Why this matters: the revert flow was entirely untested by the fullstack
 * suite — no spec visited /jobs/[jobId], no spec hit the revert endpoints,
 * and the RevertConfirmationModal + OperationRow components had zero
 * real-backend coverage. A regression in the revert pipeline (wrong
 * operation type guard, broken file-rename-back, stale revert_status
 * persistence, or the UI button not rendering) would sail through.
 *
 * Setup via API helpers (not UI): the scrape + organize setup uses the
 * `request` fixture directly because the browse→scrape UI path is already
 * covered by browse-launch-scrape.spec.ts. This spec's focus is the
 * /jobs/[jobId] revert UI, so setup is fast API calls; the assertions are
 * all against the rendered page + real disk state.
 *
 * Dedicated fixture (GOOD-901.mp4): the canonical GOOD-001.mp4 is shared
 * across specs. Organize with `copy_only: false` MOVES the file out of
 * the input dir — if this spec failed midway, GOOD-001.mp4 would be
 * missing for subsequent specs (the suite runs serially against one
 * backend). GOOD-901.mp4 is seeded per-test via seedInputFiles + re-seeded
 * in afterEach as a safety net, so canonical fixtures are never touched.
 *
 * Disk semantics: organize (move) relocates GOOD-901.mp4 from
 * DEFAULT_INPUT_DIR to <destination>/GOOD-901/GOOD-901.mp4. Revert
 * renames it back to DEFAULT_INPUT_DIR/GOOD-901.mp4 + cleans up the
 * empty <destination>/GOOD-901/ directory. The spec asserts both halves.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { existsSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

import {
	BACKEND_BASE,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	loginAgainstRealBackend,
	seedInputFiles,
	submitOrganize,
	submitScrape,
	waitForJobCompletion,
} from '../helpers';

/** Dedicated fixture — avoids touching the canonical GOOD-001.mp4. */
const REVERT_FIXTURE = 'GOOD-901.mp4';
const REVERT_MOVIE_ID = 'GOOD-901';
const REVERT_SOURCE = `${DEFAULT_INPUT_DIR}/${REVERT_FIXTURE}`;

test.describe('/jobs/[jobId] revert: real Reverter moves files back on disk', () => {
	test.beforeEach(async () => {
		// Seed the dedicated fixture fresh for each test. seedInputFiles
		// removes any stale copy first, so this is deterministic regardless
		// of whether a prior run's revert completed.
		await seedInputFiles([REVERT_FIXTURE]);
	});

	test.afterEach(async () => {
		// Safety net: re-seed the fixture so a failed test doesn't leave
		// GOOD-901.mp4 missing from the input dir for the next test. Cheap
		// (1-byte write) + idempotent.
		await seedInputFiles([REVERT_FIXTURE]);
	});

	test('organize → /jobs/[id] → Revert Batch → job reaches reverted + files restored on disk', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Setup: scrape + organize (move) to a unique destination ──────
		// copy_only: false (the submitOrganize default) → OperationTypeMove,
		// which is the only copy-mode revertible operation type (copy/hardlink/
		// symlink are rejected by the reverter's guardDoubleRevert).
		const job_id = await submitScrape(request, { files: [REVERT_SOURCE] });
		await waitForJobCompletion(request, job_id);

		const destination = `${DEFAULT_OUTPUT_DIR}/revert-ui-${Date.now()}`;
		expect(existsSync(destination), 'precondition: destination must not pre-exist').toBeFalsy();

		await submitOrganize(request, job_id, destination);
		const organized_job = await waitForJobCompletion(request, job_id, {
			expectStatus: 'organized',
		});

		// The organize phase must have reached `organized` (not just
		// `completed`) — revert rejects any other status with 400. This pins
		// that the move operation fully succeeded for the 1-byte fixture.
		expect(organized_job.status, 'job must be organized before revert').toBe('organized');

		// Disk precondition: the file was moved to the destination. The e2e
		// config's FolderFormat="<ID>" + the default SubfolderFormat=["<ID>"]
		// produce a double-nested layout: destination/<ID>/<ID>/<ID>.mp4.
		const organized_file = join(destination, REVERT_MOVIE_ID, REVERT_MOVIE_ID, `${REVERT_MOVIE_ID}.mp4`);
		expect(existsSync(organized_file), 'organized file must exist at destination before revert').toBeTruthy();
		expect(
			existsSync(REVERT_SOURCE),
			'source file must be gone after a move organize',
		).toBeFalsy();

		// ── 2. Navigate to /jobs/[jobId] ────────────────────────────────────
		await page.goto(`/jobs/${job_id}`);
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		// The page header renders the truncated job ID — confirms the detail
		// page mounted + fetched the job.
		const short_id = job_id.slice(0, 8);
		await expect(page.getByRole('heading', { name: `Job ${short_id}` })).toBeVisible({
			timeout: 10_000,
		});

		// ── 3. Assert the "Revert Batch" button is visible ─────────────────
		// The button only renders when ALL three conditions hold:
		//   pendingCount > 0  AND  jobStatus === 'organized'  AND  config.output.allow_revert
		// The e2e backend sets allow_revert = true (cmd/javinizer-e2e). If the
		// button is missing, one of those conditions failed — the most likely
		// regression is the config flag not flowing through to the frontend's
		// config query.
		const revertBatchBtn = page.getByRole('button', { name: /Revert Batch/i });
		await expect(revertBatchBtn, 'Revert Batch button must render for an organized job').toBeVisible({
			timeout: 10_000,
		});

		// The OperationRow for GOOD-901 must render with a per-file "Revert
		// File" button — pins the operations list fetched + rendered.
		const revertFileBtn = page.getByRole('button', { name: /Revert File/i }).first();
		await expect(revertFileBtn).toBeVisible({ timeout: 10_000 });

		// ── 4. Click "Revert Batch" → RevertConfirmationModal ──────────────
		await revertBatchBtn.click();

		// The modal renders in batch mode ("Revert Batch?" heading + a
		// "Revert N Files" confirm button).
		const modal = page.getByRole('dialog');
		await expect(modal).toBeVisible({ timeout: 5_000 });
		await expect(modal.getByText(/Revert Batch\?/)).toBeVisible();

		// The confirm button's label is "Revert {N} File{s}" — for one file,
		// "Revert 1 File". Its aria-label is "Revert 1 files".
		const confirmBtn = modal.getByRole('button', { name: /Revert 1 File/i });
		await expect(confirmBtn).toBeVisible();

		// ── 5. Confirm revert ──────────────────────────────────────────────
		await confirmBtn.click();

		// The modal closes on success (revertBatchMutation.onSuccess sets
		// revertModalOpen = false). Wait for it to disappear — proves the
		// mutation fired + returned 200 (a 4xx/5xx would fire onError which
		// also closes the modal, but the API poll below catches that).
		await expect(modal).not.toBeVisible({ timeout: 10_000 });

		// ── 6. Assert job reaches `reverted` via API ───────────────────────
		// The UI invalidates the job/operations queries on success, but
		// polling the API directly is the durable signal that the reverter
		// ran + the job row was updated. Reverted is terminal.
		const reverted_job = await waitForJobCompletion(request, job_id, {
			expectStatus: 'reverted',
			timeoutMs: 15_000,
		});
		expect(reverted_job.status, 'job status must be reverted after the UI revert').toBe('reverted');

		// ── 7. Assert disk state: file restored + destination cleaned ──────
		// The reverter's revertPrimaryFileFS renames the file from
		// <destination>/GOOD-901/GOOD-901.mp4 back to DEFAULT_INPUT_DIR/GOOD-901.mp4,
		// then cleanupEmptyDir removes the now-empty <destination>/GOOD-901/.
		expect(
			existsSync(REVERT_SOURCE),
			'source file must be restored to its original path after revert',
		).toBeTruthy();
		expect(
			existsSync(organized_file),
			'organized file must be gone from destination after revert',
		).toBeFalsy();

		// The per-movie subfolder tree (<ID>/<ID>/) should be cleaned up by
		// cleanupEmptyDir, which walks from filepath.Dir(NewPath) up to destRoot.
		const movie_subfolder = join(destination, REVERT_MOVIE_ID);
		expect(
			existsSync(movie_subfolder),
			'per-movie subfolder must be cleaned up after revert',
		).toBeFalsy();

		// ── 8. Assert the UI reflects the reverted state ───────────────────
		// Reload to let the page re-fetch with fresh data (the query
		// invalidation fires, but a reload is the surest way to assert the
		// post-revert render). The OperationRow should show "Reverted ✓" and
		// the "Revert Batch" button should be gone (pendingCount === 0).
		await page.reload();
		await page.waitForLoadState('networkidle');

		await expect(page.getByText('Reverted', { exact: false }).first()).toBeVisible({
			timeout: 10_000,
		});
		await expect(revertBatchBtn).not.toBeVisible({ timeout: 5_000 });
	});
});
