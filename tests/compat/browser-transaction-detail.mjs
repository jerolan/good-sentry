// Browser owner: occurrence navigation, visible fidelity, URL state and controls.
// Fixtures enter through real HTTP ingestion; no responses or browser APIs are mocked.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomBytes } from 'node:crypto';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseURL = new URL(process.env.BASE_URL || process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18005/');
const disabledURL = new URL(process.env.DISABLED_BASE_URL || process.env.GOODSENTRY_DISABLED_URL || 'http://127.0.0.1:18006/');
for (const url of [baseURL, disabledURL]) if (!url.pathname.endsWith('/')) url.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL, password = process.env.GOODSENTRY_TEST_PASSWORD;
const memberEmail = process.env.COMPAT_MEMBER_EMAIL, memberPassword = process.env.COMPAT_MEMBER_PASSWORD;
assert.ok(email && password && memberEmail && memberPassword, 'Provide real owner and organization-member fixture credentials');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/transaction-detail-browser');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const checks = [], screenshots = [], dimensions = [], pageErrors = [], consoleProblems = [], requestFailures = [], badResponses = [];
const runID = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const legacyName = `Legacy checkout ${runID}`, segmentName = `Stream checkout ${runID}`;
const legacyTrace = randomBytes(16).toString('hex'), segmentTrace = randomBytes(16).toString('hex');
const legacySpan = '1111111111111111', legacyParent = '2222222222222222';
const segmentSpan = 'aaaaaaaaaaaaaaaa', siblingSpan = 'bbbbbbbbbbbbbbbb';
const eventID = randomBytes(16).toString('hex');
const unsafe = '<img src=x onerror="window.__goodsentryOccurrenceUnsafe=1">';
const validChildName = `Load cart ${unsafe}`, missingChildName = `Missing timing ${runID}`, malformedChildName = `Malformed timing ${runID}`;
const reversedChildName = `Reversed timing ${runID}`, externalName = `Other segment span ${runID}`, siblingName = `Sibling segment ${runID}`;
const firstSecond = Math.floor(Date.now() / 1000) - 7200;
const iso = seconds => new Date(seconds * 1000).toISOString().replace('.000Z', 'Z');
const start = iso(firstSecond), end = iso(firstSecond + 3600);
const bounds = { start, end, window: '1h' };
const literalJSON = value => JSON.stringify(value).replaceAll('"INT64_MAX_LITERAL"', '9223372036854775807');
let page, projectA, projectB, organization, failure;

async function session(target, loginEmail, loginPassword, label) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
  const response = await context.request.post(new URL('api/v1/login', target).href, { headers: { Origin: target.origin }, data: { email: loginEmail, password: loginPassword } });
  assert.equal(response.status(), 200, `${label} authenticates against the real server`);
  const me = await response.json();
  const tab = await context.newPage(); tab.setDefaultTimeout(15000);
  tab.on('pageerror', error => pageErrors.push({ label, message: error.message }));
  tab.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ label, text: message.text(), url: message.location().url }); });
  tab.on('requestfailed', request => requestFailures.push({ label, url: request.url(), error: request.failure()?.errorText }));
  tab.on('response', response => { if (response.status() >= 400) badResponses.push({ label, url: response.url(), status: response.status() }); });
  return { context, tab, me };
}
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
function url(target, params) { const result = new URL(target); result.search = new URLSearchParams(params).toString(); return result.href; }
function identity(tab) {
  const params = new URL(tab.url()).searchParams;
  return Object.fromEntries(['view', 'org', 'project', 'sampleSource', 'sample', 'start', 'end', 'window'].map(key => [key, params.get(key)]));
}
function spanRows(tab) { return tab.getByRole('region', { name: 'Occurrence spans', exact: true }).getByRole('row').filter({ has: tab.getByRole('cell') }); }
async function definition(tab, value) {
  const exact = new RegExp(`^${String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`);
  const matches = tab.getByRole('definition').filter({ hasText: exact });
  assert.ok(await matches.count() >= 1, 'Expected a visible definition for ' + String(value));
  await visible(matches.first());
}
async function detailReady(tab, name) { await visible(tab.getByRole('heading', { level: 1, name, exact: true })); await visible(tab.getByRole('region', { name: 'Occurrence spans', exact: true })); }
async function capture(tab, name, withContext = false) {
  await tab.getByRole('status').filter({ hasText: 'Trace ID copied.' }).waitFor({ state: 'hidden' });
  for (const width of [320, 768, 1440]) {
    await tab.setViewportSize({ width, height: 1000 });
    await tab.getByRole('heading', { level: 1 }).focus(); await tab.keyboard.press('Control+Home'); await tab.mouse.click(5, 5);
    assert.equal(await tab.evaluate(() => scrollY), 0, 'Overview starts at the document origin');
    const size = await tab.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, `${name} fits ${width}px`);
    for (const section of ['overview', ...(withContext ? ['context'] : []), 'spans']) {
      if (section !== 'overview') await tab.getByRole('heading', { level: 2, name: section === 'context' ? 'Trace context' : /^(Transaction spans|Spans in this project and trace)$/ }).evaluate(element => element.scrollIntoView({ block: 'start' }));
      const path = resolve(output, `${name}-${section}-${width}.png`);
      await tab.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path);
    }
  }
}
async function send(context, project, type, payload, extra = {}) {
  const dsn = new URL(project.dsn), raw = literalJSON(payload);
  const envelope = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type, length: Buffer.byteLength(raw), ...extra })}\n${raw}\n`;
  const response = await context.request.post(new URL(`api/${project.id}/envelope/`, baseURL).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-occurrence-browser/1.0` }, data: envelope,
  });
  assert.equal(response.status(), 200, `${type} fixture is durably accepted: ${response.status() === 200 ? '' : await response.text()}`);
}
async function openList(tab, projectID) {
  await tab.goto(url(baseURL, { view: 'transactions', org: organization, ...bounds, ...(projectID ? { project: projectID } : {}) }), { waitUntil: 'networkidle' });
  await visible(tab.getByRole('heading', { name: 'Transactions', level: 1, exact: true }));
}
async function openSample(tab, name, source) {
  await tab.getByRole('link', { name, exact: true }).click(); await detailReady(tab, name);
  assert.equal(identity(tab).sampleSource, source); assert.match(identity(tab).sample, /^[1-9]\d*$/);
  assert.equal(identity(tab).start, start); assert.equal(identity(tab).end, end); assert.equal(identity(tab).window, bounds.window);
}
async function raw(tab, summary, expected) {
  const disclosure = tab.locator('details').filter({ has: tab.getByText(summary, { exact: true }) });
  await disclosure.locator('summary').click();
  const value = await disclosure.locator('pre').innerText();
  for (const text of expected) assert.ok(value.includes(text), `Visible raw JSON retains ${text}`);
  assert.ok(!value.includes('9223372036854776000'), 'Visible int64 digits are not rounded');
  await disclosure.locator('summary').click();
}
async function nextPage(tab, name, expectedIdentity) {
  await tab.getByRole('button', { name: 'Next spans', exact: true }).click(); await detailReady(tab, name);
  await visible(tab.getByText('51 spans · 1 on this page · Page 2', { exact: true }));
  assert.deepEqual(identity(tab), expectedIdentity); assert.equal(new URL(tab.url()).searchParams.get('page'), '2');
  assert.equal(await spanRows(tab).count(), 1); assert.equal(await tab.getByRole('button', { name: 'Next spans', exact: true }).isEnabled(), false);
}
try {
  const owner = await session(baseURL, email, password, 'owner'); page = owner.tab;
  organization = owner.me.organizations[0].slug;
  assert.notEqual(owner.me.features.logs, false, 'Main fixture has real log navigation enabled');
  for (const suffix of ['A', 'B']) {
    const created = await owner.context.request.post(new URL(`api/v1/organizations/${organization}/projects`, baseURL).href, {
      headers: { Origin: baseURL.origin, 'X-CSRF-Token': owner.me.csrfToken }, data: { name: `Occurrence ${suffix} ${runID}`, platform: 'javascript' },
    });
    assert.equal(created.status(), 201); const project = await created.json();
    if (suffix === 'A') projectA = project; else projectB = project;
  }
  const children = [
    { description: validChildName, span_id: '3333333333333333', parent_span_id: legacySpan, op: 'db.query', status: 'ok', start_timestamp: firstSecond + 5.25,
      timestamp: `${new Date((firstSecond + 5 - 21600) * 1000).toISOString().slice(0, 19)}.750000999-06:00`, custom_context: { receipt: 'INT64_MAX_LITERAL', note: unsafe } },
    { description: missingChildName, span_id: '4444444444444444', parent_span_id: legacySpan, timestamp: firstSecond + 6 },
    { description: malformedChildName, start_timestamp: 'broken', timestamp: firstSecond + 6, retained: 'malformed original timestamp' },
    { description: reversedChildName, start_timestamp: firstSecond + 7, timestamp: firstSecond + 6 },
    ...Array.from({ length: 47 }, (_, index) => ({ description: `Legacy child ${String(index + 5).padStart(2, '0')} ${runID}`, span_id: (index + 100).toString(16).padStart(16, '0'), parent_span_id: legacySpan, op: 'task', start_timestamp: firstSecond + 5, timestamp: firstSecond + 5.01, marker: index === 46 ? 'last legacy child' : 'ordered child' })),
  ];
  const legacy = { event_id: eventID, type: 'transaction', transaction: legacyName, start_timestamp: firstSecond + 5, timestamp: firstSecond + 7,
    environment: 'browser-occurrence', release: 'checkout@legacy', contexts: { trace: { trace_id: legacyTrace, span_id: legacySpan, parent_span_id: legacyParent, op: 'http.server', status: 'ok' } },
    spans: children, extra: { receipt: 'INT64_MAX_LITERAL', unknown_context: unsafe } };
  await send(owner.context, projectA, 'transaction', { ...legacy, event_id: randomBytes(16).toString('hex'), timestamp: firstSecond + 6, spans: [] });
  await send(owner.context, projectB, 'transaction', legacy);
  const segment = { trace_id: segmentTrace, span_id: segmentSpan, parent_span_id: 'cccccccccccccccc', name: segmentName, status: 'ok', is_segment: true,
    start_timestamp: firstSecond + 30, end_timestamp: firstSecond + 31, attributes: { 'sentry.op': { type: 'string', value: 'http.server' }, 'sentry.environment': { type: 'string', value: 'browser-occurrence' }, 'sentry.release': { type: 'string', value: 'checkout@segment' }, receipt: { type: 'integer', value: 'INT64_MAX_LITERAL' }, note: { type: 'string', value: unsafe } } };
  const segments = [segment,
    { ...segment, span_id: siblingSpan, parent_span_id: undefined, name: siblingName, start_timestamp: firstSecond + 20, end_timestamp: firstSecond + 21 },
    ...Array.from({ length: 49 }, (_, index) => ({ trace_id: segmentTrace, span_id: (index + 1000).toString(16).padStart(16, '0'), parent_span_id: index === 0 ? siblingSpan : segmentSpan,
      name: index === 0 ? externalName : `Trace child ${String(index).padStart(2, '0')} ${runID}`, status: 'ok', is_segment: false,
      start_timestamp: firstSecond + (index === 0 ? -60 : 32 + index), end_timestamp: firstSecond + (index === 0 ? -59.975 : 32.025 + index),
      attributes: { 'sentry.op': { type: 'string', value: 'http.client' }, receipt: { type: 'integer', value: 'INT64_MAX_LITERAL' } } })),
  ];
  await send(owner.context, projectB, 'span', { version: 2, items: segments }, { item_count: segments.length, content_type: 'application/vnd.sentry.items.span.v2+json' });
  await send(owner.context, projectA, 'span', { version: 2, items: [{ ...segment, name: `Foreign project span ${runID}` }] }, { item_count: 1, content_type: 'application/vnd.sentry.items.span.v2+json' });
  for (const [project, body] of [[projectB, `Legacy occurrence log ${runID}`], [projectA, `Wrong project log ${runID}`]]) {
    await send(owner.context, project, 'log', { items: [{ timestamp: iso(firstSecond + 7), trace_id: legacyTrace, span_id: legacySpan, level: 'info', body, attributes: {} }] }, { item_count: 1, content_type: 'application/vnd.sentry.items.log+json' });
  }

  await openList(page); await openSample(page, legacyName, 'event');
  const legacyIdentity = identity(page), legacyURL = page.url(); assert.equal(legacyIdentity.project, null, 'All-project origin is retained');
  for (const value of [projectB.name, projectB.id, eventID, legacySpan, legacyParent, '2,000 ms', 'browser-occurrence', 'checkout@legacy', iso(firstSecond + 5), iso(firstSecond + 7)]) await definition(page, value);
  assert.equal(await spanRows(page).count(), 50); await visible(page.getByText(/Shows spans nested in this transaction/));
  const validRow = spanRows(page).filter({ hasText: validChildName });
  const validCells = await validRow.getByRole('cell').allTextContents();
  assert.deepEqual(validCells.slice(3, 6), [`${iso(firstSecond + 5).slice(0, -1)}.25Z`, `${iso(firstSecond + 5).slice(0, -1)}.75Z`, '500 ms']);
  assert.equal(validCells[6], legacySpan);
  await validRow.getByText('Raw span JSON', { exact: true }).click();
  assert.ok((await validRow.locator('pre').innerText()).includes('9223372036854775807'));
  assert.ok((await validRow.locator('pre').innerText()).includes(JSON.stringify(unsafe))); await validRow.getByText('Raw span JSON', { exact: true }).click();
  for (const child of [missingChildName, malformedChildName]) {
    const row = spanRows(page).filter({ hasText: child }); const cells = await row.getByRole('cell').allTextContents();
    assert.equal(cells[3], 'Not available'); assert.equal(cells[5], 'Not available');
  }
  assert.equal((await spanRows(page).filter({ hasText: reversedChildName }).getByRole('cell').allTextContents())[5], 'Not available');
  const malformedRow = spanRows(page).filter({ hasText: malformedChildName }); await malformedRow.getByText('Raw span JSON', { exact: true }).click();
  assert.ok((await malformedRow.locator('pre').innerText()).includes('"start_timestamp": "broken"')); await malformedRow.getByText('Raw span JSON', { exact: true }).click();
  await raw(page, 'Original transaction JSON', ['9223372036854775807', JSON.stringify(unsafe), eventID]);
  assert.equal(await page.getByRole('main').locator('img').count(), 0); assert.equal(await page.evaluate(() => typeof window.__goodsentryOccurrenceUnsafe), 'undefined');
  await page.getByRole('button', { name: 'Copy trace ID', exact: true }).click(); await visible(page.getByRole('status').filter({ hasText: 'Trace ID copied.' }));
  await capture(page, 'legacy');
  await nextPage(page, legacyName, legacyIdentity); await visible(spanRows(page).filter({ hasText: `Legacy child 51 ${runID}` }));
  await page.reload({ waitUntil: 'networkidle' }); await detailReady(page, legacyName); assert.deepEqual(identity(page), legacyIdentity);
  assert.equal(await spanRows(page).count(), 1); await capture(page, 'legacy-page2');
  const emptyURL = new URL(page.url()); emptyURL.searchParams.set('page', '3'); await page.goto(emptyURL.href, { waitUntil: 'networkidle' });
  await visible(page.getByText('Choose an earlier page to inspect the recorded spans.', { exact: true }));
  await page.getByRole('link', { name: '← All transactions', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Transactions', level: 1, exact: true }));
  assert.equal(identity(page).sample, null); assert.equal(identity(page).sampleSource, null); assert.equal(identity(page).project, null);
  assert.equal(new URL(page.url()).searchParams.get('page'), null); for (const [key, value] of Object.entries(bounds)) assert.equal(new URL(page.url()).searchParams.get(key), value);
  await openSample(page, legacyName, 'event'); await page.getByRole('link', { name: legacyTrace, exact: true }).click();
  await visible(page.getByRole('heading', { name: 'Logs', level: 1, exact: true }));
  assert.equal(await page.getByLabel('Project', { exact: true }).inputValue(), projectB.id); assert.equal(await page.getByLabel('Trace ID', { exact: true }).inputValue(), legacyTrace);
  for (const [key, value] of Object.entries(bounds)) assert.equal(new URL(page.url()).searchParams.get(key), value);
  assert.equal(identity(page).sample, null); assert.equal(identity(page).sampleSource, null);
  await visible(page.getByText(`Legacy occurrence log ${runID}`, { exact: true })); assert.equal(await page.getByText(`Wrong project log ${runID}`, { exact: true }).count(), 0);
  checks.push('Latest legacy sample opens the actual project from all-project aggregates; timing, parent context, raw unsafe text and exact int64 digits render safely, missing/malformed timing stays unavailable, and 50+1 paging/reload/back/related-log project preserve origin bounds');

  await openList(page, projectB.id); await openSample(page, segmentName, 'span');
  const segmentIdentity = identity(page), segmentURL = page.url(); assert.equal(segmentIdentity.project, projectB.id);
  for (const value of [projectB.name, segmentSpan, 'cccccccccccccccc', '1,000 ms', 'checkout@segment']) await definition(page, value);
  assert.equal(await spanRows(page).count(), 50); await visible(page.getByText(/Shows all standalone spans in this exact project and trace/));
  await visible(spanRows(page).filter({ hasText: externalName })); await visible(spanRows(page).filter({ hasText: siblingName }));
  const externalCells = await spanRows(page).filter({ hasText: externalName }).getByRole('cell').allTextContents(); assert.equal(externalCells[6], siblingSpan); assert.equal(externalCells[3], iso(firstSecond - 60));
  assert.equal(await page.getByText(`Foreign project span ${runID}`, { exact: true }).count(), 0);
  const segmentRow = spanRows(page).filter({ hasText: segmentName }); await segmentRow.getByText('Raw span JSON', { exact: true }).click();
  assert.ok((await segmentRow.locator('pre').innerText()).includes('9223372036854775807')); await segmentRow.getByText('Raw span JSON', { exact: true }).click();
  await raw(page, 'Original segment JSON', ['9223372036854775807', '"type": "integer"', JSON.stringify(unsafe)]); await capture(page, 'segment');
  await nextPage(page, segmentName, segmentIdentity); await visible(spanRows(page).filter({ hasText: `Trace child 48 ${runID}` }));
  await page.getByRole('button', { name: 'Refresh', exact: true }).click(); await detailReady(page, segmentName); await visible(page.getByText('51 spans · 1 on this page · Page 2', { exact: true }));
  assert.deepEqual(identity(page), segmentIdentity); await page.reload({ waitUntil: 'networkidle' }); await detailReady(page, segmentName); assert.deepEqual(identity(page), segmentIdentity);
  await page.getByRole('link', { name: '← All transactions', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Transactions', level: 1, exact: true }));
  assert.equal(await page.getByLabel('Project', { exact: true }).inputValue(), projectB.id);
  for (const [key, value] of Object.entries(bounds)) assert.equal(new URL(page.url()).searchParams.get(key), value);
  await openSample(page, segmentName, 'span'); await page.getByRole('link', { name: 'Issues', exact: true }).click(); await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  for (const key of ['sample', 'sampleSource', 'start', 'end', 'window']) assert.equal(new URL(page.url()).searchParams.get(key), null, 'Leaving the view clears occurrence-only navigation state');
  checks.push('Standalone segment scope shows all same-project trace spans, including another segment and an out-of-window span with its actual parent; project/window, immutable identity and 50+1 pagination survive refresh/reload/back, while navigation clears sample state');

  const member = await session(baseURL, memberEmail, memberPassword, 'member');
  assert.equal(member.me.organizations.find(org => org.slug === organization)?.role, 'member', 'Fixture is an actual member');
  for (const [target, name] of [[legacyURL, legacyName], [segmentURL, segmentName]]) { await member.tab.goto(target, { waitUntil: 'networkidle' }); await detailReady(member.tab, name); await definition(member.tab, projectB.name); }
  await capture(member.tab, 'member-segment'); checks.push('A real organization member can read both occurrence formats through the product UI');
  const disabled = await session(disabledURL, email, password, 'logs-disabled');
  assert.equal(disabled.me.features.logs, false, 'Second real server disables logs');
  for (const [target, name, trace] of [[legacyURL, legacyName, legacyTrace], [segmentURL, segmentName, segmentTrace]]) {
    await disabled.tab.goto(url(disabledURL, Object.fromEntries(new URL(target).searchParams)), { waitUntil: 'networkidle' }); await detailReady(disabled.tab, name);
    assert.equal(await disabled.tab.getByRole('link', { name: 'Logs', exact: true }).count(), 0); assert.equal(await disabled.tab.getByRole('link', { name: trace, exact: true }).count(), 0);
    await visible(disabled.tab.locator('code').filter({ hasText: trace }));
    await disabled.tab.getByRole('button', { name: 'Copy trace ID', exact: true }).click(); await visible(disabled.tab.getByRole('status').filter({ hasText: 'Trace ID copied.' }));
  }
  await capture(disabled.tab, 'logs-disabled-segment', true); checks.push('Logs-disabled server still renders both occurrence formats and trace copy, hides related-log/navigation links, and fits 320/768/1440');
  assert.deepEqual(pageErrors, []); assert.deepEqual(consoleProblems, []); assert.deepEqual(requestFailures, []); assert.deepEqual(badResponses, []);
  console.log(JSON.stringify({ ok: true, checks, screenshots: screenshots.length, dimensions }, null, 2));
} catch (error) {
  failure = error; console.error(error.stack || error.message);
  if (page) await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: baseURL.href, disabledURL: disabledURL.href, checks, screenshots, dimensions, pageErrors, consoleProblems, requestFailures, badResponses, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
