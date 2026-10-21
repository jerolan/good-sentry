// Run against an empty installation with GOODSENTRY_AUTH=disabled.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:8000/');
const projectName = process.env.OPEN_PROJECT_NAME || `Open project ${Date.now()}`;
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/open-auth-browser');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const report = { checks: [], screenshots: [], pageErrors: [] };
page.on('pageerror', error => report.pageErrors.push(error.message));

try {
  await page.goto(base.href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Issues', exact: true, level: 1 }).waitFor();
  await page.getByText('Open access', { exact: true }).waitFor();
  assert.equal(await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).count(), 0);
  assert.equal(await page.getByRole('button', { name: 'Sign out', exact: true }).count(), 0);
  assert.equal((await context.cookies()).some(cookie => cookie.name === 'goodsentry_session'), false);
  const setup = await (await context.request.get(new URL('api/v1/setup', base).href)).json();
  const meResponse = await context.request.get(new URL('api/v1/me', base).href);
  assert.equal(setup.required, false);
  assert.equal(meResponse.status(), 200);
  const me = await meResponse.json();
  assert.equal(me.user.email, process.env.OPEN_EMAIL || 'owner@localhost');
  assert.equal(me.organizations.length, 1);
  assert.ok(me.csrfToken);
  report.checks.push('Open mode enters the dashboard without a cookie and reports the implicit owner');

  const organization = me.organizations[0].slug;
  const projectPath = new URL(`api/v1/organizations/${organization}/projects`, base).href;
  assert.equal((await context.request.post(projectPath, { data: { name: 'No CSRF', platform: 'go' } })).status(), 403);
  assert.equal((await context.request.post(projectPath, { headers: { Origin: 'https://foreign.example.test', 'X-CSRF-Token': me.csrfToken }, data: { name: 'Foreign', platform: 'go' } })).status(), 403);
  assert.equal((await context.request.post(projectPath, { headers: { 'X-CSRF-Token': me.csrfToken }, data: { name: projectName, platform: 'go' } })).status(), 201);
  report.checks.push('Open mode still rejects missing/foreign mutation CSRF and accepts the stable token');

  await page.reload({ waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Issues', exact: true, level: 1 }).waitFor();
  await page.getByRole('link', { name: 'Projects', exact: true }).click();
  await page.getByText(projectName, { exact: true }).waitFor();
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    assert.equal(overflow, false, `Open dashboard overflows at ${width}px`);
    const path = resolve(output, `open-dashboard-${width}.png`);
    await page.screenshot({ path, fullPage: true });
    report.screenshots.push(path);
  }
  assert.deepEqual(report.pageErrors, []);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  await context.close();
  await browser.close();
}
