// Official SDK normalization and durable capture, followed by native historical
// filtering and pagination. The primary owns the isolated server and fixture.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, isAbsolute, join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { pathToFileURL } from 'node:url';

const require = createRequire(import.meta.url);
const run = `${Date.now().toString(36)}-${randomBytes(5).toString('hex')}`;
const prefix = `Issue filter proof ${run}`;
const output = resolve(process.env.GOODSENTRY_OUTPUT_DIR || `test-results/issues-sdk-${run}`);
const report = { ok: false, run, prefix, events: [], checks: [] };
const specialKey = 'custom:key<>&"';
const specialValue = '<img src=x onerror=alert(1)> *_%\\ " & 🐛';
const alternateValue = specialValue.replace('*', 'X');
const cohortRelease = 'filters@1', cohortEnvironment = 'production';
let fixture, base, Sentry, activeClient = false, secrets = [];

function sanitize(value) {
  let text = String(value);
  for (const secret of secrets) if (secret.length > 0) text = text.split(secret).join('[REDACTED]');
  return text.replace(/\bBearer\s+[^\s"']+/gi, 'Bearer [REDACTED]')
    .replace(/([?&]sentry_key=)[^&\s"']+/gi, '$1[REDACTED]')
    .replace(/(https?:\/\/)[^\s/@]+@/gi, '$1[REDACTED]@');
}

async function jsonFile(name, value) {
  await writeFile(join(output, name), JSON.stringify(value, (_, item) => typeof item === 'string' ? sanitize(item) : item, 2) + '\n', { mode: 0o600 });
}

async function api(path) {
  const response = await fetch(new URL(path, base), {
    headers: { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin },
    signal: AbortSignal.timeout(15000),
  });
  assert.ok(response.status === 200, `Native read API returned HTTP${response.status}`);
  return response.json();
}

async function issues(parameters) {
  return api(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues?${new URLSearchParams(parameters)}`);
}

async function loadFixtureAndSDK() {
  const fixturePath = process.env.GOODSENTRY_ISSUE_FIXTURE;
  assert.ok(fixturePath, 'Provide the private issue fixture path');
  const info = await stat(fixturePath);
  assert.ok(info.isFile() && (info.mode & 0o777) === 0o600, 'Issue fixture must be a regular mode0600 file');
  try { fixture = JSON.parse(await readFile(fixturePath, 'utf8')); }
  catch (_) { throw new Error('Private issue fixture must contain valid JSON'); }
  secrets = [fixture.cookieHeader, fixture.csrfToken, fixture.project?.dsn, fixture.otherProject?.dsn].filter(value => typeof value === 'string' && value.length > 0);
  if (typeof fixture.cookieHeader === 'string') secrets.push(...fixture.cookieHeader.split(';').map(pair => pair.slice(pair.indexOf('=') + 1).trim()).filter(Boolean));
  for (const value of [fixture.cookieHeader, fixture.csrfToken, fixture.orgSlug, fixture.project?.id, fixture.project?.slug, fixture.otherProject?.id]) {
    assert.ok(typeof value === 'string' && value.length > 0, 'Issue fixture is missing required context');
  }
  base = new URL(fixture.baseURL);
  assert.ok(['http:', 'https:'].includes(base.protocol) && !base.username && !base.password && !base.search && !base.hash, 'Use an HTTP application target without URL credentials/query/fragment');
  if (!base.pathname.endsWith('/')) base.pathname += '/';
  for (const project of [fixture.project, fixture.otherProject]) {
    const dsn = new URL(project.dsn);
    secrets.push(decodeURIComponent(dsn.username), decodeURIComponent(dsn.password));
    assert.ok(dsn.origin === base.origin && dsn.pathname.split('/').at(-1) === String(project.id), 'Both SDK DSNs must target the supplied application and projects');
  }
  secrets = [...new Set(secrets.filter(value => typeof value === 'string' && value.length > 0))].sort((a, b) => b.length - a.length);
  assert.notEqual(fixture.project.id, fixture.otherProject.id, 'Project isolation control requires two distinct supplied projects');
  const sdkPath = process.env.GOODSENTRY_SDK_MODULE;
  assert.ok(sdkPath && isAbsolute(sdkPath), 'Provide the installed pinned Node SDK as an absolute module path');
  const entry = require.resolve(sdkPath);
  const imported = await import(pathToFileURL(entry).href);
  Sentry = typeof imported.default?.init === 'function' ? imported.default : imported;
  let folder = dirname(entry), version;
  for (let depth = 0; depth < 8; depth++) {
    try {
      const metadata = JSON.parse(await readFile(join(folder, 'package.json'), 'utf8'));
      if (metadata.name === '@sentry/node') { version = metadata.version; break; }
    } catch (_) { /* Package entry directories need not have package metadata. */ }
    const parent = dirname(folder); if (parent === folder) break; folder = parent;
  }
  assert.equal(version, '11.7.0', 'Use the installed pinned official Node SDK');
  assert.equal(typeof Sentry.captureEvent, 'function');
  report.sdkVersion = version;
  report.project = { id: fixture.project.id, slug: fixture.project.slug };
  report.otherProject = { id: fixture.otherProject.id, slug: fixture.otherProject.slug };
  report.orgSlug = fixture.orgSlug;
}

function startClient(project) {
  Sentry.init({ dsn: project.dsn, debug: false, sampleRate: 1, tracesSampleRate: 0,
    autoSessionTracking: false, sendDefaultPii: false, maxBreadcrumbs: 0, enableLogs: false });
  activeClient = true;
}

async function closeClient() {
  if (!activeClient) return;
  activeClient = false;
  assert.ok(await Sentry.close(10000), 'Official SDK close timed out');
}

async function storedCapture(eventID, message, project) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const result = await issues({ project: project.id, query: message, status: 'all' });
    for (const issue of result.data) {
      const detail = await api(`api/v1/issues/${encodeURIComponent(issue.id)}`);
      if (detail.event?.event_id === eventID) return detail;
    }
    await delay(200);
  }
  throw new Error('Actual SDK event was not visible through the native API within15s');
}

async function capture(index, role, timestamp, { project = fixture.project, environment = cohortEnvironment, release = cohortRelease, region = 'eu', extraTags = {} } = {}) {
  const message = `${prefix} ${role} ${String(index).padStart(2, '0')}`;
  const fingerprint = [`issue-filter-${run}-${index}`];
  const tags = { region, ...extraTags };
  const eventID = Sentry.captureEvent({ message, level: 'error', timestamp, fingerprint, release, environment, tags });
  assert.ok(typeof eventID === 'string' && /^[0-9a-f]{32}$/.test(eventID), 'Official SDK must generate the event ID');
  assert.ok(await Sentry.flush(10000), 'Official SDK flush timed out');
  const detail = await storedCapture(eventID, message, project);
  const actual = detail.event;
  assert.equal(String(detail.issue.projectId), project.id);
  assert.equal(actual.message, message);
  assert.equal(actual.timestamp, timestamp);
  assert.equal(actual.environment, environment);
  assert.equal(actual.release, release);
  assert.deepEqual(actual.fingerprint, fingerprint);
  for (const [key, value] of Object.entries(tags)) assert.equal(actual.tags?.[key], value, 'Stored SDK tag must retain its exact key/value');
  assert.equal(actual.sdk?.version, '11.7.0', 'Stored native event must retain the official SDK version');
  assert.ok(detail.events.some(item => item.eventID === eventID), 'Native history must include the actual SDK event ID');
  const raw = JSON.stringify(actual, null, 2) + '\n';
  assert.ok(!secrets.some(secret => raw.includes(secret)), 'Native capture contains a private fixture value; do not publish it');
  const rawFile = `${String(report.events.length).padStart(2, '0')}-${role}-${index}.event.json`;
  await writeFile(join(output, rawFile), raw, { mode: 0o600 });
  const metadata = { index, role, eventID, issueId: detail.issue.id, projectId: project.id, message, timestamp, environment, release, fingerprint, tags, sdkVersion: actual.sdk.version, rawFile, nativeEventSHA256: createHash('sha256').update(raw).digest('hex') };
  report.events.push(metadata);
  return { detail, metadata };
}

async function completePage(parameters, expected, label) {
  const first = await issues({ ...parameters, page: '1' });
  const second = await issues({ ...parameters, page: '2' });
  assert.equal(first.page, 1); assert.equal(second.page, 2);
  assert.equal(first.pageSize, 25); assert.equal(second.pageSize, 25);
  assert.equal(first.total, 26); assert.equal(second.total, 26);
  assert.equal(first.data.length, 25); assert.equal(second.data.length, 1);
  const actual = [...first.data, ...second.data].map(issue => issue.id);
  assert.deepEqual(actual, expected, `Native ${label} order/pages must follow independently seeded lifetime summaries`);
  report.checks.push({ name: label, pageSizes: [first.data.length, second.data.length], total: first.total, issueIds: actual });
}

async function main() {
  await mkdir(output, { recursive: true, mode: 0o700 });
  await loadFixtureAndSDK();
  const baseTimestamp = Math.floor(Date.now() / 1000) - 86400;
  const issueIdsByIndex = [];
  startClient(fixture.project);
  try {
    for (let index = 0; index < 26; index++) {
      const extraTags = index === 1 ? { [specialKey]: specialValue } : index === 2 ? { [specialKey]: alternateValue } : {};
      const result = await capture(index, 'cohort', baseTimestamp + index * 60, { extraTags });
      issueIdsByIndex[index] = result.metadata.issueId;
    }
    const latest = await capture(0, 'latest', baseTimestamp + 10000, { environment: 'staging', release: 'filters@2', region: 'us' });
    assert.equal(latest.metadata.issueId, issueIdsByIndex[0], 'Same SDK fingerprint must group historical and latest samples');
    assert.equal(Number(latest.detail.issue.count), 2);
    for (const eventID of [report.events[0].eventID, latest.metadata.eventID]) {
      assert.ok(latest.detail.events.some(item => item.eventID === eventID), 'Historical and latest SDK IDs must remain in the same native issue history');
    }
    await capture(26, 'wrong-release', baseTimestamp + 26 * 60, { release: 'filters@other' });
    await capture(27, 'wrong-environment', baseTimestamp + 27 * 60, { environment: 'staging' });
    await capture(28, 'wrong-tag', baseTimestamp + 28 * 60, { region: 'us' });
    const splitFirst = await capture(29, 'split-first', baseTimestamp + 29 * 60, { release: 'filters@other' });
    const splitLatest = await capture(29, 'split-latest', baseTimestamp + 11000, { environment: 'staging', region: 'us' });
    assert.equal(splitFirst.metadata.issueId, splitLatest.metadata.issueId, 'Split control must actually share one issue');
  } finally { await closeClient(); }
  startClient(fixture.otherProject);
  try { await capture(30, 'other-project', baseTimestamp + 30 * 60, { project: fixture.otherProject }); }
  finally { await closeClient(); }
  assert.equal(report.events.length, 33, 'Expected 33 actual SDK captures');
  assert.equal(new Set(report.events.map(event => event.eventID)).size, 33);
  assert.equal(new Set(report.events.map(event => event.issueId)).size, 31);
  const all = await issues({ query: prefix, status: 'all' });
  const projectA = await issues({ query: prefix, status: 'all', project: fixture.project.id });
  assert.equal(all.total, 31, 'All seeded SDK issues must be visible through the supplied organization');
  assert.equal(projectA.total, 30, 'Selected project must exclude the second project');
  const descending = Array.from({ length: 25 }, (_, index) => 25 - index);
  const expectedOrders = {
    date: [0, ...descending].map(index => issueIdsByIndex[index]),
    new: [...descending, 0].map(index => issueIdsByIndex[index]),
    freq: [issueIdsByIndex[0], ...issueIdsByIndex.slice(1).sort((a, b) => BigInt(a) > BigInt(b) ? -1 : 1)],
  };
  const filters = { project: fixture.project.id, environment: cohortEnvironment, release: cohortRelease, query: `region:eu ${prefix}`, status: 'all' };
  for (const sort of ['date', 'new', 'freq']) await completePage({ ...filters, sort }, expectedOrders[sort], sort);
  for (let index = 0; index < 26; index++) {
    const detail = await api(`api/v1/issues/${encodeURIComponent(issueIdsByIndex[index])}`);
    const seed = report.events.find(event => event.role === 'cohort' && event.index === index);
    assert.ok(detail.events.some(event => event.eventID === seed.eventID), 'Cohort issue history lost its actual SDK event');
    assert.equal(Number(detail.issue.count), index === 0 ? 2 : 1);
    assert.equal(Date.parse(detail.issue.firstSeen) / 1000, baseTimestamp + index * 60);
    assert.equal(Date.parse(detail.issue.lastSeen) / 1000, index === 0 ? baseTimestamp + 10000 : baseTimestamp + index * 60);
    assert.equal(detail.issue.environment, index === 0 ? 'staging' : cohortEnvironment);
    assert.equal(detail.issue.release, index === 0 ? 'filters@2' : cohortRelease);
  }
  for (const [index, value] of [[1, specialValue], [2, alternateValue]]) {
    const query = `tags[${JSON.stringify(specialKey)}]:${JSON.stringify(value)} ${prefix}`;
    const literal = await issues({ project: fixture.project.id, query, status: 'all' });
    assert.equal(literal.total, 1, 'Quoted special value must not behave as a wildcard');
    assert.deepEqual(literal.data.map(issue => issue.id), [issueIdsByIndex[index]]);
  }
  report.cohort = { ...filters, total: 26, issueIdsByIndex, expectedOrders, baseTimestamp };
  report.tagCases = [{ issueId: issueIdsByIndex[1], key: specialKey, value: specialValue }, { issueId: issueIdsByIndex[2], key: 'region', value: 'eu' }];
  report.literalControl = { issueId: issueIdsByIndex[2], key: specialKey, value: alternateValue };
  report.sdkEventCount = 33;
  report.issueCount = 31;
  report.checks.push({ name: 'historical matching preserves lifetime summaries', cohortSize: 26, historicalIssueId: issueIdsByIndex[0], lifetimeCount: 2, splitEventControlExcluded: true, projectControlExcluded: true });
  report.ok = true;
  await jsonFile('report.json', report);
  console.log(JSON.stringify({ ok: true, run, sdkVersion: report.sdkVersion, sdkEventCount: report.sdkEventCount, issueCount: report.issueCount, cohortTotal: 26, report: join(output, 'report.json') }));
}

try { await main(); }
catch (error) {
  try { await closeClient(); } catch (_) { /* Preserve the original failed assertion. */ }
  report.error = sanitize(error.stack || error.message || error);
  await mkdir(output, { recursive: true, mode: 0o700 });
  await jsonFile('report.json', report);
  console.error(report.error);
  process.exitCode = 1;
}
