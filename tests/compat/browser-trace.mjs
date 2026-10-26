// Browser owner: trace entry/back, waveform geometry, parent jumps, lazy raw data,
// independent scope/pages and related views. SQL graph policy belongs to Go tests.
// Run the external hold-span-projection helper with PENDING_REPORT_DIR equal to
// this output folder; its real row lock coordinates the historical503 scenario.
import assert from 'node:assert/strict';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomBytes } from 'node:crypto';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.BASE_URL || process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18005/');
const disabledBase = new URL(process.env.DISABLED_BASE_URL || 'http://127.0.0.1:18006/');
for (const url of [base, disabledBase]) if (!url.pathname.endsWith('/')) url.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL, password = process.env.GOODSENTRY_TEST_PASSWORD;
assert.ok(email && password, 'Provide the existing browser fixture owner credentials');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-trace');
const handshake = resolve(process.env.GOODSENTRY_INDEXING_HANDSHAKE_DIR || output);
await mkdir(output, { recursive: true }); await mkdir(handshake, { recursive: true });
for (const name of ['pending-reference.json', 'indexing-held', 'indexing-observed', 'indexing-released']) await rm(resolve(handshake, name), { force: true });
const browser = await chromium.launch({ headless: true });
const checks = [], screenshots = [], dimensions = [], geometries = [], pageErrors = [], requestFailures = [], consoleProblems = [], badResponses = [], rawRequests = [];
const traceKeys = ['traceView', 'traceProject', 'tracePage', 'traceNode', 'traceErrorsPage', 'returnView'];
const run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`, traceID = randomBytes(16).toString('hex');
const rootID = '0000000000000001', duplicateID = 'dddddddddddddddd';
const rootName = `Trace checkout ${run}`, logPrefix = `Trace log ${run}`;
const unsafe = '<img src=x onerror="window.__goodsentryTraceUnsafe=1">';
const longName = `Long preview ${'λ'.repeat(2050)} ${unsafe}`;
const second = Math.floor(Date.now() / 1000) - 7200;
const iso = value => new Date(value * 1000).toISOString().replace('.000Z', 'Z');
const bounds = { start: iso(second), end: iso(second + 3600), window: '1h' };
const wireJSON = value => JSON.stringify(value).replaceAll('"INT64_MAX_LITERAL"', '9223372036854775807');
let page, owner, organization, projectA, projectB, pendingPath, absentPath, failure;
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
function route(target, params) { const url = new URL(target); url.search = new URLSearchParams(params).toString(); return url.href; }
function params(tab = page) { return Object.fromEntries(new URL(tab.url()).searchParams); }
function origin(tab = page) { return Object.fromEntries(Object.entries(params(tab)).filter(([key]) => !traceKeys.includes(key))); }
function preserved(before, tab = page) {
  const current = origin(tab); delete current.view;
  const expected = { ...before }; delete expected.view;
  assert.deepEqual(current, expected, 'Trace navigation preserves every originating filter/source/page key');
}
const forest = (tab = page) => tab.getByRole('region', { name: 'Trace waterfall', exact: true });
const rows = (tab = page) => forest(tab).getByRole('row').filter({ has: tab.getByRole('cell') });
const row = (name, tab = page) => rows(tab).filter({ hasText: name });
async function ready(tab = page) { await visible(tab.getByRole('heading', { level: 1, name: 'Trace', exact: true })); await visible(forest(tab)); }
async function statistic(label, expected, tab = page) {
  assert.equal(await tab.getByRole('region', { name: 'Trace summary', exact: true }).getByText(label, { exact: true }).locator('..').getByRole('definition').innerText(), String(expected));
}
async function session(target) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
  const login = await context.request.post(new URL('api/v1/login', target).href, { headers: { Origin: target.origin }, data: { email, password } });
  assert.equal(login.status(), 200); const me = await login.json();
  const tab = await context.newPage(); tab.setDefaultTimeout(20000);
  tab.on('pageerror', error => pageErrors.push(error.message));
  tab.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  tab.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: message.text(), url: message.location().url }); });
  tab.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), status: response.status() }); });
  tab.on('request', request => { if (/\/traces\/[^/]+\/nodes\//.test(new URL(request.url()).pathname)) rawRequests.push(request.url()); });
  return { context, me, tab };
}
async function send(project, type, value, extra = {}) {
  const dsn = new URL(project.dsn), payload = wireJSON(value);
  const envelope = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type, length: Buffer.byteLength(payload), ...extra })}\n${payload}\n`;
  const response = await owner.context.request.post(new URL(`api/${project.id}/envelope/`, base).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-trace-browser/1.0` }, data: envelope,
  });
  assert.equal(response.status(), 200, `Trace fixture admitted: ${response.status() === 200 ? type : await response.text()}`);
}
async function capture(name, tab = page, section = 'Waterfall') {
  for (const width of [320, 768, 1440]) {
    await tab.setViewportSize({ width, height: 1000 }); await tab.getByRole('heading', { level: 1 }).focus(); await tab.evaluate(() => window.scrollTo(0, 0)); await tab.mouse.click(5, 5);
    assert.equal(await tab.evaluate(() => scrollY), 0);
    const size = await tab.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, `${name} fits ${width}px`);
    const save = async part => { const path = resolve(output, `${name}-${part}-${width}.png`); await tab.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path); };
    await save('overview');
    if (section) {
      await tab.getByRole('heading', { level: 2, name: section, exact: true }).evaluate(element => element.scrollIntoView({ block: 'start' })); await save('detail');
      if (section === 'Waterfall' && width === 320 && await rows(tab).count()) {
        const scroll = await forest(tab).getByRole('table').evaluate(table => { const container = table.parentElement; container.scrollLeft = container.scrollWidth; return { width: container.clientWidth, scrollWidth: container.scrollWidth, left: container.scrollLeft }; });
        assert.ok(scroll.left > 0, 'Mobile waterfall retains reachable timing/relationship columns'); await save('relationships');
        await forest(tab).getByRole('table').evaluate(table => { table.parentElement.scrollLeft = 0; });
      }
    }
  }
}
async function geometry(name, leftRatio, widthRatio, point = false) {
  const image = row(name).getByRole('img');
  const measured = await image.evaluate(element => {
    const track = element.getBoundingClientRect(), marker = element.querySelector('[aria-hidden="true"]')?.getBoundingClientRect();
    return { trackWidth: element.clientWidth, markerWidth: marker?.width, left: marker ? marker.left - track.left - element.clientLeft : null };
  });
  assert.ok(measured.markerWidth > 0, `${name} has a real drawn marker`);
  const center = measured.left + (point ? measured.markerWidth / 2 : 0);
  assert.ok(Math.abs(center / measured.trackWidth - leftRatio) < .01, `${name} has the expected recorded position`);
  if (point) assert.ok(measured.markerWidth <= 8, 'A zero/incomplete interval is a point rather than a duration bar');
  else assert.ok(Math.abs(measured.markerWidth / measured.trackWidth - widthRatio) < .01, `${name} has the expected recorded duration scale`);
  geometries.push({ name, leftRatio, widthRatio, point, ...measured });
}
async function signal(name) {
  const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    try { await readFile(resolve(handshake, name)); return; } catch (error) { if (error.code !== 'ENOENT') throw error; }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error(`Trace indexing fixture timed out after120s waiting for ${resolve(handshake, name)}. Run the external projection-lock helper.`);
}
try {
  owner = await session(base); page = owner.tab; organization = owner.me.organizations[0].slug;
  for (const suffix of ['A', 'B']) {
    const response = await owner.context.request.post(new URL(`api/v1/organizations/${organization}/projects`, base).href, { headers: { Origin: base.origin, 'X-CSRF-Token': owner.me.csrfToken }, data: { name: `Trace ${suffix} ${run}`, platform: 'javascript' } });
    assert.equal(response.status(), 201); const project = await response.json(); if (suffix === 'A') projectA = project; else projectB = project;
  }
  const chain = Array.from({ length: 55 }, (_, index) => ({ span_id: (index + 2).toString(16).padStart(16, '0'), parent_span_id: (index + 1).toString(16).padStart(16, '0'), description: `Chain ${String(index + 1).padStart(2, '0')} ${run}`, op: 'db', status: 'ok', start_timestamp: second + 2, timestamp: second + 4, receipt: 'INT64_MAX_LITERAL' }));
  const children = [...chain,
    { span_id: '00000000000000a1', parent_span_id: rootID, description: `Zero point ${run}`, start_timestamp: second + 5, timestamp: second + 5 },
    { span_id: '00000000000000a2', parent_span_id: rootID, description: `End only ${run}`, timestamp: second - 2 },
    { span_id: '00000000000000a3', parent_span_id: rootID, description: `Start only ${run}`, start_timestamp: second + 20 },
    { span_id: '00000000000000a4', parent_span_id: rootID, description: `Reversed ${run}`, start_timestamp: second + 12, timestamp: second + 11 },
    { span_id: duplicateID, parent_span_id: rootID, description: `Duplicate A ${run}`, start_timestamp: second + 6, timestamp: second + 7 },
    { span_id: duplicateID, parent_span_id: rootID, description: `Duplicate B ${run}`, start_timestamp: second + 6.5, timestamp: second + 7.5 },
    { span_id: 'broken', parent_span_id: 'invalid-parent', description: `Invalid IDs ${run}`, start_timestamp: second + 17, timestamp: second + 18 },
    { description: `Missing IDs ${run}` },
    { span_id: '00000000000000a5', parent_span_id: rootID, description: longName, start_timestamp: second + 8.5, timestamp: second + 9.5, marker: 'full-selected-child', receipt: 'INT64_MAX_LITERAL' },
  ];
  const legacyEventID = randomBytes(16).toString('hex');
  const legacy = { event_id: legacyEventID, type: 'transaction', transaction: rootName, start_timestamp: second, timestamp: second + 10,
    contexts: { trace: { trace_id: traceID, span_id: rootID, op: 'http.server', status: 'ok' } }, spans: children, receipt: 'INT64_MAX_LITERAL' };
  await send(projectA, 'transaction', legacy);
  const standalone = (id, parent, name, begin, finish, segment = false) => ({ trace_id: traceID, span_id: id, parent_span_id: parent, name, status: 'ok', is_segment: segment, start_timestamp: begin, end_timestamp: finish, attributes: { 'sentry.op': { type: 'string', value: 'http.server' }, 'sentry.segment.id': { type: 'string', value: 'bbbbbbbbbbbbbbb0' } } });
  const streamed = [standalone('bbbbbbbbbbbbbbb0', rootID, `Mixed segment ${run}`, second + 9, second + 10, true), standalone('bbbbbbbbbbbbbbb1', rootID, `Cross-project ${run}`, second + 3, second + 4),
    standalone('bbbbbbbbbbbbbbb2', 'ffffffffffffffff', `Missing parent ${run}`, second + 1, second + 2), standalone('ccccccccccccccc1', 'ccccccccccccccc2', `Cycle A ${run}`, second + 14, second + 15), standalone('ccccccccccccccc2', 'ccccccccccccccc1', `Cycle B ${run}`, second + 15, second + 16)];
  await send(projectB, 'span', { version: 2, items: streamed }, { item_count: streamed.length, content_type: 'application/vnd.sentry.items.span.v2+json' });
  await send(projectA, 'span', { version: 2, items: [standalone('aaaaaaaaaaaaaaa1', duplicateID, `Ambiguous parent ${run}`, second + 8, second + 9)] }, { item_count: 1, content_type: 'application/vnd.sentry.items.span.v2+json' });
  for (let index = 0; index < 26; index++) await send(index % 2 ? projectB : projectA, 'event', { event_id: randomBytes(16).toString('hex'), message: `Trace error ${run} #${String(index).padStart(2, '0')}${index === 0 ? ` ${'ε'.repeat(2050)} ${unsafe}` : ''}`, timestamp: second - 60 + index, fingerprint: [`trace-errors-${run}`], contexts: { trace: { trace_id: traceID, span_id: rootID } } });
  const logs = Array.from({ length: 26 }, (_, index) => ({ timestamp: iso(second + 40 + index), trace_id: traceID, span_id: rootID, level: 'warn', body: `${logPrefix} #${String(index).padStart(2, '0')}`, attributes: { environment: { type: 'string', value: 'browser-trace' }, 'service.name': { type: 'string', value: 'trace-service' } } }));
  await send(projectA, 'log', { items: logs }, { item_count: logs.length, content_type: 'application/vnd.sentry.items.log+json' });
  await send(projectB, 'log', { items: [{ ...logs[0], trace_id: traceID.toUpperCase(), body: `Cross-project trace log ${run}` }] }, { item_count: 1, content_type: 'application/vnd.sentry.items.log+json' });

  const transactionURL = route(base, { view: 'transactions', org: organization, project: projectA.id, ...bounds });
  await page.goto(transactionURL, { waitUntil: 'networkidle' });
  const aggregate = page.getByRole('row').filter({ has: page.getByRole('link', { name: rootName, exact: true }) });
  const occurrenceURL = new URL(await aggregate.getByRole('link', { name: rootName, exact: true }).getAttribute('href'), base).href;
  assert.equal(new URL(await aggregate.getByRole('link', { name: traceID, exact: true }).getAttribute('href'), base).searchParams.has('traceMatch'), false, 'Ordinary trace-to-log links keep exact matching');
  await aggregate.getByRole('link', { name: 'View trace', exact: true }).click(); await ready();
  const initialTrace = page.url(), initialOrigin = origin();
  assert.equal(params().traceProject, undefined); assert.equal(params().project, projectA.id); assert.equal(params().returnView, 'transactions');
  await statistic('Nodes', 71); await statistic('Projects', 2); await statistic('Errors', 26); await statistic('Logs', 27); assert.equal(await rows().count(), 50);
  await visible(page.getByRole('region', { name: 'Trace errors', exact: true }).getByText('Error title preview truncated.', { exact: true }));
  const traceSummary = page.getByRole('region', { name: 'Trace summary', exact: true });
  await visible(traceSummary.getByText(iso(second - 2), { exact: true })); await visible(traceSummary.getByText(iso(second + 20), { exact: true }));
  await visible(traceSummary.getByText('22,000 ms', { exact: true })); assert.equal(rawRequests.length, 0, 'Raw records are not fetched before a node is selected');
  await geometry(`Chain 01 ${run}`, 4 / 22, 2 / 22); await geometry(`End only ${run}`, 0, 0, true);
  await visible(row(`End only ${run}`).getByText(/Recorded end; duration unavailable/));
  await row(`Chain 01 ${run}`).getByRole('link', { name: `Chain 01 ${run}`, exact: true }).click(); await ready();
  await visible(page.getByRole('region', { name: 'Selected trace node', exact: true }).locator('pre'));
  assert.ok((await page.getByRole('region', { name: 'Selected trace node', exact: true }).locator('pre').innerText()).includes('9223372036854775807'));
  const selectedChain = params().traceNode; assert.ok(rawRequests.every(url => new URL(url).pathname.endsWith(`/nodes/${selectedChain}`)));
  await page.getByRole('button', { name: 'Next nodes', exact: true }).click(); await ready(); await visible(row(`Chain 49 ${run}`));
  assert.equal(params().tracePage, '2'); preserved(initialOrigin); await visible(row(`Chain 49 ${run}`).getByText('Depth 49 · 1 children', { exact: true }));
  const parentLink = row(`Chain 49 ${run}`).getByRole('link', { name: 'Jump to parent (page 1)', exact: true });
  await parentLink.click(); await ready(); assert.equal(params().tracePage, '1');
  assert.equal(await row(`Chain 48 ${run}`).getByRole('link', { name: `Chain 48 ${run}`, exact: true }).getAttribute('aria-current'), 'true', 'Parent jump selects its actual node on the parent page');
  assert.notEqual(params().traceNode, selectedChain); preserved(initialOrigin);
  await page.getByRole('button', { name: 'Next nodes', exact: true }).click(); await ready(); await visible(row(`Zero point ${run}`));
  await geometry(`Zero point ${run}`, 7 / 22, 0, true); await geometry(`Start only ${run}`, 1, 0, true);
  await visible(row(`Reversed ${run}`).getByText(/Invalid interval/)); assert.equal(await row(`Reversed ${run}`).getByRole('img').locator('[aria-hidden="true"]').count(), 0);
  for (const [name, label] of [[`Missing parent ${run}`, 'Missing parent'], [`Ambiguous parent ${run}`, 'Ambiguous parent'], [`Cycle A ${run}`, 'Cycle reference'], [`Invalid IDs ${run}`, 'Invalid parent ID']]) await visible(row(name).getByText(label, { exact: true }));
  await visible(row(`Missing IDs ${run}`).getByText('Span ID: missing', { exact: true })); await visible(row(`Invalid IDs ${run}`).getByText('Span ID: invalid', { exact: true }));
  const preview = rows().filter({ hasText: 'Long preview ' }); await visible(preview.getByText('Preview truncated. Select this node to inspect the full original JSON.', { exact: true }));
  await preview.getByRole('link').first().click(); await ready(); const rawPanel = page.getByRole('region', { name: 'Selected trace node', exact: true });
  const rawText = await rawPanel.locator('pre').innerText(); assert.ok(rawText.includes('λ'.repeat(2050))); assert.ok(rawText.includes('9223372036854775807')); assert.ok(rawText.includes('full-selected-child')); assert.ok(rawText.includes(JSON.stringify(unsafe).slice(1, -1)));
  assert.equal(await page.getByRole('main').locator('img').count(), 0); assert.equal(await page.evaluate(() => typeof window.__goodsentryTraceUnsafe), 'undefined');
  const selectedLong = params().traceNode; await page.reload({ waitUntil: 'networkidle' }); await ready(); assert.equal(params().traceNode, selectedLong); assert.equal(params().tracePage, '2'); preserved(initialOrigin);
  await capture('trace-forest'); await capture('selected-raw', page, 'Selected node JSON');
  await page.getByRole('button', { name: 'Next errors', exact: true }).click(); await ready(); await visible(page.getByText('26 errors · Page 2', { exact: true }));
  assert.equal(params().tracePage, '2'); assert.equal(params().traceNode, selectedLong); assert.equal(params().traceErrorsPage, '2'); preserved(initialOrigin);
  await page.getByLabel('Trace project', { exact: true }).selectOption(projectB.id); await page.getByRole('button', { name: 'Apply scope', exact: true }).click(); await ready(); await statistic('Nodes', 5);
  assert.equal(params().project, projectA.id); assert.equal(params().traceProject, projectB.id); for (const key of ['tracePage', 'traceNode', 'traceErrorsPage']) assert.equal(params()[key], undefined); preserved(initialOrigin);
  await page.reload({ waitUntil: 'networkidle' }); await ready(); assert.equal(await page.getByLabel('Trace project', { exact: true }).inputValue(), projectB.id);
  await page.getByRole('link', { name: 'View related logs', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: 'Logs', exact: true }));
  assert.equal(await page.getByLabel('Project', { exact: true }).inputValue(), projectB.id); assert.equal(await page.getByLabel('Trace ID', { exact: true }).inputValue(), traceID);
  for (const key of ['start', 'end', 'window', ...traceKeys]) assert.equal(params()[key], undefined);
  await visible(page.getByText(`Cross-project trace log ${run}`, { exact: true }));
  assert.equal(params().traceMatch, 'canonical'); await visible(page.getByText('Identifies traces regardless of letter case', { exact: false }));
  await page.reload({ waitUntil: 'networkidle' }); await visible(page.getByText(`Cross-project trace log ${run}`, { exact: true })); assert.equal(params().traceMatch, 'canonical');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click(); await visible(page.getByText(`Cross-project trace log ${run}`, { exact: true })); assert.equal(params().traceMatch, 'canonical');
  await page.getByLabel('Trace ID', { exact: true }).fill('not-a-trace'); await page.getByRole('button', { name: 'Apply filters', exact: true }).click(); await visible(page.getByRole('heading', { name: 'No matching logs', exact: true }));
  assert.equal(params().traceMatch, undefined); assert.equal(await page.getByText('Identifies traces regardless of letter case', { exact: false }).count(), 0);
  checks.push('Actual Transactions entry opens all-project/all-time trace; server-scaled waveform/zero/end/start points, invalid intervals, relationship labels/depth and cross-page parent jump render accurately; lazy raw retains full truncated previews/int64 safely and scope/pages/selection survive reload');

  await page.goto(occurrenceURL, { waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { level: 1, name: rootName, exact: true }));
  await page.getByRole('button', { name: 'Next spans', exact: true }).click(); await visible(page.getByText(/Page 2/)); const occurrenceOrigin = params();
  await page.getByRole('link', { name: 'View trace', exact: true }).click(); await ready(); assert.equal(params().page, '2'); assert.equal(params().tracePage, undefined); preserved(occurrenceOrigin);
  await page.getByRole('link', { name: '← Back to transactions', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: rootName, exact: true })); assert.deepEqual(params(), occurrenceOrigin);
  await page.goto(route(base, { view: 'logs', org: organization, project: projectA.id, query: logPrefix, level: 'warn', environment: 'browser-trace', service: 'trace-service', trace: traceID, traceMatch: 'canonical', ...bounds }), { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: 'Next', exact: true }).click(); await visible(page.getByText('1 log on this page · Page 2', { exact: true })); const logOrigin = params();
  await page.getByRole('row').filter({ hasText: `${logPrefix} #00` }).getByRole('link', { name: 'View trace', exact: true }).click(); await ready(); assert.equal(params().returnView, 'logs'); preserved(logOrigin);
  await page.getByRole('link', { name: '← Back to logs', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Logs', exact: true, level: 1 })); assert.deepEqual(params(), logOrigin);
  await page.getByRole('row').filter({ hasText: `${logPrefix} #00` }).getByRole('link', { name: 'View trace', exact: true }).click(); await ready();
  await page.getByRole('link', { name: 'Issues', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  for (const key of [...traceKeys, 'traceMatch']) assert.equal(params()[key], undefined);
  await page.goto(initialTrace, { waitUntil: 'networkidle' }); await ready(); await page.getByRole('button', { name: 'Next errors', exact: true }).click(); await ready(); await visible(page.getByText('26 errors · Page 2', { exact: true }));
  await page.getByRole('region', { name: 'Trace errors', exact: true }).getByRole('link', { name: 'Open issue', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: `Trace error ${run} #25`, exact: true })); const issueOrigin = params();
  await page.getByRole('link', { name: 'View trace', exact: true }).click(); await ready(); assert.equal(params().returnView, 'issues'); preserved(issueOrigin);
  await page.getByRole('link', { name: '← Back to issues', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: `Trace error ${run} #25`, exact: true })); assert.deepEqual(params(), issueOrigin);
  checks.push('Occurrence page2, filtered Logs page2 and selected issue each enter View trace and return with every original source/filter/page intact; errors page25+1 and related links work, including an actual uppercase log with explicit canonical matching/reload/application and restoration of exact mode for nonhex input');

  const orphanTrace = randomBytes(16).toString('hex'), orphanTitle = `Only related error ${run}`;
  await send(projectA, 'event', { event_id: randomBytes(16).toString('hex'), message: orphanTitle, contexts: { trace: { trace_id: orphanTrace, span_id: rootID } } });
  await page.goto(route(base, { view: 'transactions', org: organization, traceView: orphanTrace, returnView: 'transactions', project: projectA.id, ...bounds }), { waitUntil: 'networkidle' }); await ready();
  await statistic('Nodes', 0); await statistic('Errors', 1); await visible(page.getByRole('heading', { name: 'No transaction or span nodes', exact: true })); await visible(page.getByText(orphanTitle, { exact: true })); await capture('related-only-trace');
  const disabled = await session(disabledBase); await disabled.tab.goto(route(disabledBase, Object.fromEntries(new URL(initialTrace).searchParams)), { waitUntil: 'networkidle' }); await ready(disabled.tab);
  await statistic('Nodes', 71, disabled.tab); assert.equal(await disabled.tab.getByRole('link', { name: 'View related logs', exact: true }).count(), 0); assert.equal(await disabled.tab.getByRole('link', { name: 'Logs', exact: true }).count(), 0);
  assert.equal(await disabled.tab.getByRole('region', { name: 'Trace summary', exact: true }).getByText('Logs', { exact: true }).count(), 0, 'Disabled logs do not expose a misleading count');
  await visible(disabled.tab.getByText('Related logs are disabled on this server.', { exact: true })); await capture('logs-disabled-trace', disabled.tab);
  const absent = randomBytes(16).toString('hex'); absentPath = new URL(`api/v1/organizations/${organization}/traces/${absent}`, base).pathname;
  await page.goto(route(base, { view: 'transactions', org: organization, traceView: absent, returnView: 'transactions', project: projectA.id, ...bounds }), { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Could not load this trace', exact: true })); await visible(page.getByRole('button', { name: 'Try again', exact: true })); await capture('trace-not-found', page, null);
  await page.getByRole('link', { name: '← Back to transactions', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Transactions', level: 1, exact: true }));
  checks.push('Related-error-only traces show an honest empty waterfall; disabled logs leave graph inspection working without log links/counts; sidebar clears trace state and a real404 keeps useful Back/retry controls');

  const pendingTrace = randomBytes(16).toString('hex'), pendingEvent = randomBytes(16).toString('hex'), pendingName = `Pending trace ${run}`;
  await send(projectA, 'transaction', { ...legacy, event_id: pendingEvent, transaction: pendingName, contexts: { trace: { trace_id: pendingTrace, span_id: rootID, op: 'http.server' } }, spans: [chain[0]] });
  await page.goto(transactionURL, { waitUntil: 'networkidle' }); const pendingRow = page.getByRole('row').filter({ has: page.getByRole('link', { name: pendingName, exact: true }) });
  const reference = new URL(await pendingRow.getByRole('link', { name: pendingName, exact: true }).getAttribute('href'), base);
  const pendingURL = new URL(await pendingRow.getByRole('link', { name: 'View trace', exact: true }).getAttribute('href'), base);
  await writeFile(resolve(handshake, 'pending-reference.json'), JSON.stringify({ organization, projectId: projectA.id, eventId: pendingEvent, traceId: pendingTrace, sample: { source: reference.searchParams.get('sampleSource'), id: reference.searchParams.get('sample'), projectId: projectA.id } }, null, 2));
  console.log(JSON.stringify({ stage: 'waiting-for-indexing-held', handshake })); await signal('indexing-held');
  pendingPath = new URL(`api/v1/organizations/${organization}/traces/${pendingTrace}`, base).pathname;
  await page.goto(pendingURL.href, { waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'Historical trace data is being indexed', exact: true })); assert.equal(await forest().count(), 0);
  await page.getByRole('button', { name: 'Retry trace', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Historical trace data is being indexed', exact: true })); await capture('trace-indexing', page, null);
  await writeFile(resolve(handshake, 'indexing-observed'), 'Real trace503, no partial forest and retry observed.\n'); await signal('indexing-released');
  let indexed = false; const deadline = Date.now() + 120000;
  while (Date.now() < deadline) {
    const response = page.waitForResponse(response => new URL(response.url()).pathname === pendingPath); await page.getByRole('button', { name: 'Retry trace', exact: true }).click();
    if ((await response).status() === 200) { indexed = true; break; }
    await visible(page.getByRole('heading', { name: 'Historical trace data is being indexed', exact: true })); await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.ok(indexed, 'Normal historical worker completes trace projection within120s'); await ready(); await statistic('Nodes', 2);
  checks.push('Real historical trace503 shows indexing guidance with no partial forest, and retry displays actual graph metadata after the external row lock is released');
  const expected = entry => (entry.status === 503 && new URL(entry.url).pathname === pendingPath) || (entry.status === 404 && new URL(entry.url).pathname === absentPath);
  assert.ok(badResponses.filter(entry => entry.status === 503).length >= 2); assert.deepEqual(badResponses.filter(entry => !expected(entry)), []);
  assert.deepEqual(consoleProblems.filter(entry => !((entry.url && new URL(entry.url).pathname === pendingPath && /503/.test(entry.text)) || (entry.url && new URL(entry.url).pathname === absentPath && /404/.test(entry.text)))), []);
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []); console.log(JSON.stringify({ ok: true, checks, screenshots: screenshots.length, geometries }, null, 2));
} catch (error) {
  failure = error; console.error(error.stack || error.message); if (page) await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: base.href, checks, screenshots, dimensions, geometries, pageErrors, requestFailures, consoleProblems, badResponses, rawRequests, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
