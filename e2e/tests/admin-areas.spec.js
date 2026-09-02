// @ts-check
const { test, expect } = require('@playwright/test');

test.describe.configure({ mode: 'serial' });

test.describe('admin-only areas', () => {
  test('create an API key and see the plaintext exactly once', async ({ page }) => {
    await page.goto('/admin/api-keys');
    await page.fill('input[name="name"]', 'e2e-key');
    await page.selectOption('select[name="role"]', 'author');
    await page.click('button:has-text("Create")');

    const banner = page.locator('.alert.success');
    await expect(banner).toContainText('copy it now');
    await expect(banner.locator('.mono')).toContainText(/^gort_/);

    const row = page.locator('tr', { hasText: 'e2e-key' });
    await expect(row.locator('.badge', { hasText: 'author' })).toBeVisible();
    await expect(row.locator('.badge.green')).toHaveText('enabled');
  });

  test('disable and re-enable the API key', async ({ page }) => {
    await page.goto('/admin/api-keys');
    const row = page.locator('tr', { hasText: 'e2e-key' });
    await row.locator('button:has-text("Disable")').click();
    await expect(page.locator('tr', { hasText: 'e2e-key' }).locator('.badge.red')).toHaveText('disabled');
    await page.locator('tr', { hasText: 'e2e-key' }).locator('button:has-text("Enable")').click();
    await expect(page.locator('tr', { hasText: 'e2e-key' }).locator('.badge.green')).toHaveText('enabled');
  });

  test('register an extra domain', async ({ page }) => {
    await page.goto('/admin/domains');
    await expect(page.locator('.badge.green', { hasText: 'default' })).toBeVisible();
    await page.fill('input[name="authority"]', 'links.e2e.test');
    await page.click('button:has-text("Add domain")');
    await expect(page.locator('tr', { hasText: 'links.e2e.test' })).toHaveCount(1);
  });

  test('create a webhook and see its secret once', async ({ page, baseURL }) => {
    await page.goto('/admin/webhooks');
    await page.fill('input[name="name"]', 'e2e-hook');
    await page.fill('input[name="url"]', `${baseURL}/rest/health`);
    await page.check('input[name="event_visit_recorded"]');
    await page.click('button:has-text("Create webhook")');

    await expect(page.locator('.alert.success')).toContainText('signing secret');
    const row = page.locator('tr', { hasText: 'e2e-hook' });
    await expect(row.locator('.badge.gray', { hasText: 'url.created' })).toBeVisible();
    await expect(row.locator('.badge.gray', { hasText: 'visit.recorded' })).toBeVisible();
  });

  test('create a regular user who sees no admin navigation', async ({ page, browser, baseURL }) => {
    await page.goto('/admin/users');
    await page.fill('input[name="username"]', 'viewer');
    await page.locator('form.row input[name="password"]').fill('viewer-pass-123');
    await page.click('button:has-text("Create user")');
    await expect(page.locator('tr', { hasText: 'viewer' })).toHaveCount(1);

    // Fresh signed-out session for the new non-admin user. newContext()
    // inherits the project's storageState (admin cookies), so clear it.
    const context = await browser.newContext({
      baseURL,
      storageState: { cookies: [], origins: [] },
    });
    const userPage = await context.newPage();
    await userPage.goto('/admin/login');
    await userPage.fill('input[name="username"]', 'viewer');
    await userPage.fill('input[name="password"]', 'viewer-pass-123');
    await userPage.click('button:has-text("Log in")');
    await expect(userPage.locator('h1')).toHaveText('Overview');

    const nav = userPage.locator('.topbar nav');
    await expect(nav.locator('a', { hasText: 'Short URLs' })).toBeVisible();
    await expect(nav.locator('a', { hasText: 'Users' })).toHaveCount(0);
    await expect(nav.locator('a', { hasText: 'API keys' })).toHaveCount(0);
    await expect(nav.locator('a', { hasText: 'Webhooks' })).toHaveCount(0);

    // Deep-linking into an admin page is refused too.
    const response = await userPage.goto('/admin/users');
    expect(response.status()).toBe(403);
    await context.close();
  });

  test('the last admin cannot be deleted or demoted', async ({ page }) => {
    await page.goto('/admin/users');
    const adminRow = page.locator('tr', { hasText: /admin.*you/s }).first();
    // No delete button for yourself / last admin; role select disabled.
    await expect(adminRow.locator('button:has-text("Delete")')).toHaveCount(0);
    await expect(adminRow.locator('select[name="role"]')).toBeDisabled();
  });
});
