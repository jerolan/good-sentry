// Official SDK transport owns retries and late unique captures at the actual
// runtime boundary. Provider tests cannot detect SDK envelope integration loss.
// No mocked transport, production clock seam or external recipient is used.
import assert from 'node:assert/strict';
import { randomBytes, createHash } from 'node:crypto';
import { readFile, writeFile, mkdir, stat } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve, isAbsolute } from 'node:path';

const output = resolve(process.env.GOODSENTRY_IGNORE_OUTPUT || 'test-results/ignore-sdk');
const report = { passed: false, checks: [], acknowledgedEvents: [] };
const secrets = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let sdk, active = false;
try {
  const path = process.env.GOODSENTRY_IGNORE_FIXTURE;
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
  sdk.init({ dsn: fixture.project.dsn, defaultIntegrations: false, autoSessionTracking: false, enableLogs: false, sampleRate: 1, tracesSampleRate: 0, sendDefaultPii: false,
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
  const message = `Conditional mute SDK ${run}`;
  const fingerprint = [`conditional-mute-sdk-${run}`];
  const stamp = Math.floor(Date.now() / 1000);
  const ids = Array.from({ length: 4 }, () => randomBytes(16).toString('hex'));
  const capture = async (id, timestamp) => {
    assert.equal(sdk.captureEvent({ event_id: id, message, fingerprint, timestamp, level: 'error', environment: 'ignore-sdk' }), id);
    assert.ok(await sdk.flush(10000), 'Official SDK flush succeeds');
  };
  await capture(ids[0], stamp);
  const listing = await api(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues?${new URLSearchParams({ project: fixture.project.id, query: message, status: 'all' })}`);
  assert.equal(listing.total, 1);
  const issueID = listing.data[0].id;
  const pathIssue = `api/v1/issues/${encodeURIComponent(issueID)}`;
  const initial = await api(pathIssue);
  const rawHash = hash(initial.event);
  const change = { status: 'ignored', ignore: { minutes: 120, count: 2 } };
  const muted = await api(pathIssue, change);
  assert.equal(muted.ignore.remaining, 2);
  assert.ok(muted.ignore.until.endsWith('Z'));
  const history = await api(`${pathIssue}/activity`);
  assert.deepEqual((await api(pathIssue, change)).ignore, muted.ignore);
  assert.deepEqual(await api(`${pathIssue}/activity`), history);
  await capture(ids[0], stamp);
  let current = await api(pathIssue);
  assert.equal(current.issue.count, 1);
  assert.deepEqual(current.issue.ignore, muted.ignore);
  assert.deepEqual(await api(`${pathIssue}/activity`), history);
  report.checks.push('same policy and acknowledged duplicate preserve deadline, baseline and activity');
  await capture(ids[1], stamp - 86400);
  await capture(ids[1], stamp - 86400);
  current = await api(pathIssue);
  assert.equal(current.issue.status, 'ignored');
  assert.equal(current.issue.count, 2);
  assert.equal(current.issue.ignore.remaining, 1);
  assert.equal(current.issue.ignore.until, muted.ignore.until);
  report.checks.push('fresh one-day-late event counts once while its retry leaves one remaining');
  await capture(ids[2], stamp - 3600);
  current = await api(pathIssue);
  assert.equal(current.issue.status, 'unresolved');
  assert.equal(current.issue.count, 3);
  assert.equal(current.issue.ignore, null);
  assert.equal(current.issue.regressedAt, null);
  assert.equal(hash(current.event), rawHash);
  const ended = (await api(`${pathIssue}/activity`)).data.filter(row => row.type === 'unignored');
  assert.equal(ended.length, 1);
  assert.equal(ended[0].user, null);
  assert.equal(ended[0].data.reason, 'count');
  assert.equal(ended[0].data.eventID, ids[2]);
  assert.equal(ended[0].data.ignore.remaining, 0);
  report.checks.push('second additional unique SDK capture reopens once with system count activity and unchanged raw latest event');
  await api(pathIssue, { status: 'ignored', ignore: null });
  await capture(ids[3], stamp - 1800);
  current = await api(pathIssue);
  assert.equal(current.issue.status, 'ignored');
  assert.equal(current.issue.count, 4);
  assert.equal(current.issue.ignore, null);
  assert.deepEqual(report.acknowledgedEvents, [ids[0], ids[0], ids[1], ids[1], ids[2], ids[3]]);
  report.checks.push('explicit indefinite mute continues accepting distinct SDK errors');
  Object.assign(report, { passed: true, projectID: fixture.project.id, issueID, eventIDs: ids, message, fingerprint, occurrenceTimestamp: stamp, originalRawSHA256: rawHash });
} catch (error) { report.failure = safe(error.stack || error); process.exitCode = 1; }
finally {
  if (active) try { assert.ok(await sdk.close(10000)); } catch (error) { report.passed = false; report.failure = safe(error.stack || error); process.exitCode = 1; }
  await mkdir(output, { recursive: true });
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify({ passed: report.passed, checks: report.checks.length, acknowledgedEvents: report.acknowledgedEvents.length, report: resolve(output, 'report.json') }));
}
