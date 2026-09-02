// @ts-check
const { test, expect } = require('@playwright/test');

test.describe('orphan visits', () => {
  test('hitting an unknown short code is tracked and shown', async ({ page }) => {
    const response = await page.goto('/definitely-not-a-code');
    expect(response.status()).toBe(404);
    await expect(page.locator('h1')).toHaveText('404');

    await page.goto('/admin/visits/orphan');
    await expect(page.locator('h1')).toHaveText('Orphan visits');
    const table = page.locator('table').last();
    await expect(table.locator('tbody tr', { hasText: 'definitely-not-a-code' }).first()).toBeVisible();

    // Filter down to invalid-short-url orphans only.
    await page.click('a:has-text("Invalid short URLs")');
    await expect(page.locator('table').last().locator('tbody tr').first()).toContainText(
      'definitely-not-a-code'
    );
  });
});
