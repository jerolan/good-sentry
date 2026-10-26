// Run against an isolated, initialized account. This changes its password.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18166/');
const email = process.env.COMPAT_EMAIL || 'organization-browser@example.test';
const password = process.env.COMPAT_PASSWORD || 'organization-browser-password';
const replacement = 'changed-browser-password-20261020';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/account-browser');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage();
page.setDefaultTimeout(10000);
const report = { checks: [], screenshots: [], pageErrors: [] };
page.on('pageerror', error => report.pageErrors.push(error.message));
const login = value => context.request.post(new URL('api/v1/login', base).href, { data: { email, password: value } });
try {
  assert.equal((await login(password)).status(), 200);
  const other = await browser.newContext();
  try {
    assert.equal((await other.request.post(new URL('api/v1/login', base).href, { data: { email, password } })).status(), 200);
    await page.goto(new URL('?view=account', base).href, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'Account', exact: true, level: 1 }).waitFor();
    if (process.env.COMPAT_WITHOUT_ORGANIZATION === '1') {
      const me = await (await context.request.get(new URL('api/v1/me', base).href)).json();
      assert.deepEqual(me.organizations, []);
      report.checks.push('Account form remains available without organization membership');
    }
    await page.getByLabel('Current password', { exact: true }).fill(password);
    await page.getByLabel('New password', { exact: true }).fill(replacement);
    await page.getByLabel('Confirm new password', { exact: true }).fill('different-password');
    await page.getByRole('button', { name: 'Change password', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: 'The new passwords do not match.' }).waitFor();
    report.checks.push('Confirmation mismatch keeps the account form available');
    await page.getByLabel('Confirm new password', { exact: true }).fill(replacement);
    for (const width of [320, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 1000 });
      const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
      assert.ok(size.document <= size.viewport, `Account form overflows at ${width}px`);
      const path = resolve(output, `account-${width}.png`);
      await page.screenshot({ path, fullPage: true });
      report.screenshots.push(path);
    }
    const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/api/v1/account/password'));
    await page.getByRole('button', { name: 'Change password', exact: true }).click();
    assert.equal((await response).status(), 204);
    await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
    await page.getByText('Password changed. Sign in with your new password.', { exact: true }).waitFor();
    assert.equal(await page.getByLabel('Current password', { exact: true }).count(), 0);
    assert.equal((await other.request.get(new URL('api/v1/me', base).href)).status(), 401);
    assert.equal((await login(password)).status(), 401);
    assert.equal((await login(replacement)).status(), 200);
    report.checks.push('Password change clears the form, revokes another session and requires the new password');
    assert.deepEqual(report.pageErrors, []);
  } finally {
    await other.close();
  }
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  await browser.close();
}
