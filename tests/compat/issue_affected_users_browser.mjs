// Browser owner: Affected users count in native issue detail.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18105/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-affected-users');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const checks = [], screenshots = [], pageErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));

async function send(project, message, user) {
  const dsn = new URL(project.dsn);
  const payload = JSON.stringify({ event_id: randomBytes(16).toString('hex'), message, level: 'error', platform: 'go', ...(user ? { user } : {}) });
  const body = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const response = await context.request.post(new URL(`api/${project.id}/envelope/?sentry_version=7&sentry_key=${encodeURIComponent(dsn.username)}`, base).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope' }, data: body,
  });
  assert.equal(response.status(), 200);
}

try {
  const me = await (await context.request.get(new URL('api/v1/me', base).href)).json();
  const org = me.organizations[0].slug;
  const projectResponse = await context.request.post(new URL(`api/v1/organizations/${org}/projects`, base).href, {
    headers: { 'X-CSRF-Token': me.csrfToken, Origin: base.origin },
    data: { name: `Affected users ${randomBytes(3).toString('hex')}`, platform: 'go' },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();
  const run = randomBytes(4).toString('hex');
  const message = `Affected users failure ${run}`;
  await send(project, message, { id: 'user-a', email: 'a@example.test' });
  await send(project, message, { id: 'user-b', email: 'b@example.test' });
  await send(project, message, null);

  await page.goto(new URL('?view=issues', base).href, { waitUntil: 'networkidle' });
  await page.getByRole('link', { name: new RegExp(message) }).click();
  await page.getByRole('heading', { level: 1, name: new RegExp(message) }).waitFor();
  const affected = page.locator('.stat').filter({ hasText: 'Affected users' });
  await affected.waitFor();
  assert.equal((await affected.locator('dd').innerText()).trim(), '2');
  await affected.getByText('Identified retained users', { exact: true }).waitFor();
  checks.push('Affected users counts two distinct identified users and labels the value');
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `detail overflows at ${width}px`);
    const path = resolve(output, `affected-users-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
  assert.deepEqual(pageErrors, []);
  const report = { ok: true, checks, screenshots, pageErrors };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
