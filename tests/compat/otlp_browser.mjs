// Test-audit gate: the visible OpenTelemetry recipe in Projects and the lazy
// OTLP row detail in Logs own this browser contract. Backend Go tests cover
// authentication, storage and canonical detail DTO; this driver proves the
// actual rendered recipe, badge, typed detail, retry and safe navigation.
// Reuse a disposable runtime with pre-seeded OTLP data; no mocked responses.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
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
const browser = await chromium.launch({ headless: true }), context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [];
const secrets = [fixture.ownerEmail, fixture.ownerPassword, fixture.publicKey].filter(Boolean);
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const url = path => new URL(path, base).href;
let page, me, failure;
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 }); await page.evaluate(() => scrollTo(0, 0));
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
  const endpoint = disclosure.getByText('/v1/logs', { exact: false });
  await visible(endpoint.first());
  const recipe = await disclosure.innerText();
  assert.match(recipe, /X-GoodSentry-Key/);
  assert.match(recipe, /OTEL_EXPORTER_OTLP_LOGS_ENDPOINT/);
  assert.match(recipe, /OTEL_EXPORTER_OTLP_LOGS_HEADERS/);
  assert.match(recipe, /protobuf|gzip/i);
  assert.match(recipe, /DSN/);
  await capture('projects-otlp-recipe');
  checks.push('Projects exposes the exact prefixed OTLP endpoint, copyable key header, exporter variables and DSN distinction');

  await page.goto(new URL('?org=' + encodeURIComponent(fixture.orgSlug) + '&view=logs', base).href, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Logs', exact: true, loc0: 1 }));
  const otlpRow = page.getByRole('row').filter({ hasText: fixture.expectedBody });
  await visible(otlpRow);
  assert.equal(await otlpRow.getByText('OTLP', { exact: true }).count(), 1, 'OTLP badge visible exactly once');
  await otlpRow.locator('details.otlp-entry summary').click();
  const detail = page.locator('.otlp-detail[data-loaded="done"]');
  await visible(detail.getByText('resource', { exact: true }));
  const detailText = await detail.innerText();
  for (const token of [fixture.expectedBody, fixture.expectedSeverityText, fixture.expectedService, fixture.expectedEnvironment, fixture.expectedTraceId, fixture.expectedSpanId, fixture.expectedTimestampNano]) assert.match(detailText, new RegExp(token));
  assert.match(detailText, /timestampSource/); assert.match(detailText, /receivedAt/); assert.match(detailText, /traceFlags/);
  await capture('logs-otlp-detail');
  checks.push('Logs OTLP row shows badge and lazy detail with resource, scope, record, severity, exact nanoseconds and metadata');

  await otlpRow.locator('details.otlp-entry summary').click();
  await visible(page.getByRole('heading', { name: 'Logs', exact: true }));
  await capture('logs-after-close');
  checks.push('Detail close returns to Logs without stale detail content');

  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []); assert.deepEqual(badResponses, []);
  assert.deepEqual(consoleProblems, []);
} catch (error) { failure = error; console.error(safe(error.stack || error)); }
finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ passed: !failure, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure && safe(failure.message) }, null, 2) + '\n');
  await browser.close();
}
if (failure) throw new Error(safe(failure.message));
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
