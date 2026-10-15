// External-client owner: real CLI injection/upload plus official SDK exception
// capture, followed by native retrieval of mapped and preserved raw frames.
// The primary supplies a mode0600 private fixture and owns the disposable target.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { basename, dirname, join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath, pathToFileURL } from 'node:url';

const require = createRequire(import.meta.url);
const run = `${Date.now().toString(36)}-${randomBytes(5).toString('hex')}`;
const output = resolve(process.env.GOODSENTRY_OUTPUT_DIR || `test-results/artifact-cli-sdk-${run}`);
const report = { ok: false, run, cliCommands: [], captures: [] };
let secrets = [], Sentry, fixture, base, cliEnvironment;
const normalizedID = value => String(value || '').replaceAll('-', '').toLowerCase();
const validID = value => /^[0-9a-f]{32}$/.test(normalizedID(value));

function sanitize(value) {
  let result = String(value);
  for (const secret of secrets) if (typeof secret === 'string' && secret.length) result = result.split(secret).join('[REDACTED]');
  return result.replace(/\bBearer\s+[^\s"']+/gi, 'Bearer [REDACTED]')
    .replace(/([?&]sentry_key=)[^&\s"']+/gi, '$1[REDACTED]')
    .replace(/(https?:\/\/)[^\s/@]+@/gi, '$1[REDACTED]@');
}

async function jsonFile(path, value) {
  await writeFile(path, JSON.stringify(value, (_, item) => typeof item === 'string' ? sanitize(item) : item, 2), { mode: 0o600 });
}

async function loadSDK() {
  let specifier = process.env.GOODSENTRY_SDK_MODULE || '@sentry/node';
  if (specifier.startsWith('file:')) specifier = fileURLToPath(specifier);
  const entry = require.resolve(specifier);
  const imported = await import(pathToFileURL(entry).href);
  Sentry = typeof imported.default?.init === 'function' ? imported.default : imported;
  let folder = dirname(entry), version;
  for (let depth = 0; depth < 8; depth++) {
    try {
      const metadata = JSON.parse(await readFile(join(folder, 'package.json'), 'utf8'));
      if (metadata.name === '@sentry/node') { version = metadata.version; break; }
    } catch (_) { /* An entry directory need not contain package metadata. */ }
    const parent = dirname(folder); if (parent === folder) break; folder = parent;
  }
  assert.equal(version, '11.7.0', 'Use the already-installed pinned official Node SDK');
  assert.equal(typeof Sentry.captureException, 'function');
  report.sdkVersion = version;
}

async function command(name, args, cwd) {
  const result = spawnSync(process.env.GOODSENTRY_CLI_PATH, args, {
    cwd, env: cliEnvironment, encoding: 'utf8', timeout: 60000, maxBuffer: 4 * 1024 * 1024,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  const stdout = sanitize(result.stdout || ''), stderr = sanitize(result.stderr || '');
  await writeFile(join(output, `${name}.stdout.log`), stdout, { mode: 0o600 });
  await writeFile(join(output, `${name}.stderr.log`), stderr, { mode: 0o600 });
  const entry = { name, args, exitCode: result.status, signal: result.signal, stdout, stderr, error: result.error?.message };
  report.cliCommands.push(entry);
  assert.ok(!result.error && result.status === 0, `Official CLI ${name} failed; inspect its sanitized output logs`);
  console.log(JSON.stringify({ cliStage: name, exitCode: result.status }));
  return stdout + '\n' + stderr;
}

async function api(path) {
  const response = await fetch(new URL(path, base), {
    headers: { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin },
    signal: AbortSignal.timeout(15000),
  });
  assert.ok(response.status === 200, `Native read API returned HTTP${response.status}`);
  return response.json();
}

async function maps() {
  const result = await api(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/projects/${encodeURIComponent(fixture.project.id)}/sourcemaps`);
  return result.data;
}
async function mapsFor(fileID) { return (await maps()).filter(map => normalizedID(map.debugId) === normalizedID(fileID)); }

function matchingException(event, message) {
  const values = Array.isArray(event.exception) ? event.exception : event.exception?.values || [];
  return values.find(value => value.value === message);
}

async function findEvent(eventID, message, project) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const query = new URLSearchParams({ project: project.id, query: message });
    const issues = await api(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues?${query}`);
    for (const issue of issues.data) {
      const detail = await api(`api/v1/issues/${encodeURIComponent(issue.id)}`);
      if (detail.event?.event_id === eventID) return detail;
    }
    await delay(250);
  }
  throw new Error('Official SDK event was not retrievable through the authenticated native API within15s');
}

async function capture(stage, build, expected, fileID, { targetProject = fixture.project, release = '', dist = '', expectMapped = true } = {}) {
  process.setSourceMapsEnabled?.(false);
  Sentry.init({
    dsn: targetProject.dsn, environment: 'artifact-cli-sdk', release, dist,
    debug: false, sampleRate: 1, tracesSampleRate: 0, autoSessionTracking: false,
    sendDefaultPii: false, maxBreadcrumbs: 0, enableLogs: false,
  });
  let eventID, rawLocation;
  try {
    const throwFromInjectedBuild = require(build);
    try { throwFromInjectedBuild(); assert.fail('The original fixture must actually throw'); }
    catch (error) {
      assert.equal(error.message, expected.message);
      const line = error.stack.split('\n').find(line => line.includes(build + ':'));
      assert.ok(line, 'V8 must identify the actual injected minified build');
      const position = line.match(/:(\d+):(\d+)\)?$/);
      assert.ok(position, 'The real thrown exception must have a minified line and column');
      rawLocation = { filename: build, lineno: Number(position[1]), colno: Number(position[2]) };
      eventID = Sentry.captureException(error);
    }
    assert.ok(typeof eventID === 'string' && /^[0-9a-f]{32}$/.test(eventID), 'The official SDK must produce an event ID');
    assert.ok(await Sentry.flush(10000), 'Official SDK flush timed out');
  } finally { assert.ok(await Sentry.close(10000), 'Official SDK close timed out'); }
  const detail = await findEvent(eventID, expected.message, targetProject);
  const event = detail.event, exception = matchingException(event, expected.message);
  await jsonFile(join(output, `${stage}.event.json`), event);
  assert.ok(exception, 'Stored event must contain the real thrown exception');
  assert.ok(detail.events.some(item => item.eventID === eventID), 'Issue history must include the actual SDK event');
  assert.equal(String(detail.issue.projectId), String(targetProject.id));
  assert.equal(event.sdk?.version, '11.7.0', 'Stored event must identify the official pinned SDK');
  const matchesBuild = frame => String(frame.abs_path || frame.filename).endsWith('/' + basename(build));
  const image = event.debug_meta?.images?.find(image => image.type === 'sourcemap' && String(image.code_file).endsWith('/' + basename(build)));
  if (fileID) assert.ok(image && normalizedID(image.debug_id) === normalizedID(fileID), 'Actual emitted SDK debug_meta must reference this injected script and file DebugID');
  else assert.ok(!image, 'An un-injected legacy script must not emit an SDK DebugID image for this file');
  const mapped = exception.stacktrace?.frames?.find(frame => typeof frame.filename === 'string' && (frame.filename === expected.sourceName || frame.filename.endsWith('/' + expected.sourceName)));
  const raw = exception.raw_stacktrace?.frames?.find(matchesBuild);
  const unchanged = exception.stacktrace?.frames?.find(matchesBuild);
  report.captures.push({ stage, eventID, issueId: detail.issue.id, projectId: targetProject.id, expectMapped, sdkDebugImage: image || null, expected, expectedRaw: rawLocation, mapped: mapped || null, raw: raw || null, unchanged: unchanged || null, release: event.release || null, dist: event.dist || null });
  if (expectMapped) {
    assert.ok(mapped, 'Actual SDK exception must map to the independently authored original file');
    assert.equal(mapped.lineno, 7, 'Original throw is independently fixed at line7');
    assert.equal(mapped.colno, 1, 'The explicit mapping targets original zero-based column0');
    assert.equal(mapped.context_line, expected.contextLine);
    assert.deepEqual(mapped.pre_context, expected.preContext);
    assert.deepEqual(mapped.post_context, expected.postContext);
    assert.ok(raw, 'The original minified stack must remain in raw_stacktrace');
    assert.equal(raw.lineno, rawLocation.lineno); assert.equal(raw.colno, rawLocation.colno);
  } else {
    assert.ok(!mapped, 'A nonmatching project/release/dist must not expose the uploaded original file');
    assert.ok(!exception.raw_stacktrace, 'A rejected artifact selector must not add a mapped raw_stacktrace');
    assert.ok(unchanged, 'The real minified frame must remain present');
    assert.equal(unchanged.filename, rawLocation.filename);
    assert.equal(unchanged.lineno, rawLocation.lineno); assert.equal(unchanged.colno, rawLocation.colno);
    assert.ok(!exception.stacktrace.frames.some(frame => frame.context_line === expected.contextLine), 'A rejected artifact selector must not leak original source context');
  }
  assert.equal(event.release || '', release); assert.equal(event.dist || '', dist);
  console.log(JSON.stringify({ sdkStage: stage, eventID, mappedOriginalLine: mapped?.lineno || null, selectorRejected: !expectMapped }));
}

async function authorBuild(work, buildDir, filename, sourceName, message) {
  const sourceDir = join(work, 'source');
  await mkdir(buildDir, { recursive: true }); await mkdir(sourceDir, { recursive: true });
  const lines = ["'use strict';", "const component = 'artifact-proof';", 'function originalFailure() {', "  const precedingValue = 'independent source context';", '  const order = 42;', '  // Known line immediately before the real exception.', `  throw new Error(${JSON.stringify(message)});`, '}', 'module.exports = originalFailure;'];
  const expected = { sourceName, lineno: 7, colno: 1, message, contextLine: lines[6], preContext: lines.slice(1, 6), postContext: lines.slice(7) };
  await writeFile(join(sourceDir, sourceName), lines.join('\n') + '\n');
  const build = join(buildDir, filename);
  await writeFile(build, `module.exports=function artifactFailure(){throw new Error(${JSON.stringify(message)})};\n//# sourceMappingURL=${filename}.map\n`);
  // A=generated column0, A=source0, M=original line delta+6, A=column0.
  await writeFile(build + '.map', JSON.stringify({ version: 3, file: filename, sources: [sourceName], sourcesContent: [lines.join('\n')], names: [], mappings: 'AAMA' }));
  return { build, expected };
}

async function main() {
  const fixturePath = process.env.GOODSENTRY_ARTIFACT_FIXTURE;
  assert.ok(fixturePath && process.env.GOODSENTRY_CLI_PATH, 'Provide the private artifact fixture and pinned CLI executable paths');
  const fixtureStat = await stat(fixturePath);
  assert.ok(fixtureStat.isFile() && (fixtureStat.mode & 0o777) === 0o600, 'Artifact fixture must be a regular mode0600 file');
  try { fixture = JSON.parse(await readFile(fixturePath, 'utf8')); }
  catch (_) { throw new Error('Private artifact fixture must contain valid JSON'); }
  secrets = [fixture.cookieHeader, fixture.csrfToken, fixture.token, fixture.project?.dsn, fixture.otherProject?.dsn].filter(value => typeof value === 'string' && value.length > 0);
  if (typeof fixture.cookieHeader === 'string') secrets.push(...fixture.cookieHeader.split(';').map(pair => pair.slice(pair.indexOf('=') + 1).trim()));
  base = new URL(fixture.baseURL);
  assert.ok(['http:', 'https:'].includes(base.protocol) && !base.username && !base.password && !base.search && !base.hash, 'Use the fixture HTTP target without URL credentials/query/fragment');
  if (!base.pathname.endsWith('/')) base.pathname += '/';
  for (const project of [fixture.project, fixture.otherProject]) {
    let dsn;
    try { dsn = new URL(project.dsn); }
    catch (_) { throw new Error('Both supplied projects must have valid SDK DSNs'); }
    secrets.push(decodeURIComponent(dsn.username), decodeURIComponent(dsn.password));
    assert.ok(dsn.origin === base.origin && dsn.pathname.split('/').at(-1) === String(project.id), 'SDK DSNs must target their supplied application and projects');
  }
  secrets = [...new Set(secrets.filter(value => typeof value === 'string' && value.length > 0))].sort((a, b) => b.length - a.length);
  assert.notEqual(String(fixture.project.id), String(fixture.otherProject.id), 'Wrong-project control needs a distinct supplied project');
  for (const value of [fixture.cookieHeader, fixture.csrfToken, fixture.token, fixture.orgSlug, fixture.project.id, fixture.project.slug]) assert.ok(typeof value === 'string' && value.length > 0, 'Fixture is missing required client context');
  await mkdir(output, { recursive: true, mode: 0o700 });
  cliEnvironment = { ...process.env, SENTRY_URL: base.href, SENTRY_ORG: fixture.orgSlug, SENTRY_PROJECT: fixture.project.slug, SENTRY_AUTH_TOKEN: fixture.token, SENTRY_LOG_LEVEL: 'info', SENTRY_NO_PROGRESS_BAR: '1', SENTRY_DISABLE_UPDATE_CHECK: '1', SENTRY_LOAD_DOTENV: '0', NO_COLOR: '1', CI: '1' };
  for (const name of ['SENTRY_RELEASE', 'SENTRY_DIST', 'SENTRY_PROPERTIES', 'SENTRY_DSN']) delete cliEnvironment[name];
  await loadSDK();
  const versionOutput = await command('cli-version', ['--version'], output);
  assert.ok(/^sentry-cli 3\.8\.0\s*$/m.test(versionOutput), 'Use the pinned sentry-cli3.8.0 executable'); report.cliVersion = '3.8.0';
  const release = fixture.version || `goodsentry-artifact-cli-${run}`;
  assert.ok(typeof release === 'string' && release.length > 0, 'Optional fixture version must be a nonempty string');
  const work = join(output, `build-${run}`), buildDir = join(work, 'dist');
  await mkdir(work, { recursive: true });
  await writeFile(join(work, 'package.json'), '{"private":true,"type":"commonjs"}\n');
  const { build, expected } = await authorBuild(work, buildDir, 'app.js', `original-${run}.js`, `GoodSentry artifact SDK ${run}`);
  await command('inject', ['sourcemaps', 'inject', buildDir], work);
  const injectedMap = JSON.parse(await readFile(build + '.map', 'utf8'));
  const fileID = injectedMap.debug_id || injectedMap.debugId;
  assert.ok(validID(fileID), 'Official injection must add a valid source-file DebugID');
  const scriptID = (await readFile(build, 'utf8')).match(/\/\/#\s*debugId\s*=\s*([0-9a-f-]+)/i)?.[1];
  assert.ok(validID(scriptID) && normalizedID(scriptID) === normalizedID(fileID), 'Injected script and map must share the file DebugID');
  report.project = { id: fixture.project.id, slug: fixture.project.slug }; report.fileDebugId = fileID; report.expected = expected; report.releaseVariant = { version: release, dist: 'web' };
  async function upload(name, extra = []) {
    const log = await command(name, ['sourcemaps', 'upload', '--wait', ...extra, buildDir], work);
    const bundleID = log.match(/Bundle ID:\s*([0-9a-f-]+)/i)?.[1];
    assert.ok(validID(bundleID) && normalizedID(bundleID) !== normalizedID(fileID), 'Actual bundle ID must differ from the injected source-file DebugID');
    report.cliCommands.at(-1).bundleId = bundleID;
  }
  await upload('upload-debug-only'); const first = await mapsFor(fileID);
  assert.ok(first.length > 0, 'Official CLI upload must materialize this file DebugID');
  await upload('upload-duplicate'); const duplicate = await mapsFor(fileID);
  assert.deepEqual(duplicate.map(map => map.id).sort(), first.map(map => map.id).sort(), 'Actual duplicate CLI upload must not duplicate native artifact records');
  report.mapMetadata = { first, duplicate };
  await capture('debug-only', build, expected, fileID);
  await upload('upload-release-dist', ['--release', release, '--dist', 'web']);
  report.mapMetadata.releaseDist = await mapsFor(fileID);
  assert.ok(report.mapMetadata.releaseDist.some(map => map.release === release && map.dist === 'web'), 'The second upload must store the requested release/dist selector');
  await capture('release-dist', build, expected, fileID, { release, dist: 'web' });
  await capture('wrong-project', build, expected, fileID, { targetProject: fixture.otherProject, release, dist: 'web', expectMapped: false });
  const legacyDir = join(work, 'legacy'), legacyRelease = `goodsentry-artifact-legacy-${run}`, legacyDist = 'legacy-web';
  const legacy = await authorBuild(work, legacyDir, 'app-legacy.js', `original-legacy-${run}.js`, `GoodSentry legacy artifact SDK ${run}`);
  // Intentionally never inject this second script: the SDK must use release+dist+URL matching.
  await command('upload-legacy', ['sourcemaps', 'upload', '--wait', '--release', legacyRelease, '--dist', legacyDist, '--url-prefix', legacyDir, legacyDir], work);
  report.legacyMetadata = (await maps()).filter(map => map.release === legacyRelease && map.dist === legacyDist);
  assert.ok(report.legacyMetadata.some(map => map.name === legacy.build + '.map'), 'CLI URL prefix must store the exact absolute legacy map URL');
  await capture('legacy-release-dist', legacy.build, legacy.expected, null, { release: legacyRelease, dist: legacyDist });
  await capture('legacy-wrong-version', legacy.build, legacy.expected, null, { release: legacyRelease + '-wrong', dist: legacyDist, expectMapped: false });
  await capture('legacy-wrong-dist', legacy.build, legacy.expected, null, { release: legacyRelease, dist: legacyDist + '-wrong', expectMapped: false });
  report.ok = true;
}

try { await main(); }
catch (error) { report.failure = sanitize(error.stack || error.message); console.error(report.failure); process.exitCode = 1; }
finally {
  if (Sentry) await Sentry.close(2000).catch(() => {});
  await mkdir(output, { recursive: true, mode: 0o700 }); await jsonFile(join(output, 'report.json'), report);
  console.log(JSON.stringify({ ok: report.ok, sdkEvents: report.captures.map(item => ({ stage: item.stage, eventID: item.eventID })), report: join(output, 'report.json') }));
}
