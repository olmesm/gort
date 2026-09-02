// @ts-check
const { test, expect } = require('@playwright/test');

test.describe.configure({ mode: 'serial' });

test.describe('tag management', () => {
  test('tags created with a short URL appear with stats', async ({ page, baseURL }) => {
    await page.goto('/admin/short-urls/new');
    await page.fill('input[name="longUrl"]', `${baseURL}/`);
    await page.fill('input[name="customSlug"]', 'tag-holder');
    await page.fill('input[name="tags"]', 'renameme');
    await page.click('button:has-text("Create short URL")');

    await page.goto('/admin/tags');
    const row = page.locator('tr', { hasText: 'renameme' });
    await expect(row).toHaveCount(1);
    await expect(row.locator('td').nth(1)).toContainText('1'); // one short URL
  });

  test('rename a tag inline', async ({ page }) => {
    await page.goto('/admin/tags');
    const row = page.locator('tr', { hasText: 'renameme' });
    await row.locator('input[name="newName"]').fill('renamed-tag');
    await row.locator('button:has-text("Rename")').click();

    await expect(page.locator('tr', { hasText: 'renamed-tag' })).toHaveCount(1);
    await expect(page.locator('.badge', { hasText: /^renameme$/ })).toHaveCount(0);

    // The short URL now carries the renamed tag.
    await page.goto('/admin/short-urls');
    await expect(page.locator('tr', { hasText: 'tag-holder' })).toContainText('renamed-tag');
  });

  test('delete a tag', async ({ page }) => {
    await page.goto('/admin/tags');
    page.on('dialog', (d) => d.accept());
    await page
      .locator('tr', { hasText: 'renamed-tag' })
      .locator('button:has-text("Delete")')
      .click();
    await expect(page.locator('tr', { hasText: 'renamed-tag' })).toHaveCount(0);
  });
});
