// Test-audit gate: the official Node SDK's public modern-feedback APIs and
// fixed-ID/attachment hints are the behavior under test. Server admission,
// receipt persistence, association and browser behavior have separate owners.
// The SDK keeps its default network transport; flush is not server acceptance.
import assert from 'node:assert/strict';
import { readFile, mkdir, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, resolve } from 'node:path';

const require = createRequire(import.meta.url);
const fixturePath = process.env.GOODSENTRY_MODERN_FIXTURE;
const sdkPhase = process.env.GOODSENTRY_SDK_PHASE;
const evidencePhase = process.env.GOODSENTRY_TEST_PHASE;
const outputRoot = process.env.GOODSENTRY_TEST_OUTPUT;
const phasePattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$/;
let sdk;
let initialized = false;
let fixture;
let operations = [];
let evidence;
let evidencePath;

function loopback(hostname) {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]';
}

function validateBase64(value, label) {
  assert.equal(typeof value, 'string', `${label} must be base64 text`);
  assert.match(value, /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/, `${label} must use canonical base64`);
  assert.equal(Buffer.from(value, 'base64').toString('base64'), value, `${label} must use canonical base64`);
}

function validateFixture(value) {
  assert.ok(value && typeof value === 'object' && !Array.isArray(value), 'Fixture must be an object');
  assert.equal(typeof value.runID, 'string');
  assert.match(value.runID, /^[A-Za-z0-9._-]{1,128}$/, 'runID must be a bounded tag value');
  assert.ok(value.project && typeof value.project.dsn === 'string', 'Fixture must include project.dsn');
  assert.ok(value.sdkPlan && typeof value.sdkPlan === 'object' && !Array.isArray(value.sdkPlan), 'Fixture must include sdkPlan');

  const base = new URL(value.baseURL);
  assert.ok(['http:', 'https:'].includes(base.protocol), 'baseURL must use HTTP or HTTPS');
  assert.ok(loopback(base.hostname), 'baseURL must target the prepared local copied app');
  assert.equal(base.username, '');
  assert.equal(base.password, '');
  assert.equal(base.search, '');
  assert.equal(base.hash, '');
  if (!base.pathname.endsWith('/')) base.pathname += '/';

  const dsn = new URL(value.project.dsn);
  assert.equal(dsn.origin, base.origin, 'Project DSN must share the prepared app origin');
  assert.equal(dsn.username.length > 0, true, 'Project DSN must include its public key');
  assert.equal(dsn.password, '');
  assert.equal(dsn.search, '');
  assert.equal(dsn.hash, '');
  assert.ok(dsn.pathname.startsWith(base.pathname), 'Project DSN path must remain within baseURL');
  assert.match(dsn.pathname, /\/[0-9]+\/?$/, 'Project DSN must be scoped to a numeric project');

  assert.ok(phasePattern.test(sdkPhase || ''), 'GOODSENTRY_SDK_PHASE must name an existing fixture phase');
  assert.ok(Object.hasOwn(value.sdkPlan, sdkPhase), 'GOODSENTRY_SDK_PHASE must select an existing phase');
  operations = value.sdkPlan[sdkPhase];
  assert.ok(Array.isArray(operations) && operations.length > 0, 'Selected SDK phase must contain operations');
  const ids = new Set();
  for (const [index, operation] of operations.entries()) {
    assert.ok(operation && typeof operation === 'object' && !Array.isArray(operation), `Operation ${index} must be an object`);
    assert.match(operation.eventID || '', /^[0-9a-f]{32}$/, `Operation ${index} eventID must be canonical lowercase hex`);
    assert.ok(!ids.has(operation.eventID), `Operation ${index} repeats an eventID in this phase`);
    ids.add(operation.eventID);
    assert.ok(['error', 'feedback', 'feedback_event'].includes(operation.kind), `Operation ${index} has an unsupported kind`);
    assert.equal(typeof operation.message, 'string', `Operation ${index} message must be a string`);
    if (operation.slot !== undefined) {
      assert.equal(typeof operation.slot, 'string', `Operation ${index} slot must be a string`);
      assert.match(operation.slot, /^[A-Za-z0-9._-]{1,128}$/, `Operation ${index} slot must be a bounded tag value`);
    }
    for (const field of ['name', 'email', 'associatedEventId']) {
      if (operation[field] !== undefined) assert.equal(typeof operation[field], 'string', `Operation ${index} ${field} must be text`);
    }
    if (operation.kind === 'feedback_event') assert.ok(operation.payload, `Operation ${index} must include its explicit feedback payload`);
    if (operation.payload !== undefined) {
      assert.ok(operation.payload && typeof operation.payload === 'object' && !Array.isArray(operation.payload), `Operation ${index} payload must be an object`);
      if (operation.payload.event_id !== undefined) assert.equal(operation.payload.event_id, operation.eventID, `Operation ${index} payload event_id contradicts eventID`);
      if (operation.payload.type !== undefined) assert.equal(operation.payload.type, 'feedback', `Operation ${index} payload type must be feedback`);
    }
    if (operation.attachments !== undefined) {
      assert.ok(Array.isArray(operation.attachments), `Operation ${index} attachments must be an array`);
      for (const [attachmentIndex, attachment] of operation.attachments.entries()) {
        assert.ok(attachment && typeof attachment === 'object' && !Array.isArray(attachment), `Operation ${index} attachment ${attachmentIndex} must be an object`);
        assert.equal(typeof attachment.filename, 'string');
        assert.ok(attachment.filename.length > 0 && attachment.filename.length <= 255 && !/[\\/\u0000-\u001f]/.test(attachment.filename), `Operation ${index} attachment filename must be a plain filename`);
        assert.equal(typeof attachment.contentType, 'string');
        assert.match(attachment.contentType, /^[A-Za-z0-9!#$&^_.+-]+\/[A-Za-z0-9!#$&^_.+-]+$/, `Operation ${index} attachment contentType must be a media type`);
        validateBase64(attachment.dataBase64, `Operation ${index} attachment ${attachmentIndex} dataBase64`);
      }
    }
  }
  return base;
}

async function main() {
  assert.ok(fixturePath && isAbsolute(fixturePath), 'GOODSENTRY_MODERN_FIXTURE must be an absolute private fixture path');
  assert.ok(phasePattern.test(evidencePhase || ''), 'GOODSENTRY_TEST_PHASE must name a new evidence phase');
  assert.ok(outputRoot && isAbsolute(outputRoot), 'GOODSENTRY_TEST_OUTPUT must be an absolute evidence directory');
  const fixtureStat = await stat(fixturePath);
  assert.ok(fixtureStat.isFile(), 'Fixture must be a regular file');
  fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
  validateFixture(fixture);
  const output = resolve(outputRoot, `${evidencePhase}.json`);
  try {
    await stat(output);
    throw new Error('Evidence already exists; choose a new GOODSENTRY_TEST_PHASE');
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
  evidencePath = output;

  const moduleName = process.env.GOODSENTRY_SDK_MODULE || '@sentry/node';
  sdk = require(moduleName);
  assert.equal(sdk.SDK_VERSION, '11.7.0', 'Use official @sentry/node 11.7.0');
  evidence = {
    passed: false,
    fixturePhase: sdkPhase,
    evidencePhase,
    runID: fixture.runID,
    sdkVersion: sdk.SDK_VERSION,
    plannedCount: operations.length,
    plannedByKind: Object.fromEntries(['error', 'feedback', 'feedback_event'].map(kind => [kind, operations.filter(operation => operation.kind === kind).length])),
    operations: [],
    defaultTransportConfigured: true,
    flushSucceeded: false,
    serverAcceptance: false,
  };

  sdk.init({
    dsn: fixture.project.dsn,
    defaultIntegrations: false,
    autoSessionTracking: false,
    enableLogs: false,
    sampleRate: 1,
    sendDefaultPii: false,
    release: '',
  });
  initialized = true;

  for (const operation of operations) {
    const slot = operation.slot || operation.eventID;
    const hint = {
      event_id: operation.eventID,
      captureContext: { tags: { 'goodsentry.modern.run': fixture.runID, 'goodsentry.modern.operation': slot } },
      ...(operation.attachments?.length ? {
        attachments: operation.attachments.map(attachment => ({
          filename: attachment.filename,
          contentType: attachment.contentType,
          data: Buffer.from(attachment.dataBase64, 'base64'),
        })),
      } : {}),
    };
    let returnedID;
    let api;
    if (operation.kind === 'error') {
      api = 'captureException';
      returnedID = sdk.captureException(new Error(operation.message), hint);
    } else if (operation.kind === 'feedback') {
      api = 'captureFeedback';
      const feedback = { message: operation.message };
      for (const field of ['name', 'email', 'associatedEventId']) if (operation[field] !== undefined) feedback[field] = operation[field];
      returnedID = sdk.captureFeedback(feedback, hint);
    } else {
      api = 'captureEvent';
      returnedID = sdk.captureEvent({ ...operation.payload, type: 'feedback', event_id: operation.eventID }, hint);
    }
    assert.equal(returnedID, operation.eventID, `${api} must return the prepared eventID`);
    const entry = { eventID: operation.eventID, kind: operation.kind, api, returnedID, flushed: false };
    evidence.operations.push(entry);
    // Backpressure each real request rather than overflowing the SDK buffer
    // when a planned cohort exceeds its transport queue capacity.
    entry.flushed = await sdk.flush(10000);
    assert.equal(entry.flushed, true, 'Each official SDK capture must finish flushing');
  }

  evidence.flushSucceeded = await sdk.flush(10000);
  assert.equal(evidence.flushSucceeded, true, 'Official SDK transport flush must succeed');
  for (const operation of evidence.operations) operation.flushed = true;
  evidence.passed = true;
}

try {
  await main();
} catch (error) {
  if (evidence) {
    evidence.failure = error instanceof assert.AssertionError ? 'assertion' : 'runner';
    evidence.passed = false;
  }
  console.error(error instanceof assert.AssertionError ? 'Modern Feedback SDK assertion failed' : 'Modern Feedback SDK runner failed');
  process.exitCode = 1;
} finally {
  if (initialized) {
    try {
      assert.equal(await sdk.close(10000), true, 'Official SDK client must close successfully');
      evidence.clientClosed = true;
    } catch {
      if (evidence) evidence.passed = false;
      process.exitCode = 1;
    }
  }
  if (evidence && evidencePath) {
    try {
      await mkdir(dirname(evidencePath), { recursive: true });
      await writeFile(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`, { flag: 'wx' });
      if (evidence.passed) console.log(JSON.stringify({ passed: true, phase: evidencePhase, plannedCount: operations.length, submitted: evidence.operations.length, flushSucceeded: evidence.flushSucceeded, serverAcceptance: false, evidence: evidencePath }));
    } catch {
      process.exitCode = 1;
      console.error('Modern Feedback SDK evidence could not be saved');
    }
  }
}
