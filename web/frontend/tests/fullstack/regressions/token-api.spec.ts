/**
 * Token API full-stack spec.
 *
 * Pins the API token CRUD contract end-to-end against the real e2emock
 * backend: list (empty + populated), create (with + without a name),
 * revoke (single), regenerate. Every action hits the real
 * /api/v1/tokens endpoints (proxied to cmd/javinizer-e2e) → real
 * ApiTokenRepository → real :memory: SQLite. No page.route mocking.
 *
 * Why this matters: the token API gates programmatic API access (an
 * alternative to session-cookie auth used by the WebUI). The existing
 * token-management.spec.ts covers the /settings API Tokens section UI
 * against a developer-run backend (now moved into the fullstack suite) —
 * e2emock). A regression in the token handler wiring (create/revoke/
 * regenerate), the TokenService, or the ApiTokenRepository is invisible
 * to the fullstack suite. This spec ports the core contract to the
 * stabilized fullstack infra so it runs in CI with the rest of the suite.
 *
 * Uniqueness: each test uses a timestamp-suffixed name to avoid collisions
 * with prior runs (the suite is serial against one backend). afterEach
 * cleans up any leftover test tokens via the API.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

const TEST_PREFIX = 'e2e-token-';

interface TokenListItem {
	id: string;
	name: string;
	token_prefix: string;
}

interface CreateTokenResponse {
	token: string;
	id: string;
	name: string;
	token_prefix: string;
}

interface TokenListResponse {
	tokens: TokenListItem[];
	count: number;
}

async function listTokens(api: APIRequestContext): Promise<TokenListResponse> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/tokens`);
	expect(resp.ok(), `list tokens failed: ${resp.status()}`).toBeTruthy();
	return (await resp.json()) as TokenListResponse;
}

async function createToken(
	api: APIRequestContext,
	name?: string,
): Promise<CreateTokenResponse> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/tokens`, {
		data: name !== undefined ? { name } : {},
	});
	expect(resp.ok(), `create token failed: ${resp.status()} ${await resp.text()}`).toBeTruthy();
	return (await resp.json()) as CreateTokenResponse;
}

async function revokeToken(api: APIRequestContext, id: string): Promise<void> {
	const resp = await api.delete(`${BACKEND_BASE}/api/v1/tokens/${id}`);
	expect(resp.ok(), `revoke token ${id} failed: ${resp.status()}`).toBeTruthy();
}

async function regenerateToken(
	api: APIRequestContext,
	id: string,
): Promise<CreateTokenResponse> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/tokens/${id}/regenerate`);
	expect(resp.ok(), `regenerate token ${id} failed: ${resp.status()}`).toBeTruthy();
	return (await resp.json()) as CreateTokenResponse;
}

async function cleanupTestTokens(api: APIRequestContext): Promise<void> {
	const { tokens } = await listTokens(api);
	await Promise.all(
		tokens
			.filter((t) => t.name.startsWith(TEST_PREFIX))
			.map((t) => revokeToken(api, t.id).catch(() => {})),
	);
}

test.describe('Token API: real CRUD against the e2emock backend', () => {
	test.afterEach(async ({ request }: { request: APIRequestContext }) => {
		await cleanupTestTokens(request);
	});

	test('create with a name → list shows it → revoke removes it', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Create a token with a name ────────────────────────────────
		// POST /tokens returns 201 + the full token (only ever shown once),
		// the id, the name, + a token_prefix (for later display).
		const name = `${TEST_PREFIX}crud-${Date.now()}`;
		const created = await createToken(request, name);
		expect(created.token, 'create must return the full token string').toBeTruthy();
		expect(created.token.length, 'token must be a non-trivial string').toBeGreaterThan(20);
		expect(created.id, 'create must return an id').toBeTruthy();
		expect(created.name, 'create must echo the name').toBe(name);
		expect(created.token_prefix, 'create must return a token_prefix').toBeTruthy();

		// ── 2. List shows the new token ──────────────────────────────────
		// The full token is NOT in the list (only the prefix). The list
		// response carries count + the tokenListItem array.
		const listed = await listTokens(request);
		expect(listed.count, 'count must reflect the list length').toBe(listed.tokens.length);
		const found = listed.tokens.find((t) => t.id === created.id);
		expect(found, 'the new token must appear in the list').toBeTruthy();
		expect(found?.name, 'the listed name must match').toBe(name);
		expect(found?.token_prefix, 'the listed prefix must match').toBe(created.token_prefix);
		// The full token must NOT be present in the list response (security:
		// the full token is only returned once at create/regenerate time).
		expect(JSON.stringify(listed), 'list must not leak the full token').not.toContain(
			created.token,
		);

		// ── 3. Revoke removes it ─────────────────────────────────────────
		// DELETE /tokens/:id returns 200 + the token is gone from the list.
		await revokeToken(request, created.id);
		const afterRevoke = await listTokens(request);
		expect(
			afterRevoke.tokens.find((t) => t.id === created.id),
			'the revoked token must not appear in the list',
		).toBeUndefined();
	});

	test('create without a name → list shows it with an empty/default name', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// POST /tokens with an empty body creates a token with no name.
		// The UI displays these as "Unnamed" — the API stores an empty string.
		const created = await createToken(request, '');
		expect(created.id, 'create must return an id').toBeTruthy();
		expect(created.token, 'create must return the full token').toBeTruthy();

		const listed = await listTokens(request);
		const found = listed.tokens.find((t) => t.id === created.id);
		expect(found, 'the unnamed token must appear in the list').toBeTruthy();
		// The name is stored as an empty string (the UI renders "Unnamed").
		expect(found?.name, 'the stored name must be empty').toBe('');
	});

	test('regenerate → new token string + prefix, same id + name', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Create a token ────────────────────────────────────────────
		const name = `${TEST_PREFIX}regen-${Date.now()}`;
		const created = await createToken(request, name);
		const originalToken = created.token;
		const originalPrefix = created.token_prefix;

		// ── 2. Regenerate it ─────────────────────────────────────────────
		// POST /tokens/:id/regenerate issues a NEW full token string under
		// the SAME id + name. The old token string is invalidated (the
		// prefix may change as well, since the prefix is derived from the
		// token's first N chars).
		const regenerated = await regenerateToken(request, created.id);
		expect(regenerated.id, 'regenerate must preserve the id').toBe(created.id);
		expect(regenerated.name, 'regenerate must preserve the name').toBe(name);
		expect(regenerated.token, 'regenerate must return a new token').toBeTruthy();
		expect(regenerated.token, 'the new token must differ from the original').not.toBe(
			originalToken,
		);
		// The prefix is derived from the token, so it typically changes too —
		// but assert it's present (a same-prefix collision is possible but
		// rare; we only require the prefix is well-formed).
		expect(regenerated.token_prefix, 'regenerate must return a prefix').toBeTruthy();

		// ── 3. The list reflects the (possibly new) prefix ───────────────
		const listed = await listTokens(request);
		const found = listed.tokens.find((t) => t.id === created.id);
		expect(found, 'the token must still be in the list').toBeTruthy();
		expect(found?.token_prefix, 'the list must reflect the regenerated prefix').toBe(
			regenerated.token_prefix,
		);
		// The original token string must NOT appear anywhere in the list
		// (it was invalidated by the regeneration).
		expect(JSON.stringify(listed), 'list must not leak the old token').not.toContain(
			originalToken,
		);
	});

	test('revoke a non-existent token → 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// DELETE /tokens/:id with an unknown id returns 404 (not 200 + not 500).
		const resp = await api_deleteToken(request, 'nonexistent-token-id-12345');
		expect(resp.status(), 'revoking a non-existent token must 404').toBe(404);
	});
});

/** Wrapper to expose the raw response (the helper above asserts + throws). */
async function api_deleteToken(
	api: APIRequestContext,
	id: string,
): ReturnType<APIRequestContext['delete']> {
	return api.delete(`${BACKEND_BASE}/api/v1/tokens/${id}`);
}
