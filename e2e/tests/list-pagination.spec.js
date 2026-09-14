// @ts-check
const { test, expect } = require('@playwright/test');

test('100 webhooks paginate and retain combined filters', async ({ page, request }, testInfo) => {
  await page.goto('/admin/api-keys');
  const createKey = page.locator('form[action="/admin/api-keys"][method="post"]');
  await createKey.locator('input[name="name"]').fill('pagination-seed');
  await createKey.getByRole('button', { name: 'Create', exact: true }).click();
  const key = await page.locator('.alert.success .mono').innerText();
  const webhookIDs = [];
  try {
    for (let i = 0; i < 100; i++) {
      const response = await request.post('/rest/v1/webhooks', {
        headers: { 'X-Api-Key': key },
        data: {
          name: `pagination-hook-${String(i).padStart(3, '0')}`,
          url: `https://example.test/hooks/${i}`,
          events: i % 2 ? ['url.created', 'visit.recorded'] : ['url.created'],
        },
      });
      expect(response.status()).toBe(201);
      webhookIDs.push((await response.json()).id);
    }

    await page.goto('/admin/webhooks?search=pagination-hook-');
    await expect(page.locator('tbody tr')).toHaveCount(25);
    await expect(page.locator('.pager')).toContainText('Page 1 of 4 · 100 items');
    await page.getByRole('link', { name: 'Next →' }).click();
    await expect(page).toHaveURL(/page=2.*search=pagination-hook-/);
    await expect(page.locator('tbody tr').first()).toContainText('pagination-hook-025');

    const filters = page.getByRole('form', { name: 'List filters' });
    await filters.getByLabel('Status').selectOption('enabled');
    await filters.getByLabel('Event', { exact: true }).selectOption('visit.recorded');
    await filters.getByRole('button', { name: 'Filter', exact: true }).click();
    await expect(page.locator('.pager')).toContainText('Page 1 of 2 · 50 items');
    await page.getByRole('link', { name: 'Next →' }).click();
    await expect(filters.getByLabel('Status')).toHaveValue('enabled');
    await expect(filters.getByLabel('Event', { exact: true })).toHaveValue('visit.recorded');
    await expect(page.locator('.pager')).toContainText('Page 2 of 2 · 50 items');
    await page.screenshot({ path: testInfo.outputPath('webhooks-desktop.png'), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(() => window.scrollTo(0, 0));
    const header = await page.locator('.topbar').boundingBox();
    const heading = await page.locator('h1').boundingBox();
    expect(heading.y).toBeGreaterThanOrEqual(header.y + header.height);
    await page.screenshot({ path: testInfo.outputPath('webhooks-mobile.png'), fullPage: true });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

    await filters.getByLabel('Search', { exact: true }).fill('no-matching-webhook');
    await filters.getByRole('button', { name: 'Filter', exact: true }).click();
    await expect(page.getByText('No webhooks match these filters.')).toBeVisible();
    await expect(page.locator('.pager')).toContainText('Page 1 of 1 · 0 items');
    await filters.getByRole('link', { name: 'Clear filters' }).click();
    await expect(filters.getByLabel('Search', { exact: true })).toHaveValue('');
    await expect(filters.getByLabel('Status')).toHaveValue('');
  } finally {
    for (const id of webhookIDs) {
      const response = await request.delete(`/rest/v1/webhooks/${id}`, { headers: { 'X-Api-Key': key } });
      expect(response.status()).toBe(204);
    }
  }
});
