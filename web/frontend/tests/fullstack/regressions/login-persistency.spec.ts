/**
 * Login persistency full-stack spec.
 *
 * Pins the "Remember me" session-persistency contract end-to-end: a session
 * created with Remember me=true must survive a real server restart, so the
 * user is NOT prompted to log in again. This is the user-reported regression:
 * "despite selecting Remember me, I get prompted to login after server
 * restarts."
 *
 * How it works: the Playwright-managed backend (port 18080) is a single
 * long-lived process. To test restart-persistency without disrupting the
 * suite's shared backend, this spec spawns a SECOND e2e backend instance on
 * a different port (18081) from the SAME working directory. Both instances
 * share the auth.sessions.json + auth.credentials.json files (stored in the
 * repo root, gitignored). The second instance's NewAuthManager →
 * loadSessionsFromDisk() loads the persisted session, so the cookie from the
 * first instance authenticates against the second.
 *
 * What this guards:
 *   1. The login handler persists remember-me sessions to disk
 *      (writePersistentSessionsLocked).
 *   2. The cookie value is a stable session ID (not process-specific).
 *   3. A fresh process loads persisted sessions on startup
 *      (loadSessionsFromDisk in NewAuthManager).
 *   4. The loaded session authenticates (AuthenticateSession returns the
 *      username, not ErrInvalidSession).
 *
 * The TTL bug (remember-me sessions used the same 24h TTL as ephemeral
 * sessions, so they expired between daily restarts) is covered by the Go
 * unit tests in internal/api/auth/manager_test.go
 * (TestAuthManager_RememberedSessionOutlivesEphemeralTTL +
 * TestAuthManager_RememberedSessionSurvivesRestartPastEphemeralTTL). This
 * spec covers the restart-persistency contract at the HTTP layer.
 */
import { spawn, type ChildProcess } from 'node:child_process';
import { resolve } from 'node:path';
import { test, expect, type APIRequestContext } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

const RESTART_PORT = 18081;
const RESTART_BASE = `http://127.0.0.1:${RESTART_PORT}`;
// Playwright runs from web/frontend; the repo root (where cmd/ + the shared
// auth.sessions.json live) is two levels up.
const REPO_ROOT = resolve(process.cwd(), '../..');

/** Kill any stale process bound to RESTART_PORT so the spawn gets a clean port. */
async function freeRestartPort(): Promise<void> {
	// lsof + kill via a shell one-liner; ignore errors (nothing to kill).
	const { execFileSync } = await import('node:child_process');
	try {
		const pids = execFileSync('lsof', ['-ti', `:${RESTART_PORT}`], { encoding: 'utf8' })
			.trim()
			.split('\n')
			.filter(Boolean);
		for (const pid of pids) {
			try {
				process.kill(Number(pid), 'SIGKILL');
			} catch {
				// already gone
			}
		}
		if (pids.length) await new Promise((r) => setTimeout(r, 500));
	} catch {
		// lsof exits non-zero when nothing is listening — expected
	}
}

/**
 * Spawn a second e2e backend on RESTART_PORT. Returns the ChildProcess.
 * Spawned with detached:true so afterEach can kill the whole process group
 * (go run spawns a child binary that survives killing the go parent).
 */
function spawnRestartBackend(): ChildProcess {
	return spawn('go', ['run', './cmd/javinizer-e2e'], {
		cwd: REPO_ROOT,
		detached: true,
		env: {
			...process.env,
			JAVINIZER_E2E_PORT: String(RESTART_PORT),
			JAVINIZER_E2E_AUTH: 'true',
			JAVINIZER_E2E_USERNAME: 'admin',
			JAVINIZER_E2E_PASSWORD: 'adminpassword123',
		},
		stdio: 'ignore',
	});
}

/** Kill the spawned backend's whole process group (go parent + binary child). */
function killRestartBackend(child: ChildProcess | undefined): void {
	if (!child || child.pid === undefined) return;
	try {
		// Negative PID kills the entire process group (detached:true makes the
		// child a group leader). SIGKILL because SIGTERM to `go run` doesn't
		// propagate to the compiled binary child.
		process.kill(-child.pid, 'SIGKILL');
	} catch {
		// process group may already be gone
	}
}

/** Poll /health until the restarted backend is ready (or timeout). */
async function waitForBackendReady(base: string, timeoutMs = 90_000): Promise<void> {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		try {
			const resp = await fetch(`${base}/health`);
			if (resp.ok) return;
		} catch {
			// not up yet
		}
		await new Promise((r) => setTimeout(r, 500));
	}
	throw new Error(`restarted backend at ${base} did not become ready within ${timeoutMs}ms`);
}

/** Extract the raw session cookie value from a login response. */
async function loginAndCaptureCookie(
	api: APIRequestContext,
): Promise<string> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/auth/login`, {
		data: { username: 'admin', password: 'adminpassword123', remember_me: true },
	});
	expect(resp.ok(), `login failed: ${resp.status()}`).toBeTruthy();
	const setCookie = resp.headers()['set-cookie'];
	expect(setCookie, 'login must set a session cookie').toBeTruthy();
	// Parse "javinizer_session=<value>; Path=/; ..."
	const match = /javinizer_session=([^;]+)/.exec(setCookie);
	expect(match, 'the cookie must be javinizer_session').toBeTruthy();
	return match![1];
}

test.describe('Login persistency: Remember me survives server restart', () => {
	// Spawning a second e2e backend (go run + DB migration + scraper setup)
	// takes ~20-40s; allow 120s per test.
	test.setTimeout(120_000);
	let restartedBackend: ChildProcess | undefined;

	test.afterEach(() => {
		killRestartBackend(restartedBackend);
		restartedBackend = undefined;
	});

	test('a remember-me session authenticates against a freshly-started backend', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Log in with Remember me against the Playwright-managed backend.
		// The session is persisted to auth.sessions.json on disk.
		const cookieValue = await loginAndCaptureCookie(request);

		// Sanity: the session is valid on the original backend.
		const originalStatus = await request.get(`${BACKEND_BASE}/api/v1/auth/status`, {
			headers: { Cookie: `javinizer_session=${cookieValue}` },
		});
		expect(originalStatus.ok()).toBeTruthy();
		const originalBody = await originalStatus.json();
		expect(originalBody.authenticated, 'session must be valid on the original backend').toBe(true);

		// ── 2. Spawn a SECOND e2e backend on a different port, same CWD.
		// The second instance shares the repo-root working directory, so its
		// NewAuthManager("") resolves auth.sessions.json + auth.credentials.json
		// to the same files the first instance wrote. loadSessionsFromDisk()
		// runs inside NewAuthManager, loading the persisted session before the
		// test can interact with it.
		await freeRestartPort();
		restartedBackend = spawnRestartBackend();

		await waitForBackendReady(RESTART_BASE);

		// ── 3. The same cookie must authenticate against the restarted backend.
		// This is the core persistency assertion: the session survived the
		// process restart because it was persisted to disk + reloaded on
		// startup. If this fails, the user sees a login prompt after a restart.
		const restartedStatus = await request.get(`${RESTART_BASE}/api/v1/auth/status`, {
			headers: { Cookie: `javinizer_session=${cookieValue}` },
		});
		expect(restartedStatus.ok(), `auth/status on restarted backend failed: ${restartedStatus.status()}`).toBeTruthy();
		const restartedBody = await restartedStatus.json();
		expect(restartedBody.authenticated, 'remember-me session must survive server restart').toBe(true);
		expect(restartedBody.username, 'the restarted backend must recognize the session username').toBe('admin');

		// ── 4. An authenticated endpoint also works on the restarted backend.
		// Proves the session isn't just "recognized" but actually gates a
		// protected route (the /batch list requires RequireTokenOrSession).
		const protectedResp = await request.get(`${RESTART_BASE}/api/v1/batch`, {
			headers: { Cookie: `javinizer_session=${cookieValue}` },
		});
		expect(protectedResp.ok(), 'a protected endpoint must accept the persisted session').toBeTruthy();
	});

	test('a non-remember-me session does NOT survive server restart', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// ── 1. Log in WITHOUT Remember me. The session is in-memory only —
		// not persisted to auth.sessions.json. The cookie still carries the
		// session ID, but no other process can validate it.
		const resp = await request.post(`${BACKEND_BASE}/api/v1/auth/login`, {
			data: { username: 'admin', password: 'adminpassword123', remember_me: false },
		});
		expect(resp.ok()).toBeTruthy();
		const setCookie = resp.headers()['set-cookie'];
		const match = /javinizer_session=([^;]+)/.exec(setCookie);
		expect(match).toBeTruthy();
		const cookieValue = match![1];

		// ── 2. Spawn a fresh backend (shares the session file, but the
		// non-remembered session was never written to it).
		await freeRestartPort();
		restartedBackend = spawnRestartBackend();
		await waitForBackendReady(RESTART_BASE);

		// ── 3. The ephemeral session must NOT authenticate on the restarted
		// backend. This is the inverse contract: only remember-me sessions
		// survive restarts. A regression that persisted ALL sessions (not just
		// remember-me ones) would surface here.
		const restartedStatus = await request.get(`${RESTART_BASE}/api/v1/auth/status`, {
			headers: { Cookie: `javinizer_session=${cookieValue}` },
		});
		expect(restartedStatus.ok()).toBeTruthy();
		const restartedBody = await restartedStatus.json();
		expect(restartedBody.authenticated, 'ephemeral session must NOT survive restart').toBe(false);
	});
});
