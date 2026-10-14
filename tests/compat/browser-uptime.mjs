// Real browser ownership: uptime configuration, observed worker results, history,
// permissions and feature gating. Store tests cannot detect these UI failures.
// The probe target/control are real local fixtures supplied by the test operator.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseURL = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18001/');
if (!baseURL.pathname.endsWith('/')) baseURL.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL;
const password = process.env.GOODSENTRY_TEST_PASSWORD;
assert.ok(email && password, 'Provide existing test owner credentials');
const target = process.env.GOODSENTRY_MONITOR_TARGET;
const controlURL = process.env.GOODSENTRY_MONITOR_CONTROL_URL;
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-uptime');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const runID = Date.now().toString(36);
const consoleProblems = [];
const pageErrors = [];
const requestFailures = [];
const checks = [];
const screenshots = [];
let expectedInvalidMonitor = false;
function observe(page) {
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: message.text(), location: message.location().url }); });
  page.on('pageerror', error => pageErrors.push(error.message));
  page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
}
observe(page);
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function login(page, loginEmail = email, loginPassword = password, url) {
  if (url) await page.goto(url.href, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }));
  await page.getByLabel('Email', { exact: true }).fill(loginEmail);
  await page.getByLabel('Password', { exact: true }).fill(loginPassword);
  const response = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/login'));
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  assert.equal((await response).status(), 200, 'Test account signs in');
  await visible(page.getByRole('navigation', { name: 'Main navigation', exact: true }));
}
async function capture(page, name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dialog = page.getByRole('dialog');
    const focusTarget = await dialog.count() ? dialog.getByRole('button', { name: 'Close', exact: true }) : page.getByRole('heading', { level: 1 });
    await focusTarget.focus();
    await page.keyboard.press('Control+Home');
    const dimensions = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(dimensions.document <= dimensions.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
}
async function confirmed(status) {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const state = page.getByRole('region', { name: 'Current monitor state', exact: true });
    await visible(state);
    if (await state.getByRole('group', { name: 'Confirmed monitor status', exact: true }).getByText(status, { exact: true }).isVisible()) return;
    await new Promise(resolve => setTimeout(resolve, 500));
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  }
  throw new Error(`Real monitor did not reach ${status} within 30 seconds`);
}
async function control(status) {
  const response = await context.request.post(controlURL, { data: { status, body: 'uptime browser proof' } });
  assert.ok(response.ok(), 'Local probe fixture accepts its status change');
}
async function create(kind, name) {
  await page.getByRole('link', { name: 'Uptime', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Uptime', exact: true }));
  await page.getByRole('button', { name: 'Create monitor', exact: true }).first().click();
  const dialog = await visible(page.getByRole('dialog', { name: 'Create monitor', exact: true }));
  await dialog.getByLabel('Monitor name', { exact: true }).fill(name);
  await dialog.getByLabel('Monitor type', { exact: true }).selectOption(kind);
  return dialog;
}
async function submitCreate(dialog) {
  const response = page.waitForResponse(response => response.request().method() === 'POST' && /\/monitors$/.test(response.url()));
  await dialog.getByRole('button', { name: 'Create monitor', exact: true }).click();
  const created = await response;
  assert.equal(created.status(), 201, 'Monitor creation succeeds');
  const monitor = await created.json();
  await visible(page.getByRole('heading', { level: 1, name: monitor.name, exact: true }));
  await visible(page.getByRole('region', { name: 'Current monitor state', exact: true }));
  return monitor;
}
async function deleteMonitor(monitor) {
  const url = new URL(baseURL);
  url.search = new URLSearchParams({ view: 'monitors', monitor: monitor.id }).toString();
  await page.goto(url.href, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { level: 1, name: monitor.name, exact: true }));
  await page.getByRole('button', { name: 'Delete monitor', exact: true }).click();
  await visible(page.getByText(`Delete ${monitor.name} and all its check history?`, { exact: true }));
  await page.getByRole('button', { name: 'Cancel deletion', exact: true }).click();
  assert.equal(await page.getByRole('button', { name: 'Delete permanently', exact: true }).count(), 0, 'Cancel retains the monitor');
  await page.getByRole('button', { name: 'Delete monitor', exact: true }).click();
  const deleted = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().endsWith(`/monitors/${monitor.id}`));
  await page.getByRole('button', { name: 'Delete permanently', exact: true }).click();
  assert.equal((await deleted).status(), 204, 'Confirmed monitor deletion succeeds');
  await visible(page.getByRole('heading', { level: 1, name: 'Uptime', exact: true }));
  assert.equal(await page.getByRole('link', { name: monitor.name, exact: true }).count(), 0, 'Deleted monitor leaves the list');
}
let failure;
try {
  assert.ok(target && controlURL, 'Provide a real probe target and control URL');
  await control(200);
  await login(page, email, password, baseURL);
  const me = await (await context.request.get(new URL('api/v1/me', baseURL).href)).json();
  assert.equal(me.features.uptime, true, 'Enabled server reports uptime availability');
  await visible(page.getByRole('link', { name: 'Uptime', exact: true }));
  assert.equal(await page.getByRole('link', { name: 'Logs', exact: true }).count(), me.features.logs ? 1 : 0, 'Logs navigation follows real server availability');
  const httpName = `Browser HTTP ${runID}`;
  const httpForm = await create('HEAD', httpName);
  for (const [label, value] of [['Interval (seconds)', '60'], ['Timeout (seconds)', '20'], ['Expected HTTP status', '200'], ['Failure confirmation threshold', '1']]) {
    assert.equal(await httpForm.getByLabel(label, { exact: true }).inputValue(), value, `${label} has the documented default`);
  }
  assert.equal(await httpForm.getByLabel('Enabled', { exact: true }).isChecked(), true, 'New monitoring defaults to enabled');
  assert.equal(await httpForm.getByLabel('Expected body substring', { exact: true }).isVisible(), false, 'HEAD omits body matching');
  await httpForm.getByLabel('Monitor type', { exact: true }).selectOption('TCP');
  await visible(httpForm.getByLabel('TCP host and port', { exact: true }));
  assert.equal(await httpForm.getByLabel('Expected HTTP status', { exact: true }).isVisible(), false, 'TCP omits HTTP-only fields');
  await httpForm.getByLabel('Monitor type', { exact: true }).selectOption('heartbeat');
  assert.equal(await httpForm.getByLabel('Target URL', { exact: true }).isVisible(), false, 'Heartbeat has no target URL');
  await httpForm.getByLabel('Monitor type', { exact: true }).selectOption('GET');
  await httpForm.getByLabel('Target URL', { exact: true }).fill('not-an-http-url');
  await httpForm.getByLabel('Interval (seconds)', { exact: true }).fill('1');
  await httpForm.getByLabel('Timeout (seconds)', { exact: true }).fill('2');
  await httpForm.getByLabel('Expected body substring', { exact: true }).fill('uptime browser proof');
  expectedInvalidMonitor = true;
  const invalid = page.waitForResponse(response => response.request().method() === 'POST' && /\/monitors$/.test(response.url()));
  await httpForm.getByRole('button', { name: 'Create monitor', exact: true }).click();
  assert.equal((await invalid).status(), 400, 'Invalid target is rejected by the real API');
  await visible(httpForm.getByRole('alert'));
  assert.equal(await httpForm.getByLabel('Monitor name', { exact: true }).inputValue(), httpName, 'Invalid input retains monitor name');
  assert.equal(await httpForm.getByLabel('Target URL', { exact: true }).inputValue(), 'not-an-http-url', 'Invalid input retains target');
  await capture(page, 'uptime-create-form');
  await httpForm.getByLabel('Target URL', { exact: true }).fill(target);
  const http = await submitCreate(httpForm);
  await confirmed('up');
  await control(503);
  await confirmed('down');
  await capture(page, 'uptime-http-down');
  await control(200);
  await confirmed('up');
  await page.getByLabel('Check history', { exact: true }).selectOption('false');
  await page.getByRole('button', { name: 'Apply history filter', exact: true }).click();
  await visible(page.getByRole('region', { name: 'Monitor check history', exact: true }));
  await page.reload({ waitUntil: 'networkidle' });
  await visible(page.getByRole('region', { name: 'Monitor check history', exact: true }));
  assert.equal(await page.getByLabel('Check history', { exact: true }).inputValue(), 'false', 'All-check filter survives reload');
  await capture(page, 'uptime-http-recovered');
  checks.push('Documented defaults, conditional fields, retained rejection inputs and real HTTP down/recovery work');

  const heartbeatName = `Browser heartbeat ${runID}`;
  const heartbeatForm = await create('heartbeat', heartbeatName);
  await heartbeatForm.getByLabel('Interval (seconds)', { exact: true }).fill('2');
  const heartbeat = await submitCreate(heartbeatForm);
  assert.ok(heartbeat.heartbeatURL, 'Created heartbeat exposes its capability URL');
  assert.equal(await page.getByLabel('Heartbeat URL', { exact: true }).inputValue(), heartbeat.heartbeatURL, 'Heartbeat URL is visible');
  await page.getByRole('button', { name: 'Copy heartbeat URL', exact: true }).click();
  await visible(page.getByRole('status').filter({ hasText: /Heartbeat URL copied|Copy unavailable/ }));
  async function beat() { assert.equal((await context.request.post(heartbeat.heartbeatURL)).status(), 200, 'Real heartbeat is accepted'); }
  await beat(); await confirmed('up');
  await confirmed('down');
  await beat(); await confirmed('up');
  for (let index = 0; index < 26; index++) await beat();
  await page.getByLabel('Check history', { exact: true }).selectOption('false');
  await page.getByRole('button', { name: 'Apply history filter', exact: true }).click();
  await visible(page.getByText('25 checks on this page · Page 1', { exact: true }));
  await page.getByRole('button', { name: 'Next checks', exact: true }).click();
  await visible(page.getByText(/checks on this page · Page 2/));
  await page.getByRole('button', { name: 'Previous checks', exact: true }).click();
  await visible(page.getByText('25 checks on this page · Page 1', { exact: true }));
  await capture(page, 'uptime-heartbeat');
  await page.getByRole('button', { name: 'Pause monitor', exact: true }).click();
  await visible(page.getByRole('group', { name: 'Confirmed monitor status', exact: true }).getByText('Paused', { exact: true }));
  await page.getByRole('button', { name: 'Edit monitor', exact: true }).click();
  const edit = await visible(page.getByRole('dialog', { name: 'Edit monitor', exact: true }));
  assert.equal(await edit.getByLabel('Monitor type', { exact: true }).isDisabled(), true, 'Monitor type cannot change');
  await edit.getByLabel('Interval (seconds)', { exact: true }).fill('5');
  const updated = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().endsWith(`/monitors/${heartbeat.id}`));
  await edit.getByRole('button', { name: 'Save monitor', exact: true }).click();
  assert.equal((await updated).status(), 200, 'Monitor configuration edit succeeds');
  await visible(page.getByRole('button', { name: 'Resume monitor', exact: true }));
  await page.getByRole('button', { name: 'Resume monitor', exact: true }).click();
  await visible(page.getByRole('button', { name: 'Pause monitor', exact: true }));
  await beat(); await confirmed('up');
  checks.push('Real heartbeat expiry/recovery, URL copy, 25-row history, pause/edit/resume work');

  let tcp;
  if (process.env.GOODSENTRY_MONITOR_TCP_TARGET) {
    const tcpForm = await create('TCP', `Browser TCP ${runID}`);
    await tcpForm.getByLabel('TCP host and port', { exact: true }).fill(process.env.GOODSENTRY_MONITOR_TCP_TARGET);
    await tcpForm.getByLabel('Interval (seconds)', { exact: true }).fill('1');
    await tcpForm.getByLabel('Timeout (seconds)', { exact: true }).fill('2');
    tcp = await submitCreate(tcpForm);
    await confirmed('up');
    await capture(page, 'uptime-tcp');
    checks.push('TCP configuration reaches confirmed up through a real connection');
  }

  const memberEmail = process.env.COMPAT_MEMBER_EMAIL;
  const memberPassword = process.env.COMPAT_MEMBER_PASSWORD;
  if (memberEmail && memberPassword) {
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await login(page, memberEmail, memberPassword);
    await page.getByRole('link', { name: 'Uptime', exact: true }).click();
    await visible(page.getByRole('link', { name: httpName, exact: true }));
    assert.equal(await page.getByRole('button', { name: 'Create monitor', exact: true }).count(), 0, 'Member has no create control');
    await page.getByRole('link', { name: httpName, exact: true }).click();
    await visible(page.getByRole('region', { name: 'Monitor check history', exact: true }));
    for (const label of ['Edit monitor', 'Pause monitor', 'Resume monitor', 'Delete monitor']) assert.equal(await page.getByRole('button', { name: label, exact: true }).count(), 0, `Member has no ${label} control`);
    await capture(page, 'uptime-member');
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await login(page);
    checks.push('Real organization member reads current state/history without write controls');
  }
  await page.getByRole('link', { name: 'Uptime', exact: true }).click();
  await visible(page.getByRole('link', { name: httpName, exact: true }));
  await capture(page, 'uptime-monitor-list');
  if (tcp) await deleteMonitor(tcp);
  await deleteMonitor(heartbeat);
  await deleteMonitor(http);
  await capture(page, 'uptime-list');
  checks.push('Concrete delete confirmation cancels safely and removes only the selected monitors');

  if (process.env.GOODSENTRY_DISABLED_URL) {
    const disabledURL = new URL(process.env.GOODSENTRY_DISABLED_URL);
    if (!disabledURL.pathname.endsWith('/')) disabledURL.pathname += '/';
    const disabledContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    const disabledPage = await disabledContext.newPage(); observe(disabledPage);
    await login(disabledPage, email, password, disabledURL);
    const disabledMe = await (await disabledContext.request.get(new URL('api/v1/me', disabledURL).href)).json();
    assert.equal(disabledMe.features.uptime, false, 'Disabled server reports uptime unavailable');
    assert.equal(await disabledPage.getByRole('link', { name: 'Uptime', exact: true }).count(), 0, 'Disabled uptime has no navigation');
    assert.equal(await disabledPage.getByRole('link', { name: 'Logs', exact: true }).count(), disabledMe.features.logs ? 1 : 0, 'Disabled-server logs navigation follows its availability');
    const orgSlug = disabledMe.organizations[0].slug;
    assert.equal((await disabledContext.request.get(new URL(`api/v1/organizations/${encodeURIComponent(orgSlug)}/monitors`, disabledURL).href)).status(), 404, 'Disabled uptime API is unavailable');
    disabledURL.search = 'view=monitors';
    await disabledPage.goto(disabledURL.href, { waitUntil: 'networkidle' });
    await visible(disabledPage.getByText('Uptime is disabled on this server.', { exact: true }));
    await capture(disabledPage, 'uptime-disabled');
    await disabledContext.close();
    checks.push('Feature-disabled instance hides navigation and removes monitor API access');
  }
  assert.deepEqual(pageErrors, [], 'No uncaught browser errors');
  assert.deepEqual(requestFailures, [], 'No failed transport requests');
  const unexpected = consoleProblems.filter(problem => !(/\/api\/v1\/me$/.test(problem.location) && /401/.test(problem.text)) && !(expectedInvalidMonitor && /\/monitors$/.test(problem.location) && /400/.test(problem.text)));
  assert.deepEqual(unexpected, [], 'No unexpected browser console messages');
  console.log(JSON.stringify({ passed: checks, screenshots, expectedConsoleMessages: consoleProblems.length }, null, 2));
} catch (error) {
  failure = error;
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
  console.error(error.stack || error.message);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, checks, screenshots, pageErrors, requestFailures, consoleProblems, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
