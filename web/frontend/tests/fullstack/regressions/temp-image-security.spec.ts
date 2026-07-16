/**
 * Temp image API full-stack spec.
 *
 * Pins the security guards on the temp-image + poster-serving endpoints
 * end-to-end against the real e2emock backend: path-traversal rejection
 * (serveTempPoster, serveCroppedPoster), .jpg extension enforcement,
 * missing-URL rejection, invalid-URL rejection, + SSRF protection
 * (serveTempImage). Every assertion hits the real /api/v1/temp/* +
 * /api/v1/posters/* endpoints (proxied to cmd/javinizer-e2e).
 *
 * Why this matters: these endpoints serve files from disk + fetch remote
 * URLs — path traversal + SSRF are the two highest-severity risks in the
 * API surface. The guards (isSafePathSegment, ssrf.CheckURL, the .jpg
 * suffix check, the posterDir prefix defense-in-depth) had ZERO e2e
 * coverage. A regression that weakens any guard (e.g., removing the
 * filepath.Base check, allowing a non-.jpg extension, or bypassing
 * ssrf.CheckURL) would open a security hole with no test signal. This
 * spec pins the rejection contract for each guard.
 *
 * Scope: these tests exercise the GUARDS (the rejection paths), which
 * fire before any file/URL fetch — so they don't need real poster files
 * or network egress. The happy-path "serve a real temp poster" is
 * covered indirectly by poster-cover-reactivity.spec.ts (which crops a
 * poster + views it). This spec focuses on the security-relevant
 * rejections that have no coverage.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

test.describe('Temp image API: security guards reject path traversal + invalid URLs', () => {
	test('GET /temp/posters/:jobId/:filename rejects path traversal in jobId', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// jobId=".." would resolve posters/.. to the temp root — the
		// isSafePathSegment guard rejects "." + ".." + any path separator.
		// The guard fires before the .jpg check, so the filename is valid here
		// to isolate the jobId guard.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/temp/posters/../poster.jpg`);
		expect(resp.status(), 'jobId=".." must be rejected (404, not 200/500)').toBe(404);
	});

	test('GET /temp/posters/:jobId/:filename rejects path traversal in filename', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// filename="../etc/passwd" — the isSafePathSegment guard rejects
		// path separators. Even with a .jpg-less filename, the guard fires
		// first + returns 404.
		const resp = await request.get(
			`${BACKEND_BASE}/api/v1/temp/posters/validjob/..%2Fetc%2Fpasswd`,
		);
		expect(resp.status(), 'filename with path separators must be rejected').toBe(404);
	});

	test('GET /temp/posters/:jobId/:filename rejects non-.jpg extensions', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// A safe-path filename with a non-.jpg extension (e.g., .png) is
		// rejected by the .jpg suffix check. This prevents serving arbitrary
		// file types from the temp dir.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/temp/posters/validjob/poster.png`);
		expect(resp.status(), 'non-.jpg extensions must be rejected').toBe(404);
	});

	test('GET /posters/:filename rejects path traversal', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// serveCroppedPoster has the same isSafePathSegment guard. filename
		// with a path separator is rejected before the posterDir prefix check.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/posters/..%2Fsecret.jpg`);
		expect(resp.status(), 'path traversal in /posters/:filename must be rejected').toBe(404);
	});

	test('GET /posters/:filename rejects non-.jpg extensions', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const resp = await request.get(`${BACKEND_BASE}/api/v1/posters/poster.txt`);
		expect(resp.status(), 'non-.jpg extensions in /posters must be rejected').toBe(404);
	});

	test('GET /temp/image without a url param returns 400', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// serveTempImage requires ?url=... — missing it returns 400 (not 500).
		const resp = await request.get(`${BACKEND_BASE}/api/v1/temp/image`);
		expect(resp.status(), 'missing url param must 400').toBe(400);
		const body = await resp.json();
		expect(body.error, 'the 400 must explain the required param').toContain('url');
	});

	test('GET /temp/image with a non-http(s) scheme returns 400', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// file:// + empty host are rejected by the scheme/host validation
		// (before the SSRF check). This blocks javascript:/data:/file: URLs.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/temp/image?url=file:///etc/passwd`);
		expect(resp.status(), 'non-http(s) schemes must 400').toBe(400);
	});

	test('GET /temp/image with a malformed URL returns 400', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// A non-parseable URL string is rejected.
		const resp = await request.get(`${BACKEND_BASE}/api/v1/temp/image?url=not-a-url`);
		expect(resp.status(), 'malformed URLs must 400').toBe(400);
	});

	test('GET /temp/image with a localhost URL is rejected by SSRF guard (403)', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// A well-formed http:// URL pointing at localhost/loopback is
		// rejected by ssrf.CheckURL (403). This is the core SSRF guard —
		// without it, an attacker could proxy requests to internal services.
		// The exact status is 403 (Forbidden) from the SSRF check, distinct
		// from the 400 (Bad Request) input-validation rejections above.
		const resp = await request.get(
			`${BACKEND_BASE}/api/v1/temp/image?url=http://127.0.0.1:18080/api/v1/health`,
		);
		expect(resp.status(), 'localhost URLs must be rejected by the SSRF guard').toBe(403);
	});
});
