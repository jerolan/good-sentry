import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18150/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/goal-browser-organization');
await mkdir(output, { recursive: true });
const runID = Date.now().toString(36);
const email = 'organization-browser@example.test';
const password = 'organization-browser-password';
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const checks = [], screenshots = [], pageErrors = [], requestFailures = [], consoleErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', message => { if (message.type() === 'error') consoleErrors.push({ text: message.text(), location: message.location().url }); });

async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dialog = page.getByRole('dialog');
    await (await dialog.count() ? dialog.getByRole('button', { name: 'Close', exact: true }) : page.locator('#main')).focus();
    await page.evaluate(() => new Promise(done => { scrollTo(0, 0); requestAnimationFrame(done); }));
    const dimensions = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(dimensions.document <= dimensions.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
}

try {
  const state = await context.request.get(new URL('api/v1/setup', base).href);
  assert.equal((await state.json()).required, true);
  const setup = await context.request.post(new URL('api/v1/setup', base).href, { data: { email, password, name: 'Organization browser owner', organization: `Organization browser ${runID}` } });
  assert.equal(setup.status(), 201);
  const me = await (await context.request.get(new URL('api/v1/me', base).href)).json();
  const org = me.organizations[0];
  await page.goto(new URL(`?view=organization&org=${encodeURIComponent(org.slug)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Organization', exact: true, level: 1 }).waitFor();
  assert.equal(await page.getByRole('link', { name: 'Organization', exact: true }).count(), 1);
  assert.equal(await page.getByLabel('Organization name', { exact: true }).inputValue(), org.name);
  assert.equal(await page.getByText(org.slug, { exact: true }).count() >= 1, true);
  await page.getByLabel('Organization name', { exact: true }).fill(`  Renamed browser ${runID}  `);
  const rename = page.waitForResponse(response => response.request().method() === 'PUT' && /\/api\/0\/organizations\/[^/]+\/$/.test(new URL(response.url()).pathname));
  await page.getByRole('button', { name: 'Save organization name', exact: true }).click();
  assert.equal((await rename).status(), 200);
  await page.getByRole('heading', { name: 'Organization', exact: true, level: 1 }).waitFor();
  await page.getByLabel('Organization name', { exact: true }).waitFor();
  assert.equal(await page.getByLabel('Organization name', { exact: true }).inputValue(), `Renamed browser ${runID}`);
  assert.equal((await page.getByLabel('Organization', { exact: true }).locator('option:checked').textContent()).trim(), `Renamed browser ${runID}`);
  checks.push('Organization settings render real id/slug/role and rename through the compatible API');
  await capture('organization-settings');
  await page.reload({ waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Organization', exact: true, level: 1 }).waitFor();
  assert.equal(await page.getByLabel('Organization name', { exact: true }).inputValue(), `Renamed browser ${runID}`);
  checks.push('Renamed organization persists through reload');
  await page.getByRole('button', { name: 'Delete organization', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: /Delete organization/ });
  await dialog.waitFor();
  assert.match(await dialog.innerText(), /permanently removes all projects and stored data/);
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(await page.getByRole('button', { name: 'Delete organization', exact: true }).count(), 1);
  await page.getByRole('button', { name: 'Delete organization', exact: true }).click();
  const reopened = page.getByRole('dialog', { name: /Delete organization/ });
  await reopened.waitFor();
  await capture('organization-delete-confirmation');
  const deletion = page.waitForResponse(response => response.request().method() === 'DELETE' && /\/api\/0\/organizations\/[^/]+\/$/.test(new URL(response.url()).pathname));
  await reopened.getByRole('button', { name: 'Delete organization permanently', exact: true }).click();
  assert.equal((await deletion).status(), 204);
  await page.getByText('No organization access', { exact: true }).waitFor();
  checks.push('Delete confirmation can be cancelled; confirmed owner deletion removes the workspace and shows the no-organization state');
  await capture('organization-after-delete');
  assert.deepEqual(pageErrors, []);
  assert.deepEqual(requestFailures.filter(item => item.error !== 'net::ERR_ABORTED'), []);
  assert.deepEqual(consoleErrors, []);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ base: base.href, runID, checks, screenshots, pageErrors, requestFailures, consoleErrors }, null, 2));
  await browser.close();
}
