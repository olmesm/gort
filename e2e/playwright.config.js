// @ts-check
const { defineConfig } = require('@playwright/test');

const PORT = process.env.E2E_PORT || '18100';
if (!/^\d+$/.test(PORT)) throw new Error('E2E_PORT must be numeric');
const BASE_URL = `http://localhost:${PORT}`;

// CI sandboxes can point at a preinstalled Chromium instead of downloading one.
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_PATH || undefined;

module.exports = defineConfig({
  testDir: './tests',
  outputDir: `test-results/${PORT}`,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  timeout: 30_000,
  use: {
    baseURL: BASE_URL,
    chromiumSandbox: false,
    launchOptions: { executablePath },
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'setup', testMatch: /auth\.setup\.js/ },
    {
      name: 'chromium',
      dependencies: ['setup'],
      use: { storageState: `.auth/admin-${PORT}.json` },
      testIgnore: /auth\.setup\.js/,
    },
  ],
  webServer: {
    // The helper creates and drops an isolated PostgreSQL schema for this run.
    command: `E2E_PORT=${PORT} uv run --project .. python run_server.py`,
    url: `${BASE_URL}/rest/health`,
    reuseExistingServer: false,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 15000 },
    timeout: 120_000,
  },
});
