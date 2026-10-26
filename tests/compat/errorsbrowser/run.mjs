// Official Sentry browser bundle 11.7.0 error acceptance fixture.
// Required: GOODSENTRY_DSN. Optional: GOODSENTRY_RUN, GOODSENTRY_ERRORS_MODE
// (single|group|retry), GOODSENTRY_ERRORS_COUNT, GOODSENTRY_ERRORS_EVENT_ID,
// GOODSENTRY_ERRORS_MESSAGE, GOODSENTRY_ERRORS_OUTPUT,
// GOODSENTRY_BROWSER_BUNDLE and PLAYWRIGHT_MODULE.
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';

const dsn = process.env.GOODSENTRY_DSN;
assert.ok(dsn, 'GOODSENTRY_DSN is required');
const run = process.env.GOODSENTRY_RUN || String(Date.now());
const mode = process.env.GOODSENTRY_ERRORS_MODE || 'single';
const message = process.env.GOODSENTRY_ERRORS_MESSAGE || `Browser SDK error ${run}`;
const bundlePath = process.env.GOODSENTRY_BROWSER_BUNDLE || '/tmp/sentry-browser-11.7.0.min.js';
const bundle = await readFile(bundlePath);
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');

const pageHTML = `<!doctype html><html><head><meta charset="utf-8"><title>GoodSentry browser SDK fixture</title></head><body>
<h1>Browser SDK fixture</h1>
<script src="/sentry-browser.js"></script>
<script>
window.sentryFixture = {
  init(dsn) {
    Sentry.init({ dsn, environment: 'errors-browser-acceptance', release: 'errors-browser@1.0.0', tracesSampleRate: 0, defaultIntegrations: false, sendDefaultPii: false });
    return Sentry.SDK_VERSION;
  },
  single(message, tag, userID) {
    let eventID;
    Sentry.withScope(scope => {
      scope.setTag('acceptance.run', tag);
      scope.setTag('component', 'errors-browser');
      scope.setContext('acceptance', { run: tag, kind: 'error' });
      scope.setUser({ id: userID });
      eventID = Sentry.captureException(new Error(message));
    });
    return eventID;
  },
  group(message, run, count) {
    const ids = [];
    const fingerprint = ['errors-browser-group', run];
    for (let index = 0; index < count; index++) {
      Sentry.withScope(scope => {
        scope.setFingerprint(fingerprint);
        scope.setTag('acceptance.run', 'errors-browser-' + run);
        scope.setTag('component', 'errors-browser');
        scope.setContext('acceptance', { run, kind: 'error', occurrence: index });
        scope.setUser({ id: 'errors-browser-user-' + run });
        ids.push(Sentry.captureException(new Error(message)));
      });
    }
    return { ids, fingerprint };
  },
  retry(message, run, fixed) {
    const ids = [];
    const fingerprint = ['errors-browser-retry', run];
    for (let attempt = 0; attempt < 2; attempt++) {
      ids.push(Sentry.captureEvent({
        event_id: fixed,
        message,
        level: 'error',
        platform: 'javascript',
        timestamp: Date.now() / 1000,
        environment: 'errors-browser-acceptance',
        release: 'errors-browser@1.0.0',
        fingerprint,
        tags: { 'acceptance.run': 'errors-browser-' + run, component: 'errors-browser' },
        contexts: { acceptance: { run, kind: 'error', attempt } },
        user: { id: 'errors-browser-user-' + run },
        exception: { values: [{ type: 'Error', value: message }] },
      }));
    }
    return { ids, fingerprint };
  },
  flush() { return Sentry.flush(10000); },
};
</script></body></html>`;

const server = createServer((request, response) => {
  if (request.url === '/sentry-browser.js') {
    response.writeHead(200, { 'Content-Type': 'application/javascript', 'Cache-Control': 'no-store' });
    response.end(bundle);
    return;
  }
  response.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
  response.end(pageHTML);
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;

const browser = await chromium.launch({ args: ['--no-sandbox'] });
const result = { sdkVersion: null, mode, run, message, environment: 'errors-browser-acceptance', release: 'errors-browser@1.0.0', tag: `errors-browser-${run}`, userID: `errors-browser-user-${run}`, eventIDs: [] };
try {
  const page = await browser.newPage();
  const errors = [];
  const failures = [];
  page.on('pageerror', error => errors.push(String(error)));
  page.on('requestfailed', request => failures.push({ url: request.url(), error: request.failure()?.errorText }));
  await page.goto(origin, { waitUntil: 'networkidle' });
  await page.waitForFunction(() => window.Sentry && window.sentryFixture);
  result.sdkVersion = await page.evaluate(dsn => window.sentryFixture.init(dsn), dsn);
  assert.equal(result.sdkVersion, '11.7.0');
  if (mode === 'single') {
    const id = await page.evaluate(({ message, tag, userID }) => window.sentryFixture.single(message, tag, userID), { message, tag: result.tag, userID: result.userID });
    assert.ok(id, 'browser SDK dropped the exception');
    assert.ok(await page.evaluate(() => window.sentryFixture.flush()));
    result.eventIDs = [id];
  } else if (mode === 'group') {
    const count = Number(process.env.GOODSENTRY_ERRORS_COUNT || 3);
    assert.ok(Number.isInteger(count) && count >= 1 && count <= 61);
    const grouped = await page.evaluate(({ message, run, count }) => window.sentryFixture.group(message, run, count), { message, run, count });
    assert.ok(grouped.ids.every(Boolean));
    assert.ok(await page.evaluate(() => window.sentryFixture.flush()));
    result.eventIDs = grouped.ids;
    result.fingerprint = grouped.fingerprint;
  } else if (mode === 'retry') {
    const fixed = process.env.GOODSENTRY_ERRORS_EVENT_ID || '';
    assert.match(fixed, /^[0-9a-f]{32}$/);
    const retried = await page.evaluate(({ message, run, fixed }) => window.sentryFixture.retry(message, run, fixed), { message, run, fixed });
    assert.ok(retried.ids.every(Boolean));
    assert.ok(await page.evaluate(() => window.sentryFixture.flush()));
    result.eventIDs = retried.ids;
    result.fingerprint = retried.fingerprint;
  } else {
    throw new Error(`unknown GOODSENTRY_ERRORS_MODE ${mode}`);
  }
  assert.deepEqual(errors, [], 'no page errors');
  assert.deepEqual(failures, [], 'no failed requests');
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}

const encoded = JSON.stringify(result);
if (process.env.GOODSENTRY_ERRORS_OUTPUT) await writeFile(process.env.GOODSENTRY_ERRORS_OUTPUT, encoded, { mode: 0o600 });
console.log(encoded);
