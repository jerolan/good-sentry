// Test-audit gate: actual Node SDK serialization/retries consumed through a
// restricted writer own this proof. SQL/HTTP own parser, transaction and policy
// matrices; they cannot prove real SDK recurrence or retrieve its retained raw.
// beforeSend controls release/occurrence metadata only, never exception payloads.
// No synthetic telemetry, mocked response, SQL or production-only test seam.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const output = resolve(process.env.GOODSENTRY_ISSUE_WRITE_SDK_REPORT || 'test-results/issue-write-sdk-image/report.json');
const report = { passed: false, checks: [], acknowledged: [], requests: [], captures: [], legacyProjectionSHA256: {} };
const secrets = [], firstPayloads = new Map(), sentPayloads = new Map(), metadataByOrdinal = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let sdk, active = false, native, tokenPath, credential, nativeIssuePath, modified = false, revoked = false;
try {
  const fixturePath = process.env.GOODSENTRY_ISSUE_WRITE_FIXTURE;
  assert.ok(fixturePath && isAbsolute(fixturePath), 'Provide the absolute private isolated fixture');
  const metadata = await stat(fixturePath); assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
  const fixture = JSON.parse(await readFile(fixturePath, 'utf8')); assert.ok(fixture.cookieHeader && fixture.csrfToken && fixture.orgSlug);
  secrets.push(fixture.cookieHeader, fixture.csrfToken);
  const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
  assert.equal(base.protocol, 'http:'); assert.equal(base.port, '18584'); assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'Use only the new isolated image runtime');
  const url = path => new URL(path, base).href;
  native = async (method, path, body, expected = 200) => {
    const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
    if (method !== 'GET') headers['X-CSRF-Token'] = fixture.csrfToken;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(url(path), { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    assert.equal(response.status, expected, `Session ${method} ${path}`); return expected === 204 ? null : response.json();
  };
  const writer = async (method, path, body, expected = 200, options = {}) => {
    const headers = { Accept: 'application/json', Authorization: options.authorization || `Bearer ${credential.token}`, ...(options.cookie ? { Cookie: fixture.cookieHeader } : {}) };
    if (options.cookie && method !== 'GET') Object.assign(headers, { Origin: base.origin, 'X-CSRF-Token': fixture.csrfToken });
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const response = await fetch(url(path), { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    report.requests.push({ method, path, status: response.status }); assert.equal(response.status, expected, `Writer ${method} ${path}`); return response.json();
  };
  const me = await native('GET', 'api/v1/me'); assert.ok(me.user.id && me.user.email && me.organizations.some(value => value.slug === fixture.orgSlug));
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`, org = encodeURIComponent(fixture.orgSlug), family = `issue-write-${run}`;
  const version = value => `${family}@${value}`, projectName = `Issue writer SDK ${run}`;
  const project = await native('POST', `api/v1/organizations/${org}/projects`, { name: projectName, platform: 'node' }, 201);
  assert.equal(new URL(project.dsn).origin, base.origin); secrets.push(project.dsn, new URL(project.dsn).username);
  Object.assign(report, { orgSlug: fixture.orgSlug, projectID: project.id, projectSlug: project.slug, projectName });
  const releasesPath = `api/0/organizations/${org}/releases/`;
  await native('POST', releasesPath, { version: version('1.0.0'), projects: [project.slug] }, 201);
  tokenPath = `api/v1/organizations/${org}/api-tokens`;
  credential = await native('POST', tokenPath, { name: `Issue SDK writer ${run}`, scopes: ['event:write'], projectIds: [project.id], expiresInDays: 1 }, 201);
  secrets.push(credential.token); assert.deepEqual(credential.scopes, ['event:write']); assert.equal(credential.allProjects, false); assert.deepEqual(credential.projects.map(value => value.id), [project.id]);
  const module = process.env.GOODSENTRY_NODE_SDK || '/tmp/goodsentry-node-sdk/node_modules/@sentry/node'; assert.ok(isAbsolute(module));
  sdk = createRequire(import.meta.url)(module); assert.equal(sdk.SDK_VERSION, '11.7.0'); report.sdkVersion = sdk.SDK_VERSION;
  sdk.init({ dsn: project.dsn, environment: 'issue-write-sdk', defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1, tracesSampleRate: 0, sendDefaultPii: false,
    beforeSend(event) {
      const capture = metadataByOrdinal[event.extra.acceptanceOrdinal]; assert.ok(capture);
      event.timestamp = capture.timestamp; event.release = capture.release; return event;
    },
    transport(options) {
      const actual = sdk.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const response = await actual.send(envelope);
        for (const [header, payload] of envelope[1]) if (header.type === 'event') {
          assert.equal(response.statusCode, 200, 'Official SDK envelope acknowledgement');
          const copy = JSON.parse(JSON.stringify(payload)); if (!firstPayloads.has(payload.event_id)) firstPayloads.set(payload.event_id, copy);
          sentPayloads.set(payload.event_id, copy); report.acknowledged.push(payload.event_id);
        }
        return response;
      } };
    } }); active = true;
  const stamp = Math.floor(Date.now() / 1000) - 600;
  const capture = async (release, retryID) => {
    const ordinal = metadataByOrdinal.length, timestamp = stamp - ordinal * 10; metadataByOrdinal.push({ release, timestamp });
    const id = sdk.withScope(scope => {
      scope.setFingerprint([family]); scope.setTag('consumer', 'issue-write-sdk'); scope.setUser({ id: `writer-${run}` }); scope.setExtra('acceptanceOrdinal', ordinal);
      return sdk.captureException(new Error(`Issue writer SDK ${run} #${ordinal}`), retryID ? { event_id: retryID } : undefined);
    });
    assert.match(id, /^[0-9a-f]{32}$/); if (retryID) assert.equal(id, retryID); assert.ok(await sdk.flush(10000));
    const sent = sentPayloads.get(id); assert.ok(sent); assert.equal(sent.release, release); assert.equal(sent.timestamp, timestamp); assert.equal(sent.sdk.version, '11.7.0');
    report.captures.push({ eventID: id, release, timestamp, duplicate: !!retryID }); return id;
  };
  const firstID = await capture(version('1.0.0'));
  const listing = await native('GET', `api/v1/organizations/${org}/issues?${new URLSearchParams({ project: project.id, status: 'all' })}`); assert.equal(listing.total, 1);
  const issueID = listing.data[0].id; nativeIssuePath = `api/v1/issues/${encodeURIComponent(issueID)}`; report.issueID = issueID;
  const globalPath = `api/0/issues/${issueID}/`, orgPath = `api/0/organizations/${org}/issues/${issueID}/`;
  const detail = () => native('GET', nativeIssuePath), legacyPath = id => `${orgPath}events/${id}/json/`;
  const initial = await detail(); assert.equal(initial.issue.release, version('1.0.0')); assert.deepEqual(initial.event, firstPayloads.get(firstID));
  const rawSHA256 = hash(initial.event); report.nativeRawSHA256 = rawSHA256;
  const rememberLegacy = async id => {
    const legacy = await writer('GET', legacyPath(id)); assert.equal(legacy.event_id, id); assert.deepEqual(legacy.exception, firstPayloads.get(id).exception);
    const actual = hash(legacy); if (report.legacyProjectionSHA256[id]) assert.equal(actual, report.legacyProjectionSHA256[id]); else report.legacyProjectionSHA256[id] = actual;
  };
  await rememberLegacy(firstID);
  const checkpoint = async (all = false) => {
    const current = await detail(); assert.equal(current.event.event_id, firstID); assert.equal(hash(current.event), rawSHA256, 'Original native SDK raw is unchanged');
    for (const id of all ? firstPayloads.keys() : [firstID]) await rememberLegacy(id); return current;
  };
  const put = async (path, body) => {
    await checkpoint(); modified = true; const result = await writer('PUT', path, body); assert.equal(result.id, issueID); await checkpoint(); return result;
  };
  const assign = async (reference, type, id, path = globalPath) => {
    await put(path, { assignedTo: reference }); const current = await detail(); assert.equal(current.issue.assignedTo?.type, type); assert.equal(current.issue.assignedTo?.id, id);
  };
  const unassign = async reference => { await put(orgPath, { assignedTo: reference }); assert.equal((await detail()).issue.assignedTo, null); };
  await assign(me.user.id, 'user', me.user.id); await unassign('');
  await assign(`user:${me.user.id}`, 'user', me.user.id, orgPath); await unassign(null);
  await assign(me.user.email.toUpperCase(), 'user', me.user.id); await unassign(null);
  let team, candidatesRead = 0;
  for (let page = 1; !team; page++) {
    const candidates = await native('GET', `${nativeIssuePath}/assignees?page=${page}`); candidatesRead += candidates.data.length; team = candidates.data.find(actor => actor.type === 'team');
    if (page * candidates.pageSize >= candidates.total || !candidates.data.length) break;
  }
  if (team) { await assign(`team:${team.id}`, 'team', team.id, orgPath); await unassign(null); report.teamAssignment = { tested: true, id: team.id }; }
  else report.teamAssignment = { tested: false, limitation: `No current team returned by the real native assignee API (${candidatesRead} candidates inspected); no team creation API is implemented.` };
  await assign(`user:${me.user.id}`, 'user', me.user.id);
  report.checks.push('Restricted writer changes assignment using actual owner bare ID, user prefix and case-insensitive primary email, plus a real team when listed; null/empty unassign through both PUT families without changing SDK raw');
  assert.equal((await put(globalPath, { status: 'muted', statusDetails: { ignoreCount: 2 } })).status, 'ignored');
  let current = await detail(); assert.equal(current.issue.ignore.remaining, 2); assert.equal(current.issue.assignedTo.id, me.user.id);
  const countedID = await capture(version('1.0.0')); await rememberLegacy(countedID); current = await checkpoint(); assert.equal(current.issue.status, 'ignored'); assert.equal(current.issue.ignore.remaining, 1);
  const beforeRetry = current.issue, beforeRetryActivity = await native('GET', `${nativeIssuePath}/activity`);
  await capture(version('9.0.0'), countedID); current = await checkpoint(); assert.deepEqual(current.issue, beforeRetry); assert.deepEqual(await native('GET', `${nativeIssuePath}/activity`), beforeRetryActivity); await rememberLegacy(countedID);
  assert.equal((await native('GET', `${nativeIssuePath}/releases?${new URLSearchParams({ query: version('9.0.0') })}`)).total, 0);
  const exhaustedID = await capture(version('1.0.0')); await rememberLegacy(exhaustedID); current = await checkpoint(); assert.equal(current.issue.status, 'unresolved'); assert.equal(current.issue.ignore, null); assert.equal(current.issue.count, 3);
  report.checks.push('Actual SDK captures consume two additional-error receipts; a changed SDK retry with the same event_id is acknowledged without consuming the last receipt, observing its release, adding activity or rewriting raw');
  await put(orgPath, { status: 'ignored', ignoreDuration: 1 }); current = await detail(); assert.equal(current.issue.ignore.minutes, 1);
  const until = current.issue.ignore.until, deadline = Date.parse(until) + 1500, started = Date.now(); assert.ok(Number.isFinite(deadline) && deadline > started && deadline - started < 75000, 'Real one-minute mute has a bounded future expiry');
  console.log(JSON.stringify({ phase: 'waiting-real-mute-expiry', seconds: Math.ceil((deadline - started) / 1000) }));
  let progressAt = started + 30000;
  while (Date.now() < deadline) {
    await delay(Math.min(1000, deadline - Date.now()));
    if (Date.now() >= progressAt) { console.log(JSON.stringify({ phase: 'waiting-real-mute-expiry', secondsRemaining: Math.max(0, Math.ceil((deadline - Date.now()) / 1000)) })); progressAt += 30000; }
  }
  const beforeExpiryCapture = await detail(), expiryID = await capture(version('1.0.0')); await rememberLegacy(expiryID); current = await checkpoint(); assert.equal(current.issue.status, 'unresolved'); assert.equal(current.issue.ignore, null);
  report.expiry = { until, elapsedMilliseconds: Date.now() - started, beforeCaptureStatus: beforeExpiryCapture.issue.status, afterCaptureStatus: current.issue.status };
  report.checks.push('A one-minute mute elapses on the actual runtime clock across a fresh official SDK capture; no database timestamp update or clock seam is used');
  await put(orgPath, { status: 'resolved', statusDetails: { inRelease: version('1.0.0') } }); const chosen = (await detail()).issue.resolution; assert.equal(chosen.mode, 'release'); assert.equal(chosen.release.version, version('1.0.0'));
  const sameID = await capture(version('1.0.0')); await rememberLegacy(sameID); current = await checkpoint(); assert.equal(current.issue.status, 'resolved'); assert.deepEqual(current.issue.resolution, chosen);
  const newerID = await capture(version('1.1.0')); await rememberLegacy(newerID); current = await checkpoint(); assert.equal(current.issue.status, 'unresolved'); assert.equal(current.issue.resolution, null);
  let history = await native('GET', `${nativeIssuePath}/activity`); assert.ok(history.data.some(row => row.type === 'regressed' && row.data.eventID === newerID && row.data.reason === 'newer_release'));
  await native('POST', releasesPath, { version: version('1.2.0'), projects: [project.slug] }, 201);
  await put(globalPath, { status: 'resolved', statusDetails: { inRelease: 'latest' } }); current = await detail(); assert.equal(current.issue.resolution.release.version, version('1.2.0'));
  await put(orgPath, { status: 'resolvedInNextRelease' }); current = await detail(); assert.equal(current.issue.status, 'resolved'); assert.equal(current.issue.resolution.mode, 'next_release'); assert.equal(current.issue.resolution.release, null);
  const nextID = await capture(version('2.0.0')); await rememberLegacy(nextID); current = await checkpoint(); assert.equal(current.issue.status, 'unresolved'); assert.equal(current.issue.resolution, null);
  history = await native('GET', `${nativeIssuePath}/activity`); assert.ok(history.data.some(row => row.type === 'regressed' && row.data.eventID === nextID && row.data.reason === 'next_release'));
  assert.ok(history.data.some(row => row.type === 'resolution_bound' && row.data.resolution.release.version === version('2.0.0')));
  report.checks.push('Chosen release keeps an equal SDK recurrence resolved and a newer release regresses; latest selects a real project association, then a pending next-release policy binds and regresses through a new SDK release');
  const ids = [...firstPayloads.keys()]; assert.equal(ids.length, 7); assert.equal(report.acknowledged.length, 8); assert.equal(current.issue.count, ids.length);
  const global = await writer('GET', globalPath), scoped = await writer('GET', orgPath); assert.equal(global.id, issueID); assert.equal(scoped.count, String(ids.length));
  for (const path of [`api/0/organizations/${org}/issues/?${new URLSearchParams({ project: project.id, query: '' })}`, `api/0/projects/${org}/${encodeURIComponent(project.slug)}/issues/?query=`]) assert.deepEqual((await writer('GET', path)).map(issue => issue.id), [issueID]);
  const stats = await writer('GET', `api/0/organizations/${org}/issues-stats/?groups=${issueID}`); assert.equal(stats[0].id, issueID); assert.equal(stats[0].count, String(ids.length));
  for (const path of [`${orgPath}events/`, `${globalPath}events/`]) assert.deepEqual(new Set((await writer('GET', path)).map(event => event.eventID)), new Set(ids));
  await checkpoint(true);
  await writer('GET', `${releasesPath}${encodeURIComponent(version('1.0.0'))}/`, undefined, 403, { cookie: true });
  await writer('PATCH', nativeIssuePath, { status: 'unresolved' }, 401);
  await writer('GET', globalPath, undefined, 401, { cookie: true, authorization: 'Bearer invalid-sdk-control' });
  await writer('PUT', globalPath, {}, 401, { cookie: true, authorization: 'Bearer' });
  report.checks.push('Writer inherits issue/list/stats/event/legacy reads; release reads return403 and token-only native mutation without a session returns401. Invalid or malformed Bearer on compatible routes cannot fall back to the valid session cookie');
  await put(orgPath, { status: 'unresolved', assignedTo: null, statusDetails: null }); current = await checkpoint(true);
  assert.equal(current.issue.status, 'unresolved'); assert.equal(current.issue.assignedTo, null); assert.equal(current.issue.ignore, null); assert.equal(current.issue.resolution, null); assert.equal(current.issue.count, ids.length); modified = false;
  await native('DELETE', `${tokenPath}/${credential.id}`, undefined, 204); revoked = true;
  await writer('GET', globalPath, undefined, 401, { cookie: true }); await writer('PUT', orgPath, {}, 401, { cookie: true });
  const final = await detail(); assert.equal(hash(final.event), rawSHA256); assert.equal(final.issue.count, ids.length);
  report.checks.push('Revocation denies reads and writes with a live cookie; all native latest/raw and legacy projection hashes survive and the SDK issue remains unresolved, unassigned and without policies for browser acceptance');
  Object.assign(report, { passed: true, eventIDs: ids, releaseVersion: version('2.0.0'), baseURL: base.href, basePath: base.pathname, finalStatus: final.issue.status, finalCount: final.issue.count });
} catch (error) { report.error = safe(error.stack || error); process.exitCode = 1; }
finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.error = safe(error.stack || error); process.exitCode = 1; }
  if (modified && native && nativeIssuePath) try { await native('PATCH', nativeIssuePath, { status: 'unresolved', assignedTo: null }); } catch (error) { report.passed = false; report.cleanupError = safe(error.message); process.exitCode = 1; }
  if (native && tokenPath && credential && !revoked) try { await native('DELETE', `${tokenPath}/${credential.id}`, undefined, 204); } catch (error) { report.passed = false; report.cleanupError = safe(error.message); process.exitCode = 1; }
  await mkdir(dirname(output), { recursive: true }); await writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ passed: report.passed, checks: report.checks.length, acknowledged: report.acknowledged.length, projectID: report.projectID, issueID: report.issueID, report: output, ...(report.error ? { error: report.error } : {}) }));
}
