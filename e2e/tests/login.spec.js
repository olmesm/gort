// @ts-check
const { test, expect } = require('@playwright/test');

// These tests exercise the auth flow itself, so start signed out.
test.use({ storageState: { cookies: [], origins: [] } });

test.describe('authentication', () => {
  test('wrong credentials show an error and stay on the login page', async ({ page }) => {
    await page.goto('/admin/login');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'wrong-password');
    await page.click('button:has-text("Log in")');
    await expect(page.locator('.alert.error')).toContainText('Invalid username or password');
    await expect(page.locator('input[name="username"]')).toBeVisible();
  });

  test('dashboard pages redirect anonymous visitors to login', async ({ page }) => {
    await page.goto('/admin/short-urls');
    await expect(page).toHaveURL(/\/admin\/login/);
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });

  test('login lands on the overview and logout returns to login', async ({ page }) => {
    await page.goto('/admin/login');
    await page.fill('input[name="username"]', 'admin');
    await page.fill('input[name="password"]', 'e2e-password-123');
    await page.click('button:has-text("Log in")');
    await expect(page.locator('h1')).toHaveText('Overview');
    await expect(page.locator('.stat')).toHaveCount(5);

    await page.click('button:has-text("Log out")');
    await expect(page).toHaveURL(/\/admin\/login/);

    // The session is really gone.
    await page.goto('/admin');
    await expect(page).toHaveURL(/\/admin\/login/);
  });
});
