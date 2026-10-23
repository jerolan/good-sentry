// Project key management is a browser contract; HTTP tests cannot verify that
// the DSN controls are reachable, labeled, or usable from the Projects screen.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18081/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const runID = randomUUID().slice(0, 8);
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/project-keys-browser');
await mkdir(output, { recursive: true });

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: base.origin });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const pageErrors = [];
const consoleProblems = [];
const apiResponses = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('console', message => {
  if (['warning', 'error'].includes(message.type())) consoleProblems.push(message.text());
});
page.on('response', async response => {
  const url = new URL(response.url());
  if (url.pathname.includes('/api/0/projects/') && /\/keys(?:\/|$)/.test(url.pathname)) {
    apiResponses.push({ method: response.request().method(), path: url.pathname, status: response.status() });
  }
});

let project;
let organization;
let me;
let dialog;
let failureState = '';
const checks = [];
const screenshots = [];
let failure;

try {
  const meResponse = await context.request.get(new URL('api/v1/me', base).href);
  assert.equal(meResponse.status(), 200, 'Run this browser contract in an authenticated or open-mode instance');
  me = await meResponse.json();
  organization = me.organizations[0]?.slug;
  assert.ok(organization, 'The test owner needs one organization');

  const created = await context.request.post(new URL(`api/v1/organizations/${encodeURIComponent(organization)}/projects`, base).href, {
    headers: { 'X-CSRF-Token': me.csrfToken },
    data: { name: `Project keys ${runID}`, platform: 'go' },
  });
  assert.equal(created.status(), 201);
  project = await created.json();

  await page.goto(new URL(`?org=${encodeURIComponent(organization)}&view=projects`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Projects', exact: true, level: 1 }).waitFor();
  const row = page.getByRole('row').filter({ hasText: project.slug });
  assert.equal(await row.getByRole('button', { name: 'Manage SDK keys', exact: true }).count(), 1,
    'Projects must expose a keyboard-accessible SDK-key manager for each project');
  await row.getByRole('button', { name: 'Manage SDK keys', exact: true }).click();
  dialog = page.getByRole('dialog', { name: `SDK keys for ${project.name}`, exact: true });
  await dialog.waitFor();
  checks.push('Projects exposes a labeled SDK-key manager');

  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dimensions = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(dimensions.document <= dimensions.viewport, `Key dialog overflows at ${width}px`);
    const path = resolve(output, `project-keys-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }

  const label = `SDK key ${runID}`;
  await dialog.getByLabel('Key label', { exact: true }).fill(label);
  await dialog.getByLabel('Rate limit window (seconds)', { exact: true }).fill('60');
  await dialog.getByLabel('Rate limit count', { exact: true }).fill('3');
  await dialog.getByRole('button', { name: 'Create SDK key', exact: true }).click();
  const keyRow = dialog.getByRole('row').filter({ hasText: label });
  await keyRow.waitFor();
  const dsn = new URL(await keyRow.locator('code').first().textContent());
  assert.equal(dsn.origin, base.origin);
  assert.match(dsn.pathname, new RegExp(`/${project.id}/?$`));
  assert.ok(dsn.username, 'The copyable DSN must contain the new public key');
  assert.equal(await keyRow.getByRole('button', { name: 'Copy DSN', exact: true }).count(), 1);
  await keyRow.getByRole('button', { name: 'Copy DSN', exact: true }).click();
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), await keyRow.locator('code').first().textContent());
  checks.push('Creating an SDK key saves its rate limit and copies a usable project DSN');

  await keyRow.getByRole('button', { name: 'Edit', exact: true }).click();
  const edit = dialog.getByRole('heading', { name: 'Edit SDK key', exact: true });
  await edit.waitFor();
  await dialog.getByLabel('Key label', { exact: true }).fill(`${label} edited`);
  await dialog.getByLabel('Rate limit count', { exact: true }).fill('5');
  await dialog.getByRole('button', { name: 'Save SDK key', exact: true }).click();
  const edited = dialog.getByRole('row').filter({ hasText: `${label} edited` });
  await edited.waitFor();
  assert.match(await edited.innerText(), /5 events per 60 seconds/);

  await edited.getByRole('button', { name: 'Deactivate', exact: true }).click();
  await dialog.getByRole('button', { name: 'Confirm deactivation', exact: true }).click();
  await edited.getByText('Inactive', { exact: true }).waitFor();
  await edited.getByRole('button', { name: 'Activate', exact: true }).click();
  await dialog.getByRole('button', { name: 'Confirm activation', exact: true }).click();
  await edited.getByText('Active', { exact: true }).waitFor();
  checks.push('SDK key edits persist rate limits and activation state');

  await edited.getByRole('button', { name: 'Delete', exact: true }).click();
  await dialog.getByRole('button', { name: 'Confirm delete key', exact: true }).click();
  await edited.waitFor({ state: 'detached' });
  checks.push('Permanent key deletion requires confirmation');

  const keyPath = new URL(`api/0/projects/${encodeURIComponent(organization)}/${encodeURIComponent(project.slug)}/keys/`, base).href;
  for (let index = 0; index < 25; index++) {
    const seeded = await context.request.post(keyPath, {
      headers: { 'X-CSRF-Token': me.csrfToken },
      data: { label: `Pagination fixture ${String(index).padStart(2, '0')}` },
    });
    assert.equal(seeded.status(), 201);
  }
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await row.getByRole('button', { name: 'Manage SDK keys', exact: true }).click();
  dialog = page.getByRole('dialog', { name: `SDK keys for ${project.name}`, exact: true });
  await dialog.getByRole('row').filter({ hasText: 'Pagination fixture 00' }).waitFor();
  await dialog.getByRole('button', { name: 'Next', exact: true }).click();
  await dialog.getByRole('row').filter({ hasText: 'Pagination fixture 24' }).waitFor();
  await dialog.getByRole('button', { name: 'Previous', exact: true }).click();
  await dialog.getByRole('row').filter({ hasText: 'Pagination fixture 00' }).waitFor();
  checks.push('SDK key pagination follows compatible Link cursors in both directions');

  assert.deepEqual(pageErrors, []);
  assert.deepEqual(consoleProblems, []);
} catch (error) {
  failure = error;
  failureState = await dialog?.innerText().catch(() => '') || '';
} finally {
  if (project && organization && me?.csrfToken) {
    try {
      const removed = await context.request.delete(new URL(`api/v1/organizations/${encodeURIComponent(organization)}/projects/${encodeURIComponent(project.id)}`, base).href, {
        headers: { 'X-CSRF-Token': me.csrfToken },
      });
      assert.equal(removed.status(), 204, 'Remove the isolated project after the browser contract');
    } catch (error) {
      failure ||= error;
    }
  }
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, checks, screenshots, pageErrors, consoleProblems, apiResponses, failureState, failure: failure?.message }, null, 2));
  await browser.close();
}

if (failure) {
  console.error(failure);
  process.exitCode = 1;
}
