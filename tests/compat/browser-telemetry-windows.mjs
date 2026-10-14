// Browser owner: real telemetry inclusion and visible filter/URL persistence.
// Synthetic payloads use public ingestion; there are no mocked API responses.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomBytes } from 'node:crypto';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseURL = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18005/');
if (!baseURL.pathname.endsWith('/')) baseURL.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL, password = process.env.GOODSENTRY_TEST_PASSWORD;
assert.ok(email && password, 'Provide existing browser owner credentials');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-telemetry-windows');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US' });
const page = await context.newPage(); page.setDefaultTimeout(15000);
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], consoleProblems = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', message => { if (['error', 'warning'].includes(message.type())) consoleProblems.push({ text: message.text(), location: message.location().url }); });
const visible = async locator => { await locator.waitFor({ state: 'visible' }); return locator; };
const runID = Date.now().toString(36), traceID = randomBytes(16).toString('hex');
const service = `checkout/service ${runID}`, environment = 'browser-window';
const groupName = `Window checkout ${runID}`, recentName = `Recent checkout ${runID}`;
const logPrefix = `Window log ${runID}`;
const unsafe = '<img src=x onerror="window.__goodSentryWindowUnsafe=1">';
const startMS = Math.floor(Date.now() / 1000) * 1000 - 3 * 86400000, endMS = startMS + 3600000;
const start = new Date(startMS).toISOString(), end = new Date(endMS).toISOString();
let project, failure;
async function open(view, filters = {}, expectError = false) {
  const url = new URL(baseURL);
  url.search = new URLSearchParams({ view, project: project.id, ...filters }).toString();
  await page.goto(url.href, { waitUntil: 'networkidle' });
  if (!expectError) await visible(page.getByRole('heading', { name: view === 'logs' ? 'Logs' : 'Transactions', exact: true, level: 1 }));
}
async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.getByRole('heading', { level: 1 }).focus(); await page.keyboard.press('Control+Home');
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.ok(size.document <= size.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' }); screenshots.push(path);
  }
}
async function send(type, payload, extra = {}) {
  const dsn = new URL(project.dsn);
  const raw = typeof payload === 'string' ? payload : JSON.stringify(payload);
  const envelope = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type, length: Buffer.byteLength(raw), ...extra })}\n${raw}\n`;
  const response = await context.request.post(new URL(`api/${encodeURIComponent(project.id)}/envelope/`, baseURL).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-window-browser/1.0` }, data: envelope,
  });
  assert.equal(response.status(), 200, `${type} fixture is durably accepted`);
}
async function transaction(name, atMS, duration = 100) {
  await send('transaction', { event_id: randomBytes(16).toString('hex'), type: 'transaction', transaction: name, platform: 'javascript', level: 'info',
    start_timestamp: new Date(atMS - duration).toISOString(), timestamp: new Date(atMS).toISOString(),
    contexts: { trace: { trace_id: traceID, span_id: randomBytes(8).toString('hex'), op: 'http.server', status: 'ok' } } });
}
const logsPanel = () => page.getByRole('region', { name: 'Logs', exact: true });
function bounds() { const p = new URL(page.url()).searchParams; return { start: p.get('start'), end: p.get('end'), window: p.get('window') }; }
async function applyLogs() {
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await visible(logsPanel());
}
try {
  await page.goto(baseURL.href, { waitUntil: 'networkidle' });
  await page.getByLabel('Email', { exact: true }).fill(email); await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await visible(page.getByRole('navigation', { name: 'Main navigation', exact: true }));
  const me = await (await context.request.get(new URL('api/v1/me', baseURL).href)).json();
  const org = me.organizations[0].slug;
  const created = await context.request.post(new URL(`api/v1/organizations/${encodeURIComponent(org)}/projects`, baseURL).href, {
    headers: { 'X-CSRF-Token': me.csrfToken, Origin: baseURL.origin }, data: { name: `Window browser ${runID}`, platform: 'javascript' },
  });
  assert.equal(created.status(), 201); project = await created.json();
  await transaction(groupName, startMS, 100);
  await transaction(groupName, endMS, 900);
  await transaction(groupName, startMS - 1000, 1000);
  await transaction(recentName, Date.now() - 300000, 500);
  await send('span', { version: 2, items: [
    { trace_id: traceID, span_id: '1111111111111111', name: groupName, status: 'error', is_segment: true, start_timestamp: startMS / 1000 + 599.7, end_timestamp: startMS / 1000 + 600, attributes: { 'sentry.op': { type: 'string', value: 'http.server' } } },
    { trace_id: traceID, span_id: '2222222222222222', parent_span_id: '1111111111111111', name: groupName, status: 'ok', is_segment: false, start_timestamp: startMS / 1000 + 700, end_timestamp: startMS / 1000 + 701, attributes: { 'sentry.op': { type: 'string', value: 'http.server' } } },
    { trace_id: traceID, span_id: '3333333333333333', name: groupName, status: 'ok', is_segment: true, start_timestamp: endMS / 1000 - 1, end_timestamp: endMS / 1000, attributes: { 'sentry.op': { type: 'string', value: 'http.server' } } },
  ] }, { item_count: 3, content_type: 'application/vnd.sentry.items.span.v2+json' });
  const log = (name, timestamp, overrides = {}) => ({ timestamp, trace_id: traceID, span_id: '1111111111111111', level: 'warn', body: `${logPrefix} ${name} ${unsafe}`, attributes: {
    'service.name': { type: 'string', value: service }, environment: { type: 'string', value: environment }, maxInt64: { type: 'integer', value: 'INT64_MAX_LITERAL' }, active: { type: 'boolean', value: true },
  }, ...overrides });
  const items = Array.from({ length: 26 }, (_, index) => log(`#${String(index).padStart(2, '0')}`, new Date(startMS + index * 1000).toISOString()));
  items.push(log('at-end-excluded', end), log('before-start-excluded', new Date(startMS - 1000).toISOString()),
    log('wrong-service-excluded', new Date(startMS + 30000).toISOString(), { attributes: { 'service.name': { type: 'string', value: `${service}-other` }, environment: { type: 'string', value: environment } } }),
    log('wrong-level-excluded', new Date(startMS + 31000).toISOString(), { level: 'info' }),
    log('wrong-trace-excluded', new Date(startMS + 32000).toISOString(), { trace_id: randomBytes(16).toString('hex') }),
    log('wrong-environment-excluded', new Date(startMS + 33000).toISOString(), { attributes: { 'service.name': { type: 'string', value: service }, environment: { type: 'string', value: 'other-environment' } } }),
    log('recent', new Date(Date.now() - 300000).toISOString()));
  await send('log', JSON.stringify({ items }).replaceAll('"INT64_MAX_LITERAL"', '9223372036854775807'), { item_count: items.length, content_type: 'application/vnd.sentry.items.log+json' });

  await open('transactions');
  assert.equal(await page.getByLabel('Time window', { exact: true }).inputValue(), '');
  await visible(page.getByRole('row').filter({ hasText: recentName }));
  assert.equal(await page.getByRole('row').filter({ hasText: groupName }).count(), 0, 'Default transactions exclude older fixtures');
  await open('transactions', { start, end });
  assert.equal(await page.getByLabel('Time window', { exact: true }).inputValue(), 'custom');
  const row = await visible(page.getByRole('row').filter({ hasText: groupName }));
  const cells = row.getByRole('cell');
  for (const [index, value] of [[2, '2'], [3, '1'], [4, '200 ms'], [5, '200 ms'], [6, '290 ms']]) assert.equal(await cells.nth(index).innerText(), value);
  assert.equal(await page.getByRole('row').filter({ hasText: recentName }).count(), 0);
  await visible(page.getByRole('region', { name: 'Active time window', exact: true }));
  await capture('transactions-custom-window');
  await row.getByRole('link', { name: traceID, exact: true }).click();
  await visible(logsPanel()); assert.equal(bounds().start, start); assert.equal(bounds().end, end);
  assert.equal(await page.getByLabel('Trace ID', { exact: true }).inputValue(), traceID);
  checks.push('Custom half-open interval includes the start, excludes the end, combines legacy/segment aggregates without child counts and preserves trace-to-log bounds');

  await page.getByLabel('Service', { exact: true }).fill(service);
  await page.getByLabel('Environment', { exact: true }).fill(environment);
  await page.getByLabel('Level', { exact: true }).selectOption('warn');
  await page.getByLabel('Search logs', { exact: true }).fill(logPrefix);
  await applyLogs();
  assert.equal(await logsPanel().getByRole('row').count(), 26);
  assert.equal(await logsPanel().getByText(/excluded/).count(), 0);
  const expandedRow = logsPanel().getByRole('row').nth(1);
  await expandedRow.locator('summary').click();
  const integer = expandedRow.locator('dt').filter({ hasText: /^maxInt64$/ }).locator('+ dd');
  assert.equal(await integer.locator('pre').innerText(), '9223372036854775807');
  assert.equal(await page.evaluate(() => window.__goodSentryWindowUnsafe), undefined);
  await capture('logs-combined-window');
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await visible(page.getByText('1 log on this page · Page 2', { exact: true }));
  await visible(logsPanel().getByText(`${logPrefix} #00 ${unsafe}`, { exact: true }));
  assert.deepEqual(bounds(), { start, end, window: null });
  await page.reload({ waitUntil: 'networkidle' }); await visible(logsPanel());
  assert.equal(await page.getByLabel('Service', { exact: true }).inputValue(), service);
  assert.equal(await page.getByLabel('Trace ID', { exact: true }).inputValue(), traceID);
  assert.deepEqual(bounds(), { start, end, window: null });
  checks.push('Combined exact service/environment/level/text/trace filters, typed int64 and start-boundary log survive pagination/reload');

  for (const [preset, duration] of [['1h', 3600000], ['24h', 86400000], ['7d', 604800000], ['30d', 2592000000]]) {
    await page.getByLabel('Time window', { exact: true }).selectOption(preset);
    const before = Date.now(); await applyLogs(); const after = Date.now();
    const selected = bounds();
    assert.equal(selected.window, preset); assert.ok(selected.start.endsWith('Z') && selected.end.endsWith('Z'));
    assert.equal(Date.parse(selected.end) - Date.parse(selected.start), duration);
    assert.ok(Date.parse(selected.end) >= before && Date.parse(selected.end) <= after, 'Preset end uses the actual current instant');
    assert.equal(new URL(page.url()).searchParams.get('page'), null, 'Changing time window resets pagination');
    await page.reload({ waitUntil: 'networkidle' }); await visible(logsPanel());
    assert.equal(await page.getByLabel('Time window', { exact: true }).inputValue(), preset);
    assert.deepEqual(bounds(), selected);
    await page.getByLabel('Search logs', { exact: true }).fill(logPrefix); await applyLogs();
    assert.deepEqual(bounds(), selected, 'Applying other filters does not move the time window');
  }
  await capture('logs-preset-window');
  await page.getByLabel('Time window', { exact: true }).selectOption('1h'); await applyLogs();
  await visible(logsPanel().getByText(`${logPrefix} recent ${unsafe}`, { exact: true }));
  assert.equal(await logsPanel().getByText(/#\d\d/).count(), 0, 'Past-hour preset excludes older logs');
  await page.getByLabel('Time window', { exact: true }).selectOption(''); await applyLogs();
  assert.deepEqual(bounds(), { start: null, end: null, window: null });
  assert.equal(await page.getByLabel('Time window', { exact: true }).locator('option[value="custom"]').count(), 0);
  checks.push('All four presets store absolute UTC ranges, reuse bounds across filter/reload and reset pages; endpoint default removes bounds');

  await open('logs', { start, end, window: '7d' });
  assert.equal(await page.getByLabel('Time window', { exact: true }).inputValue(), 'custom', 'Edited bounds cannot retain a misleading preset label');
  const offsetStart = new Date(startMS - 6 * 3600000).toISOString().replace('.000Z', '.123456-06:00');
  const offsetEnd = new Date(endMS - 6 * 3600000).toISOString().replace('.000Z', '.123456-06:00');
  await open('logs', { start: offsetStart, end: offsetEnd });
  const summary = await page.getByRole('region', { name: 'Active time window', exact: true }).innerText();
  assert.ok(summary.includes(start.replace('.000Z', '.123456Z')) && summary.includes(end.replace('.000Z', '.123456Z')), 'UTC summary preserves custom microsecond precision while normalizing offsets');
  await capture('logs-offset-window');
  await page.getByRole('link', { name: 'Transactions', exact: true }).click();
  await visible(page.getByRole('region', { name: 'Transaction aggregates', exact: true }));
  assert.deepEqual(bounds(), { start: null, end: null, window: null });
  assert.equal(new URL(page.url()).searchParams.get('service'), null);
  checks.push('Custom URL ranges retain precise UTC display, reject mismatched preset labels and clear with sidebar navigation');

  await open('logs', { start }, true);
  await visible(page.getByRole('alert').filter({ hasText: 'Could not load this page' }));
  assert.equal(new URL(page.url()).searchParams.get('start'), start);
  await visible(page.getByRole('button', { name: 'Try again', exact: true }));
  checks.push('Malformed URL interval reaches real HTTP validation and displays the retryable error state');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  const unexpected = consoleProblems.filter(item => !(/\/api\/v1\/me$/.test(item.location) && /401/.test(item.text)) && !(/\/logs\?/.test(item.location) && /400/.test(item.text)));
  assert.deepEqual(unexpected, []);
  console.log(JSON.stringify({ passed: checks, screenshots, dimensions, expectedConsoleMessages: consoleProblems.length }, null, 2));
} catch (error) {
  failure = error; console.error(error.stack || error.message);
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, checks, screenshots, dimensions, pageErrors, requestFailures, consoleProblems, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
