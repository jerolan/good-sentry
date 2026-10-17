// Test-audit gate: the visible OpenTelemetry recipe in Projects and the lazy
// OTLP row detail in Logs own this browser contract. Backend Go tests cover
// authentication, storage and canonical detail DTO; this driver proves the
// actual rendered recipe, badge, typed detail, retry and safe navigation.
// Credible regressions include wrong endpoints, collapsed empty keys, rounded
// integers, coalesced toggle retries and a stale result after navigation.
// No production seam. One declared failure and a held real response exercise
// lifecycle states; every successful response comes from the running server.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_OTLP_FIXTURE;
assert.ok(fixturePath && typeof fixturePath === 'string', 'Provide GOODSENTRY_OTLP_FIXTURE pointing at a private fixture JSON');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const base = new URL(fixture.baseURL);
assert.equal(base.protocol, 'http:'); assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
assert.ok(!['18080', '18081', '18009'].includes(base.port), 'Protected acceptance runtimes are not browser targets');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/otlp-browser'); await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH } : {}) }), context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [];
const secrets = [fixture.ownerEmail, fixture.ownerPassword, fixture.publicKey].filter(Boolean);
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const url = path => new URL(path, base).href;
let page, me, failure;
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function capture(name, focus) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 }); await page.evaluate(() => scrollTo(0, 0));
    if (focus) await focus.scrollIntoViewIfNeeded();
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, name + ' fits ' + width + 'px');
    const file = resolve(output, name + '-' + width + '.png');
    await page.screenshot({ path: file, fullPage: false, animations: 'disabled' }); screenshots.push(file);
  }
}
try {
  const login = await context.request.post(url('api/v1/login'), { headers: { Origin: base.origin }, data: { email: fixture.ownerEmail, password: fixture.ownerPassword } });
  assert.equal(login.status(), 200); me = await login.json();
  assert.ok(me.organizations.some(value => value.slug === fixture.orgSlug));
  page = await context.newPage(); page.setDefaultTimeout(20000);
  page.on('pageerror', error => pageErrors.push(safe(error.message)));
  page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  page.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), status: response.status() }); });
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: safe(message.text()), url: message.location().url }); });

  await page.goto(new URL('?org=' + encodeURIComponent(fixture.orgSlug) + '&view=projects', base).href, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Projects', exact: true, level: 1 }));
  const disclosure = page.locator('details.otlp-disclosure', { hasText: fixture.projectName });
  await disclosure.locator('summary').click();
  const endpoint = disclosure.getByLabel('OTLP endpoint', { exact: true });
  assert.equal(await endpoint.textContent(), url('v1/logs'));
  const recipe = await disclosure.innerText();
  assert.match(recipe, /X-GoodSentry-Key/);
  assert.match(recipe, /OTEL_EXPORTER_OTLP_LOGS_ENDPOINT/);
  assert.match(recipe, /OTEL_EXPORTER_OTLP_LOGS_HEADERS/);
  assert.match(recipe, /protobuf|gzip/i);
  assert.match(recipe, /DSN/);
  assert.match(recipe, /BatchLogRecordProcessor/);
  assert.match(recipe, /Compression.Gzip/);
  await writeFile(resolve(output, 'rendered-example.py'), await disclosure.getByLabel('Python exporter example').textContent());
  await capture('projects-otlp-recipe', disclosure);
  checks.push('Projects exposes the exact prefixed OTLP endpoint, copyable key header, exporter variables and DSN distinction');

  let reads = 0;
  page.on('request', request => { if (/\/logs\/[1-9][0-9]*$/.test(new URL(request.url()).pathname)) reads++; });
  const logsURL = new URL('?org=' + encodeURIComponent(fixture.orgSlug) + '&view=logs&project=' + fixture.projectId, base).href;
  await page.goto(logsURL, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Logs', exact: true }));
  assert.equal(reads, 0, 'Detail is not prefetched');
  const otlpRow = page.getByRole('row').filter({ hasText: fixture.expectedBody }).first();
  await visible(otlpRow);
  assert.equal(await otlpRow.getByText('OTLP', { exact: true }).count(), 1, 'OTLP badge visible exactly once');
  await otlpRow.locator('details.otlp-entry summary').click();
  const detail = otlpRow.locator('.otlp-detail');
  await visible(detail.getByRole('heading', { name: 'Resource', exact: true }));
  const detailText = await detail.innerText();
  for (const token of [fixture.expectedBody, fixture.expectedSeverityText, fixture.expectedService, fixture.expectedEnvironment, fixture.expectedTraceId, fixture.expectedSpanId, fixture.expectedTimestampNano, '9223372036854775807', '-9223372036854775808', 'AAH/', 'NaN', '\\u0000']) assert.ok(detailText.includes(token), token);
  assert.match(detailText, /Timestamp source/); assert.match(detailText, /Received at/); assert.match(detailText, /Trace flags/);
  assert.equal(await detail.locator('dt').filter({ hasText: /^"duplicate"$/ }).count(), 2);
  assert.equal(await detail.locator('script,img,a[href^="https://example.invalid"]').count(), 0);
  if (fixture.expectEmptyKey) {
    assert.equal(await detail.locator('dt').filter({ hasText: /^""$/ }).count(), 1, 'An empty key remains empty');
    assert.ok(!detailText.includes('Dictionary key index: 0'));
  }
  assert.equal(reads, 1);
  await capture('logs-otlp-detail', detail);
  checks.push('Logs OTLP row shows badge and lazy detail with resource, scope, record, severity, exact nanoseconds and metadata');

  await otlpRow.locator('details.otlp-entry summary').click();
  await otlpRow.locator('details.otlp-entry summary').click();
  assert.equal(reads, 1, 'Reopening a cached detail does not refetch');
  await visible(page.getByRole('heading', { name: 'Logs', exact: true }));
  await capture('logs-after-close');
  checks.push('Detail close returns to Logs without stale detail content');

  // The browser owns retry/coalesced-toggle and navigation state. Inject only
  // an explicit read failure; every success is the actual backend response.
  const pattern = '**/api/v1/organizations/*/logs/*';
  let failed = false;
  const failOnce = async route => {
    const actual = await route.fetch(); assert.equal(actual.status(), 200);
    if (!failed) { failed = true; await route.fulfill({ status: 503, contentType: 'application/json', body: '{"detail":"Declared detail failure"}' }); }
    else await route.fulfill({ response: actual });
  };
  await page.goto(logsURL, { waitUntil: 'networkidle' }); await page.route(pattern, failOnce);
  await otlpRow.locator('summary').click();
  await detail.getByRole('button', { name: 'Try again', exact: true }).click();
  await visible(detail.getByRole('heading', { name: 'OpenTelemetry log detail', exact: true }));
  await page.unroute(pattern, failOnce);
  checks.push('The failed lazy read retries and renders the actual selected backend record');

  await page.goto(logsURL, { waitUntil: 'networkidle' });
  let release, arrived, finished;
  const gate = new Promise(resolve => { release = resolve; }), arrival = new Promise(resolve => { arrived = resolve; }), completion = new Promise(resolve => { finished = resolve; });
  const hold = async route => { const actual = await route.fetch(); arrived(); await gate; await route.fulfill({ response: actual }); finished(); };
  await page.route(pattern, hold); const beforePending = reads;
  await otlpRow.locator('summary').click(); await arrival;
  await visible(detail.getByRole('status'));
  await capture('logs-loading', detail);
  await otlpRow.locator('summary').click(); await otlpRow.locator('summary').click(); assert.equal(reads, beforePending + 1);
  await page.getByRole('link', { name: 'Projects', exact: true }).click();
  await visible(page.getByRole('heading', { name: 'Projects', level: 1, exact: true }));
  release(); await completion; await page.unroute(pattern, hold);
  assert.equal(await page.locator('.otlp-detail').count(), 0);
  checks.push('Close/reopen during loading sends once and a pending response cannot attach after navigation');

  await page.goto(logsURL, { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: 'Next', exact: true }).click(); await visible(page.getByText(/Page 2/));
  assert.equal(new URL(page.url()).searchParams.get('project'), String(fixture.projectId));
  checks.push('Project selection survives log pagination');

  await page.goto(logsURL, { waitUntil: 'networkidle' });
  await page.getByLabel('Search logs', { exact: true }).fill(fixture.expectedBody);
  await page.getByLabel('Service', { exact: true }).fill(fixture.expectedService);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await visible(otlpRow);
  assert.equal(new URL(page.url()).searchParams.get('service'), fixture.expectedService);
  assert.equal(await page.getByRole('row').filter({ hasText: 'Pagination filler' }).count(), 0);
  await otlpRow.getByRole('link', { name: 'View trace', exact: true }).click();
  await visible(page.getByRole('heading', { name: /Trace/ }).first());
  assert.equal(new URL(page.url()).searchParams.get('traceView'), fixture.expectedTraceId);
  checks.push('Real text/service filters isolate OTLP records and the stored trace link opens its trace');

  if (fixture.sdkBody && fixture.sdkProjectId) {
    await page.goto(new URL(`?org=${fixture.orgSlug}&view=logs&project=${fixture.sdkProjectId}`, base).href, { waitUntil: 'networkidle' });
    const sdkRow = page.getByRole('row').filter({ hasText: fixture.sdkBody }).first();
    assert.equal(await sdkRow.getByText('OTLP', { exact: true }).count(), 0);
    await sdkRow.locator('summary').click();
    await visible(sdkRow.getByRole('heading', { name: 'Log attributes', exact: true }));
    assert.ok((await sdkRow.innerText()).includes('9223372036854775807'));
    checks.push('Existing SDK logs retain their separate attributes disclosure and exact integer text');
  }

  if (fixture.memberEmail) {
    const member = await browser.newContext();
    const auth = await member.request.post(url('api/v1/login'), { data: { email: fixture.memberEmail, password: fixture.memberPassword } }); assert.equal(auth.status(), 200);
    const memberPage = await member.newPage(); await memberPage.goto(logsURL, { waitUntil: 'networkidle' });
    await memberPage.getByRole('row').filter({ hasText: fixture.expectedBody }).first().locator('summary').click();
    await visible(memberPage.getByRole('heading', { name: 'OpenTelemetry log detail', exact: true })); await member.close();
    checks.push('Actual organization member reads selected OTLP detail');
  }
  if (fixture.disabledBaseURL) {
    const disabled = await browser.newContext();
    const auth = await disabled.request.post(new URL('api/v1/login', fixture.disabledBaseURL).href, { data: { email: fixture.ownerEmail, password: fixture.ownerPassword } }); assert.equal(auth.status(), 200);
    const disabledPage = await disabled.newPage(); await disabledPage.goto(new URL(`?org=${fixture.orgSlug}&view=projects`, fixture.disabledBaseURL).href, { waitUntil: 'networkidle' });
    const unavailable = disabledPage.locator('details.otlp-disclosure', { hasText: fixture.projectName }); await unavailable.locator('summary').click();
    assert.ok((await unavailable.innerText()).includes('disabled')); assert.equal(await unavailable.getByLabel('Python exporter example').count(), 0); await disabled.close();
    checks.push('Disabled ingestion shows unavailability without offering a working recipe');
  }

  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.ok(badResponses.every(response => response.status === 503 && /\/logs\/[1-9][0-9]*$/.test(new URL(response.url).pathname)));
  assert.ok(consoleProblems.every(message => /503/.test(message.text) || (new URL(message.url).pathname === '/favicon.ico' && /404/.test(message.text))));
} catch (error) { failure = error; console.error(safe(error.stack || error)); }
finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ passed: !failure, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure && safe(failure.message) }, null, 2) + '\n');
  await browser.close();
}
if (failure) throw new Error(safe(failure.message));
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
