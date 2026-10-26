// Browser owner: combined text/environment/release/tag filters persist in URLs.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18101/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-issue-filters');
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

try {
  const meResponse = await context.request.get(new URL('api/v1/me', base).href);
  assert.equal(meResponse.status(), 200);
  const me = await meResponse.json();
  const org = me.organizations[0].slug;
  const run = randomBytes(4).toString('hex');
  const projectResponse = await context.request.post(new URL(`api/v1/organizations/${org}/projects`, base).href, {
    headers: { 'X-CSRF-Token': me.csrfToken, Origin: base.origin },
    data: { name: `Filter project ${run}`, platform: 'go' },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();
  const events = [
    ['Alpha filter failure ' + run, 'prod', 'app@1.0', 'eu'],
    ['Beta filter failure ' + run, 'stage', 'app@2.0', 'us'],
    ['Gamma filter failure ' + run, 'prod', 'app@2.0', 'eu'],
  ];
  for (const [message, environment, release, region] of events) {
    await send(project, JSON.stringify({ event_id: randomBytes(16).toString('hex'), message, level: 'error', platform: 'go', environment, release, tags: { region } }));
  }

  async function apply({ query, environment, release, status }) {
    await page.getByLabel('Search issues', { exact: true }).fill(query);
    await page.getByLabel('Environment', { exact: true }).fill(environment);
    await page.getByLabel('Release', { exact: true }).fill(release);
    await page.getByLabel('Status', { exact: true }).selectOption(status);
    await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
    await page.waitForLoadState('networkidle');
  }

  await page.goto(new URL('?view=issues', base).href, { waitUntil: 'networkidle' });
  await apply({ query: 'filter', environment: 'prod', release: 'app@1.0', status: 'unresolved' });
  await page.getByText(`Alpha filter failure ${run}`, { exact: false }).waitFor({ state: 'visible' });
  assert.equal(await page.getByRole('row').filter({ hasText: `Alpha filter failure ${run}` }).count(), 1);
  assert.equal(await page.getByRole('row').filter({ hasText: `Gamma filter failure ${run}` }).count(), 0);
  assert.equal(await page.getByRole('row').filter({ hasText: `Beta filter failure ${run}` }).count(), 0);
  const params = new URL(page.url()).searchParams;
  assert.equal(params.get('query'), 'filter');
  assert.equal(params.get('environment'), 'prod');
  assert.equal(params.get('release'), 'app@1.0');
  assert.equal(params.get('status'), 'unresolved');
  checks.push('Combined text/environment/release/status returns only the same-event issue and writes URL filters');

  await page.reload({ waitUntil: 'networkidle' });
  assert.equal(await page.getByLabel('Environment', { exact: true }).inputValue(), 'prod');
  assert.equal(await page.getByLabel('Release', { exact: true }).inputValue(), 'app@1.0');
  await page.getByText(`Alpha filter failure ${run}`, { exact: false }).waitFor({ state: 'visible' });
  assert.equal(await page.getByRole('row').filter({ hasText: `Alpha filter failure ${run}` }).count(), 1);
  checks.push('Filters survive reload');

  await apply({ query: 'region:eu', environment: 'prod', release: 'app@2.0', status: 'all' });
  await page.getByText(`Gamma filter failure ${run}`, { exact: false }).waitFor({ state: 'visible' });
  assert.equal(await page.getByRole('row').filter({ hasText: `Gamma filter failure ${run}` }).count(), 1);
  assert.equal(await page.getByRole('row').filter({ hasText: `Alpha filter failure ${run}` }).count(), 0);
  checks.push('Structured tag, environment and release intersect on the same stored event');

  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `filters overflow at ${width}px`);
    const path = resolve(output, `issue-filters-${width}.png`);
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
