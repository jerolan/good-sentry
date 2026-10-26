// Official @sentry/node 11.7.0 error acceptance driver.
// Required: GOODSENTRY_DSN. Optional: GOODSENTRY_RUN, GOODSENTRY_ERRORS_MODE
// (single|group|retry), GOODSENTRY_ERRORS_COUNT, GOODSENTRY_ERRORS_EVENT_ID,
// GOODSENTRY_ERRORS_MESSAGE, GOODSENTRY_ERRORS_OUTPUT and GOODSENTRY_NODE_SDK.
// The SDK keeps its default network transport.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { writeFile } from 'node:fs/promises';

const sdkPath = process.env.GOODSENTRY_NODE_SDK || '@sentry/node';
const Sentry = createRequire(import.meta.url)(sdkPath);
assert.equal(Sentry.SDK_VERSION, '11.7.0', 'Use official @sentry/node 11.7.0');

const dsn = process.env.GOODSENTRY_DSN;
assert.ok(dsn, 'GOODSENTRY_DSN is required');
const run = process.env.GOODSENTRY_RUN || String(Date.now());
const mode = process.env.GOODSENTRY_ERRORS_MODE || 'single';
const message = process.env.GOODSENTRY_ERRORS_MESSAGE || `Node SDK error ${run}`;
const environment = 'errors-node-acceptance';
const release = 'errors-node@1.0.0';
const tag = `errors-node-${run}`;
const userID = `errors-node-user-${run}`;

Sentry.init({ dsn, environment, release, tracesSampleRate: 0, defaultIntegrations: false, sendDefaultPii: false });

const result = { sdkVersion: Sentry.SDK_VERSION, mode, run, message, environment, release, tag, userID, eventIDs: [] };
const flush = async label => assert.ok(await Sentry.flush(10000), `flush ${label}`);

if (mode === 'single') {
  let eventID;
  Sentry.withScope(scope => {
    scope.setTag('acceptance.run', tag);
    scope.setTag('component', 'errors-node');
    scope.setContext('acceptance', { run, kind: 'error' });
    scope.setUser({ id: userID });
    eventID = Sentry.captureException(new Error(message));
  });
  assert.ok(eventID, 'official @sentry/node dropped the exception');
  await flush('single');
  result.eventIDs = [String(eventID)];
} else if (mode === 'group') {
  const count = Number(process.env.GOODSENTRY_ERRORS_COUNT || 3);
  assert.ok(Number.isInteger(count) && count >= 1 && count <= 61, 'GOODSENTRY_ERRORS_COUNT must be 1..61');
  const fingerprint = ['errors-node-group', run];
  result.fingerprint = fingerprint;
  for (let index = 0; index < count; index++) {
    let eventID;
    Sentry.withScope(scope => {
      scope.setFingerprint(fingerprint);
      scope.setTag('acceptance.run', tag);
      scope.setTag('component', 'errors-node');
      scope.setContext('acceptance', { run, kind: 'error', occurrence: index });
      scope.setUser({ id: userID });
      eventID = Sentry.captureException(new Error(message));
    });
    assert.ok(eventID, `official @sentry/node dropped grouping capture ${index}`);
    await flush(`group ${index}`);
    result.eventIDs.push(String(eventID));
  }
} else if (mode === 'retry') {
  const fixed = process.env.GOODSENTRY_ERRORS_EVENT_ID || '';
  assert.match(fixed, /^[0-9a-f]{32}$/, 'GOODSENTRY_ERRORS_EVENT_ID must be 32 hex characters');
  const fingerprint = ['errors-node-retry', run];
  result.fingerprint = fingerprint;
  for (let attempt = 0; attempt < 2; attempt++) {
    const eventID = Sentry.captureEvent({
      event_id: fixed,
      message,
      level: 'error',
      platform: 'node',
      timestamp: Date.now() / 1000,
      environment,
      release,
      fingerprint,
      tags: { 'acceptance.run': tag, component: 'errors-node' },
      contexts: { acceptance: { run, kind: 'error', attempt } },
      user: { id: userID },
      exception: { values: [{ type: 'Error', value: message }] },
    });
    assert.ok(eventID, `official @sentry/node dropped retry attempt ${attempt}`);
    await flush(`retry ${attempt}`);
    result.eventIDs.push(String(eventID));
  }
} else {
  throw new Error(`unknown GOODSENTRY_ERRORS_MODE ${mode}`);
}

const encoded = JSON.stringify(result);
if (process.env.GOODSENTRY_ERRORS_OUTPUT) await writeFile(process.env.GOODSENTRY_ERRORS_OUTPUT, encoded, { mode: 0o600 });
console.log(encoded);
