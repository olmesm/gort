// @ts-check
// End-to-end check of gort's OIDC login + group scoping against a real
// Keycloak container (see setup.sh). Drives a real browser through the
// Keycloak login form, so it exercises discovery, PKCE, the code exchange
// and ID-token verification for real.
//
//   ./setup.sh                      # boot + configure Keycloak
//   (start gort as printed by setup.sh)
//   node test.js                    # this file
//
// Users: alice (gort-admins + team-a → admin), bob (team-a → regular).
const { chromium } = require('../node_modules/playwright-core');

const GORT = `http://localhost:${process.env.GORT_PORT || '18300'}`;
let failures = 0;

function check(name, ok, detail = '') {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? ' — ' + detail : ''}`);
  if (!ok) failures++;
}

async function keycloakLogin(page, username, password) {
  await page.goto(`${GORT}/admin/login`);
  await page.click(`a:has-text("Continue with Keycloak")`);
  await page.waitForSelector('#username');
  await page.fill('#username', username);
  await page.fill('#password', password);
  await page.click('#kc-login');
  await page.waitForURL(`${GORT}/admin`);
}

async function main() {
  const browser = await chromium.launch({
    executablePath: process.env.PLAYWRIGHT_CHROMIUM_PATH || undefined,
    chromiumSandbox: false,
  });

  // ---- alice: member of gort-admins + team-a → dashboard admin ----
  const aliceCtx = await browser.newContext();
  const alice = await aliceCtx.newPage();
  await keycloakLogin(alice, 'alice', 'alice-pass-123');

  check('alice lands on the overview', (await alice.locator('h1').textContent()) === 'Overview');
  check('alice username shown in topbar', (await alice.locator('.topbar .who').textContent()) === 'alice');
  check('alice sees admin nav (Users)', (await alice.locator('.topbar nav a', { hasText: 'Users' }).count()) === 1);

  await alice.goto(`${GORT}/admin/users`);
  check('alice provisioned as admin user', (await alice.locator('tr', { hasText: 'alice' }).count()) >= 1);

  // Create three links: ungrouped, team-a, team-b (admin group input is free text).
  const editUrls = {};
  for (const [slug, group] of [['kc-open', ''], ['kc-team-a', 'team-a'], ['kc-team-b', 'team-b']]) {
    await alice.goto(`${GORT}/admin/short-urls/new`);
    await alice.fill('input[name="longUrl"]', `https://example.com/${slug}`);
    await alice.fill('input[name="customSlug"]', slug);
    if (group) await alice.fill('input[name="group"]', group);
    await alice.click('button:has-text("Create short URL")');
    await alice.waitForURL(`${GORT}/admin/short-urls`);
    editUrls[slug] = await alice
      .locator('tr', { hasText: slug })
      .locator('a:has-text("Edit")')
      .getAttribute('href');
  }
  check('alice sees all three links', (await alice.locator('#su-table tbody tr').count()) === 3);
  check('group badge rendered from Keycloak group name',
    (await alice.locator('.badge.gray', { hasText: 'team-a' }).count()) === 1);
  await aliceCtx.close();

  // ---- bob: member of team-a only → regular user, scoped visibility ----
  const bobCtx = await browser.newContext();
  const bob = await bobCtx.newPage();
  await keycloakLogin(bob, 'bob', 'bob-pass-123');

  check('bob lands on the overview', (await bob.locator('h1').textContent()) === 'Overview');
  check('bob has no admin nav (Users hidden)',
    (await bob.locator('.topbar nav a', { hasText: 'Users' }).count()) === 0);
  const usersResp = await bob.goto(`${GORT}/admin/users`);
  check('bob gets 403 on /admin/users', usersResp.status() === 403);

  await bob.goto(`${GORT}/admin/short-urls`);
  const bobBody = await bob.locator('#su-table').textContent();
  check('bob sees the ungrouped link', bobBody.includes('kc-open'));
  check('bob sees his team-a link', bobBody.includes('kc-team-a'));
  check('bob does NOT see the team-b link', !bobBody.includes('kc-team-b'));
  check('bob sees exactly 2 links', (await bob.locator('#su-table tbody tr').count()) === 2);

  const foreign = await bob.goto(`${GORT}${editUrls['kc-team-b']}`);
  check('team-b edit page is 404 for bob', foreign.status() === 404);
  const own = await bob.goto(`${GORT}${editUrls['kc-team-a']}`);
  check('team-a edit page opens for bob', own.status() === 200);

  await bob.goto(`${GORT}/admin/short-urls/new`);
  const options = await bob.locator('select[name="group"] option').allTextContents();
  check('bob group picker = [No group, team-a]',
    JSON.stringify(options) === JSON.stringify(['No group', 'team-a']), JSON.stringify(options));

  await bob.fill('input[name="longUrl"]', 'https://example.com/bob-link');
  await bob.fill('input[name="customSlug"]', 'kc-bob');
  await bob.selectOption('select[name="group"]', 'team-a');
  await bob.click('button:has-text("Create short URL")');
  await bob.waitForURL(`${GORT}/admin/short-urls`);
  check('bob created a team-a link', (await bob.locator('tr', { hasText: 'kc-bob' }).count()) === 1);
  await bobCtx.close();

  await browser.close();
  console.log(failures === 0 ? '\nALL CHECKS PASSED' : `\n${failures} CHECK(S) FAILED`);
  process.exit(failures === 0 ? 0 : 1);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
