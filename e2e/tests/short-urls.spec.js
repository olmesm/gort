// @ts-check
const { test, expect } = require('@playwright/test');

test.describe.configure({ mode: 'serial' });

const SLUG = 'e2e-journey';

test.describe('short URL lifecycle', () => {
  test('create a short URL through the form', async ({ page, baseURL }) => {
    await page.goto('/admin/short-urls/new');
    // Point the long URL at this app's own landing page so the final
    // browser navigation can be verified end-to-end without internet access.
    await page.fill('input[name="longUrl"]', `${baseURL}/`);
    await page.fill('input[name="customSlug"]', SLUG);
    await page.fill('input[name="tags"]', 'e2e, journey');
    await page.click('button:has-text("Create short URL")');

    await expect(page).toHaveURL(/\/admin\/short-urls$/);
    const row = page.locator('tr', { hasText: SLUG });
    await expect(row).toHaveCount(1);
    await expect(row.locator('.badge').first()).toContainText('e2e');
  });

  test('duplicate slugs are rejected with an inline error', async ({ page, baseURL }) => {
    await page.goto('/admin/short-urls/new');
    await page.fill('input[name="longUrl"]', `${baseURL}/`);
    await page.fill('input[name="customSlug"]', SLUG);
    await page.click('button:has-text("Create short URL")');
    await expect(page.locator('.alert.error')).toContainText('already in use');
  });

  test('htmx live search filters the list without a reload', async ({ page }) => {
    await page.goto('/admin/short-urls');
    await expect(page.locator('#su-table tbody tr')).toHaveCount(1);

    await page.fill('input[name="search"]', 'no-such-thing-xyz');
    await expect(page.locator('#su-table tbody tr')).toHaveCount(0);

    await page.fill('input[name="search"]', SLUG);
    await expect(page.locator('#su-table tbody tr')).toHaveCount(1);
    // Full page was never reloaded: htmx swapped only the table fragment.
    await expect(page.locator('h1')).toHaveText('Short URLs');
  });

  test('edit the title and see it in the list', async ({ page }) => {
    await page.goto('/admin/short-urls');
    await page.locator('tr', { hasText: SLUG }).locator('a:has-text("Edit")').click();
    await expect(page.locator('h1')).toHaveText('Edit short URL');

    await page.fill('input[name="title"]', 'E2E Journey Page');
    await page.click('button:has-text("Save changes")');
    await expect(page.locator('input[name="title"]')).toHaveValue('E2E Journey Page');

    await page.goto('/admin/short-urls');
    await expect(page.locator('tr', { hasText: SLUG })).toContainText('E2E Journey Page');
  });

  test('add a device redirect rule through the builder', async ({ page, baseURL }) => {
    await page.goto('/admin/short-urls');
    await page.locator('tr', { hasText: SLUG }).locator('a:has-text("Edit")').click();

    await page.fill('input[name="ruleLongUrl"]', `${baseURL}/admin/login`);
    await page.selectOption('select[name="device"]', 'android');
    await page.click('button:has-text("Add rule")');

    await expect(page.locator('.rule-conditions li')).toContainText('Device is android');

    // Remove it again so the redirect test below hits the default target.
    await page.click('button:has-text("Remove")');
    await expect(page.locator('.rule-conditions li')).toHaveCount(0);
  });

  test('the short URL redirects a real browser to its target', async ({ page, baseURL }) => {
    await page.goto(`/${SLUG}`);
    // Landed on the app's landing page (the configured long URL).
    await expect(page).toHaveURL(`${baseURL}/`);
    await expect(page.locator('h1')).toHaveText('Gort');
  });

  test('the visit shows up in the analytics page', async ({ page }) => {
    await page.goto('/admin/short-urls');
    const row = page.locator('tr', { hasText: SLUG });
    await row.locator('td:nth-child(5) a').click();

    await expect(page.locator('h1')).toContainText('Visits');
    await expect(page.locator('.chart-card svg')).toBeVisible();
    const visitRow = page.locator('table').last().locator('tbody tr').first();
    await expect(visitRow).toBeVisible();
    // Headless Chromium advertises itself as HeadlessChrome, so bot
    // detection correctly flags this visit.
    await expect(visitRow.locator('.badge.red')).toHaveText('bot');
  });

  test('delete the short URL and its slug stops resolving', async ({ page }) => {
    await page.goto('/admin/short-urls');
    await page.locator('tr', { hasText: SLUG }).locator('a:has-text("Edit")').click();
    page.on('dialog', (d) => d.accept());
    await page.click('button:has-text("Delete short URL")');
    await expect(page).toHaveURL(/\/admin\/short-urls$/);
    await expect(page.locator('tr', { hasText: SLUG })).toHaveCount(0);

    const response = await page.goto(`/${SLUG}`);
    expect(response.status()).toBe(404);
  });
});
