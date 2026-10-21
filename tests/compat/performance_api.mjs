// Run against an empty isolated installation. Validates compatible performance reads.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:8000/');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/performance-api');
await mkdir(output, { recursive: true });
const report = { checks: [] };

async function call(path, { method = 'GET', body, cookie, token, csrf } = {}) {
  const headers = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (cookie) headers.Cookie = cookie;
  if (token) headers.Authorization = `Bearer ${token}`;
  if (csrf) headers['X-CSRF-Token'] = csrf;
  const response = await fetch(new URL(path, base), { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  const raw = await response.text();
  let data = null;
  if (raw) data = JSON.parse(raw);
  return { status: response.status, data, headers: response.headers };
}

try {
  const setup = await call('api/v1/setup', { method: 'POST', body: { email: 'performance@example.test', password: 'performance-api-password', name: 'Performance owner', organization: 'Performance API' } });
  assert.equal(setup.status, 201);
  const cookie = setup.headers.get('set-cookie').split(';', 1)[0];
  const csrf = setup.data.csrfToken;
  const organization = setup.data.organizations[0].slug;
  const createProject = async name => {
    const response = await call(`api/v1/organizations/${organization}/projects`, { method: 'POST', cookie, csrf, body: { name, platform: 'javascript' } });
    assert.equal(response.status, 201);
    return response.data;
  };
  const first = await createProject('Performance first');
  const second = await createProject('Performance second');
  const baseTime = Date.now() - 2 * 60 * 60 * 1000;
  async function capture(project, index, children) {
    const at = baseTime + index * 60 * 1000;
    const eventID = (500 + index).toString(16).padStart(32, '0');
    const traceID = (700 + index).toString(16).padStart(32, '0');
    const spans = Array.from({ length: children }, (_, child) => ({
      span_id: (child + 1).toString(16).padStart(16, '0'),
      parent_span_id: 'a'.repeat(16),
      op: 'db.query',
      description: 'SELECT performance_api',
      start_timestamp: at / 1000 + 0.01,
      timestamp: at / 1000 + 0.02,
    }));
    const event = {
      type: 'transaction', event_id: eventID, transaction: 'GET /compat/performance',
      start_timestamp: at / 1000, timestamp: at / 1000 + 0.1,
      contexts: { trace: { trace_id: traceID, span_id: 'a'.repeat(16), op: 'http.server', status: 'ok' } },
      spans,
    };
    const dsn = new URL(project.dsn);
    const prefix = dsn.pathname.slice(0, dsn.pathname.lastIndexOf('/'));
    const endpoint = new URL(`${prefix}/api/${project.id}/envelope/?sentry_version=7&sentry_key=${encodeURIComponent(dsn.username)}`, base);
    const body = `${JSON.stringify({ event_id: eventID })}\n{"type":"event"}\n${JSON.stringify(event)}`;
    const response = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body });
    assert.equal(response.status, 200);
  }
  for (let index = 0; index < 5; index++) await capture(first, index, 6);
  await capture(first, 5, 2);
  await capture(second, 6, 7);

  const start = new Date(baseTime - 60 * 1000).toISOString();
  const end = new Date(baseTime + 60 * 60 * 1000).toISOString();
  const spanQuery = new URLSearchParams({ start, end, op: 'db.query', sort: '-count', limit: '50' });
  spanQuery.append('project', first.id);
  spanQuery.append('project', second.id);
  const spanGroups = await call(`api/0/organizations/${organization}/span-groups/?${spanQuery}`, { cookie });
  assert.equal(spanGroups.status, 200);
  assert.equal(spanGroups.data.length, 1);
  assert.deepEqual(Object.keys(spanGroups.data[0]).sort(), ['avgDuration', 'count', 'description', 'op', 'p95Duration', 'totalTime']);
  assert.equal(spanGroups.data[0].count, 39);
  report.checks.push('Session span groups combine both selected projects and expose the public fields');

  const nPlusQuery = new URLSearchParams({ start, end, project: first.id, op: 'db', threshold: '5', limit: '50' });
  const patterns = await call(`api/0/organizations/${organization}/n-plus-one/?${nPlusQuery}`, { cookie });
  assert.equal(patterns.status, 200);
  assert.equal(patterns.data.length, 1);
  assert.deepEqual(Object.keys(patterns.data[0]).sort(), ['avgDuration', 'description', 'op', 'spansPerTxn', 'totalSpans', 'totalTime', 'transactionCount', 'transactionName']);
  assert.equal(patterns.data[0].totalSpans, 30);
  assert.equal(patterns.data[0].transactionCount, 5);
  assert.equal(patterns.data[0].spansPerTxn, 6);
  report.checks.push('N+1 patterns aggregate repeated children per trace and then by transaction');

  const credential = await call(`api/v1/organizations/${organization}/api-tokens`, {
    method: 'POST', cookie, csrf, body: { name: 'Performance API', scopes: ['event:read'], projectIds: [first.id], expiresInDays: 1 },
  });
  assert.equal(credential.status, 201);
  const scopedPath = `api/0/organizations/${organization}/span-groups/?${new URLSearchParams({ start, end, op: 'db.query' })}`;
  const scoped = await call(scopedPath, { token: credential.data.token });
  assert.equal(scoped.status, 200);
  assert.equal(scoped.data[0].count, 32);
  const hidden = await call(`api/0/organizations/${organization}/span-groups/?start=${encodeURIComponent(start)}&end=${encodeURIComponent(end)}&op=db.query&project=${second.id}`, { token: credential.data.token });
  assert.equal(hidden.status, 404);
  report.checks.push('Bearer project grants contain span groups and reject hidden project filters');

  const invalidSort = await call(`api/0/organizations/${organization}/span-groups/?sort=bad`, { cookie });
  const invalidLimit = await call(`api/0/organizations/${organization}/n-plus-one/?limit=0`, { cookie });
  assert.equal(invalidSort.status, 400);
  assert.equal(invalidLimit.status, 400);
  report.checks.push('Strict query admission rejects unsupported sort and bound values');
} catch (error) {
  report.error = String(error);
  throw error;
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
}
