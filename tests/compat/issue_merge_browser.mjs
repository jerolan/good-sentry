// Browser owner: native selected merge and original-group separation.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18106/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-issue-merge');
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
    data: { name: `Merge project ${randomBytes(3).toString('hex')}`, platform: 'go' },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();
  const run = randomBytes(4).toString('hex');
  for (const label of ['Alpha', 'Beta', 'Gamma']) await send(project, `${label} merge failure ${run}`);

  await page.goto(new URL('?view=issues', base).href, { waitUntil: 'networkidle' });
  await page.getByText(`Alpha merge failure ${run}`, { exact: false }).waitFor();
  const row = label => page.getByRole('row').filter({ hasText: `${label} merge failure ${run}` });
  await row('Alpha').getByRole('checkbox').check();
  await row('Beta').getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Merge selected', exact: true }).click();
  const mergeResponse = page.waitForResponse(response => response.request().method() === 'POST' && response.url().includes('/issues/merge'));
  await page.getByRole('button', { name: 'Confirm merge', exact: true }).click();
  assert.equal((await mergeResponse).status(), 200);
  await page.reload({ waitUntil: 'networkidle' });
  const roots = await (await context.request.get(new URL(`api/v1/organizations/${org}/issues?project=${project.id}&status=all&category=error`, base).href)).json();
  assert.equal(roots.data.length, 2);
  const root = roots.data.find(issue => issue.title.includes('Alpha') || issue.title.includes('Beta'));
  assert.ok(root);
  checks.push('Selected merge produces one root plus the unselected issue');

  await page.goto(new URL(`?view=issues&issue=${root.id}`, base).href, { waitUntil: 'networkidle' });
  const origins = page.getByRole('region', { name: 'Original groups' });
  await origins.waitFor();
  const origin = origins.getByRole('checkbox').first();
  await origin.waitFor();
  await origin.check();
  await page.getByRole('button', { name: 'Separate selected', exact: true }).click();
  const unmergeResponse = page.waitForResponse(response => response.request().method() === 'POST' && response.url().includes('/unmerge'));
  await page.getByRole('button', { name: 'Confirm separation', exact: true }).click();
  assert.equal((await unmergeResponse).status(), 200);
  await page.getByText('Original groups separated.', { exact: true }).waitFor();
  checks.push('Original-group separation succeeds with confirmation');
  await capture('merge-separation');
  assert.deepEqual(pageErrors, []);
  const report = { ok: true, checks, screenshots, pageErrors };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
