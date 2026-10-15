// The official SDK and real HTTP runtime own this acceptance boundary. Provider
// tests cannot detect lost per-event release identity or envelope retry handling.
import assert from 'node:assert/strict';
import { randomBytes, createHash } from 'node:crypto';
import { readFile, writeFile, mkdir, stat } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve, isAbsolute } from 'node:path';

const output = resolve(process.env.GOODSENTRY_RESOLUTION_OUTPUT || 'test-results/resolution-sdk');
const report = { passed: false, checks: [], acknowledgedEvents: [] };
const secrets = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let sdk, active = false;
try {
  const path = process.env.GOODSENTRY_RESOLUTION_FIXTURE;
  assert.ok(path && isAbsolute(path), 'Provide an absolute private fixture path');
  assert.equal((await stat(path)).mode & 0o077, 0, 'Fixture must be private');
  const fixture = JSON.parse(await readFile(path, 'utf8'));
  for (const value of [fixture.cookieHeader, fixture.csrfToken, fixture.project?.dsn, fixture.ownerPassword]) if (value) secrets.push(value);
  const base = new URL(fixture.baseURL.endsWith('/') ? fixture.baseURL : `${fixture.baseURL}/`);
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'Use a local acceptance runtime');
  const api = async (path, body) => {
    const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
    if (body !== undefined) Object.assign(headers, { 'X-CSRF-Token': fixture.csrfToken, 'Content-Type': 'application/json' });
    const response = await fetch(new URL(path, base), { method: body === undefined ? 'GET' : 'PATCH', headers, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    assert.equal(response.status, 200, 'Native acceptance request succeeds');
    return response.json();
  };
  const module = process.env.GOODSENTRY_NODE_SDK;
  assert.ok(module && isAbsolute(module), 'Provide the pinned SDK module path');
  sdk = createRequire(import.meta.url)(module);
  assert.equal(sdk.SDK_VERSION, '11.7.0');
  report.sdkVersion = sdk.SDK_VERSION;
  sdk.init({ dsn: fixture.project.dsn, release: '', defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1, tracesSampleRate: 0, sendDefaultPii: false,
    transport(options) {
      const actual = sdk.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const result = await actual.send(envelope);
        for (const [header, payload] of envelope[1]) if (header.type === 'event') {
          assert.equal(result.statusCode, 200, 'Official envelope transport acknowledgement');
          report.acknowledgedEvents.push(payload.event_id);
        }
        return result;
      } };
    } });
  active = true;
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const family = `resolution-sdk-${run}`, version = value => `${family}@${value}`;
  const message = `Release resolution SDK ${run}`, fingerprint = [`release-resolution-sdk-${run}`];
  const stamp = Math.floor(Date.now() / 1000), ids = Array.from({ length: 8 }, () => randomBytes(16).toString('hex'));
  const capture = async (index, release, timestamp) => {
    assert.equal(sdk.captureEvent({ event_id: ids[index], message, fingerprint, release, timestamp, level: 'error', environment: 'resolution-sdk' }), ids[index]);
    assert.ok(await sdk.flush(10000), 'Official SDK flush succeeds');
  };
  await capture(0, version('1.2.0'), stamp);
  const listing = await api(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues?${new URLSearchParams({ project: fixture.project.id, query: message, status: 'all' })}`);
  assert.equal(listing.total, 1);
  const issueID = listing.data[0].id, pathIssue = `api/v1/issues/${encodeURIComponent(issueID)}`;
  const initial = await api(pathIssue), rawHash = hash(initial.event);
  assert.equal(initial.event.release, version('1.2.0'));
  assert.equal(initial.event.sdk.version, '11.7.0');
  const selected = await api(pathIssue, { status: 'resolved', resolution: { mode: 'release', version: version('1.2.0') } });
  assert.equal(selected.resolution.comparison, 'semantic');
  assert.equal(selected.resolution.release.version, version('1.2.0'));
  assert.ok(selected.resolution.release.dateCreated.endsWith('Z'));
  const selectedHistory = await api(`${pathIssue}/activity`);
  await capture(0, version('9.0.0'), stamp);
  assert.deepEqual((await api(pathIssue)).issue, selected);
  assert.deepEqual(await api(`${pathIssue}/activity`), selectedHistory);
  assert.equal((await api(`${pathIssue}/releases?${new URLSearchParams({ query: version('9.0.0') })}`)).total, 0);
  report.checks.push('Acknowledged event-ID retry preserves policy/history/raw identity and does not observe its changed release');
  await capture(1, version('1.1.0'), stamp - 86400);
  await capture(2, version('1.2.0+build.002'), stamp - 7200);
  let detail = await api(pathIssue);
  assert.equal(detail.issue.count, 3);
  assert.equal(detail.issue.status, 'resolved');
  assert.deepEqual(detail.issue.resolution, selected.resolution);
  assert.equal(hash(detail.event), rawHash);
  report.checks.push('Late older and build-only equal releases are captured and stay resolved');
  await capture(3, version('1.3.0'), stamp - 3600);
  detail = await api(pathIssue);
  assert.equal(detail.issue.count, 4);
  assert.equal(detail.issue.status, 'unresolved');
  assert.equal(detail.issue.resolution, null);
  let regressions = (await api(`${pathIssue}/activity`)).data.filter(row => row.type === 'regressed');
  assert.equal(regressions.length, 1);
  assert.equal(regressions[0].data.reason, 'newer_release');
  assert.equal(regressions[0].data.eventID, ids[3]);
  assert.deepEqual(regressions[0].data.resolution, selected.resolution);
  assert.equal(regressions[0].data.resolvedAt, selected.resolvedAt);
  report.checks.push('A late newer SDK release regresses once and records the immutable previous policy');
  const pending = await api(pathIssue, { status: 'resolved', resolution: { mode: 'next_release' } });
  assert.equal(pending.resolution.release, null);
  assert.equal(pending.resolution.baseline.version, version('1.2.0'));
  const pendingHistory = await api(`${pathIssue}/activity`);
  await capture(3, version('99.0.0'), stamp - 3600);
  assert.deepEqual((await api(pathIssue)).issue, pending);
  assert.deepEqual(await api(`${pathIssue}/activity`), pendingHistory);
  await capture(4, version('1.1.0'), stamp - 90000);
  assert.equal((await api(pathIssue)).issue.resolution.release, null);
  await capture(5, version('1.4.0'), stamp - 172800);
  detail = await api(pathIssue);
  assert.equal(detail.issue.count, 6);
  assert.equal(detail.issue.status, 'unresolved');
  const history = await api(`${pathIssue}/activity`), bound = history.data.filter(row => row.type === 'resolution_bound');
  assert.equal(bound.length, 1);
  assert.equal(bound[0].user, null);
  assert.equal(bound[0].data.resolution.release.version, version('1.4.0'));
  regressions = history.data.filter(row => row.type === 'regressed');
  assert.equal(regressions.length, 2);
  assert.equal(regressions[0].data.reason, 'next_release');
  assert.equal(regressions[0].data.eventID, ids[5]);
  assert.deepEqual(regressions[0].data.resolution, bound[0].data.resolution);
  assert.ok(Number(bound[0].id) < Number(regressions[0].id));
  report.checks.push('Pending next policy survives old/retried events, then a new late release binds and regresses in one fresh capture');
  await api(pathIssue, { status: 'resolved', resolution: { mode: 'release', version: version('1.4.0') } });
  await capture(6, '.', stamp - 200000);
  assert.equal((await api(pathIssue)).issue.status, 'unresolved');
  regressions = (await api(`${pathIssue}/activity`)).data.filter(row => row.type === 'regressed');
  assert.equal(regressions[0].data.reason, 'missing_release');
  assert.equal(regressions[0].data.eventID, ids[6]);
  await api(pathIssue, { status: 'resolved', resolution: { mode: 'next_release' } });
  await capture(7, '', stamp - 210000);
  detail = await api(pathIssue);
  assert.equal(detail.issue.count, 8);
  assert.equal(detail.issue.status, 'unresolved');
  assert.equal(detail.issue.resolution, null);
  assert.equal(hash(detail.event), rawHash);
  regressions = (await api(`${pathIssue}/activity`)).data.filter(row => row.type === 'regressed');
  assert.equal(regressions.length, 4);
  assert.equal(regressions[0].data.reason, 'missing_release');
  assert.equal(regressions[0].data.eventID, ids[7]);
  report.checks.push('Invalid and absent SDK release identities reopen their policies while the latest original raw event remains unchanged');
  const events = detail.events;
  assert.equal(events.length, 8);
  assert.equal(new Set(events.map(row => row.eventID)).size, 8);
  assert.equal(report.acknowledgedEvents.length, 10);
  assert.deepEqual(new Set(report.acknowledgedEvents), new Set(ids));
  Object.assign(report, { passed: true, issueID, projectID: fixture.project.id, uniqueEvents: ids, count: detail.issue.count, rawLatestSHA256: rawHash, finalIssue: detail.issue, activity: (await api(`${pathIssue}/activity`)).data });
} catch (error) {
  report.error = safe(error.stack || error);
  process.exitCode = 1;
} finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.error = safe(error.stack || error); process.exitCode = 1; }
  await mkdir(output, { recursive: true });
  await writeFile(resolve(output, 'report.json'), `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ passed: report.passed, sdkVersion: report.sdkVersion, checks: report.checks.length, acknowledged: report.acknowledgedEvents.length, uniqueEvents: report.uniqueEvents?.length, issueID: report.issueID, report: resolve(output, 'report.json'), ...(report.error ? { error: report.error } : {}) }));
}
