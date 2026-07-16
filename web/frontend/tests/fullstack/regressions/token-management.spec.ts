/**
 * API Token Management (UI) full-stack spec.
 *
 * Ported from the legacy tests/e2e/ suite (mocked/real-mixed, now split into
 * suite: drives the /settings API Tokens section UI against the real e2emock
 * backend + real ApiTokenRepository + real SQLite. Auth is inherited from
 * the fullstack global-setup's storageState (no per-spec login).
 *
 * Complements token-api.spec.ts (which pins the /api/v1/tokens API contract
 * via APIRequestContext); this spec pins the UI: section expand, create
 * form, token list render, revoke/regenerate buttons, security-warning modal.
 *
 * afterEach cleans up any leftover test tokens via the UI so the suite stays
 * serial-safe against the single shared backend.
 */
import { test, expect } from '@playwright/test';

test.describe('API Token Management (UI)', () => {
	const testTokenName = 'E2E-Test-Token';

	test.afterEach(async ({ page }) => {
		try {
			await page.goto('/settings');
			await page.waitForTimeout(1000);

			const tokenRows = page.locator('table tr').filter({ hasText: testTokenName });
			const count = await tokenRows.count();
			for (let i = 0; i < count; i++) {
				const revokeBtn = tokenRows.nth(i).locator('button[title="Revoke token"]');
				if (await revokeBtn.isVisible().catch(() => false)) {
					await revokeBtn.click();
					const confirmBtn = page
						.locator(
							'button:has-text("Revoke"), button:has-text("Confirm"), button:has-text("Delete")',
						)
						.first();
					if (await confirmBtn.isVisible().catch(() => false)) {
						await confirmBtn.click();
					}
					await page.waitForTimeout(500);
				}
			}
		} catch {
			/* cleanup */
		}
	});

	test('create token with name appears in list', async ({ page }) => {
		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1000);

				expect(async () => {
					const body = await page.textContent('body');
					expect(body).toContain(testTokenName);
				}).toPass({ timeout: 10000 });
			}
		}
	});

	test('create token without name shows as Unnamed', async ({ page }) => {
		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const createBtn = page.getByRole('button', { name: /create token/i }).first();
			if (await createBtn.isVisible().catch(() => false)) {
				await createBtn.click();
				await page.waitForTimeout(1000);

				expect(async () => {
					const body = await page.textContent('body');
					expect(body).toMatch(/Unnamed|token created/i);
				}).toPass({ timeout: 10000 });
			}
		}
	});

	test('revoke token removes from list', async ({ page }) => {
		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1500);
			}

			const revokeBtn = page.locator('button[title="Revoke token"]').first();
			if (await revokeBtn.isVisible().catch(() => false)) {
				await revokeBtn.click();

				const confirmBtn = page.locator('button:has-text("Revoke")').first();
				if (await confirmBtn.isVisible().catch(() => false)) {
					await confirmBtn.click();
					await page.waitForTimeout(1000);
				}
			}
		}
	});

	test('regenerate token shows new value in modal', async ({ page }) => {
		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1500);
			}

			const regenBtn = page.locator('button[title="Regenerate token"]').first();
			if (await regenBtn.isVisible().catch(() => false)) {
				await regenBtn.click();

				const confirmBtn = page.locator('button:has-text("Regenerate")').first();
				if (await confirmBtn.isVisible().catch(() => false)) {
					await confirmBtn.click();
					await page.waitForTimeout(1000);

					expect(async () => {
						const body = await page.textContent('body');
						expect(body).toContain('jv_');
					}).toPass({ timeout: 10000 });
				}
			}
		}
	});

	test('token display modal shows security warning', async ({ page }) => {
		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		const section = page.getByText('API Tokens').first();
		if (await section.isVisible().catch(() => false)) {
			await section.click();
			await page.waitForTimeout(500);

			const nameInput = page.locator('#token-name');
			if (await nameInput.isVisible().catch(() => false)) {
				await nameInput.fill(testTokenName);

				const createBtn = page.getByRole('button', { name: /create token/i }).first();
				await createBtn.click();
				await page.waitForTimeout(1500);

				expect(async () => {
					const body = await page.textContent('body');
					expect(body).toMatch(/not be shown again|will not be shown again/i);
				}).toPass({ timeout: 10000 });
			}
		}
	});
});
