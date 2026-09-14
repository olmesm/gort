// @ts-check
const { test, expect } = require('@playwright/test');

test('tab navigation keeps text stable while the font loads', async ({ page }) => {
  await page.goto('/admin');
  await page.evaluate(() => document.fonts.ready);
  // Force a slow font response on each new document to expose fallback swaps.
  await page.route('**/inter-var.woff2*', async route => {
    await new Promise(resolve => setTimeout(resolve, 500));
    await route.continue();
  });
  await page.addInitScript(() => {
    const sample = () => {
      const links = [...document.querySelectorAll('.topbar nav a')];
      if (links.length) {
        performance.mark('navigation-layout', {
          detail: links.map(link => {
            const { x, y, width, height } = link.getBoundingClientRect();
            return { x, y, width, height };
          }),
        });
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
  for (const tab of ['Tags', 'Domains', 'Overview']) {
    await page.locator('.topbar nav').getByRole('link', { name: tab, exact: true }).click();
    await expect(page.locator('h1')).toHaveText(tab);
    await page.evaluate(async () => {
      await document.fonts.ready;
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    });
    const samples = await page.evaluate(() =>
      performance.getEntriesByName('navigation-layout').map(entry =>
        (/** @type {PerformanceMark} */ (entry)).detail));
    expect(samples.length).toBeGreaterThan(1);
    for (const boxes of samples) {
      expect(boxes, `${tab} navigation moved while loading the font`).toEqual(samples[0]);
    }
  }
});

test('tab navigation reuses the downloaded font', async ({ page }) => {
  await page.goto('/admin');
  await page.evaluate(() => document.fonts.ready);
  for (const tab of ['Tags', 'Domains', 'Overview']) {
    await page.locator('.topbar nav').getByRole('link', { name: tab, exact: true }).click();
    await expect(page.locator('h1')).toHaveText(tab);
    const font = await page.evaluate(async () => {
      await document.fonts.ready;
      const url = new URL('/inter-var.woff2', location.href).href;
      const entry = /** @type {PerformanceResourceTiming} */ (performance.getEntriesByName(url).at(-1));
      return entry && { transferSize: entry.transferSize, decodedBodySize: entry.decodedBodySize };
    });
    expect(font).toBeDefined();
    expect(font.decodedBodySize).toBeGreaterThan(0);
    expect(font.transferSize).toBe(0);
  }
});
