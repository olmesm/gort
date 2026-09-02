// @ts-check
const { defineConfig } = require('@playwright/test');

const PORT = process.env.E2E_PORT || '18100';
const BASE_URL = `http://localhost:${PORT}`;

// CI sandboxes can point at a preinstalled Chromium instead of downloading one.
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_PATH || undefined;

module.exports = defineConfig({
  testDir: './tests',
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
      use: { storageState: '.auth/admin.json' },
      testIgnore: /auth\.setup\.js/,
    },
  ],
  webServer: {
    // Build the server binary, then run it against a throw-away data dir.
    command:
      'rm -rf "$PWD/.run" && mkdir -p "$PWD/.run" && ' +
      '(cd .. && go build -o e2e/.run/gort ./cmd/gort) && ' +
      `GORT_DATA_DIR="$PWD/.run" GORT_PORT=${PORT} GORT_DEFAULT_DOMAIN=localhost:${PORT} ` +
      'GORT_AUTO_RESOLVE_TITLES=false ' +
      'GORT_INITIAL_ADMIN_USERNAME=admin GORT_INITIAL_ADMIN_PASSWORD=e2e-password-123 ' +
      '"$PWD/.run/gort"',
    url: `${BASE_URL}/rest/health`,
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
