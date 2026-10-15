// Test-audit gate: actual pinned SDK serialization and transport, followed by
// consumer reads with newly issued credentials, owns this proof. SQL/HTTP tests
// cannot catch a retained SDK shape that compatible consumers cannot retrieve.
// No synthetic event payload, mocked response, production seam or SQL matrix.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { isAbsolute, resolve } from 'node:path';

const output = resolve(process.env.GOODSENTRY_EVENTS_OUTPUT || 'test-results/events-sdk');
const report = { passed: false, checks: [], requests: [], acknowledged: [], rawSHA256: {} };
const secrets = [], createdTokens = [], sdkPayloads = new Map();
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let sdk, active = false, native, tokenPath;
try {
  const fixturePath = process.env.GOODSENTRY_EVENTS_FIXTURE;
  assert.ok(fixturePath && isAbsolute(fixturePath), 'Provide an absolute isolated private fixture');
  assert.equal((await stat(fixturePath)).mode & 0o077, 0, 'Fixture must be private');
  const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
  for (const value of [fixture.cookieHeader, fixture.csrfToken, fixture.ownerPassword, fixture.ownerEmail]) if (value) secrets.push(value);
  const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
  assert.equal(base.protocol, 'http:');
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'SDK stays on a local isolated runtime');
  assert.ok(!['18080', '18081', '18009'].includes(base.port), 'Protected acceptance runtimes are not test targets');
  const url = path => new URL(path, base).href;
  native = async (method, path, body, expected = 200) => {
    const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
    if (method !== 'GET') headers['X-CSRF-Token'] = fixture.csrfToken;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(url(path), { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    assert.equal(response.status, expected, `Native ${method} ${path}`);
    return expected === 204 ? null : response.json();
  };
  const compatible = async (path, credential, expected = 200, cookie = false) => {
    const target = new URL(path, base);
    assert.equal(target.origin, base.origin, 'Pagination remains on the configured local origin');
    const response = await fetch(target, { headers: { Accept: 'application/json', ...(credential === undefined ? {} : { Authorization: `Bearer ${credential}` }), ...(cookie ? { Cookie: fixture.cookieHeader } : {}) } });
    report.requests.push({ path: target.pathname + target.search, status: response.status });
    assert.equal(response.status, expected, `Compatible GET ${target.pathname}`);
    return { body: await response.json(), response };
  };
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const org = encodeURIComponent(fixture.orgSlug), projectName = `Event SDK ${run}`;
  const project = await native('POST', `api/v1/organizations/${org}/projects`, { name: projectName, platform: 'node' }, 201);
  const excluded = await native('POST', `api/v1/organizations/${org}/projects`, { name: `Excluded SDK control ${run}`, platform: 'node' }, 201);
  secrets.push(project.dsn, new URL(project.dsn).username, excluded.dsn, new URL(excluded.dsn).username);
  assert.equal(new URL(project.dsn).origin, base.origin);
  tokenPath = `api/v1/organizations/${org}/api-tokens`;
  const issueToken = async scope => {
    const token = await native('POST', tokenPath, { name: `Event SDK ${scope} ${run}`, scopes: [scope], projectIds: [project.id], expiresInDays: 1 }, 201);
    createdTokens.push(token.id); secrets.push(token.token);
    assert.deepEqual(token.scopes, [scope]); assert.equal(token.allProjects, false);
    assert.deepEqual(token.projects.map(value => value.id), [project.id]);
    return token;
  };
  const projectReader = await issueToken('project:read'), eventReader = await issueToken('event:read'), metadataOnly = await issueToken('org:read');
  const module = process.env.GOODSENTRY_NODE_SDK;
  assert.ok(module && isAbsolute(module), 'Provide the pinned official SDK module');
  sdk = createRequire(import.meta.url)(module); assert.equal(sdk.SDK_VERSION, '11.7.0');
  report.sdkVersion = sdk.SDK_VERSION;
  // The official SDK defaults to streamed spans. Its documented static lifecycle
  // emits the real transaction envelope required by project-event inspection.
  sdk.init({ dsn: project.dsn, release: `event-sdk-${run}@1.0.0`, environment: 'events-sdk', traceLifecycle: 'static', defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1, tracesSampleRate: 1, sendDefaultPii: false,
    transport(options) {
      const actual = sdk.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const result = await actual.send(envelope);
        for (const [header, payload] of envelope[1]) if (['event', 'transaction'].includes(header.type)) {
          assert.equal(result.statusCode, 200, 'Official SDK receives actual ingestion acknowledgement');
          sdkPayloads.set(payload.event_id, JSON.parse(JSON.stringify(payload)));
          report.acknowledged.push({ id: payload.event_id, kind: header.type });
        }
        return result;
      } };
    } });
  active = true;
  const issueListPath = `api/v1/organizations/${org}/issues?${new URLSearchParams({ project: project.id, status: 'all' })}`;
  const ids = [], stored = new Map(); let issueID;
  for (let index = 0; index < 3; index++) {
    const id = sdk.withScope(scope => {
      scope.setFingerprint([`event-sdk-${run}`]); scope.setTag('consumer', 'events-sdk'); scope.setExtra('ordinal', index);
      scope.setUser({ id: `qa-${run}`, data: { consent: 'fixture' } });
      sdk.addBreadcrumb({ category: 'acceptance', message: `Actual capture ${index}`, level: 'info' });
      return sdk.captureException(new Error(`GoodSentry event inspection ${run} #${index}`));
    });
    assert.match(id, /^[0-9a-f]{32}$/); ids.push(id); assert.ok(await sdk.flush(10000));
    const listing = await native('GET', issueListPath); assert.equal(listing.total, 1);
    issueID = listing.data[0].id;
    const detail = await native('GET', `api/v1/issues/${issueID}`);
    assert.equal(detail.event.event_id, id, 'Native latest is the acknowledged SDK capture');
    assert.deepEqual(detail.event, sdkPayloads.get(id), 'Stored raw object retains the actual SDK payload');
    stored.set(id, detail.event); report.rawSHA256[id] = hash(detail.event);
  }
  const transactionName = `SDK event transaction ${run}`;
  await sdk.startSpan({ name: transactionName, op: 'compat.events' }, async () => {
    await sdk.startSpan({ name: 'actual SDK child', op: 'compat.child' }, async () => { await new Promise(done => setTimeout(done, 5)); });
  });
  assert.ok(await sdk.flush(10000));
  const transactionIDs = report.acknowledged.filter(value => value.kind === 'transaction').map(value => value.id);
  assert.equal(transactionIDs.length, 1, 'The official SDK emits one actual transaction');
  const transactionID = transactionIDs[0];
  const nativeTransactions = await native('GET', `api/v1/organizations/${org}/transactions?${new URLSearchParams({ project: project.id })}`);
  const transaction = nativeTransactions.data.find(value => value.transaction === transactionName);
  assert.ok(transaction?.latestSample);
  const transactionNativePath = `api/v1/organizations/${org}/transactions/${transaction.latestSample.source}/${transaction.latestSample.id}`;
  const rawTransaction = (await native('GET', transactionNativePath)).data;
  assert.deepEqual(rawTransaction, sdkPayloads.get(transactionID));
  report.rawSHA256[transactionID] = hash(rawTransaction);
  const projectPath = `api/0/projects/${org}/${encodeURIComponent(project.slug)}/events/`;
  const issuePath = `api/0/organizations/${org}/issues/${issueID}/events/`, globalPath = `api/0/issues/${issueID}/events/`;
  const projectList = (await compatible(projectPath, projectReader.token)).body;
  assert.deepEqual(new Set(projectList.map(value => value.eventID)), new Set(ids));
  assert.ok(projectList.every(value => value.groupID === issueID && value.projectID === Number(project.id) && !Object.hasOwn(value, 'entries') && !Object.hasOwn(value, 'previousEventID')));
  assert.deepEqual((await compatible(issuePath, eventReader.token)).body.map(value => value.eventID), projectList.map(value => value.eventID));
  assert.deepEqual((await compatible(globalPath, eventReader.token)).body, (await compatible(issuePath, eventReader.token)).body);
  await compatible(issuePath, projectReader.token, 403); await compatible(projectPath, eventReader.token, 403);
  await compatible(projectPath, metadataOnly.token, 403); await compatible(issuePath, metadataOnly.token, 403);
  await compatible(`api/0/projects/${org}/${encodeURIComponent(excluded.slug)}/events/`, projectReader.token, 404);
  await compatible(projectPath, undefined, 401); await compatible(issuePath, `gs_${'0'.repeat(64)}`, 401, true);
  report.checks.push('Project and issue credentials consume actual SDK errors through org/global routes; scopes stay distinct and hidden-project/unauthenticated/malformed-Bearer controls deny access');
  const latest = (await compatible(`${issuePath}latest/`, eventReader.token)).body;
  assert.equal(latest.eventID, ids[2]); assert.equal(latest.previousEventID, ids[1]); assert.equal(latest.nextEventID, null); assert.equal(latest.userReport, null);
  assert.equal(latest.sdk.version, '11.7.0'); assert.equal(latest.user.id, `qa-${run}`);
  const exception = latest.entries.find(value => value.type === 'exception').data.values;
  assert.ok(exception.some(value => value.value === sdkPayloads.get(ids[2]).exception.values.at(-1).value && value.stacktrace.frames.length > 0));
  const uuid = ids[2].replace(/^(.{8})(.{4})(.{4})(.{4})(.{12})$/, '$1-$2-$3-$4-$5').toUpperCase();
  assert.equal((await compatible(`${globalPath}${uuid}/`, eventReader.token)).body.eventID, ids[2]);
  assert.equal((await compatible(`${projectPath}${ids[2]}/`, projectReader.token)).body.eventID, ids[2]);
  const legacy = (await compatible(`${issuePath}${ids[2]}/json/`, eventReader.token)).body;
  assert.equal(legacy.event_id, ids[2]); assert.equal(legacy.project, Number(project.id));
  // PostgreSQL stores occurrence instants at microsecond precision; raw SDK
  // JSON retains its original floating-point timestamp independently.
  const instant = latest.dateCreated.match(/^(.*:\d{2})(\.\d+)?Z$/); assert.ok(instant);
  const occurrenceSeconds = Date.parse(`${instant[1]}Z`) / 1000 + Number(instant[2] || 0);
  assert.equal(legacy.timestamp, occurrenceSeconds); assert.equal(legacy.datetime, latest.dateCreated);
  assert.ok(Math.abs(legacy.timestamp - stored.get(ids[2]).timestamp) <= 0.000002);
  assert.equal(legacy.tags.consumer, 'events-sdk'); assert.deepEqual(legacy.exception, stored.get(ids[2]).exception);
  assert.equal((await compatible(`${projectPath}${transactionID}/`, projectReader.token)).body.type, 'transaction');
  await compatible(`${issuePath}${transactionID}/`, eventReader.token, 404);
  report.checks.push('Concrete/UUID/latest/detail and legacy JSON preserve real SDK exception/user/tags; transaction is retrievable only in project detail and is excluded from error listings');
  const positive = (await compatible(`${issuePath}?environment=events-sdk&environment=absent`, eventReader.token)).body;
  assert.equal(positive.length, 3); assert.deepEqual((await compatible(`${issuePath}?environment=absent`, eventReader.token)).body, []);
  const oldest = projectList.at(-1).dateCreated, newest = projectList[0].dateCreated;
  const bounded = (await compatible(`${issuePath}?${new URLSearchParams({ start: oldest, end: newest })}`, eventReader.token)).body;
  assert.ok(bounded.some(value => value.eventID === ids[0])); assert.ok(!bounded.some(value => value.eventID === ids[2]));
  const first = await compatible(`${issuePath}?limit=1&environment=events-sdk`, eventReader.token);
  assert.equal(first.body.length, 1);
  const nextLink = first.response.headers.get('link').split(',').find(value => value.includes('rel="next"'));
  assert.match(nextLink, /results="true"/); assert.match(nextLink, /cursor="0:1:0"/);
  const nextURL = new URL(nextLink.match(/<([^>]+)>/)[1]);
  assert.ok(nextURL.pathname.startsWith(base.pathname)); assert.equal(nextURL.searchParams.get('environment'), 'events-sdk'); assert.equal(nextURL.searchParams.get('limit'), '1');
  const second = await compatible(nextURL.href, eventReader.token); assert.equal(second.body.length, 1); assert.notEqual(second.body[0].eventID, first.body[0].eventID);
  report.checks.push('Real SDK occurrence dates support start-inclusive/end-exclusive reads, environment OR filters and navigable prefixed pagination Links');
  await native('DELETE', `${tokenPath}/${eventReader.id}`, undefined, 204);
  await compatible(issuePath, eventReader.token, 401, true);
  const final = await native('GET', `api/v1/issues/${issueID}`);
  assert.equal(final.issue.count, 3); assert.equal(hash(final.event), report.rawSHA256[ids[2]]);
  assert.equal(hash((await native('GET', transactionNativePath)).data), report.rawSHA256[transactionID]);
  assert.equal(report.acknowledged.length, 4);
  report.checks.push('Explicit revocation overrides the still-valid session; all compatible reads leave native SDK payload hashes and error counts unchanged');
  Object.assign(report, { passed: true, baseURL: base.href, basePath: base.pathname, orgSlug: fixture.orgSlug, projectID: project.id, projectSlug: project.slug, projectName, excludedProjectID: excluded.id, issueID, eventIDs: ids, transactionID });
} catch (error) {
  report.error = safe(error.stack || error); process.exitCode = 1;
} finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.error = safe(error.stack || error); process.exitCode = 1; }
  report.cleanupFailures = [];
  if (native && tokenPath) for (const id of createdTokens) try { await native('DELETE', `${tokenPath}/${id}`, undefined, 204); } catch (error) { report.cleanupFailures.push({ id, error: safe(error.message) }); }
  if (report.cleanupFailures.length) { report.passed = false; process.exitCode = 1; }
  await mkdir(output, { recursive: true }); await writeFile(resolve(output, 'report.json'), `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ passed: report.passed, projectID: report.projectID, issueID: report.issueID, checks: report.checks.length, acknowledged: report.acknowledged.length, report: resolve(output, 'report.json'), ...(report.error ? { error: report.error } : {}) }));
}
