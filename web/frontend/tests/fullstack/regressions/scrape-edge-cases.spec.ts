/**
 * Scrape pipeline edge-cases spec.
 *
 * Pins the scrape pipeline's behavior under non-happy-path inputs against
 * the real e2emock backend: mixed batches (partial failure), empty file
 * lists, duplicate paths, non-existent scrapers, missing files on disk,
 * force-refresh, and cache idempotency. Every action hits the real
 * /api/v1/batch/scrape endpoint (proxied to cmd/javinizer-e2e) → real
 * worker.ScrapePhase → real e2emock scraper. No page.route mocking.
 *
 * Why this matters: the existing scrape specs (browse-launch-scrape,
 * manual-scrape, multipart, rescrape, batch-rescrape, field-drop) all
 * drive the SINGLE-file happy path or a single-error path. Real users
 * select batches of 10–50 files where some match + some don't. The
 * pipeline's partial-failure handling — does a failed file poison the
 * batch? does the job status reflect per-file failures? are successful
 * results preserved alongside failed ones? — had ZERO coverage. The
 * input-validation edge cases (empty list, duplicates, bad scraper
 * names) likewise had no coverage, so a regression that silently
 * accepted or silently dropped them would go unnoticed.
 *
 * All behaviors below were verified empirically against the running
 * e2emock backend before writing the assertions — the spec pins the
 * CURRENT correct behavior so future changes are intentional.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend, submitScrape, submitOrganize } from '../helpers';
import { waitForJobCompletion } from '../helpers';
import { DEFAULT_INPUT_DIR, DEFAULT_OUTPUT_DIR, seedInputFiles } from '../helpers';
import type { BatchJobResponse, FileResult } from '../helpers';

const GOOD_FILE = `${DEFAULT_INPUT_DIR}/GOOD-001.mp4`;
const FAIL_FILE = `${DEFAULT_INPUT_DIR}/FAIL-002.mp4`;

interface JobSummary {
	status: string;
	totalFiles: number;
	results: Record<string, FileResult>;
}

async function scrapeAndAwait(
	api: APIRequestContext,
	files: string[],
	opts: { force?: boolean; selectedScrapers?: string[] } = {},
): Promise<JobSummary> {
	const jobId = await submitScrape(api, {
		files,
		...opts,
	});
	const job = await waitForJobCompletion(api, jobId, { timeoutMs: 30_000 });
	return {
		status: job.status,
		totalFiles: job.total_files,
		results: job.results,
	};
}

test.describe('Scrape pipeline edge cases: real e2emock backend', () => {
	test.describe('mixed batches (partial failure)', () => {
		test('GOOD + FAIL in one job → job completes, per-file statuses independent, success not poisoned by failure', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// The most realistic real-world scenario: a user selects a batch
			// where some files scrape successfully + some fail. The pipeline
			// must NOT abort the whole batch on the first failure, must NOT
			// mark the successful files as failed, + must surface the failed
			// file's verbose per-scraper error verbatim.
			const { status, totalFiles, results } = await scrapeAndAwait(request, [
				GOOD_FILE,
				FAIL_FILE,
			]);

			// ── 1. Job-level: the job COMPLETES (status=completed), not failed.
			// A mixed batch is a successful batch scrape — individual file
			// failures are per-file, not job-level. (If this flipped to
			// "failed", the review UI would never show the successful results.)
			expect(status, 'a mixed batch must complete (not fail) at the job level').toBe(
				'completed',
			);

			// ── 2. Both files are present in the results (no file dropped).
			expect(totalFiles, 'total_files must reflect both input files').toBe(2);
			expect(Object.keys(results).length, 'results must contain both files').toBe(2);
			expect(results[GOOD_FILE], 'the GOOD file must have a result').toBeTruthy();
			expect(results[FAIL_FILE], 'the FAIL file must have a result').toBeTruthy();

			// ── 3. The GOOD file succeeded — its result is NOT poisoned by the
			// sibling failure. The movie payload (title) must be present.
			const goodResult = results[GOOD_FILE];
			expect(goodResult.status, 'the GOOD file must be status=completed').toBe('completed');
			expect(goodResult.movie_id, 'the GOOD file must carry its movie_id').toBe('GOOD-001');
			expect(goodResult.movie, 'the GOOD file must carry its scraped movie payload').toBeTruthy();
			expect(goodResult.movie?.title, 'the GOOD movie title must be the scraped value').toContain(
				'E2E Movie GOOD-001',
			);
			expect(goodResult.error, 'a successful scrape must have no error').toBeFalsy();

			// ── 4. The FAIL file failed — its verbose per-scraper error
			// survives end-to-end (the "e2emock:" substring from the scraper
			// must NOT be collapsed to a hardcoded "no result"). This guards
			// the commit 42d89e65 regression class.
			const failResult = results[FAIL_FILE];
			expect(failResult.status, 'the FAIL file must be status=failed').toBe('failed');
			expect(failResult.movie_id, 'the FAIL file must still carry its movie_id').toBe('FAIL-002');
			expect(failResult.error, 'the FAIL file must carry a verbose error').toBeTruthy();
			expect(failResult.error, 'the verbose per-scraper error must survive verbatim').toContain(
				'e2emock',
			);
			expect(failResult.movie, 'a failed scrape must not carry a movie payload').toBeFalsy();
		});

		test('2 GOOD + 1 FAIL → multiple successes preserved, failure isolated', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// A larger mixed batch: two successful files + one failure. Proves
			// the partial-failure handling scales beyond 1+1 — no off-by-one
			// in the result tracker, no cross-contamination between the two
			// successful results. Uses GOOD-001 + GOOD-001 (duplicate path,
			// deduped to one result — see the duplicate-paths test) + a second
			// GOOD file seeded ad-hoc.
			await seedInputFiles(['GOOD-099.mp4']);
			const goodFile2 = `${DEFAULT_INPUT_DIR}/GOOD-099.mp4`;

			const { status, results } = await scrapeAndAwait(request, [
				GOOD_FILE,
				goodFile2,
				FAIL_FILE,
			]);

			expect(status, 'the mixed batch must complete').toBe('completed');
			expect(Object.keys(results).length, 'all three files must have results').toBe(3);

			// Both GOOD files succeeded with their own movie payloads.
			expect(results[GOOD_FILE].status, 'first GOOD file must succeed').toBe('completed');
			expect(results[GOOD_FILE].movie?.title).toContain('GOOD-001');
			expect(results[goodFile2].status, 'second GOOD file must succeed').toBe('completed');
			expect(results[goodFile2].movie?.title).toContain('GOOD-099');

			// The FAIL file is isolated — its failure doesn't bleed into
			// either successful result.
			expect(results[FAIL_FILE].status, 'the FAIL file must be failed in isolation').toBe(
				'failed',
			);
			expect(results[FAIL_FILE].error).toContain('e2emock');
		});
	});

	test.describe('input validation edge cases', () => {
		test('empty files array → job created + completes with 0 results (current behavior)', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// `files` has `binding:"required"` — but Go's binding "required" on
			// a slice only rejects nil/missing, NOT an empty slice. So
			// files:[] is accepted + creates a job that completes with zero
			// results. This pins the CURRENT behavior: if a future change
			// tightens this to 400 (arguably more correct), this test will
			// fail + prompt an intentional update.
			const { status, totalFiles, results } = await scrapeAndAwait(request, []);

			expect(status, 'an empty batch must still reach a terminal status').toBe('completed');
			expect(totalFiles, 'an empty batch must report 0 files').toBe(0);
			expect(Object.keys(results).length, 'an empty batch must produce 0 results').toBe(0);
		});

		test('duplicate file paths → results deduped by path key (one result, not two)', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// Submitting the same file path twice: total_files counts both
			// submissions, but the results map is keyed by file path, so only
			// one result row exists. The single result is the successful
			// scrape. This pins the dedup-by-key behavior — a regression that
			// produced two results for the same path (or panicked on the
			// duplicate key) would surface here.
			const { status, totalFiles, results } = await scrapeAndAwait(request, [
				GOOD_FILE,
				GOOD_FILE,
			]);

			expect(status, 'a duplicate-path batch must complete').toBe('completed');
			expect(totalFiles, 'total_files must count both submissions').toBe(2);
			expect(
				Object.keys(results).length,
				'results must dedupe to one entry by path key',
			).toBe(1);
			expect(results[GOOD_FILE].status, 'the deduped result must be the successful scrape').toBe(
				'completed',
			);
		});

		test('non-existent scraper name → file fails with "No results from any scraper" (no 400)', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// selected_scrapers=['nonexistent'] is NOT validated at the API
			// layer — the job is created + the scrape runs, but no scraper
			// matches the name, so the file fails with "No results from any
			// scraper". This pins the current silent-acceptance behavior: a
			// future change that 400s on an unknown scraper name would fail
			// this test + prompt an intentional update.
			const { status, results } = await scrapeAndAwait(request, [GOOD_FILE], {
				selectedScrapers: ['nonexistent-scraper'],
			});

			expect(status, 'the job must still complete (per-file failure, not job failure)').toBe(
				'completed',
			);
			const result = results[GOOD_FILE];
			expect(result, 'the file must have a result').toBeTruthy();
			expect(result.status, 'the file must fail (no scraper matched)').toBe('failed');
			expect(result.error, 'the error must explain no scraper produced results').toContain(
				'No results',
			);
		});

		test('file not on disk → scrape still runs (matcher extracts ID from filename)', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// A file path that doesn't exist on disk is still accepted: the
			// scrape pipeline derives the MovieID from the FILENAME via the
			// matcher (it never reads the file content — the e2emock scraper
			// is keyed on ID). The file's directory is allowed (it's in the
			// input dir), so the security check passes; the scrape then runs
			// + fails with the e2emock's "unrecognized ID" error (since the
			// nonexistent filename doesn't carry a GOOD-/FAIL-/etc. prefix).
			//
			// This pins that the scrape does NOT require the file to exist —
			// only the filename matters for ID extraction. A regression that
			// added a stat-before-scrape guard (rejecting missing files) would
			// surface here.
			const missingFile = `${DEFAULT_INPUT_DIR}/NONEXISTENT-999.mp4`;
			const { status, results } = await scrapeAndAwait(request, [missingFile]);

			expect(status, 'the job must complete (per-file failure)').toBe('completed');
			const result = results[missingFile];
			expect(result, 'the missing file must still get a result row').toBeTruthy();
			expect(result.status, 'the scrape must fail (unrecognized ID)').toBe('failed');
			expect(result.error, 'the error must be the verbose per-scraper message').toBeTruthy();
		});
	});

	test.describe('mixed batch → organize (full pipeline with partial failure)', () => {
		test('organize skips the failed scrape result + organizes only the successful one', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// The full pipeline edge case: scrape a mixed batch (GOOD + FAIL),
			// then organize. The apply phase filters at line 78 of
			// apply_phase.go: `if fileResult.Status != completed || Movie == nil`.
			// So the FAIL file (status=failed from scrape) is SKIPPED by organize —
			// it's never touched, its error is preserved. Only the GOOD file is
			// organized. The job reaches "organized" status with completed=1,
			// failed=1. This guards the partial-failure organize contract: a
			// regression that tried to organize a failed-scrape file (and 500'd
			// on its nil Movie) would surface here.
			//
			// Uses a unique GOOD file (GOOD-077) + a unique destination dir so
			// the organize phase always succeeds even when run after other
			// organize specs that already organized GOOD-001 to the default
			// output dir (a file-exists collision would make orgCount=0/
			// failCount>0 → MarkCompleted instead of MarkOrganized, masking
			// the contract under test).
			await seedInputFiles(['GOOD-077.mp4']);
			const goodFile = `${DEFAULT_INPUT_DIR}/GOOD-077.mp4`;
			const uniqueDest = `${DEFAULT_OUTPUT_DIR}/edge-${Date.now()}`;
			const jobId = await submitScrape(request, { files: [goodFile, FAIL_FILE] });
			const scraped = await waitForJobCompletion(request, jobId, { timeoutMs: 30_000 });
			expect(scraped.status, 'the mixed batch scrape must complete').toBe('completed');

			// ── 1. Submit organize on the mixed-batch job ─────────────────────
			await submitOrganize(request, jobId, uniqueDest);

			// ── 2. Wait for the organize phase to finish ──────────────────────
			// The job transitions completed → organized (MarkOrganized). Poll
			// until terminal.
			const organized = await waitForJobCompletion(request, jobId, {
				timeoutMs: 30_000,
			});
			expect(organized.status, 'the job must reach "organized" after apply').toBe('organized');
			expect(organized.completed, 'completed count must be 1 (the GOOD file)').toBe(1);
			expect(organized.failed, 'failed count must be 1 (the FAIL file, skipped)').toBe(1);

			// ── 3. The GOOD file was organized — status stays "completed" ────
			// (the apply phase doesn't flip a successful file's status to
			// "organized"; it stays "completed" + the job-level status reflects
			// the organize outcome). The movie payload is still present.
			const goodResult = organized.results[goodFile];
			expect(goodResult, 'the GOOD file must still have a result').toBeTruthy();
			expect(goodResult.status, 'the organized GOOD file stays completed').toBe('completed');
			expect(goodResult.movie, 'the GOOD movie payload must survive organize').toBeTruthy();
			expect(goodResult.error, 'the GOOD file must have no error').toBeFalsy();

			// ── 4. The FAIL file was SKIPPED by organize ─────────────────────
			// Its status is still "failed" (from the scrape phase), + its verbose
			// scrape error is preserved verbatim — organize didn't touch it,
			// didn't clear the error, didn't try to organize a nil Movie.
			const failResult = organized.results[FAIL_FILE];
			expect(failResult, 'the FAIL file must still have a result').toBeTruthy();
			expect(failResult.status, 'the skipped FAIL file stays failed').toBe('failed');
			expect(failResult.error, 'the FAIL scrape error must be preserved').toContain('e2emock');
			expect(failResult.movie, 'the FAIL file must still have no movie').toBeFalsy();
		});
	});

	test.describe('force refresh + cache', () => {
		test('force=true on initial scrape → cache bypass, job completes with the result', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// force=true sets ForceRefresh on the ScrapeCmd, bypassing the
			// scraper cache. The e2emock is deterministic, so the result is
			// identical to a non-force scrape — but the point is that the
			// force path doesn't error + still produces the result. The
			// batch-rescrape spec covers force on the RESCRAPE path; this
			// covers force on the INITIAL scrape path.
			const { status, results } = await scrapeAndAwait(request, [GOOD_FILE], {
				force: true,
			});

			expect(status, 'a force-refresh scrape must complete').toBe('completed');
			expect(results[GOOD_FILE].status, 'the file must succeed under force').toBe('completed');
			expect(results[GOOD_FILE].movie?.title).toContain('E2E Movie GOOD-001');
		});

		test('re-scrape the same ID without force → same result (cache idempotency)', async ({
			request,
		}: {
			request: APIRequestContext;
		}) => {
			await loginAgainstRealBackend(request);

			// Scrape GOOD-001 once, then scrape it again. The second scrape
			// (without force) hits the cache + returns the same result. This
			// pins that re-scraping an already-cached ID is idempotent — no
			// error, no duplicate, the same movie payload. A regression in
			// the cache lookup (e.g., a cache-miss that re-scrapes + produces
			// a conflicting result) would surface as a title mismatch.
			const first = await scrapeAndAwait(request, [GOOD_FILE]);
			const second = await scrapeAndAwait(request, [GOOD_FILE]);

			expect(first.status, 'the first scrape must complete').toBe('completed');
			expect(second.status, 'the second (cached) scrape must complete').toBe('completed');

			const firstMovie = first.results[GOOD_FILE].movie;
			const secondMovie = second.results[GOOD_FILE].movie;
			expect(firstMovie, 'the first scrape must produce a movie').toBeTruthy();
			expect(secondMovie, 'the cached re-scrape must produce a movie').toBeTruthy();
			expect(secondMovie?.title, 'the cached result must match the first scrape').toBe(
				firstMovie?.title,
			);
			expect(secondMovie?.id, 'the cached result must carry the same movie ID').toBe(
				firstMovie?.id,
			);
		});
	});
});
