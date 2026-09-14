// @ts-check
const { test, expect } = require('@playwright/test');

test('API docs load locally and GraphQL runs with an API key', async ({ page, baseURL }) => {
  const externalRequests = [];
  const errors = [];
  page.on('request', (request) => {
    if (/^https?:/.test(request.url()) && new URL(request.url()).origin !== new URL(baseURL).origin) externalRequests.push(request.url());
  });
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/admin/api-keys');
  await expect(page.getByRole('link', { name: 'REST docs', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'OpenAPI' })).toHaveAttribute('href', '/rest/openapi.json');
  await page.getByText('Example request', { exact: true }).click();
  await expect(page.locator('pre')).toContainText(`${baseURL}/rest/v1/short-urls`);
  await page.fill('input[name="name"]', 'docs-test-key');
  await page.locator('form[action="/admin/api-keys"][method="post"] select[name="role"]').selectOption('author');
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  const key = await page.locator('.alert.success .mono').innerText();

  await page.getByRole('link', { name: 'REST docs', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Gort API', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Ask AI', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Short URLs', exact: true })).toBeVisible();
  // Opening a request verifies Scalar's bundled client, including lazy UI code.
  await page.getByRole('button', { name: 'Test Request (get /rest/health)', exact: true }).click();
  await expect(page.getByRole('button', { name: /^Send Request/ })).toBeVisible();
  await page.getByRole('button', { name: /^Send Request/ }).click();
  await expect(page.getByText('200 OK', { exact: true }).first()).toBeVisible();
  await page.keyboard.press('Escape');

  await page.goto('/graphql/docs');
  await page.getByLabel('API key', { exact: true }).fill(key);
  await page.getByRole('button', { name: 'Run request', exact: true }).click();
  await expect(page.locator('#graphql-result')).toContainText('"shortURLs"');
  await expect(page.locator('#graphql-result')).not.toContainText('"errors"');
  await page.getByText('Schema reference', { exact: true }).click();
  await expect(page.locator('#graphql-schema-text')).toContainText('type Mutation');
  await page.reload();
  await expect(page.getByLabel('API key', { exact: true })).toHaveValue('');
  expect(errors).toEqual([]);
  expect(externalRequests).toEqual([]);
});
