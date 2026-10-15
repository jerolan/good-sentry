// The official SDK owns its real envelope transport. This acceptance proves
// fresh/late versus retry behavior after triage, beyond direct store fixtures.
import assert from 'node:assert/strict';
import { randomBytes, createHash } from 'node:crypto';
import { readFile, writeFile, mkdir, stat } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve, isAbsolute } from 'node:path';

const require = createRequire(import.meta.url);
const output = resolve(process.env.GOODSENTRY_TRIAGE_OUTPUT || 'test-results/triage-sdk');
const report = { passed: false, captures: [], attempts: [], checks: [] };
let fixture, Sentry, active = false;
const secrets = [];
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
let base;
async function api(path, method = 'GET', body) {
  const headers = { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin };
  if (method !== 'GET') headers['X-CSRF-Token'] = fixture.csrfToken;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const response = await fetch(new URL(path, base), { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  assert.equal(response.status, 200, `Native ${method} acceptance response`);
  return response.json();
}
try {
  const path = process.env.GOODSENTRY_TRIAGE_FIXTURE;
  assert.ok(path && isAbsolute(path), 'Provide an absolute private fixture path');
  assert.equal((await stat(path)).mode & 0o077, 0, 'Fixture credentials must be private');
  fixture = JSON.parse(await readFile(path, 'utf8'));
  for (const value of [fixture.cookieHeader, fixture.csrfToken, fixture.project?.dsn, fixture.ownerPassword, fixture.memberPassword]) if (value) secrets.push(value);
  base = new URL(fixture.baseURL.endsWith('/') ? fixture.baseURL : `${fixture.baseURL}/`);
  assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'Use a local acceptance runtime');
  const sdkPath = process.env.GOODSENTRY_NODE_SDK;
  assert.ok(sdkPath && isAbsolute(sdkPath), 'Provide the pinned official SDK module path');
  Sentry = require(sdkPath);
  assert.equal(Sentry.SDK_VERSION, '11.7.0');
  report.sdkVersion = Sentry.SDK_VERSION;
  const run = `${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const message = `Triage SDK ${run}`;
  const fingerprint = [`triage-sdk-${run}`];
  const stamp = Math.floor(Date.now() / 1000);
  const initialID = randomBytes(16).toString('hex');
  const lateID = randomBytes(16).toString('hex');
  const ignoredID = randomBytes(16).toString('hex');
  Sentry.init({ dsn: fixture.project.dsn, defaultIntegrations: false, autoSessionTracking: false,
    enableLogs: false, sampleRate: 1, tracesSampleRate: 0, sendDefaultPii: false,
    transport(options) {
      const actual = Sentry.makeNodeTransport(options);
      return { flush: timeout => actual.flush(timeout), async send(envelope) {
        const ids = envelope[1].filter(([header]) => header.type === 'event').map(([, event]) => event.event_id);
        const result = await actual.send(envelope);
        if (ids.length) { assert.equal(result.statusCode, 200, 'Official transport acknowledged an event envelope'); report.attempts.push(...ids); }
        return result;
      } };
    } });
  active = true;
  const capture = async (eventID, timestamp) => {
    assert.equal(Sentry.captureEvent({ event_id: eventID, message, fingerprint, timestamp, level: 'error', environment: 'triage-sdk' }), eventID);
    assert.ok(await Sentry.flush(10000), 'Official transport flush');
    report.captures.push({ eventID, timestamp });
  };
  await capture(initialID, stamp);
  const result = await api(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues?${new URLSearchParams({ project: fixture.project.id, query: message, status: 'all' })}`);
  assert.equal(result.total, 1);
  const issueID = result.data[0].id;
  const pathIssue = `api/v1/issues/${encodeURIComponent(issueID)}`;
  const first = await api(pathIssue);
  assert.equal(first.issue.count, 1);
  assert.equal(first.event.event_id, initialID);
  const rawHash = hash(first.event);
  const original = { firstSeen: first.issue.firstSeen, lastSeen: first.issue.lastSeen };
  const target = fixture.triage.team;
  assert.ok(target?.id && target.type === 'team');
  const resolved = await api(pathIssue, 'PATCH', { status: 'resolved', assignedTo: { type: target.type, id: target.id } });
  assert.equal(resolved.assignedTo.id, target.id);
  assert.ok(resolved.resolvedAt.endsWith('Z'));
  const history = await api(`${pathIssue}/activity`);
  await capture(initialID, stamp);
  let current = await api(pathIssue);
  assert.equal(current.issue.status, 'resolved');
  assert.equal(current.issue.count, 1);
  assert.equal(current.issue.resolvedAt, resolved.resolvedAt);
  assert.deepEqual(await api(`${pathIssue}/activity`), history);
  report.checks.push('acknowledged original-ID retry preserves resolution, activity and count');
  await capture(lateID, stamp - 86400);
  current = await api(pathIssue);
  assert.equal(current.issue.status, 'unresolved');
  assert.equal(current.issue.count, 2);
  assert.equal(current.issue.assignedTo.id, target.id);
  assert.equal(Date.parse(current.issue.firstSeen), (stamp - 86400) * 1000);
  assert.equal(current.issue.lastSeen, original.lastSeen);
  assert.equal(hash(current.event), rawHash);
  assert.ok(current.events.some(event => event.eventID === lateID));
  let activities = await api(`${pathIssue}/activity`);
  const regressions = activities.data.filter(activity => activity.type === 'regressed');
  assert.equal(regressions.length, 1);
  assert.equal(regressions[0].user, null);
  assert.equal(regressions[0].data.eventID, lateID);
  assert.equal(regressions[0].dateCreated, current.issue.regressedAt);
  assert.equal(regressions[0].data.resolvedAt, resolved.resolvedAt);
  report.checks.push('fresh SDK occurrence dated one day earlier regresses once using UTC receipt time');
  await api(pathIssue, 'PATCH', { status: 'resolved' });
  activities = await api(`${pathIssue}/activity`);
  await capture(lateID, stamp - 86400);
  current = await api(pathIssue);
  assert.equal(current.issue.status, 'resolved');
  assert.equal(current.issue.count, 2);
  assert.equal(current.issue.regressedAt, null);
  assert.deepEqual(await api(`${pathIssue}/activity`), activities);
  report.checks.push('acknowledged late-ID retry cannot reopen a later resolution');
  await api(pathIssue, 'PATCH', { status: 'ignored' });
  activities = await api(`${pathIssue}/activity`);
  await capture(ignoredID, stamp - 3600);
  current = await api(pathIssue);
  assert.equal(current.issue.status, 'ignored');
  assert.equal(current.issue.count, 3);
  assert.equal(current.issue.assignedTo.id, target.id);
  assert.equal(hash(current.event), rawHash);
  assert.deepEqual(await api(`${pathIssue}/activity`), activities);
  assert.deepEqual(report.attempts, [initialID, initialID, lateID, lateID, ignoredID]);
  report.checks.push('ignored fresh captures remain stored without status activity');
  report.issueID = issueID;
  report.message = message;
  report.fingerprint = fingerprint;
  report.occurrenceTimestamp = stamp;
  report.originalRawSHA256 = rawHash;
  report.projectID = fixture.project.id;
  report.passed = true;
} catch (error) {
  report.failure = safe(error.stack || error);
  process.exitCode = 1;
} finally {
  if (active) try { assert.ok(await Sentry.close(10000)); } catch (error) { report.passed = false; report.failure = safe(error.stack || error); process.exitCode = 1; }
  await mkdir(output, { recursive: true });
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify({ passed: report.passed, checks: report.checks.length, captures: report.captures.length, acknowledgedAttempts: report.attempts.length, report: resolve(output, 'report.json') }));
}
