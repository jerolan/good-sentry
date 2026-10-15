// Test-audit gate: the real pinned SDK serializer/transport owns this proof.
// Provider and native HTTP tests cannot detect lost SDK default/custom grouping
// identities or an acknowledged SDK retry that changes stored data. Observe the
// actual transport without changing envelopes; use a new isolated project and
// no production seam. SQL topology/policy matrices belong to provider tests.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { isAbsolute, resolve } from 'node:path';

const output = resolve(process.env.GOODSENTRY_GROUPING_OUTPUT || 'test-results/grouping-sdk-native');
const report = { passed: false, checks: [], acknowledgedEvents: [], states: [] };
const secrets = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let sdk, active = false;
try {
  const fixturePath = process.env.GOODSENTRY_GROUPING_FIXTURE;
  assert.ok(fixturePath && isAbsolute(fixturePath), 'Provide an absolute isolated private fixture path');
  assert.equal((await stat(fixturePath)).mode & 0o077, 0, 'Fixture must be private');
  const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
  for (const value of [fixture.cookieHeader, fixture.csrfToken, fixture.ownerPassword, fixture.ownerEmail]) if (value) secrets.push(value);
  const base = new URL(fixture.baseURL || 'http://localhost:18009/');
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'Use the isolated local acceptance runtime');
  assert.equal(base.protocol, 'http:');
  assert.ok(['18009', '18081'].includes(base.port), 'Use an isolated native or image grouping acceptance runtime');
  base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
  const api = async (method, path, body, expected = 200) => {
    const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
    if (body !== undefined) Object.assign(headers, { 'Content-Type': 'application/json', 'X-CSRF-Token': fixture.csrfToken });
    const response = await fetch(new URL(path, base), { method, headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    assert.equal(response.status, expected, `${method} native acceptance request succeeds`);
    return response.json();
  };
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const project = await api('POST', `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/projects`, { name: `SDK grouping ${run}`, platform: 'node' }, 201);
  assert.ok(project.id && project.dsn, 'The test creates its own SDK project');
  secrets.push(project.dsn, new URL(project.dsn).username);
  const target = new URL(project.dsn);
  assert.equal(target.origin, base.origin, 'Official SDK stays on the isolated runtime');
  const module = process.env.GOODSENTRY_NODE_SDK;
  assert.ok(module && isAbsolute(module), 'Provide the pinned official Node SDK module path');
  sdk = createRequire(import.meta.url)(module);
  assert.equal(sdk.SDK_VERSION, '11.7.0');
  report.sdkVersion = sdk.SDK_VERSION;
  sdk.init({ dsn: project.dsn, release: '', defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1, tracesSampleRate: 0, sendDefaultPii: false,
    transport(options) {
      const actual = sdk.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const response = await actual.send(envelope);
        for (const [header, payload] of envelope[1]) if (header.type === 'event') {
          assert.equal(response.statusCode, 200, 'Official SDK envelope acknowledgement');
          report.acknowledgedEvents.push(payload.event_id);
        }
        return response;
      } };
    } });
  active = true;
  const ids = Array.from({ length: 8 }, () => randomBytes(16).toString('hex'));
  const stamp = Math.floor(Date.now() / 1000) - 600;
  const release = `grouping-sdk-${run}@1.0.0`, retryRelease = `grouping-sdk-${run}@9.9.9`;
  const customMessage = `Custom grouping SDK ${run}`, defaultMessage = `Default grouping SDK ${run}`;
  const capture = async (index, family, timestamp, eventRelease = release) => {
    const event = { event_id: ids[index], timestamp, release: eventRelease, environment: 'grouping-sdk', level: 'error', extra: { acceptance: run, family, receipt: index } };
    if (family === 'custom') Object.assign(event, { message: customMessage, fingerprint: [`grouping-sdk-${run}`] });
    else Object.assign(event, { exception: { values: [{ type: 'GroupingSDKFailure', value: defaultMessage, stacktrace: { frames: [{ filename: '/acceptance/grouping.js', function: 'checkout', lineno: 42, colno: 7, in_app: true }] } }] } });
    assert.equal(sdk.captureEvent(event), ids[index]);
    assert.ok(await sdk.flush(10000), 'Official SDK flush succeeds');
  };
  const orgIssues = `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues`;
  const list = () => api('GET', `${orgIssues}?${new URLSearchParams({ project: project.id, status: 'all' })}`);
  const issuePath = id => `api/v1/issues/${encodeURIComponent(id)}`;
  const detail = id => api('GET', issuePath(id));
  const eventIDs = value => value.events.map(row => row.eventID).sort();
  const state = (label, ...details) => report.states.push({ label, issues: details.map(value => ({ id: value.issue.id, count: value.issue.count, groupSize: value.issue.groupSize, status: value.issue.status, resolution: value.issue.resolution, eventIDs: eventIDs(value), rawLatestSHA256: hash(value.event) })) });
  await capture(0, 'custom', stamp);
  await capture(1, 'default', stamp + 1);
  const initial = await list();
  assert.equal(initial.total, 2, 'Custom and default grouping begin as distinct issues');
  const custom = initial.data.find(issue => issue.title === customMessage);
  const fallback = initial.data.find(issue => issue.title.includes(defaultMessage));
  assert.ok(custom && fallback && custom.id !== fallback.id);
  const rootID = custom.id, originID = fallback.id;
  assert.ok(BigInt(rootID) < BigInt(originID), 'First capture owns the retained lowest-ID root');
  const originalRoot = await detail(rootID), originalDefault = await detail(originID);
  assert.equal(originalRoot.event.sdk.version, '11.7.0');
  assert.deepEqual(originalRoot.event.fingerprint, [`grouping-sdk-${run}`]);
  assert.equal(Object.hasOwn(originalDefault.event, 'fingerprint'), false, 'Default grouping has no SDK custom fingerprint');
  assert.equal(originalDefault.event.exception.values[0].stacktrace.frames[0].function, 'checkout');
  assert.equal(originalDefault.event.exception.values[0].stacktrace.frames[0].lineno, 42);
  const rootRawHash = hash(originalRoot.event), defaultRawHash = hash(originalDefault.event);
  const selected = await api('PATCH', issuePath(rootID), { status: 'resolved', resolution: { mode: 'release', version: release } });
  assert.ok(selected.resolution && selected.resolution.release.version === release);
  const merged = await api('POST', `${orgIssues}/merge`, { issueIds: [originID, rootID] });
  assert.equal(merged.changed, true);
  assert.equal(merged.issue.id, rootID);
  assert.equal(merged.issue.count, 2);
  assert.equal(merged.issue.groupSize, 2);
  assert.deepEqual(merged.issue.resolution, selected.resolution);
  assert.equal(merged.issue.status, 'resolved');
  report.checks.push('Real official SDK custom fingerprint and constant-frame default captures form two original groups; native merge retains the lowest root and existing policy');
  for (const [index, family, seconds] of [[2, 'custom', 60], [3, 'default', 30], [4, 'custom', 120], [5, 'default', 90]]) await capture(index, family, stamp - seconds);
  const combined = await detail(rootID), alias = await detail(originID);
  assert.equal((await list()).total, 1);
  assert.equal(combined.issue.count, 6);
  assert.equal(alias.issue.id, rootID);
  assert.deepEqual(eventIDs(combined), ids.slice(0, 6).sort());
  assert.equal(hash(combined.event), defaultRawHash, 'Late captures retain latest raw event');
  assert.deepEqual(combined.issue.resolution, selected.resolution);
  assert.equal(combined.issue.status, 'resolved');
  const origins = await api('GET', `${issuePath(originID)}/origins`);
  assert.equal(origins.issueId, rootID);
  assert.deepEqual(origins.data.map(row => [row.id, row.count]), [[rootID, 3], [originID, 3]]);
  state('both SDK families routed to merged root', combined);
  report.checks.push('Four late fresh SDK events retain both original grouping identities while routing through the merged root');
  const history = await api('GET', `${issuePath(rootID)}/activity`);
  const releases = await api('GET', `${issuePath(rootID)}/releases`);
  await capture(0, 'custom', stamp, retryRelease);
  await capture(1, 'default', stamp + 1, retryRelease);
  assert.deepEqual(await detail(rootID), combined, 'Acknowledged retries do not alter issue policy/count/raw event summaries');
  assert.deepEqual(await api('GET', `${issuePath(rootID)}/activity`), history);
  assert.deepEqual(await api('GET', `${issuePath(rootID)}/releases`), releases);
  assert.deepEqual(await api('GET', `${issuePath(rootID)}/origins`), origins);
  assert.equal((await api('GET', `${issuePath(rootID)}/releases?${new URLSearchParams({ query: retryRelease })}`)).total, 0);
  report.checks.push('Both previously accepted SDK IDs receive200 without changing policy/count/raw/history/origins or observing the altered retry release');
  const split = await api('POST', `${issuePath(rootID)}/unmerge`, { originIds: [originID] });
  assert.equal(split.changed, true);
  assert.deepEqual(split.detachedIssueIds, [originID]);
  assert.equal(split.remainingIssueId, rootID);
  const splitRoot = await detail(rootID), splitDefault = await detail(originID);
  assert.equal(splitRoot.issue.count, 3);
  assert.equal(splitDefault.issue.count, 3);
  assert.equal(splitRoot.issue.groupSize, 1);
  assert.equal(splitDefault.issue.groupSize, 1);
  assert.deepEqual(eventIDs(splitRoot), [ids[0], ids[2], ids[4]].sort());
  assert.deepEqual(eventIDs(splitDefault), [ids[1], ids[3], ids[5]].sort());
  assert.equal(splitDefault.issue.status, 'unresolved');
  assert.equal(splitDefault.issue.resolution, null);
  assert.deepEqual(splitRoot.issue.resolution, selected.resolution);
  assert.equal(hash(splitRoot.event), rootRawHash);
  assert.equal(hash(splitDefault.event), defaultRawHash);
  state('default origin separated', splitRoot, splitDefault);
  await capture(6, 'default', stamp - 180);
  assert.deepEqual(await detail(rootID), splitRoot, 'A default-family capture no longer reaches the former merged root');
  await capture(7, 'custom', stamp - 240);
  const finalRoot = await detail(rootID), finalDefault = await detail(originID);
  assert.equal((await list()).total, 2);
  assert.equal(finalRoot.issue.count, 4);
  assert.equal(finalDefault.issue.count, 4);
  assert.deepEqual(eventIDs(finalRoot), [ids[0], ids[2], ids[4], ids[7]].sort());
  assert.deepEqual(eventIDs(finalDefault), [ids[1], ids[3], ids[5], ids[6]].sort());
  assert.equal(hash(finalRoot.event), rootRawHash);
  assert.equal(hash(finalDefault.event), defaultRawHash);
  assert.equal(finalRoot.issue.status, 'resolved');
  assert.deepEqual(finalRoot.issue.resolution, selected.resolution);
  assert.equal(finalDefault.issue.status, 'unresolved');
  state('fresh SDK events follow separated original groups', finalRoot, finalDefault);
  report.checks.push('Separation restores original event partitions and subsequent fresh default/custom SDK captures follow their own groups without changing latest raw events');
  assert.equal(report.acknowledgedEvents.length, 10);
  assert.deepEqual(new Set(report.acknowledgedEvents), new Set(ids));
  Object.assign(report, { passed: true, projectID: project.id, rootID, originID, uniqueEvents: ids, rawLatestSHA256: { custom: rootRawHash, default: defaultRawHash } });
} catch (error) {
  report.error = safe(error.stack || error);
  process.exitCode = 1;
} finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.error = safe(error.stack || error); process.exitCode = 1; }
  await mkdir(output, { recursive: true });
  await writeFile(resolve(output, 'report.json'), `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ passed: report.passed, sdkVersion: report.sdkVersion, projectID: report.projectID, rootID: report.rootID, originID: report.originID, checks: report.checks.length, acknowledged: report.acknowledgedEvents.length, uniqueEvents: report.uniqueEvents?.length, report: resolve(output, 'report.json'), ...(report.error ? { error: report.error } : {}) }));
}
