// @ts-check
const { test, expect } = require('@playwright/test');

test.describe.configure({ mode: 'serial' });

// Admin-side link grouping (group-scoped *visibility* needs an OIDC login,
// which the e2e stack does not run; that path is covered by the Go
// integration tests against a fake IdP).
test.describe('link groups', () => {
  test('an admin can create a grouped short URL and see its badge', async ({ page, baseURL }) => {
    await page.goto('/admin/short-urls/new');
    await page.fill('input[name="longUrl"]', `${baseURL}/`);
    await page.fill('input[name="customSlug"]', 'grouped-e2e');
    await page.fill('input[name="group"]', 'team-e2e');
    await page.click('button:has-text("Create short URL")');

    await expect(page).toHaveURL(/\/admin\/short-urls$/);
    const row = page.locator('tr', { hasText: 'grouped-e2e' });
    await expect(row.locator('.badge.gray', { hasText: 'team-e2e' })).toBeVisible();
  });

  test('the group filter narrows the list', async ({ page }) => {
    await page.goto('/admin/short-urls');
    await page.selectOption('select[name="group"]', 'team-e2e');
    await expect(page.locator('#su-table tbody tr')).toHaveCount(1);
    await expect(page.locator('#su-table tbody tr').first()).toContainText('grouped-e2e');
  });

  test('clearing the group in the edit form ungroups the link', async ({ page }) => {
    await page.goto('/admin/short-urls');
    await page.locator('tr', { hasText: 'grouped-e2e' }).locator('a:has-text("Edit")').click();
    await page.fill('input[name="group"]', '');
    await page.click('button:has-text("Save changes")');
    await expect(page.locator('input[name="group"]')).toHaveValue('');

    await page.goto('/admin/short-urls');
    const row = page.locator('tr', { hasText: 'grouped-e2e' });
    await expect(row.locator('.badge.gray', { hasText: 'team-e2e' })).toHaveCount(0);

    // Clean up so other specs' counts stay stable.
    await row.locator('a:has-text("Edit")').click();
    page.on('dialog', (d) => d.accept());
    await page.click('button:has-text("Delete short URL")');
    await expect(page).toHaveURL(/\/admin\/short-urls$/);
  });
});
