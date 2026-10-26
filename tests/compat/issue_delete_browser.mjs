// Browser owner: individual and selected issue deletion from the native UI.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18102/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-issue-delete');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const checks = [], screenshots = [], pageErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));

async function send(project, payload) {
  const dsn = new URL(project.dsn);
  const body = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const response = await context.request.post(new URL(`api/${project.id}/envelope/?sentry_version=7&sentry_key=${encodeURIComponent(dsn.username)}`, base).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope' },
    data: body,
  });
  assert.equal(response.status(), 200);
}

async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
}

try {
  const me = await (await context.request.get(new URL('api/v1/me', base).href)).json();
  const org = me.organizations[0].slug;
  const projectResponse = await context.request.post(new URL(`api/v1/organizations/${org}/projects`, base).href, {
    headers: { 'X-CSRF-Token': me.csrfToken, Origin: base.origin },
    data: { name: `Delete project ${randomBytes(3).toString('hex')}`, platform: 'go' },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();
  const run = randomBytes(4).toString('hex');
  for (const label of ['Alpha', 'Beta', 'Gamma', 'Delta']) {
    await send(project, JSON.stringify({ event_id: randomBytes(16).toString('hex'), message: `${label} delete failure ${run}`, level: 'error', platform: 'go' }));
  }

  await page.goto(new URL('?view=issues', base).href, { waitUntil: 'networkidle' });
  await page.getByText(`Alpha delete failure ${run}`, { exact: false }).waitFor();
  const row = label => page.getByRole('row').filter({ hasText: `${label} delete failure ${run}` });
  await row('Alpha').getByRole('checkbox').check();
  await row('Beta').getByRole('checkbox').check();
  await page.getByText(/2 selected/).waitFor();
  await capture('selected-issues');
  await page.getByRole('button', { name: 'Delete selected', exact: true }).click();
  const bulkResponse = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().includes('/api/0/organizations/'));
  await page.getByRole('button', { name: 'Confirm deletion', exact: true }).click();
  assert.equal((await bulkResponse).status(), 200);
  await page.getByText(`Gamma delete failure ${run}`, { exact: false }).waitFor();
  assert.equal(await row('Alpha').count(), 0);
  assert.equal(await row('Beta').count(), 0);
  checks.push('Bulk deletion removes exactly the selected groups and leaves the unselected issue');

  await row('Gamma').getByRole('link', { name: new RegExp(`Gamma delete failure ${run}`) }).click();
  await page.getByRole('heading', { level: 1, name: new RegExp(`Gamma delete failure ${run}`) }).waitFor();
  await page.getByRole('button', { name: 'Delete issue', exact: true }).click();
  await page.getByRole('region', { name: 'Confirm issue deletion' }).waitFor();
  await capture('confirm-detail-delete');
  const individualResponse = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().includes('/api/0/organizations/'));
  await page.getByRole('button', { name: 'Confirm deletion', exact: true }).click();
  assert.equal((await individualResponse).status(), 204);
  await page.getByRole('heading', { level: 1, name: 'Issues', exact: true }).waitFor();
  assert.equal(await page.getByText(`Gamma delete failure ${run}`, { exact: false }).count(), 0);
  checks.push('Individual deletion from issue detail requires confirmation and returns to the list without the group');

  await row('Delta').getByRole('link', { name: new RegExp(`Delta delete failure ${run}`) }).click();
  await page.getByRole('heading', { level: 1, name: new RegExp(`Delta delete failure ${run}`) }).waitFor();
  await page.getByRole('button', { name: 'Delete and discard forever', exact: true }).click();
  await page.getByRole('region', { name: 'Confirm permanent issue discard' }).waitFor();
  const discardResponse = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().includes('/api/0/organizations/'));
  await page.getByRole('button', { name: 'Confirm permanent discard', exact: true }).click();
  assert.equal((await discardResponse).status(), 204);
  await page.getByRole('heading', { level: 1, name: 'Issues', exact: true }).waitFor();
  await send(project, JSON.stringify({ event_id: randomBytes(16).toString('hex'), message: `Delta delete failure ${run}`, level: 'error', platform: 'go' }));
  await page.reload({ waitUntil: 'networkidle' });
  assert.equal(await page.getByText(`Delta delete failure ${run}`, { exact: false }).count(), 0);
  checks.push('Native permanent discard removes the issue and suppresses a new matching capture');
  assert.deepEqual(pageErrors, []);
  const report = { ok: true, checks, screenshots, pageErrors };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
