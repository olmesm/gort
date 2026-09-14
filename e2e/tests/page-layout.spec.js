// @ts-check
const { test, expect } = require('@playwright/test');

test('management pages fit desktop and mobile viewports', async ({ page }) => {
  const pages = [
    ['/admin', 'Overview'], ['/admin/short-urls', 'Short URLs'],
    ['/admin/tags', 'Tags'], ['/admin/domains', 'Domains'],
    ['/admin/api-keys', 'API keys'], ['/admin/webhooks', 'Webhooks'],
    ['/admin/users', 'Users'], ['/admin/visits/orphan', 'Orphan visits'],
    ['/admin/short-urls/new', 'New short URL'], ['/graphql/docs', 'GraphQL'],
  ];
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const [path, title] of pages) {
      const response = await page.goto(path);
      expect(response.status()).toBe(200);
      await expect(page.locator('h1')).toContainText(title);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${path} at ${width}`).toBe(true);
      const header = await page.locator('.topbar').boundingBox();
      const heading = await page.locator('h1').boundingBox();
      expect(heading.y).toBeGreaterThanOrEqual(header.y + header.height);
    }
  }
});
