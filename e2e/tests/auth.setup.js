// @ts-check
const { test: setup, expect } = require('@playwright/test');

setup('log in as admin and save session', async ({ page }) => {
  await page.goto('/admin/login');
  await page.fill('input[name="username"]', 'admin');
  await page.fill('input[name="password"]', 'e2e-password-123');
  await page.click('button:has-text("Log in")');
  await expect(page.locator('h1')).toHaveText('Overview');
  await page.context().storageState({ path: '.auth/admin.json' });
});
