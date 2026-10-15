// Browser owner: Operations routes, visible metrics, nulls, pagination and indexing UI.
// The historical fixture uses external DB locking through ignored handshake files;
// all application data and retry responses still cross the real HTTP boundary.
// Start the companion helper from the repository root before this harness:
// DATABASE_URL=<fixture URL> PENDING_REPORT_DIR=<this output directory> \
//   go run tests/compat/tools/hold-span-projection.go
// Provide GOODSENTRY_TEST_EMAIL/PASSWORD, BASE_URL and PLAYWRIGHT_MODULE as usual.
import assert from 'node:assert/strict';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomBytes } from 'node:crypto';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseURL = new URL(process.env.BASE_URL || process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18005/');
if (!baseURL.pathname.endsWith('/')) baseURL.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL, password = process.env.GOODSENTRY_TEST_PASSWORD;
assert.ok(email && password, 'Provide existing browser owner credentials');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/transaction-span-groups-browser');
const handshake = resolve(process.env.GOODSENTRY_INDEXING_HANDSHAKE_DIR || output);
await mkdir(output, { recursive: true }); await mkdir(handshake, { recursive: true });
for (const name of ['pending-reference.json', 'indexing-held', 'indexing-observed', 'indexing-released']) await rm(resolve(handshake, name), { force: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage(); page.setDefaultTimeout(15000);
const checks = [], screenshots = [], dimensions = [], pageErrors = [], consoleProblems = [], requestFailures = [], badResponses = [], scrollEvidence = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: message.text(), url: message.location().url }); });
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), status: response.status() }); });
const runID = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const unsafe = '<img src=x onerror="window.__goodsentryOperationsUnsafe=1">';
const groupName = `Checkout ${runID} ${unsafe}`, description = `Fetch cart ${unsafe} ${'Long context '.repeat(12)}`;
const emptyName = `Empty operations ${runID}`, pendingName = `Historical operations ${runID}`;
const traceID = randomBytes(16).toString('hex'), segmentID = 'aaaaaaaaaaaaaaaa';
const second = Math.floor(Date.now() / 1000) - 7200;
const iso = seconds => new Date(seconds * 1000).toISOString().replace('.000Z', 'Z');
const bounds = { start: iso(second), end: iso(second + 3600), window: '1h' };
let projectA, projectB, organization, pendingEndpoint, failure;

async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
function locationURL(params) { const url = new URL(baseURL); url.search = new URLSearchParams(params).toString(); return url.href; }
function identity() {
  const params = new URL(page.url()).searchParams;
  return Object.fromEntries(['view', 'org', 'project', 'spanGroupsSource', 'spanGroups', 'start', 'end', 'window'].map(key => [key, params.get(key)]));
}
const operationRows = () => page.getByRole('region', { name: 'Operation breakdown', exact: true }).getByRole('row').filter({ has: page.getByRole('cell') });
async function ready() { await visible(page.getByRole('heading', { level: 1, name: 'Transaction operations', exact: true })); await visible(page.getByRole('region', { name: 'Operation breakdown', exact: true })); }
async function counts(transactions, spans, timed) {
  const summary = page.getByRole('region', { name: 'Operation summary', exact: true });
  for (const [label, value] of [['Transactions', transactions], ['Attributed spans', spans], ['Timed spans', timed]]) {
    assert.equal(await summary.getByText(label, { exact: true }).locator('..').getByRole('definition').innerText(), String(value));
  }
}
async function list(projectID) {
  await page.goto(locationURL({ view: 'transactions', org: organization, ...bounds, ...(projectID ? { project: projectID } : {}) }), { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { level: 1, name: 'Transactions', exact: true }));
}
function aggregate(name) { return page.getByRole('region', { name: 'Transaction aggregates', exact: true }).getByRole('row').filter({ has: page.getByRole('link', { name, exact: true }) }); }
async function operations(name, source) {
  await aggregate(name).getByRole('link', { name: 'Operations', exact: true }).click(); await ready();
  assert.equal(identity().spanGroupsSource, source); assert.match(identity().spanGroups, /^[1-9]\d*$/);
  for (const [key, value] of Object.entries(bounds)) assert.equal(new URL(page.url()).searchParams.get(key), value);
  for (const key of ['sampleSource', 'sample', 'page']) assert.equal(new URL(page.url()).searchParams.get(key), null);
}
async function send(project, type, value, extra = {}) {
  const dsn = new URL(project.dsn), payload = JSON.stringify(value);
  const envelope = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type, length: Buffer.byteLength(payload), ...extra })}\n${payload}\n`;
  const response = await context.request.post(new URL(`api/${project.id}/envelope/`, baseURL).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-operations-browser/1.0` }, data: envelope,
  });
  assert.equal(response.status(), 200, `Fixture admitted: ${response.status() === 200 ? type : await response.text()}`);
}
async function capture(name, table = true) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.getByRole('heading', { level: 1 }).focus(); await page.keyboard.press('Control+Home'); await page.mouse.click(5, 5);
    assert.equal(await page.evaluate(() => scrollY), 0);
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, `${name} fits ${width}px`);
    const save = async section => { const path = resolve(output, `${name}-${section}-${width}.png`); await page.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path); };
    await save('overview');
    if (table) {
      await page.getByRole('heading', { level: 2, name: 'Operations', exact: true }).evaluate(element => element.scrollIntoView({ block: 'start' }));
      const metrics = page.getByRole('region', { name: 'Operation breakdown', exact: true }).getByRole('table');
      await metrics.evaluate(element => { element.parentElement.scrollLeft = 0; }); await save('table');
      if (width === 320) {
        const scroll = await metrics.evaluate(element => { const container = element.parentElement; container.scrollLeft = container.scrollWidth; return { clientWidth: container.clientWidth, scrollWidth: container.scrollWidth, scrollLeft: container.scrollLeft }; });
        assert.ok(scroll.scrollLeft > 0, 'Mobile metric columns remain reachable by horizontal scrolling');
        scrollEvidence.push({ name, ...scroll }); await save('metrics'); await metrics.evaluate(element => { element.parentElement.scrollLeft = 0; });
      }
    }
  }
}
async function signal(name) {
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    try { await readFile(resolve(handshake, name)); return; } catch (error) { if (error.code !== 'ENOENT') throw error; }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`Historical-indexing fixture timed out after 120s waiting for ${resolve(handshake, name)}; run the external test-only DB locking helper.`);
}
try {
  const login = await context.request.post(new URL('api/v1/login', baseURL).href, { headers: { Origin: baseURL.origin }, data: { email, password } });
  assert.equal(login.status(), 200); const me = await login.json(); organization = me.organizations[0].slug;
  for (const suffix of ['A', 'B']) {
    const response = await context.request.post(new URL(`api/v1/organizations/${organization}/projects`, baseURL).href, { headers: { Origin: baseURL.origin, 'X-CSRF-Token': me.csrfToken }, data: { name: `Operations ${suffix} ${runID}`, platform: 'javascript' } });
    assert.equal(response.status(), 201); const project = await response.json(); if (suffix === 'A') projectA = project; else projectB = project;
  }
  const legacy = { event_id: randomBytes(16).toString('hex'), type: 'transaction', transaction: groupName, start_timestamp: second, timestamp: second + 2,
    contexts: { trace: { trace_id: traceID, span_id: '1111111111111111', op: 'http.server', status: 'ok' } }, spans: [
      { op: 'db.query', description, start_timestamp: second, timestamp: second + .5 },
      { op: 'zero', description: 'Zero duration', start_timestamp: second, timestamp: second },
      { op: 'unknown', description: 'No valid timing', timestamp: second + 1 },
      ...Array.from({ length: 48 }, (_, index) => ({ op: 'task', description: `Operation ${String(index + 1).padStart(2, '0')}`, start_timestamp: second + .25, timestamp: second + .5 })),
    ] };
  await send(projectA, 'transaction', legacy);
  const segment = { trace_id: traceID, span_id: segmentID, name: groupName, is_segment: true, status: 'ok', start_timestamp: second + 2, end_timestamp: second + 4,
    attributes: { 'sentry.op': { type: 'string', value: 'http.server' }, 'sentry.segment.id': { type: 'string', value: segmentID } } };
  const child = (id, name, op, begin, finish, association) => ({ trace_id: traceID, span_id: id, parent_span_id: segmentID, name, is_segment: false, status: 'ok', start_timestamp: begin, end_timestamp: finish,
    attributes: { 'sentry.op': { type: 'string', value: op }, ...(association === undefined ? {} : { 'sentry.segment.id': association }) } });
  const spans = [segment,
    child('2222222222222222', description, 'db.query', second - 60, second - 58.5, { type: 'string', value: segmentID }),
    child('3333333333333333', 'Operation 01', 'task', second - 80, second - 79.75, { type: 'string', value: segmentID }),
    child('4444444444444444', 'Unassociated child', 'excluded', second, second + 1),
    child('5555555555555555', 'Wrong typed association', 'excluded', second, second + 1, { type: 'integer', value: 123 }),
    child('6666666666666666', 'Unmatched association', 'excluded', second, second + 1, { type: 'string', value: 'bbbbbbbbbbbbbbbb' }),
  ];
  await send(projectB, 'span', { version: 2, items: spans }, { item_count: spans.length, content_type: 'application/vnd.sentry.items.span.v2+json' });
  await send(projectA, 'transaction', { ...legacy, event_id: randomBytes(16).toString('hex'), transaction: emptyName, spans: [] });
  await list(); await operations(groupName, 'span'); const original = identity(); assert.equal(original.project, null);
  await counts(2, 53, 52); const first = operationRows().filter({ hasText: description });
  assert.deepEqual((await first.getByRole('cell').allTextContents()).slice(2), ['2', '2', '1,000 ms', '1,450 ms', '2,000 ms'], 'Each visible duration metric is assigned to the right column');
  const zero = operationRows().filter({ hasText: 'Zero duration' }); assert.deepEqual((await zero.getByRole('cell').allTextContents()).slice(2), ['1', '1', '0 ms', '0 ms', '0 ms']);
  await visible(page.getByText('Accumulated time sums individual span durations. Overlapping spans can make it exceed elapsed transaction time.', { exact: true }));
  await visible(page.getByText(/Standalone spans use their recorded segment association/));
  assert.equal(await page.getByRole('main').locator('img').count(), 0); assert.equal(await page.evaluate(() => typeof window.__goodsentryOperationsUnsafe), 'undefined');
  assert.equal(await operationRows().count(), 50); await capture('mixed-operations');
  await page.getByRole('button', { name: 'Next operations', exact: true }).click(); await ready();
  await visible(page.getByText('51 operation groups · 1 on this page · Page 2', { exact: true })); assert.deepEqual(identity(), original);
  const untimed = operationRows().filter({ hasText: 'No valid timing' }); assert.deepEqual((await untimed.getByRole('cell').allTextContents()).slice(2), ['1', '0', 'Not available', 'Not available', 'Not available']);
  assert.equal(await page.getByRole('button', { name: 'Next operations', exact: true }).isEnabled(), false);
  await page.reload({ waitUntil: 'networkidle' }); await ready(); assert.deepEqual(identity(), original); assert.equal(new URL(page.url()).searchParams.get('page'), '2');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click(); await ready(); await visible(page.getByText('51 operation groups · 1 on this page · Page 2', { exact: true })); assert.deepEqual(identity(), original);
  await capture('unavailable-operation');
  await page.getByRole('link', { name: 'Open occurrence', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: groupName, exact: true }));
  const occurrence = new URL(page.url()).searchParams;
  assert.equal(occurrence.get('sampleSource'), original.spanGroupsSource); assert.equal(occurrence.get('sample'), original.spanGroups);
  for (const key of ['spanGroupsSource', 'spanGroups', 'page']) assert.equal(occurrence.get(key), null);
  for (const [key, value] of Object.entries(bounds)) assert.equal(occurrence.get(key), value);
  await page.getByRole('link', { name: '← All transactions', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: 'Transactions', exact: true }));
  for (const key of ['sampleSource', 'sample', 'spanGroupsSource', 'spanGroups', 'page']) assert.equal(new URL(page.url()).searchParams.get(key), null);
  const logs = new URL(await aggregate(groupName).getByRole('link', { name: traceID, exact: true }).getAttribute('href'), baseURL);
  assert.equal(logs.searchParams.get('project'), projectB.id); for (const [key, value] of Object.entries(bounds)) assert.equal(logs.searchParams.get(key), value);
  await list(projectA.id); await operations(groupName, 'event'); await counts(1, 51, 50); assert.equal(identity().project, projectA.id);
  await capture('legacy-operations'); await page.getByRole('link', { name: 'Open occurrence', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: groupName, exact: true })); assert.equal(new URL(page.url()).searchParams.get('sampleSource'), 'event');
  await page.getByRole('link', { name: '← All transactions', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: 'Transactions', exact: true }));
  assert.equal(await page.getByLabel('Project', { exact: true }).inputValue(), projectA.id);
  await operations(groupName, 'event'); const noTransactions = new URL(page.url());
  noTransactions.searchParams.set('start', iso(second - 7200)); noTransactions.searchParams.set('end', iso(second - 3600));
  await page.goto(noTransactions.href, { waitUntil: 'networkidle' }); await ready(); await counts(0, 0, 0); await visible(page.getByRole('heading', { name: 'No transactions in this interval', exact: true }));
  await capture('empty-window', false);
  await list(projectA.id); await operations(emptyName, 'event'); await counts(1, 0, 0); await visible(page.getByRole('heading', { name: 'No child operations recorded', exact: true }));
  await page.getByRole('link', { name: 'Issues', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  for (const key of ['spanGroupsSource', 'spanGroups', 'start', 'end', 'window']) assert.equal(new URL(page.url()).searchParams.get(key), null);
  checks.push('Mixed transports use real Operations actions alongside occurrence links; actual metric columns, null versus zero, safe long names and recorded association/overlap scope render correctly');
  checks.push('51 groups page50+1, reload/refresh/open-occurrence/back preserve immutable identity/project/absolute window; project-specific legacy route and both empty states work, and navigation clears breakdown state');

  const pendingEventID = randomBytes(16).toString('hex');
  await send(projectA, 'transaction', { ...legacy, event_id: pendingEventID, transaction: pendingName, spans: [{ op: 'db', description: 'Historical child', start_timestamp: second, timestamp: second + .125 }] });
  await list(projectA.id); const pendingLink = new URL(await aggregate(pendingName).getByRole('link', { name: 'Operations', exact: true }).getAttribute('href'), baseURL);
  const sample = { source: pendingLink.searchParams.get('spanGroupsSource'), id: pendingLink.searchParams.get('spanGroups'), projectId: projectA.id };
  await writeFile(resolve(handshake, 'pending-reference.json'), JSON.stringify({ organization, projectId: projectA.id, eventId: pendingEventID, sample }, null, 2));
  console.log(JSON.stringify({ stage: 'waiting-for-indexing-held', handshake, sample })); await signal('indexing-held');
  pendingEndpoint = new URL(`api/v1/organizations/${organization}/transactions/${sample.source}/${sample.id}/span-groups`, baseURL).pathname;
  await page.goto(pendingLink.href, { waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'Historical spans are being indexed', exact: true }));
  assert.equal(await page.getByRole('region', { name: 'Operation breakdown', exact: true }).count(), 0, 'Pending indexing never displays partial metrics');
  await page.getByRole('button', { name: 'Retry operation statistics', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Historical spans are being indexed', exact: true }));
  await capture('historical-indexing', false); await writeFile(resolve(handshake, 'indexing-observed'), 'Actual browser503 and retry observed.\n'); await signal('indexing-released');
  const deadline = Date.now() + 120000;
  let indexed = false;
  while (Date.now() < deadline) {
    const response = page.waitForResponse(response => new URL(response.url()).pathname === pendingEndpoint);
    await page.getByRole('button', { name: 'Retry operation statistics', exact: true }).click();
    if ((await response).status() === 200) { indexed = true; break; }
    await visible(page.getByRole('heading', { name: 'Historical spans are being indexed', exact: true })); await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.ok(indexed, 'Historical indexing did not complete within120s after the external row lock was released');
  await ready(); await counts(1, 1, 1); await visible(operationRows().filter({ hasText: 'Historical child' }));
  checks.push('Real historical projection503 shows useful indexing guidance and working retry without partial metrics; releasing the external row lock lets the normal worker produce visible statistics');
  const pendingFailure = response => response.status === 503 && new URL(response.url).pathname === pendingEndpoint;
  const pendingConsole = message => message.url && new URL(message.url).pathname === pendingEndpoint && /503/.test(message.text);
  assert.ok(badResponses.filter(pendingFailure).length >= 2, 'Both first load and manual retry observed actual indexing503');
  assert.deepEqual(badResponses.filter(response => !pendingFailure(response)), []); assert.deepEqual(consoleProblems.filter(message => !pendingConsole(message)), []);
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  console.log(JSON.stringify({ ok: true, checks, screenshots: screenshots.length, expectedIndexingResponses: badResponses.filter(pendingFailure).length }, null, 2));
} catch (error) {
  failure = error; console.error(error.stack || error.message); await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: baseURL.href, checks, screenshots, dimensions, scrollEvidence, pageErrors, consoleProblems, requestFailures, badResponses, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
