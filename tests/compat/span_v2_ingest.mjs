// External protocol owner: standalone span v2 envelopes against a fresh
// installation, plus trace reads, segment aggregates, deduplication, raw
// attribute preservation and strict batch admission.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:8000/');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/span-v2-ingest');
await mkdir(output, { recursive: true });
const report = { checks: [], responses: [] };

async function call(path, options = {}) {
  const headers = { Accept: 'application/json', ...options.headers };
  if (options.body !== undefined) headers['Content-Type'] = options.headers?.['Content-Type'] || 'application/json';
  const response = await fetch(new URL(path, base), {
    method: options.method || 'GET', headers,
    body: options.body === undefined ? undefined : (typeof options.body === 'string' ? options.body : JSON.stringify(options.body)),
  });
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (_) { data = text; }
  return { status: response.status, data };
}

const run = randomBytes(4).toString('hex');
const traceID = randomBytes(16).toString('hex');
const segmentID = '2222222222222222', childA = '3333333333333333', childB = '4444444444444444';
const now = Math.floor(Date.now() / 1000) - 3600;
const window = { start: new Date((now - 3600) * 1000).toISOString(), end: new Date((now + 3600) * 1000).toISOString() };

function envelope(items) {
  const chunks = [JSON.stringify({ sent_at: new Date().toISOString(), sdk: { name: 'sentry.javascript.node', version: '11.7.0' } })];
  for (const item of items) { chunks.push(JSON.stringify(item.header)); chunks.push(JSON.stringify(item.payload)); }
  return chunks.join('\n') + '\n';
}

const attributes = (release, environment) => ({
  'sentry.op': { value: 'compat.segment', type: 'string' },
  'sentry.segment.name': { value: 'spanv2 ' + run, type: 'string' },
  'sentry.segment.id': { value: segmentID, type: 'string' },
  'sentry.release': { value: release, type: 'string' },
  'sentry.environment': { value: environment, type: 'string' },
  'custom.count': { value: 7, type: 'integer' },
});

const segment = {
  name: 'spanv2 ' + run, span_id: segmentID, trace_id: traceID,
  start_timestamp: now, end_timestamp: now + 0.05, is_segment: true, status: 'ok',
  attributes: attributes('spanv2@1', 'test'),
};
const child = id => ({
  name: 'child ' + id, span_id: id, trace_id: traceID, parent_span_id: segmentID,
  start_timestamp: now + 0.001, end_timestamp: now + 0.003, is_segment: false, status: 'ok',
  attributes: { 'sentry.op': { value: 'compat.child', type: 'string' } },
});

async function post(project, items, header = {}) {
  const dsn = new URL(project.dsn);
  const prefix = dsn.pathname.slice(0, dsn.pathname.lastIndexOf('/'));
  const endpoint = new URL(prefix + '/api/' + project.id + '/envelope/?sentry_version=7&sentry_key=' + encodeURIComponent(dsn.username), base);
  const payload = {
    version: 2, items,
  };
  const result = await call(endpoint.pathname + endpoint.search, {
    method: 'POST',
    headers: { 'Content-Type': 'text/plain' },
    body: envelope([{ header: { type: 'span', item_count: items.length, content_type: 'application/vnd.sentry.items.span.v2+json', ...header }, payload }]),
  });
  report.responses.push({ route: '/envelope', status: result.status, itemCount: items.length, itemError: result.data?.detail || result.data?.error || null });
  return result;
}

async function main() {
  const setup = await call('api/v1/setup', { method: 'POST', body: { email: 'spanv2@example.test', password: 'spanv2-password-1', name: 'Span v2', organization: 'Span v2 ' + run } });
  assert.equal(setup.status, 201);
  const cookie = (setup.headers?.getSetCookie?.() || []).map(value => value.split(';')[0]).join('; ');
  // The helper above does not expose headers; reuse the CSRF body and log in.
  const login = await fetch(new URL('api/v1/login', base), { method: 'POST', headers: { Origin: base.origin, 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'spanv2@example.test', password: 'spanv2-password-1' }) });
  assert.equal(login.status, 200);
  const session = (login.headers.getSetCookie() || []).map(value => value.split(';')[0]).join('; ');
  const me = await (await fetch(new URL('api/v1/me', base), { headers: { Cookie: session } })).json();
  const org = me.organizations[0].slug;
  const projectResponse = await fetch(new URL('api/v1/organizations/' + org + '/projects', base), {
    method: 'POST',
    headers: { Cookie: session, Origin: base.origin, 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrfToken },
    body: JSON.stringify({ name: 'Span v2 ' + run, platform: 'javascript' }),
  });
  assert.equal(projectResponse.status, 201);
  const project = await projectResponse.json();

  const accepted = await post(project, [segment, child(childA), child(childB)]);
  assert.equal(accepted.status, 200);
  report.checks.push('Standalone span v2 batch with item_count is accepted');

  const trace = await call('api/v1/organizations/' + org + '/traces/' + traceID, { headers: { Cookie: session } });
  assert.equal(trace.status, 200);
  assert.equal(trace.data.nodeCount, 3);
  assert.equal(trace.data.relationships.resolved, 2);
  const root = trace.data.data.find(node => node.spanId === segmentID);
  assert.ok(root);
  assert.equal(root.op, 'compat.segment');
  assert.equal(root.release, 'spanv2@1');
  assert.equal(root.environment, 'test');
  assert.equal(root.childCount, 2);
  assert.ok(Math.abs(root.duration - 50) < 0.01);
  const children = trace.data.data.filter(node => node.parentId === root.id);
  assert.equal(children.length, 2);
  assert.ok(children.every(node => node.op === 'compat.child' && node.parentStatus === 'resolved'));
  report.trace = { nodeCount: trace.data.nodeCount, resolved: trace.data.relationships.resolved, root: { op: root.op, release: root.release, environment: root.environment, duration: root.duration, childCount: root.childCount } };
  report.checks.push('Trace read resolves segment and children with sentry.* operation, release and environment');

  const raw = await call('api/v1/organizations/' + org + '/traces/' + traceID + '/nodes/' + encodeURIComponent(root.id), { headers: { Cookie: session } });
  assert.equal(raw.status, 200);
  const payload = typeof raw.data.data === 'string' ? JSON.parse(raw.data.data) : raw.data.data;
  assert.equal(payload.attributes['sentry.release'].value, 'spanv2@1');
  assert.equal(payload.attributes['custom.count'].value, 7);
  assert.equal(payload.attributes['custom.count'].type, 'integer');
  report.checks.push('Raw span JSON keeps typed attributes');

  const groups = await call('api/0/organizations/' + org + '/transaction-groups/?start=' + encodeURIComponent(window.start) + '&end=' + encodeURIComponent(window.end), { headers: { Cookie: session } });
  assert.equal(groups.status, 200);
  const group = groups.data.find(item => item.op === 'compat.segment');
  assert.ok(group);
  assert.equal(group.count, 1);
  assert.ok(Math.abs(group.avgDuration - 50) < 0.01);
  report.checks.push('Segment contributes one transaction count and latency; children do not inflate it');

  const duplicate = await post(project, [segment, child(childA), child(childB)]);
  assert.equal(duplicate.status, 200);
  const afterDuplicate = await call('api/v1/organizations/' + org + '/traces/' + traceID, { headers: { Cookie: session } });
  assert.equal(afterDuplicate.data.nodeCount, 3);
  const groupsAfter = await call('api/0/organizations/' + org + '/transaction-groups/?start=' + encodeURIComponent(window.start) + '&end=' + encodeURIComponent(window.end), { headers: { Cookie: session } });
  assert.equal(groupsAfter.data.find(item => item.op === 'compat.segment').count, 1);
  report.checks.push('Repeated batch is deduplicated by project/trace/span identity');

  const wrongCount = await post(project, [segment], { item_count: 5 });
  assert.equal(wrongCount.status, 400);
  const wrongType = await post(project, [segment], { content_type: 'application/vnd.sentry.items.span+json' });
  assert.equal(wrongType.status, 415);
  const badID = await post(project, [{ ...segment, span_id: 'XYZ' }]);
  assert.equal(badID.status, 400);
  const badTime = await post(project, [{ ...segment, end_timestamp: now - 1 }]);
  assert.equal(badTime.status, 400);
  report.checks.push('Batch count, content type, identifiers and timestamps are admitted strictly');

  const legacyID = randomBytes(16).toString('hex'), legacyTrace = randomBytes(16).toString('hex');
  const legacyEvent = {
    type: 'transaction', event_id: legacyID, transaction: 'GET /legacy', start_timestamp: now, timestamp: now + 0.2,
    release: 'spanv2@1', environment: 'test', request: { method: 'GET', url: 'http://example.test/legacy' },
    contexts: { trace: { trace_id: legacyTrace, span_id: 'a'.repeat(16), op: 'http.server', status: 'ok' } },
  };
  const dsn = new URL(project.dsn);
  const prefix = dsn.pathname.slice(0, dsn.pathname.lastIndexOf('/'));
  const legacy = await fetch(new URL(prefix + '/api/' + project.id + '/envelope/?sentry_version=7&sentry_key=' + encodeURIComponent(dsn.username), base), {
    method: 'POST', headers: { 'Content-Type': 'text/plain' },
    body: JSON.stringify({ event_id: legacyID }) + '\n' + JSON.stringify({ type: 'event' }) + '\n' + JSON.stringify(legacyEvent) + '\n',
  });
  assert.equal(legacy.status, 200);
  const legacyGroups = await call('api/0/organizations/' + org + '/transaction-groups/?start=' + encodeURIComponent(window.start) + '&end=' + encodeURIComponent(window.end), { headers: { Cookie: session } });
  assert.equal(legacyGroups.data.find(item => item.op === 'http.server').count, 1);
  report.checks.push('Traditional transaction ingestion and aggregates remain intact');
  report.ok = true;
}

try { await main(); }
catch (error) { report.failure = String(error.stack || error); throw error; }
finally { await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2)); }
