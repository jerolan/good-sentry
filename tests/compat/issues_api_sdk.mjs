// Test-audit gate: actual official SDK errors/users/serialization consumed with
// a newly issued restricted credential own this proof. Store tests own identity,
// topology and bucket matrices. beforeSend only supplies controlled occurrence
// instants/environments; transport sends the actual SDK-produced exception data.
// No synthetic exception payload, mocked response, SQL or production test seam.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, resolve } from 'node:path';

const output = resolve(process.env.GOODSENTRY_ISSUES_API_SDK_REPORT || 'test-results/issues-api-sdk/report.json');
const report = { passed: false, checks: [], acknowledged: [], requests: [], legacyProjectionSHA256: {}, nativeRawSHA256: {} };
const secrets = [], payloads = new Map(), captures = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
const total = stats => stats['24h'].reduce((sum, [, count]) => sum + count, 0);
let sdk, active = false, native, tokenPath, credential;
try {
  const fixturePath = process.env.GOODSENTRY_ISSUES_API_FIXTURE;
  assert.ok(fixturePath && isAbsolute(fixturePath), 'Provide an absolute private isolated fixture');
  assert.equal((await stat(fixturePath)).mode & 0o077, 0, 'Fixture must be private');
  const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
  for (const value of [fixture.cookieHeader, fixture.csrfToken]) if (value) secrets.push(value);
  const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
  assert.equal(base.protocol, 'http:'); assert.equal(base.port, '18083');
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'Use the new isolated image runtime');
  const url = path => new URL(path, base).href;
  native = async (method, path, body, expected = 200) => {
    const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
    if (method !== 'GET') headers['X-CSRF-Token'] = fixture.csrfToken;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(url(path), { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    assert.equal(response.status, expected, `Native ${method} ${path}`); return expected === 204 ? null : response.json();
  };
  const compatible = async (path, expected = 200, cookie = false) => {
    const target = new URL(path, base); assert.equal(target.origin, base.origin, 'Links stay on the isolated public origin');
    const response = await fetch(target, { headers: { Accept: 'application/json', Authorization: `Bearer ${credential.token}`, ...(cookie ? { Cookie: fixture.cookieHeader } : {}) } });
    report.requests.push({ path: target.pathname + target.search, status: response.status });
    assert.equal(response.status, expected, `Compatible GET ${target.pathname}`); return { body: await response.json(), response };
  };
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`, org = encodeURIComponent(fixture.orgSlug);
  const project = await native('POST', `api/v1/organizations/${org}/projects`, { name: `Issue API SDK ${run}`, platform: 'node' }, 201);
  secrets.push(project.dsn, new URL(project.dsn).username); assert.equal(new URL(project.dsn).origin, base.origin);
  tokenPath = `api/v1/organizations/${org}/api-tokens`;
  credential = await native('POST', tokenPath, { name: `Issue SDK reader ${run}`, scopes: ['event:read'], projectIds: [project.id], expiresInDays: 1 }, 201);
  secrets.push(credential.token); assert.deepEqual(credential.scopes, ['event:read']); assert.equal(credential.allProjects, false); assert.deepEqual(credential.projects.map(value => value.id), [project.id]);
  const stamp = Math.floor(Date.now() / 1000) - 600;
  captures.push(
    { family: 'A', timestamp: stamp - 240, environment: 'issues-blue', user: { id: 42 } },
    { family: 'A', timestamp: stamp - 180, environment: 'issues-blue', user: { id: '42' } },
    { family: 'A', timestamp: stamp - 120, environment: 'issues-green', user: { email: `known-${run}@example.test` } },
    { family: 'A', timestamp: stamp - 60, environment: 'issues-green', user: {} },
    { family: 'A', timestamp: stamp - 172800, environment: 'issues-blue', user: { id: 'historical' } },
    { family: 'B', timestamp: stamp - 259200, environment: 'issues-blue', user: { id: 42 } },
    { family: 'B', timestamp: stamp - 173400, environment: 'issues-green', user: { email: `older-${run}@example.test` } },
  );
  const module = process.env.GOODSENTRY_NODE_SDK;
  assert.ok(module && isAbsolute(module), 'Provide the pinned official Node SDK'); sdk = createRequire(import.meta.url)(module); assert.equal(sdk.SDK_VERSION, '11.7.0'); report.sdkVersion = sdk.SDK_VERSION;
  sdk.init({ dsn: project.dsn, release: `issues-sdk-${run}@1.0.0`, defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1, tracesSampleRate: 0, sendDefaultPii: false,
    beforeSend(event) {
      const capture = captures[event.extra.acceptanceOrdinal]; assert.ok(capture);
      event.timestamp = capture.timestamp; event.environment = capture.environment; return event;
    },
    transport(options) {
      const actual = sdk.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const response = await actual.send(envelope);
        for (const [header, payload] of envelope[1]) if (header.type === 'event') {
          assert.equal(response.statusCode, 200, 'Official SDK ingestion acknowledgement'); payloads.set(payload.event_id, JSON.parse(JSON.stringify(payload))); report.acknowledged.push(payload.event_id);
        }
        return response;
      } };
    } }); active = true;
  const nativeList = `api/v1/organizations/${org}/issues?${new URLSearchParams({ project: project.id, status: 'all' })}`;
  const roots = {}, ids = [], rawLatest = new Map();
  for (const [index, capture] of captures.entries()) {
    const id = sdk.withScope(scope => {
      scope.setUser(capture.user); scope.setFingerprint([`issue-api-${run}-${capture.family}`]); scope.setTag('sdk-proof', 'issues'); scope.setExtra('acceptanceOrdinal', index);
      return sdk.captureException(new Error(`Issue summary ${capture.family} ${run} #${index}`));
    });
    assert.match(id, /^[0-9a-f]{32}$/); ids.push(id); capture.eventID = id; assert.ok(await sdk.flush(10000));
    const actual = payloads.get(id); assert.ok(actual); assert.equal(actual.timestamp, capture.timestamp); assert.equal(actual.environment, capture.environment);
    assert.deepEqual(actual.user || {}, capture.user, 'Official SDK retains the fixture identity input');
    const listing = await native('GET', nativeList), issue = listing.data.find(value => value.title.includes(`Issue summary ${capture.family} ${run}`)); assert.ok(issue); roots[capture.family] = issue.id;
    const detail = await native('GET', `api/v1/issues/${issue.id}`);
    if (detail.event.event_id === id) { assert.deepEqual(detail.event, actual, 'Native latest payload retains actual SDK event'); rawLatest.set(issue.id, detail.event); }
  }
  assert.equal(new Set(ids).size, 7); assert.equal((await native('GET', nativeList)).total, 2);
  const issueID = roots.A, emptyIssueID = roots.B, issue = await native('GET', `api/v1/issues/${issueID}`), empty = await native('GET', `api/v1/issues/${emptyIssueID}`);
  assert.equal(issue.issue.count, 5); assert.equal(issue.analytics.userCount, 3); assert.equal(total(issue.analytics.stats), 4);
  assert.equal(empty.issue.count, 2); assert.equal(empty.analytics.userCount, 2); assert.equal(total(empty.analytics.stats), 0);
  for (const [id, raw] of rawLatest) report.nativeRawSHA256[id] = hash(raw);
  await native('PATCH', `api/v1/issues/${emptyIssueID}`, { status: 'resolved' });
  report.checks.push('Seven actual SDK errors form two groups: numeric/string42 share one user identity, email is distinct, anonymous adds no user and old captures remain in retained-user counts; historical group has native count2/chart0');
  const orgList = `api/0/organizations/${org}/issues/`, projectList = `api/0/projects/${org}/${encodeURIComponent(project.slug)}/issues/`, detailPath = id => `api/0/organizations/${org}/issues/${id}/`;
  const orgQuery = values => `${orgList}?${new URLSearchParams({ project: project.id, ...values })}`;
  assert.deepEqual((await compatible(orgQuery({}))).body.map(value => value.id), [issueID]);
  const all = (await compatible(orgQuery({ query: '' }))).body; assert.deepEqual(new Set(all.map(value => value.id)), new Set([issueID, emptyIssueID]));
  assert.deepEqual((await compatible(projectList)).body.map(value => value.id), [issueID]);
  const projectAll = (await compatible(`${projectList}?query=`)).body; assert.equal(projectAll.length, 2);
  const main = projectAll.find(value => value.id === issueID), old = projectAll.find(value => value.id === emptyIssueID);
  assert.equal(main.count, '5'); assert.equal(main.userCount, 3); assert.equal(total(main.stats), 4);
  assert.equal(old.count, '2'); assert.equal(old.userCount, 2); assert.equal(total(old.stats), 0); assert.equal(old.status, 'resolved');
  const full = (await compatible(detailPath(issueID))).body, global = (await compatible(`api/0/issues/${issueID}/`)).body;
  assert.equal(full.id, issueID); assert.equal(full.count, '5'); assert.equal(full.userCount, 3); assert.equal(full.userReportCount, 0); assert.equal(total(full.stats), 4);
  // Separate requests may anchor rolling buckets to different transaction seconds.
  for (const field of ['id', 'count', 'userCount', 'userReportCount', 'status', 'title']) assert.deepEqual(global[field], full[field]);
  assert.deepEqual(global.project, full.project); assert.equal(total(global.stats), 4);
  const statsQuery = new URLSearchParams(); statsQuery.append('groups', issueID); statsQuery.append('groups', emptyIssueID);
  const stats = (await compatible(`api/0/organizations/${org}/issues-stats/?${statsQuery}`)).body; assert.deepEqual(stats.map(value => value.id), [issueID, emptyIssueID]);
  assert.deepEqual(stats.map(value => [value.count, value.userCount, total(value.stats)]), [['5', 3, 4], ['2', 2, 0]]);
  report.checks.push('Restricted event credential consumes organization/project lists, organization/global detail and issue-stats; omitted query selects unresolved while explicit empty query includes the resolved historical group');
  const blue = (await compatible(`${detailPath(issueID)}?environment=issues-blue`)).body;
  assert.equal(blue.count, '5'); assert.equal(blue.userCount, 3); assert.equal(blue.filtered.count, '3'); assert.equal(blue.filtered.userCount, 2); assert.equal(total(blue.stats), 2);
  const start = new Date((stamp - 300) * 1000).toISOString(), end = new Date(stamp * 1000).toISOString();
  const recent = (await compatible(orgQuery({ query: '', start, end }))).body; assert.deepEqual(recent.map(value => value.id), [issueID]);
  assert.equal(recent[0].filtered.count, '4'); assert.equal(recent[0].filtered.userCount, 2); assert.equal(recent[0].count, '5'); assert.equal(recent[0].userCount, 3);
  const disabled = (await compatible(`${projectList}?query=&statsPeriod=`)).body; assert.equal(disabled.length, 2); assert.ok(disabled.every(value => Object.keys(value.stats).length === 0));
  report.checks.push('Actual SDK environments/time bounds affect matching metrics while preserving lifetime count/users; deprecated project chart disabling preserves both groups');
  const first = await compatible(orgQuery({ query: '', limit: '1' })); assert.equal(first.body.length, 1);
  const next = first.response.headers.get('link').split(',').find(value => value.includes('rel="next"')); assert.match(next, /results="true"/);
  const nextURL = new URL(next.match(/<([^>]+)>/)[1]); assert.ok(nextURL.pathname.startsWith(base.pathname)); assert.equal(nextURL.searchParams.get('query'), ''); assert.equal(nextURL.searchParams.get('project'), project.id);
  const second = await compatible(nextURL.href); assert.equal(second.body.length, 1); assert.notEqual(second.body[0].id, first.body[0].id);
  assert.deepEqual(new Set([first.body[0].id, second.body[0].id]), new Set([issueID, emptyIssueID]));
  const legacyPaths = captures.map(capture => `api/0/organizations/${org}/issues/${roots[capture.family]}/events/${capture.eventID}/json/`);
  for (const [index, path] of legacyPaths.entries()) {
    const legacy = (await compatible(path)).body, actual = payloads.get(ids[index]);
    assert.equal(legacy.event_id, ids[index]); assert.deepEqual(legacy.user || {}, actual.user || {}); assert.deepEqual(legacy.exception, actual.exception);
    report.legacyProjectionSHA256[ids[index]] = hash(legacy);
  }
  await compatible(orgQuery({ query: '', sort: 'user', groupStatsPeriod: '14d' }));
  for (const [index, path] of legacyPaths.entries()) assert.equal(hash((await compatible(path)).body), report.legacyProjectionSHA256[ids[index]]);
  for (const [id, raw] of rawLatest) assert.equal(hash((await native('GET', `api/v1/issues/${id}`)).event), hash(raw));
  report.checks.push('Actual pagination follows prefixed Links without losing empty query/project scope; compatible analytics leave every legacy projection and both native latest SDK raw hashes unchanged');
  if (fixture.aliases?.length) {
    for (const alias of fixture.aliases) {
      assert.equal(alias.projectID, project.id, 'Optional aliases must belong to this isolated SDK project');
      assert.equal((await compatible(detailPath(alias.id))).body.id, alias.rootID);
    }
    report.aliasChecks = fixture.aliases.length;
  } else report.aliasChecks = 'Not run: no isolated project aliases supplied';
  await native('DELETE', `${tokenPath}/${credential.id}`, undefined, 204); await compatible(detailPath(issueID), 401, true);
  assert.equal((await native('GET', `api/v1/issues/${issueID}`)).issue.count, 5);
  report.checks.push('Explicit credential revocation returns401 even with the still-valid native session; captures and own project remain available for UI acceptance');
  Object.assign(report, { passed: true, baseURL: base.href, basePath: base.pathname, orgSlug: fixture.orgSlug, projectID: project.id, projectSlug: project.slug, issueID, emptyIssueID, eventIDs: ids, userCount: 3, expected24hTotal: 4, emptyUserCount: 2, emptyNativeCount: 2, captures: captures.map(({ family, eventID, timestamp, environment }) => ({ family, eventID, timestamp, environment })) });
} catch (error) { report.error = safe(error.stack || error); process.exitCode = 1; }
finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.error = safe(error.stack || error); process.exitCode = 1; }
  if (native && tokenPath && credential) try { await native('DELETE', `${tokenPath}/${credential.id}`, undefined, 204); } catch (error) { report.passed = false; report.cleanupError = safe(error.message); process.exitCode = 1; }
  await mkdir(dirname(output), { recursive: true }); await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ passed: report.passed, checks: report.checks.length, acknowledged: report.acknowledged.length, projectID: report.projectID, issueID: report.issueID, emptyIssueID: report.emptyIssueID, report: output, ...(report.error ? { error: report.error } : {}) }));
}
