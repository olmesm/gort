// @ts-check
const { test, expect } = require('@playwright/test');

test('domain text, counts, inputs and actions share a vertical centre', async ({ page }, testInfo) => {
  await page.goto('/admin/domains');
  await page.locator('input[name="authority"]').fill('row-alignment.example.test');
  await page.getByRole('button', { name: 'Add domain', exact: true }).click();
  for (const width of [1440, 1024, 390]) {
    await page.setViewportSize({ width, height: 900 });
    const row = page.locator('tbody tr', { hasText: 'row-alignment.example.test' });
    const centres = await row.evaluate(element => {
      const centre = rect => rect.y + rect.height / 2;
      const values = [...element.querySelectorAll('input, button')].map(el => centre(el.getBoundingClientRect()));
      for (const cell of [...element.querySelectorAll('td')].slice(0, 3)) {
        const range = document.createRange();
        range.selectNodeContents(cell);
        values.push(centre(range.getBoundingClientRect()));
      }
      return values;
    });
    await page.screenshot({ path: testInfo.outputPath(`domains-${width}.png`), fullPage: true });
    expect(Math.max(...centres) - Math.min(...centres), `row alignment at ${width}px`).toBeLessThan(3);
  }
});
