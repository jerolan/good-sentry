// Browser owner: a real member must not receive owner/admin management UI,
// while a direct URL still explains the permission boundary.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18001/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL;
const password = process.env.GOODSENTRY_TEST_PASSWORD;
const organization = process.env.GOODSENTRY_TEST_ORG;
assert.ok(email && password && organization, 'Provide an existing member account and organization slug');

const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-member-permission');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const pageErrors = [];
const screenshots = [];
page.on('pageerror', error => pageErrors.push(error.message));

try {
  await page.goto(base.href, { waitUntil: 'networkidle' });
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  const login = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/login'));
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  assert.equal((await login).status(), 200, 'Member password login succeeds');
  await page.getByRole('navigation', { name: 'Main navigation', exact: true }).waitFor({ state: 'visible' });
  assert.equal(await page.getByRole('link', { name: 'API tokens', exact: true }).count(), 0, 'Member has no credential navigation');

  const route = new URL(base);
  route.search = new URLSearchParams({ org: organization, view: 'api-tokens' }).toString();
  await page.goto(route.href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Permission required', exact: true }).waitFor({ state: 'visible' });
  assert.equal(await page.getByRole('form', { name: 'Create API token', exact: true }).count(), 0, 'Member has no create control');
  assert.equal(await page.getByRole('region', { name: 'API token metadata', exact: true }).count(), 0, 'Member has no credential metadata');

  const measurements = [];
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `Permission state fits ${width}px`);
    measurements.push({ width, ...size });
    const path = resolve(output, `member-permission-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
  assert.deepEqual(pageErrors, []);
  const report = { ok: true, email, organization, measurements, screenshots, pageErrors };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
