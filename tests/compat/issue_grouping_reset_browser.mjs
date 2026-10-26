// Browser owner: Future grouping reset and the next matching capture.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18103/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-grouping-reset');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const checks = [], screenshots = [], pageErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));

async function send(project, message) {
  const dsn = new URL(project.dsn);
  const payload = JSON.stringify({ event_id: randomBytes(16).toString('hex'), message, level: 'error', platform: 'go' });
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
    data: { name: `Reset project ${randomBytes(3).toString('hex')}`, platform: 'go' },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();
  const run = randomBytes(4).toString('hex');
  const message = `Reset grouping failure ${run}`;
  await send(project, message);

  await page.goto(new URL('?view=issues', base).href, { waitUntil: 'networkidle' });
  await page.getByRole('link', { name: new RegExp(message) }).click();
  await page.getByRole('heading', { level: 1, name: new RegExp(message) }).waitFor();
  const grouping = page.getByRole('region', { name: 'Future grouping' });
  await grouping.waitFor();
  const signature = page.getByLabel(/Select signature /).first();
  await signature.waitFor();
  await signature.check();
  await page.getByRole('button', { name: 'Reset selected signatures', exact: true }).click();
  const resetResponse = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().includes('/hashes/'));
  await page.getByRole('button', { name: 'Confirm grouping reset', exact: true }).click();
  assert.equal((await resetResponse).status(), 202);
  await page.getByText('Future grouping reset. Existing events remain in their recorded groups.', { exact: true }).waitFor();
  checks.push('Native Future grouping reset sends the selected signature and acknowledges 202');

  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `detail overflows at ${width}px`);
    const path = resolve(output, `grouping-reset-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }

  await send(project, message);
  await page.goto(new URL('?view=issues', base).href, { waitUntil: 'networkidle' });
  await page.getByText(message, { exact: false }).first().waitFor();
  const rows = page.getByRole('row').filter({ hasText: `${message}` });
  assert.equal(await rows.count(), 2);
  checks.push('A matching capture after reset creates a second issue while the historical one remains');
  assert.deepEqual(pageErrors, []);
  const report = { ok: true, checks, screenshots, pageErrors };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
