import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18152/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/goal-browser-people-ssl');
await mkdir(output, { recursive: true });
const runID = Date.now().toString(36);
const email = 'people-ssl-browser@example.test';
const password = 'people-ssl-browser-password';
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
  if ((await state.json()).required) {
    assert.equal((await context.request.post(new URL('api/v1/setup', base).href, { data: { email, password, name: 'People SSL owner', organization: `People SSL ${runID}` } })).status(), 201);
  } else {
    assert.equal((await context.request.post(new URL('api/v1/login', base).href, { data: { email, password } })).status(), 200);
  }
  const me = await (await context.request.get(new URL('api/v1/me', base).href)).json();
  const org = me.organizations[0].slug;
  const projectResponse = await context.request.post(new URL(`api/v1/organizations/${encodeURIComponent(org)}/projects`, base).href, {
    data: { name: `People SSL project ${runID}`, platform: 'go' }, headers: { 'X-CSRF-Token': me.csrfToken },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();

  await page.goto(new URL(`?view=people&org=${encodeURIComponent(org)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'People and teams', exact: true, level: 1 }).waitFor();
  assert.ok(await page.getByText(email, { exact: true }).count() >= 1);
  await page.getByLabel('Team name', { exact: true }).fill('On call');
  const createResponse = page.waitForResponse(response => response.request().method() === 'POST' && /\/teams$/.test(response.url()));
  await page.getByRole('button', { name: 'Create team', exact: true }).click();
  assert.equal((await createResponse).status(), 201);
  const card = page.locator('article').filter({ hasText: 'On call' });
  await card.getByRole('heading', { name: 'On call', exact: true }).waitFor();
  assert.match(await card.innerText(), /on-call/);
  const renameResponse = page.waitForResponse(response => response.request().method() === 'PATCH' && /\/teams\/\d+$/.test(response.url()));
  await card.getByLabel('Team name', { exact: true }).fill('Incident response');
  await card.getByRole('button', { name: 'Save name', exact: true }).click();
  assert.equal((await renameResponse).status(), 200);
  await page.getByRole('heading', { name: 'Incident response', exact: true }).waitFor();
  checks.push('People creates a real team and renames it through the native API');

  const renamedCard = page.locator('article').filter({ hasText: 'Incident response' });
  const addResponse = page.waitForResponse(response => response.request().method() === 'POST' && /\/members\/\d+$/.test(response.url()));
  await renamedCard.getByLabel('Member to add', { exact: true }).selectOption({ label: `People SSL owner · ${email}` });
  await renamedCard.getByRole('button', { name: 'Add member', exact: true }).click();
  assert.equal((await addResponse).status(), 204);
  await renamedCard.getByText(email, { exact: true }).waitFor();
  checks.push('Team editor adds an existing organization member');

  await page.getByLabel('Email', { exact: true }).fill(`invitee-${runID}@example.test`);
  await page.getByLabel('Invitation role', { exact: true }).selectOption('member');
  const inviteResponse = page.waitForResponse(response => response.request().method() === 'POST' && /\/invitations$/.test(response.url()));
  await page.getByRole('button', { name: 'Send invitation', exact: true }).click();
  assert.equal((await inviteResponse).status(), 201);
  await page.getByText(`invitee-${runID}@example.test`, { exact: true }).waitFor();
  checks.push('People queues and displays a real invitation');
  await capture('people-teams-and-invitation');

  const deleteResponse = page.waitForResponse(response => response.request().method() === 'DELETE' && /\/teams\/\d+$/.test(response.url()));
  await renamedCard.getByRole('button', { name: 'Delete team', exact: true }).click();
  await renamedCard.getByRole('button', { name: 'Confirm deletion', exact: true }).click();
  assert.equal((await deleteResponse).status(), 204);
  await page.getByRole('heading', { name: 'Incident response', exact: true }).waitFor({ state: 'detached' });
  checks.push('Team deletion removes its relation while preserving organization data');

  const monitorResponse = await context.request.post(new URL(`api/v1/organizations/${encodeURIComponent(org)}/monitors`, base).href, {
    data: { name: `Certificate monitor ${runID}`, type: 'SSL', url: 'https://example.test/', project: project.slug, environment: 'production', intervalSeconds: 300, timeoutSeconds: 5, expectedStatus: 200, expectedBody: '', confirmationThreshold: 1, enabled: false },
    headers: { 'X-CSRF-Token': me.csrfToken },
  });
  assert.equal(monitorResponse.status(), 201);
  const monitor = await monitorResponse.json();
  await page.goto(new URL(`?view=monitors&org=${encodeURIComponent(org)}&monitor=${encodeURIComponent(monitor.id)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: monitor.name, exact: true, level: 1 }).waitFor();
  assert.equal(await page.getByText('SSL', { exact: true }).count() >= 1, true);
  assert.equal(await page.getByText('Paused', { exact: true }).count() >= 1, true);
  await page.getByRole('button', { name: 'Edit monitor', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit monitor', exact: true });
  await dialog.waitFor();
  assert.equal(await dialog.getByLabel('Monitor type', { exact: true }).inputValue(), 'SSL');
  assert.equal(await dialog.getByLabel('HTTPS origin', { exact: true }).inputValue(), 'https://example.test/');
  assert.equal(await dialog.getByLabel('Expected body substring', { exact: true }).isVisible(), false);
  await dialog.getByText('Checks certificate trust, hostname and current validity at an HTTPS origin', { exact: false }).waitFor();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  checks.push('SSL monitor detail/edit exposes the HTTPS origin and hides HTTP-only body controls');
  await capture('ssl-monitor-detail');
  assert.deepEqual(pageErrors, []);
  assert.deepEqual(requestFailures.filter(item => item.error !== 'net::ERR_ABORTED'), []);
  assert.deepEqual(consoleErrors, []);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ base: base.href, runID, checks, screenshots, pageErrors, requestFailures, consoleErrors }, null, 2));
  await browser.close();
}
