// Test-audit gate: a real SDK HTTP integration owns the request/breadcrumb wire
// shape that fabricated payloads cannot prove. Wrong header/query adaptation or
// a numeric breadcrumb timestamp must fail at the actual compatible consumer.
// Observe transport only; never inject a request or modify an SDK envelope.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { isAbsolute, resolve } from 'node:path';

const output = resolve(process.env.GOODSENTRY_EVENTS_OUTPUT || 'test-results/events-request-sdk-image');
const report = { passed: false, checks: [], acknowledged: [], requests: [], cleanupFailures: [] };
const secrets = [], createdTokens = [], payloads = new Map(), transportErrors = [], handlerErrors = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let sdk, active = false, native, tokenPath, server;
try {
  const fixturePath = process.env.GOODSENTRY_EVENTS_FIXTURE;
  assert.ok(fixturePath && isAbsolute(fixturePath), 'Provide an absolute private copied-image fixture');
  assert.equal((await stat(fixturePath)).mode & 0o077, 0, 'Fixture must be private');
  const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
  secrets.push(fixture.cookieHeader, fixture.csrfToken);
  const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
  assert.equal(base.protocol, 'http:');
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname));
  assert.equal(base.port, '18082', 'Use only the isolated copied image assigned to this proof');
  const target = path => new URL(path, base);
  native = async (method, path, body, expected = 200) => {
    const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
    if (method !== 'GET') headers['X-CSRF-Token'] = fixture.csrfToken;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(target(path), { method, headers, signal: AbortSignal.timeout(15000), ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    assert.equal(response.status, expected, `Native ${method} ${path}`);
    return expected === 204 ? null : response.json();
  };
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const org = encodeURIComponent(fixture.orgSlug);
  const project = await native('POST', `api/v1/organizations/${org}/projects`, { name: `Request SDK ${run}`, platform: 'node' }, 201);
  Object.assign(report, { baseURL: base.href, orgSlug: fixture.orgSlug, projectID: project.id });
  secrets.push(project.dsn, new URL(project.dsn).username);
  assert.equal(new URL(project.dsn).origin, base.origin);
  tokenPath = `api/v1/organizations/${org}/api-tokens`;
  const credential = await native('POST', tokenPath, { name: `Request reader ${run}`, scopes: ['event:read'], projectIds: [project.id], expiresInDays: 1 }, 201);
  createdTokens.push(credential.id); secrets.push(credential.token);
  assert.deepEqual(credential.scopes, ['event:read']);
  assert.deepEqual(credential.projects.map(value => value.id), [project.id]);
  assert.equal(credential.allProjects, false);
  const module = process.env.GOODSENTRY_NODE_SDK;
  assert.ok(module && isAbsolute(module), 'Provide the pinned official SDK module');
  sdk = createRequire(import.meta.url)(module); assert.equal(sdk.SDK_VERSION, '11.7.0');
  report.sdkVersion = sdk.SDK_VERSION;
  sdk.init({ dsn: project.dsn, release: `request-sdk-${run}@1.0.0`, environment: 'request-sdk', defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1,
    integrations: [sdk.httpIntegration({ spans: false, sessions: false, breadcrumbs: true, tracePropagation: false, maxRequestBodySize: 'always' }), sdk.requestDataIntegration()],
    dataCollection: { userInfo: false, cookies: false, httpHeaders: { request: true, response: false }, httpBodies: ['incomingRequest'], urlQueryParams: true },
    transport(options) {
      const actual = sdk.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const observed = envelope[1].filter(([header]) => header.type === 'event').map(([, payload]) => JSON.parse(JSON.stringify(payload)));
        const response = await actual.send(envelope);
        for (const payload of observed) {
          if (response.statusCode !== 200) transportErrors.push(`SDK transport status ${response.statusCode}`);
          payloads.set(payload.event_id, payload); report.acknowledged.push({ id: payload.event_id, status: response.statusCode });
        }
        return response;
      } };
    } });
  active = true;
  // Import and create the actual HTTP server after SDK instrumentation is set up.
  const http = await import('node:http');
  const marker = `request-${run}`, sentBody = JSON.stringify({ qa: marker, amount: 42 });
  let capturedID;
  server = http.createServer(async (request, response) => {
    if (request.url === '/sdk-breadcrumb') { response.writeHead(204); response.end(); return; }
    try {
      assert.equal(request.method, 'POST'); assert.equal(request.url, '/incoming?qa=one&qa=two');
      const chunks = []; for await (const chunk of request) chunks.push(chunk);
      assert.equal(Buffer.concat(chunks).toString(), sentBody, 'The real server consumes the posted JSON body');
      await new Promise((done, reject) => {
        http.get(`http://127.0.0.1:${server.address().port}/sdk-breadcrumb`, result => { result.resume(); result.on('end', done); result.on('error', reject); }).on('error', reject);
      });
      sdk.addBreadcrumb({ category: 'qa.request', message: `Actual request consumed ${marker}`, level: 'info' });
      capturedID = sdk.captureException(new Error(`Real HTTP request projection ${marker}`));
      response.writeHead(200, { 'Content-Type': 'application/json' }); response.end(JSON.stringify({ eventID: capturedID }));
    } catch (error) {
      handlerErrors.push(safe(error.stack || error)); response.writeHead(500); response.end('Request QA failed');
    }
  });
  await new Promise((done, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', done); });
  const incoming = await fetch(`http://127.0.0.1:${server.address().port}/incoming?qa=one&qa=two`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-QA': marker }, body: sentBody, signal: AbortSignal.timeout(15000) });
  assert.equal(incoming.status, 200, `Actual HTTP handler: ${handlerErrors.join('\n')}`);
  assert.equal((await incoming.json()).eventID, capturedID); assert.match(capturedID, /^[0-9a-f]{32}$/);
  assert.ok(await sdk.flush(10000)); assert.deepEqual(transportErrors, []); assert.deepEqual(handlerErrors, []);
  assert.equal(payloads.size, 1, 'One real handler error is captured and durably acknowledged');
  const observed = payloads.get(capturedID); assert.ok(observed);
  const rawRequest = observed.request;
  Object.assign(report, { eventID: capturedID, sdkObservedRequest: rawRequest ?? null, sdkObservedBreadcrumbs: observed.breadcrumbs ?? null });
  assert.ok(rawRequest && rawRequest.headers && !Array.isArray(rawRequest.headers), 'HTTP SDK must actually emit an object of headers');
  assert.equal(rawRequest.headers['content-type'], 'application/json'); assert.equal(rawRequest.headers['x-qa'], marker);
  assert.equal(rawRequest.method, 'POST'); assert.equal(rawRequest.query_string, 'qa=one&qa=two');
  const rawBreadcrumbs = Array.isArray(observed.breadcrumbs) ? observed.breadcrumbs : observed.breadcrumbs?.values;
  assert.ok(Array.isArray(rawBreadcrumbs));
  const automatic = rawBreadcrumbs.find(value => value.category === 'http' && value.data?.url?.endsWith('/sdk-breadcrumb'));
  assert.ok(automatic, 'HTTP integration emits a real outgoing-request breadcrumb inside the handler');
  const breadcrumb = rawBreadcrumbs.find(value => value.message === `Actual request consumed ${marker}`);
  assert.equal(typeof breadcrumb?.timestamp, 'number');
  const listing = await native('GET', `api/v1/organizations/${org}/issues?${new URLSearchParams({ project: project.id, status: 'all' })}`);
  assert.equal(listing.total, 1); const issueID = listing.data[0].id;
  const nativePath = `api/v1/issues/${issueID}`, before = await native('GET', nativePath);
  assert.deepEqual(before.event, observed, 'Native raw retains exactly the observed SDK transport payload');
  const rawSHA256 = hash(before.event); report.rawSHA256 = rawSHA256;
  report.checks.push('Real HTTP integration retains object headers, repeated query, numeric SDK breadcrumbs and the acknowledged payload without an eventprocessor');
  const issuePath = `api/0/organizations/${org}/issues/${issueID}/events/${capturedID}/`;
  const compatible = async path => {
    const response = await fetch(target(path), { headers: { Authorization: `Bearer ${credential.token}`, Accept: 'application/json' }, signal: AbortSignal.timeout(15000) });
    report.requests.push({ path, status: response.status }); assert.equal(response.status, 200);
    return response.json();
  };
  const detail = await compatible(issuePath);
  const projected = detail.entries.find(value => value.type === 'request')?.data;
  assert.ok(projected); assert.equal(projected.method, 'POST');
  assert.ok(Array.isArray(projected.headers), 'Compatible request headers must be key/value pairs');
  assert.deepEqual(new Map(projected.headers), new Map(Object.entries(rawRequest.headers)));
  assert.equal(projected.inferredContentType, 'application/json');
  assert.deepEqual(projected.query, [['qa', 'one'], ['qa', 'two']], 'Repeated query values remain ordered pairs');
  const hasBody = Object.hasOwn(rawRequest, 'data');
  if (hasBody) assert.deepEqual(projected.data, rawRequest.data, 'A body actually retained by the SDK survives the request projection');
  const projectedBreadcrumbs = detail.entries.find(value => value.type === 'breadcrumbs')?.data.values;
  assert.ok(Array.isArray(projectedBreadcrumbs));
  for (const raw of [breadcrumb, automatic]) {
    const match = projectedBreadcrumbs.find(value => value.message === raw.message && value.category === raw.category && value.data?.url === raw.data?.url);
    assert.ok(match); assert.equal(typeof match.timestamp, 'string');
    assert.match(match.timestamp, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$/);
    assert.ok(Math.abs(Date.parse(match.timestamp) - raw.timestamp * 1000) <= 1, 'ISO breadcrumb keeps its actual SDK instant');
  }
  report.checks.push('Compatible request exposes actual header pairs, inferred JSON type and repeated query pairs; both SDK breadcrumbs use ISO instants');
  const legacy = await compatible(`${issuePath}json/`);
  assert.deepEqual(legacy.request, rawRequest); assert.deepEqual(legacy.breadcrumbs, observed.breadcrumbs);
  const after = await native('GET', nativePath);
  assert.equal(hash(after.event), rawSHA256); assert.deepEqual(after.event, observed); assert.equal(after.issue.count, 1);
  report.checks.push('Legacy JSON and a second native read preserve the original request, breadcrumbs, raw hash and count');
  Object.assign(report, { passed: true, baseURL: base.href, orgSlug: fixture.orgSlug, projectID: project.id, issueID, eventID: capturedID, sdkRequestHeaderType: 'object', sdkRequestHeaders: rawRequest.headers, sdkQuery: rawRequest.query_string, sdkBodyObserved: hasBody, ...(hasBody ? { sdkBodySHA256: hash(rawRequest.data) } : {}), compatibleRequest: projected, sdkBreadcrumbTimestamp: breadcrumb.timestamp });
} catch (error) {
  report.error = safe(error.stack || error); process.exitCode = 1;
} finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.error = safe(error.stack || error); process.exitCode = 1; }
  if (server) try { server.closeAllConnections(); await new Promise((done, reject) => server.close(error => error ? reject(error) : done())); } catch (error) { report.cleanupFailures.push({ server: safe(error.message) }); }
  if (native && tokenPath) for (const id of createdTokens) try { await native('DELETE', `${tokenPath}/${id}`, undefined, 204); } catch (error) { report.cleanupFailures.push({ id, error: safe(error.message) }); }
  report.handlerErrors = handlerErrors; report.transportErrors = transportErrors;
  if (report.cleanupFailures.length) { report.passed = false; process.exitCode = 1; }
  await mkdir(output, { recursive: true }); await writeFile(resolve(output, 'report.json'), `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ passed: report.passed, projectID: report.projectID, issueID: report.issueID, acknowledged: report.acknowledged.length, checks: report.checks.length, report: resolve(output, 'report.json'), ...(report.error ? { error: report.error } : {}) }));
}
