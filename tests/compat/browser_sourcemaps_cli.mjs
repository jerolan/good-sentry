// External-client owner: real Chromium running the official pinned browser SDK
// 11.7.0 against a page built by sentry-cli 3.8.0 injection and upload. The
// native API must return the mapped original function, file, line and context
// while preserving the real minified stack. The primary supplies a mode0600
// private fixture and owns the disposable target.
import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { createServer } from 'node:http';
import { copyFile, mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { basename, join, resolve, sep } from 'node:path';
import { spawnSync } from 'node:child_process';
import { setTimeout as delay } from 'node:timers/promises';
import { pathToFileURL } from 'node:url';

const run = Date.now().toString(36) + '-' + randomBytes(5).toString('hex');
const output = resolve(process.env.GOODSENTRY_OUTPUT_DIR || ('test-results/browser-sourcemaps-cli-' + run));
const report = { ok: false, run, cliCommands: [], captures: [] };
const PINNED_INTEGRITY = 'sha384-4hVyRtcJfOjrFnfkkeOAlFOf15CvnDo0eQmbzzGuWjstMU6bsoKrVJWx1YIHYTRX';
let secrets = [], fixture, base, cliEnvironment, browser, server, staticOrigin, chromium, fileDebugID = '';

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

async function loadBrowser() {
  const specifier = process.env.GOODSENTRY_PLAYWRIGHT_MODULE;
  assert.ok(specifier, 'Provide the pinned Playwright module path');
  const entry = specifier.startsWith('file:') ? new URL(specifier) : pathToFileURL(specifier);
  const imported = await import(entry.href);
  chromium = imported.chromium || imported.default?.chromium;
  assert.equal(typeof chromium?.launch, 'function', 'Pinned Playwright must expose Chromium');
  const executable = typeof chromium.executablePath === 'function' ? chromium.executablePath() : '';
  report.playwright = { entry: entry.href, executable: executable ? basename(String(executable)) : null };
}

async function serve(root) {
  server = createServer(async (request, response) => {
    try {
      const pathname = decodeURIComponent(new URL(request.url, 'http://127.0.0.1').pathname);
      const file = resolve(root, '.' + pathname);
      assert.ok(file === root || file.startsWith(root + sep), 'Static path escaped its root');
      const data = await readFile(file);
      const type = file.endsWith('.html') ? 'text/html; charset=utf-8'
        : file.endsWith('.map') ? 'application/json; charset=utf-8'
        : file.endsWith('.js') || file.endsWith('.mjs') ? 'text/javascript; charset=utf-8' : 'application/octet-stream';
      response.writeHead(200, { 'Content-Type': type, 'Cache-Control': 'no-store' });
      response.end(data);
    } catch (_) { response.writeHead(404).end(); }
  });
  await new Promise(ready => server.listen(0, '127.0.0.1', ready));
  return 'http://127.0.0.1:' + server.address().port + '/';
}

async function command(name, args, cwd) {
  const result = spawnSync(process.env.GOODSENTRY_CLI_PATH, args, {
    cwd, env: cliEnvironment, encoding: 'utf8', timeout: 60000, maxBuffer: 4 * 1024 * 1024,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  const stdout = sanitize(result.stdout || ''), stderr = sanitize(result.stderr || '');
  await writeFile(join(output, name + '.stdout.log'), stdout, { mode: 0o600 });
  await writeFile(join(output, name + '.stderr.log'), stderr, { mode: 0o600 });
  report.cliCommands.push({ name, args, exitCode: result.status, signal: result.signal, stdout, stderr, error: result.error?.message });
  assert.ok(!result.error && result.status === 0, 'Official CLI ' + name + ' failed; inspect its sanitized output logs');
  console.log(JSON.stringify({ cliStage: name, exitCode: result.status }));
  return stdout + '\n' + stderr;
}

async function api(path) {
  const response = await fetch(new URL(path, base), {
    headers: { Accept: 'application/json', Cookie: fixture.cookieHeader, Origin: base.origin },
    signal: AbortSignal.timeout(15000),
  });
  assert.ok(response.status === 200, 'Native read API returned HTTP' + response.status);
  return response.json();
}

async function maps() {
  const result = await api('api/v1/organizations/' + encodeURIComponent(fixture.orgSlug) + '/projects/' + encodeURIComponent(fixture.project.id) + '/sourcemaps');
  return result.data;
}

function matchingException(event, message) {
  const values = Array.isArray(event.exception) ? event.exception : event.exception?.values || [];
  return values.find(value => value.value === message);
}

async function findEvent(eventID, message, project) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) {
    const query = new URLSearchParams({ project: project.id, query: message });
    const issues = await api('api/v1/organizations/' + encodeURIComponent(fixture.orgSlug) + '/issues?' + query);
    for (const issue of issues.data) {
      const detail = await api('api/v1/issues/' + encodeURIComponent(issue.id));
      if (detail.event?.event_id === eventID) return detail;
    }
    await delay(250);
  }
  throw new Error('Browser SDK event was not retrievable through the authenticated native API within 20s');
}

async function capture(stage, scriptURL, expected, options) {
  const settings = options || {};
  const targetProject = settings.targetProject || fixture.project;
  const release = settings.release || '', dist = settings.dist || '';
  const expectMapped = settings.expectMapped !== false, injected = settings.injected !== false;
  const message = 'GoodSentry browser ' + run + ' ' + stage;
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await page.goto(staticOrigin + 'blank.html', { waitUntil: 'domcontentloaded' });
    await page.addScriptTag({ url: staticOrigin + 'vendor/sentry-11.7.0.min.js' });
    await page.addScriptTag({ url: staticOrigin + 'harness.js' });
    await page.addScriptTag({ url: scriptURL });
    const sdkVersion = await page.evaluate(() => globalThis.Sentry?.SDK_VERSION);
    assert.equal(sdkVersion, '11.7.0', 'The page must run the official pinned browser SDK');
    await page.evaluate(config => {
      window.__captureStage({ dsn: config.dsn, release: config.release, dist: config.dist });
      window.__stageMessage = config.message;
    }, { dsn: targetProject.dsn, release, dist, message });
    await page.click('#run');
    const outcome = await page.evaluate(() => window.__lastOutcome || null);
    assert.ok(outcome, 'The authored minified bundle must throw inside the page event handler');
    assert.ok(typeof outcome.eventID === 'string' && /^[0-9a-f]{32}$/.test(outcome.eventID), 'The official browser SDK must produce an event ID');
    assert.equal(await page.evaluate(() => globalThis.Sentry.flush(15000)), true, 'Official browser SDK flush timed out');
    await page.evaluate(() => globalThis.Sentry.close(3000));
    const stackLines = outcome.stack.split('\n').filter(line => line.includes(scriptURL));
    assert.ok(stackLines.length > 0, 'The thrown browser exception must reference the served minified script');
    const position = stackLines[0].match(/:(\d+):(\d+)\)?\s*$/);
    assert.ok(position, 'The real browser exception must carry a minified line and column');
    const rawLocation = { filename: scriptURL, lineno: Number(position[1]), colno: Number(position[2]) };
    const detail = await findEvent(outcome.eventID, message, targetProject);
    const event = detail.event, exception = matchingException(event, message);
    await jsonFile(join(output, stage + '.event.json'), event);
    assert.ok(exception, 'Stored event must contain the real thrown browser exception');
    assert.ok(detail.events.some(item => item.eventID === outcome.eventID), 'Issue history must include the actual browser event');
    assert.equal(String(detail.issue.projectId), String(targetProject.id));
    assert.equal(event.sdk?.version, '11.7.0', 'Stored event must identify the pinned browser SDK version');
    const matchesBuild = frame => String(frame.abs_path || frame.filename || '') === scriptURL;
    const sameRaw = frame => matchesBuild(frame) && frame.lineno === rawLocation.lineno && frame.colno === rawLocation.colno;
    const image = event.debug_meta?.images?.find(item => item.type === 'sourcemap' && item.code_file === scriptURL);
    if (injected) assert.ok(image && normalizedID(image.debug_id) === normalizedID(fileDebugID), 'The CLI-injected script must emit its DebugID image');
    else assert.ok(!image, 'The un-injected legacy script must emit no DebugID image for this file');
    const frames = exception.stacktrace?.frames || [];
    const mapped = frames.find(frame => typeof frame.filename === 'string' && (frame.filename === expected.sourceName || frame.filename.endsWith('/' + expected.sourceName)));
    const raw = exception.raw_stacktrace?.frames?.find(sameRaw);
    const unchanged = frames.find(sameRaw);
    report.captures.push({
      stage, eventID: outcome.eventID, issueId: detail.issue.id, projectId: targetProject.id, expectMapped,
      sdkDebugImage: image || null, expectedRaw: rawLocation, mapped: mapped || null, raw: raw || null,
      unchanged: unchanged || null, release: event.release || null, dist: event.dist || null, stack: stackLines.slice(0, 3),
    });
    if (expectMapped) {
      assert.ok(mapped, 'Actual browser exception must map to the authored original file');
      assert.equal(mapped.function, expected.functionName, 'The issue must show the original function name');
      assert.equal(mapped.lineno, 7, 'Original throw is independently fixed at line 7');
      assert.equal(mapped.colno, 1, 'The explicit mapping targets original column 1');
      assert.equal(mapped.context_line, expected.contextLine);
      assert.deepEqual(mapped.pre_context, expected.preContext);
      assert.deepEqual(mapped.post_context, expected.postContext);
      assert.ok(raw, 'The original minified browser stack must remain in raw_stacktrace');
      assert.equal(raw.lineno, rawLocation.lineno);
      assert.equal(raw.colno, rawLocation.colno);
      assert.ok(!frames.some(frame => matchesBuild(frame) && frame.lineno === rawLocation.lineno), 'The mapped frame replaces the minified location in place');
    } else {
      assert.ok(!mapped, 'A nonmatching project/release/dist must not expose the uploaded original file');
      assert.ok(!exception.raw_stacktrace, 'A rejected artifact selector must not add a mapped raw_stacktrace');
      assert.ok(unchanged, 'The real minified frame must remain present');
      assert.equal(unchanged.filename, scriptURL);
      assert.equal(unchanged.lineno, rawLocation.lineno);
      assert.equal(unchanged.colno, rawLocation.colno);
      assert.ok(!frames.some(frame => frame.context_line === expected.contextLine), 'A rejected selector must not leak original source context');
    }
    assert.equal(event.release || '', release);
    assert.equal(event.dist || '', dist);
    console.log(JSON.stringify({ browserStage: stage, eventID: outcome.eventID, mappedOriginalLine: mapped?.lineno || null, selectorRejected: !expectMapped }));
  } finally { await context.close(); }
}

async function authorSource(directory, sourceName, component, functionName) {
  const lines = [
    "'use strict';",
    "const component = '" + component + "';",
    'function ' + functionName + '(message) {',
    "  const precedingValue = 'independent browser source context';",
    '  const order = 42;',
    '  // Known line immediately before the real browser exception.',
    '  throw new Error(message);',
    '}',
    'window.__runFailure = ' + functionName + ';',
  ];
  await mkdir(directory, { recursive: true });
  await writeFile(join(directory, sourceName), lines.join('\n') + '\n');
  return {
    sourceName, functionName, lines,
    contextLine: lines[6], preContext: lines.slice(1, 6), postContext: lines.slice(7),
    source: lines.join('\n'),
  };
}

async function authorBuild(buildDirectory, filename, expected, shortName) {
  await mkdir(buildDirectory, { recursive: true });
  const build = join(buildDirectory, filename);
  await writeFile(build, 'window.__runFailure=function ' + shortName + '(message){throw new Error(message)};\n//# sourceMappingURL=' + filename + '.map\n');
  // Generated column 0 maps to source 0, original line +6 (line 7), column 0,
  // name 0. That name is what the issue must display instead of the short one.
  await writeFile(build + '.map', JSON.stringify({
    version: 3, file: filename, sources: [expected.sourceName],
    sourcesContent: [expected.source], names: [expected.functionName], mappings: 'AAMAA',
  }));
  return build;
}

async function main() {
  const fixturePath = process.env.GOODSENTRY_ARTIFACT_FIXTURE;
  assert.ok(fixturePath && process.env.GOODSENTRY_CLI_PATH && process.env.GOODSENTRY_BROWSER_BUNDLE, 'Provide the private fixture, pinned CLI and pinned browser bundle paths');
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
  const versionOutput = await command('cli-version', ['--version'], output);
  assert.ok(/^sentry-cli 3\.8\.0\s*$/m.test(versionOutput), 'Use the pinned sentry-cli 3.8.0 executable');
  report.cliVersion = '3.8.0';
  await loadBrowser();
  browser = await chromium.launch({ headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
  report.playwright.browserVersion = browser.version();

  const bundle = await readFile(process.env.GOODSENTRY_BROWSER_BUNDLE);
  const integrity = 'sha384-' + createHash('sha384').update(bundle).digest('base64');
  assert.equal(integrity, PINNED_INTEGRITY, 'Serve the official pinned browser 11.7.0 CDN bundle');

  const work = join(output, 'build-' + run), publicDir = join(work, 'public');
  const distDir = join(publicDir, 'dist'), legacyDir = join(publicDir, 'legacy'), vendorDir = join(publicDir, 'vendor');
  await mkdir(work, { recursive: true }); await mkdir(vendorDir, { recursive: true });
  await writeFile(join(publicDir, 'blank.html'), '<!doctype html><meta charset="utf-8"><title>GoodSentry browser artifact harness</title><body></body>\n');
  await copyFile(process.env.GOODSENTRY_BROWSER_BUNDLE, join(vendorDir, 'sentry-11.7.0.min.js'));
  // The harness is the page bootstrap: it initialises the pinned SDK and runs
  // the real minified entry point from a click listener, never from an
  // evaluated helper frame.
  await writeFile(join(publicDir, 'harness.js'), [
    'var runButton = document.createElement("button");',
    'runButton.id = "run";',
    'runButton.textContent = "Run the minified entry point";',
    'document.body.appendChild(runButton);',
    'window.__captureStage = function (config) {',
    '  globalThis.Sentry.init({',
    '    dsn: config.dsn, release: config.release, dist: config.dist,',
    "    environment: 'browser-artifact', debug: false, sampleRate: 1,",
    '    tracesSampleRate: 0, sendDefaultPii: false, maxBreadcrumbs: 0, enableLogs: false,',
    '  });',
    '  runButton.addEventListener("click", function () {',
    '    try { window.__runFailure(window.__stageMessage); }',
    '    catch (error) { window.__lastOutcome = { eventID: globalThis.Sentry.captureException(error), stack: String(error.stack || "") }; }',
    '  });',
    '};',
    '',
  ].join('\n'));

  const sourceDir = join(work, 'source');
  const expected = await authorSource(sourceDir, 'original-' + run + '.js', 'browser-artifact-proof', 'browserFailure');
  const legacyExpected = await authorSource(sourceDir, 'original-legacy-' + run + '.js', 'browser-artifact-legacy', 'legacyFailure');
  const build = await authorBuild(distDir, 'app.js', expected, 'b');
  const legacyBuild = await authorBuild(legacyDir, 'app-legacy.js', legacyExpected, 'l');
  staticOrigin = await serve(publicDir);
  const scriptURL = staticOrigin + 'dist/app.js', legacyURL = staticOrigin + 'legacy/app-legacy.js';
  report.harness = {
    staticOrigin, scriptURL, legacyURL,
    bundle: { bytes: bundle.length, sha384: integrity, servedAt: staticOrigin + 'vendor/sentry-11.7.0.min.js' },
    debugIdProvenance: 'sentry-cli 3.8.0 injection prefixes the official runtime shim that publishes _sentryDebugIds for the served script; the page adds no identity of its own.',
  };

  await command('inject', ['sourcemaps', 'inject', distDir], work);
  const injectedMap = JSON.parse(await readFile(build + '.map', 'utf8'));
  const fileID = injectedMap.debug_id || injectedMap.debugId;
  assert.ok(validID(fileID), 'Official injection must add a valid source-file DebugID');
  const scriptID = (await readFile(build, 'utf8')).match(/\/\/#\s*debugId\s*=\s*([0-9a-f-]+)/i)?.[1];
  assert.ok(validID(scriptID) && normalizedID(scriptID) === normalizedID(fileID), 'Injected script and map must share the file DebugID');
  assert.equal((await readFile(legacyBuild, 'utf8')).includes('debugId'), false, 'The legacy control must stay un-injected');
  report.fileDebugId = fileID; report.expected = expected; report.legacyExpected = legacyExpected;
  fileDebugID = fileID;
  report.releaseVariant = { version: 'browser-' + run, dist: 'web' };

  async function upload(name, extra) {
    const log = await command(name, ['sourcemaps', 'upload', '--wait', ...extra, distDir], work);
    const bundleID = log.match(/Bundle ID:\s*([0-9a-f-]+)/i)?.[1];
    assert.ok(validID(bundleID) && normalizedID(bundleID) !== normalizedID(fileID), 'Actual bundle ID must differ from the injected source-file DebugID');
    report.cliCommands.at(-1).bundleId = bundleID;
  }
  const distPrefix = staticOrigin + 'dist';
  await upload('upload-debug-only', ['--url-prefix', distPrefix]);
  const debugOnly = (await maps()).filter(map => normalizedID(map.debugId) === normalizedID(fileID));
  assert.ok(debugOnly.length > 0, 'Official CLI upload must materialize this file DebugID');
  await upload('upload-duplicate', ['--url-prefix', distPrefix]);
  const duplicate = (await maps()).filter(map => normalizedID(map.debugId) === normalizedID(fileID));
  assert.deepEqual(duplicate.map(map => map.id).sort(), debugOnly.map(map => map.id).sort(), 'Actual duplicate CLI upload must not duplicate native artifact records');
  report.mapMetadata = { debugOnly, duplicate };

  const release = report.releaseVariant.version;
  await upload('upload-release-dist', ['--url-prefix', distPrefix, '--release', release, '--dist', 'web']);
  report.mapMetadata.releaseDist = (await maps()).filter(map => normalizedID(map.debugId) === normalizedID(fileID));
  assert.ok(report.mapMetadata.releaseDist.some(map => map.release === release && map.dist === 'web'), 'The second upload must store the requested release/dist selector');
  assert.ok(report.mapMetadata.releaseDist.some(map => map.name === staticOrigin + 'dist/app.js.map'), 'Uploaded map must carry the served script URL');

  await capture('debug-only', scriptURL, expected, {});
  await capture('release-dist', scriptURL, expected, { release, dist: 'web' });
  await capture('wrong-project', scriptURL, expected, { targetProject: fixture.otherProject, release, dist: 'web', expectMapped: false });

  const legacyRelease = 'browser-legacy-' + run, legacyDist = 'legacy-web';
  await command('upload-legacy', ['sourcemaps', 'upload', '--wait', '--release', legacyRelease, '--dist', legacyDist, '--url-prefix', staticOrigin + 'legacy', legacyDir], work);
  report.legacyMetadata = (await maps()).filter(map => map.release === legacyRelease && map.dist === legacyDist);
  assert.ok(report.legacyMetadata.some(map => map.name === legacyURL + '.map'), 'Legacy upload must store the exact served map URL');
  await capture('legacy-release-dist', legacyURL, legacyExpected, { release: legacyRelease, dist: legacyDist, injected: false });
  await capture('legacy-wrong-version', legacyURL, legacyExpected, { release: legacyRelease + '-wrong', dist: legacyDist, expectMapped: false, injected: false });
  await capture('legacy-wrong-dist', legacyURL, legacyExpected, { release: legacyRelease, dist: legacyDist + '-wrong', expectMapped: false, injected: false });
  report.releaseVariants = { main: { release, dist: 'web' }, legacy: { release: legacyRelease, dist: legacyDist } };
  report.ok = true;
}

try { await main(); }
catch (error) { report.failure = sanitize(error.stack || error.message); console.error(report.failure); process.exitCode = 1; }
finally {
  await browser?.close().catch(() => {});
  await new Promise(done => server ? server.close(done) : done());
  await mkdir(output, { recursive: true, mode: 0o700 });
  await jsonFile(join(output, 'report.json'), report);
  console.log(JSON.stringify({ ok: report.ok, events: report.captures.map(item => ({ stage: item.stage, eventID: item.eventID })), report: join(output, 'report.json') }));
}
