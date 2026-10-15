// Test-audit gate: a keyboard-selected writer credential controls actual SDK
// issues and its saved policies appear in native UI without losing drafts.
// Existing read-token proof owns defaults; providers/HTTP own authorization and
// parser matrices. This browser owns clipboard, grants and the consumer lifecycle,
// using real requests only, with no synthetic telemetry or production test seam.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { isAbsolute, resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_ISSUE_WRITE_FIXTURE, sdkReportPath = process.env.GOODSENTRY_ISSUE_WRITE_SDK_REPORT;
assert.ok(fixturePath && isAbsolute(fixturePath) && sdkReportPath && isAbsolute(sdkReportPath), 'Provide absolute private fixture and successful SDK report paths');
const metadata = await stat(fixturePath); assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8')), sdk = JSON.parse(await readFile(sdkReportPath, 'utf8'));
assert.equal(sdk.passed, true); assert.equal(sdk.orgSlug, fixture.orgSlug);
assert.ok(sdk.projectID && sdk.projectSlug && sdk.projectName && sdk.issueID && sdk.releaseVersion && Array.isArray(sdk.eventIDs) && sdk.eventIDs.length, 'SDK report supplies its own project, issue, captures and associated release');
const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
assert.equal(base.protocol, 'http:'); assert.equal(base.port, '18084');
assert.ok(['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'Use the isolated localhost image runtime, including the secure clipboard context');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/issue-write-browser'); await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true }), context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: base.origin });
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [], requests = [], expected = new Set(), observed = new Set();
const secrets = [fixture.cookieHeader, fixture.csrfToken].filter(Boolean);
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const url = path => new URL(path, base).href, run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const tokenPath = `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/api-tokens`, nativeIssuePath = `api/v1/issues/${encodeURIComponent(sdk.issueID)}`;
const globalIssuePath = `api/0/issues/${encodeURIComponent(sdk.issueID)}/`, orgIssuePath = `api/0/organizations/${encodeURIComponent(fixture.orgSlug)}/issues/${encodeURIComponent(sdk.issueID)}/`;
let page, me, issuedID, issuedSecret, revoked = false, modified = false, failure;
const region = name => page.getByRole('region', { name, exact: true });
const comments = () => region('Comments'), mute = () => region('Mute notifications'), resolution = () => region('Resolution');
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function native(method, path, data) {
  return context.request.fetch(url(path), { method, headers: { Origin: base.origin, 'X-CSRF-Token': me.csrfToken }, ...(data === undefined ? {} : { data }) });
}
async function consume(method, path, body, status = 200, credentials = 'same-origin') {
  const key = `${method} ${url(path)} ${status}`; if (status >= 400) expected.add(key);
  const response = await page.evaluate(async ({ method, target, secret, body, credentials }) => {
    const response = await fetch(target, { method, credentials, headers: { Authorization: `Bearer ${secret}`, Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
    return { status: response.status, body: await response.json() };
  }, { method, target: url(path), secret: issuedSecret, body, credentials });
  requests.push({ method, path, status: response.status }); assert.equal(response.status, status); return response.body;
}
async function capture(name, anchor) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    if (anchor) await anchor().evaluate(element => element.scrollIntoView({ block: 'start' })); else await page.evaluate(() => scrollTo(0, 0));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, `${name} fits ${width}px`);
    const file = resolve(output, `${name}-${width}.png`); await page.screenshot({ path: file, fullPage: false, animations: 'disabled', mask: [page.getByLabel('API token secret', { exact: true })] }); screenshots.push(file);
  }
}
function destination(view) {
  const target = new URL(base); target.search = new URLSearchParams({ org: fixture.orgSlug, view, ...(view === 'issues' ? { issue: sdk.issueID, project: sdk.projectID, status: 'all', environment: 'writer-browser-kept-filter' } : {}) }).toString(); return target.href;
}
const nativeResponse = () => page.waitForResponse(response => response.request().method() === 'GET' && response.url() === url(nativeIssuePath));
async function refresh() {
  const pending = nativeResponse(); await page.getByRole('button', { name: 'Refresh issue', exact: true }).click();
  const response = await pending; assert.equal(response.status(), 200); const detail = await response.json();
  await visible(page.locator('.detail-meta').getByText(detail.issue.status, { exact: true }));
  return detail;
}
const drafts = { comment: `Unsent writer comment ${run}`, minutes: '13', count: '9', release: `unsent / release ${run}`, assignee: `unsent assignee ${run}` };
async function checkDrafts() {
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), drafts.comment);
  assert.equal(await mute().getByLabel('Minutes (optional)', { exact: true }).inputValue(), drafts.minutes);
  assert.equal(await mute().getByLabel('Additional errors (optional)', { exact: true }).inputValue(), drafts.count);
  assert.equal(await resolution().getByLabel('Chosen release version', { exact: true }).inputValue(), drafts.release);
  assert.equal(await region('Assignment').getByLabel('Search people and teams', { exact: true }).inputValue(), drafts.assignee);
  assert.equal(new URL(page.url()).searchParams.get('environment'), 'writer-browser-kept-filter');
}
try {
  assert.ok(fixture.cookieHeader && fixture.csrfToken);
  await context.addCookies(fixture.cookieHeader.split(/;\s*/).filter(Boolean).map(pair => { const split = pair.indexOf('='); assert.ok(split > 0); return { name: pair.slice(0, split), value: pair.slice(split + 1), url: base.href, httpOnly: true, sameSite: 'Lax' }; }));
  const session = await context.request.get(url('api/v1/me')); assert.equal(session.status(), 200); me = await session.json(); secrets.push(me.csrfToken);
  assert.ok(me.user.id && me.organizations.some(value => value.slug === fixture.orgSlug));
  page = await context.newPage(); page.setDefaultTimeout(20000);
  page.on('pageerror', error => pageErrors.push(safe(error.message)));
  page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  page.on('response', response => { if (response.status() >= 400) { const key = `${response.request().method()} ${response.url()} ${response.status()}`; badResponses.push({ url: response.url(), status: response.status(), expected: expected.has(key) }); if (expected.has(key)) observed.add(key); } });
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: safe(message.text()), url: message.location().url }); });
  await page.goto(destination('api-tokens'), { waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'API tokens', exact: true, level: 1 }));
  const form = page.getByRole('form', { name: 'Create API token', exact: true });
  assert.equal(await form.getByLabel('event:write', { exact: true }).isChecked(), false);
  await capture('writer-opt-in');
  const tokenName = `UI issue writer ${run}`; await form.getByLabel('Token name', { exact: true }).fill(tokenName); await form.getByLabel('Lifetime (days)', { exact: true }).fill('1');
  await form.getByLabel('org:read', { exact: true }).uncheck(); await form.getByLabel('project:read', { exact: true }).uncheck();
  await form.getByLabel('event:write', { exact: true }).focus(); await page.keyboard.press('Space'); assert.equal(await form.getByLabel('event:write', { exact: true }).isChecked(), true);
  await form.getByLabel('Project access', { exact: true }).selectOption('selected'); await form.getByLabel(sdk.projectName, { exact: true }).check();
  await capture('writer-explicit-project');
  await form.getByLabel('event:write', { exact: true }).focus(); await page.keyboard.press('Space');
  await form.getByRole('button', { name: 'Create token', exact: true }).click(); await visible(form.getByRole('alert').getByText('Choose at least one scope.', { exact: true }));
  await form.getByLabel('event:write', { exact: true }).focus(); await page.keyboard.press('Space');
  const creating = page.waitForResponse(response => response.request().method() === 'POST' && response.url() === url(tokenPath)); await form.getByRole('button', { name: 'Create token', exact: true }).click();
  const created = await creating; assert.equal(created.status(), 201); const token = await created.json(); issuedID = token.id; issuedSecret = token.token; secrets.push(issuedSecret);
  assert.deepEqual(token.scopes, ['event:write']); assert.equal(token.allProjects, false); assert.deepEqual(token.projects.map(project => project.id), [sdk.projectID]);
  const reveal = await visible(region('New API token')); assert.equal(await reveal.getByLabel('API token secret', { exact: true }).innerText(), issuedSecret);
  await reveal.getByRole('button', { name: 'Copy token', exact: true }).click(); await visible(page.getByText('API token copied.', { exact: true }));
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), issuedSecret); await capture('writer-secret-copied');
  await reveal.getByRole('button', { name: 'Dismiss token', exact: true }).click(); assert.equal(await region('New API token').count(), 0);
  assert.equal(await page.evaluate(secret => document.body.textContent.includes(secret) || location.href.includes(secret) || JSON.stringify(history.state)?.includes(secret), issuedSecret), false);
  checks.push('Keyboard choice emits an event:write-only credential restricted to the SDK project; neutral validation, real clipboard copying and one-time reveal work at320/768/1440');
  const compatible = await consume('GET', globalIssuePath); assert.equal(compatible.id, sdk.issueID);
  const events = await consume('GET', `${orgIssuePath}events/`); assert.deepEqual(new Set(events.map(event => event.eventID)), new Set(sdk.eventIDs));
  await consume('PUT', `api/0/organizations/${encodeURIComponent(fixture.orgSlug)}/releases/${encodeURIComponent(sdk.releaseVersion)}/`, {}, 403);
  await consume('PATCH', nativeIssuePath, {}, 401, 'omit');
  const opening = nativeResponse(); await page.goto(destination('issues'), { waitUntil: 'networkidle' }); const baselineResponse = await opening; assert.equal(baselineResponse.status(), 200); const baseline = await baselineResponse.json();
  assert.equal(baseline.issue.projectId, sdk.projectID); assert.equal(baseline.issue.status, 'unresolved'); assert.equal(baseline.issue.assignedTo, null); assert.equal(baseline.issue.ignore, null); assert.equal(baseline.issue.resolution, null);
  await visible(comments()); await comments().getByLabel('Add a comment', { exact: true }).fill(drafts.comment);
  await mute().getByLabel('Minutes (optional)', { exact: true }).fill(drafts.minutes); await mute().getByLabel('Additional errors (optional)', { exact: true }).fill(drafts.count);
  await resolution().getByLabel('Chosen release version', { exact: true }).fill(drafts.release); await region('Assignment').getByLabel('Search people and teams', { exact: true }).fill(drafts.assignee);
  modified = true;
  const ignored = await consume('PUT', orgIssuePath, { status: 'ignored', assignedTo: `user:${me.user.id}`, statusDetails: { ignoreCount: 7 } }); assert.equal(ignored.status, 'ignored');
  const muted = await refresh(); assert.equal(muted.issue.status, 'ignored'); assert.equal(muted.issue.ignore.count, 7); assert.equal(muted.issue.ignore.remaining, 7); assert.equal(muted.issue.assignedTo.type, 'user'); assert.equal(muted.issue.assignedTo.id, me.user.id);
  await visible(mute().getByText('Muted for 7 additional errors.', { exact: true })); await visible(mute().getByText('7 additional errors remaining.', { exact: true }));
  assert.equal(await region('Assignment').locator('form strong').innerText(), muted.issue.assignedTo.name || muted.issue.assignedTo.email || 'Member'); await checkDrafts(); await capture('writer-mute-native', mute);
  const resolved = await consume('PUT', globalIssuePath, { status: 'resolved', statusDetails: { inRelease: sdk.releaseVersion } }); assert.equal(resolved.status, 'resolved');
  const saved = await refresh(); assert.equal(saved.issue.ignore, null); assert.equal(saved.issue.resolution.mode, 'release'); assert.equal(saved.issue.resolution.release.version, sdk.releaseVersion); assert.equal(saved.issue.assignedTo.id, me.user.id);
  await visible(resolution().getByText('Resolved in a chosen release.', { exact: true })); assert.equal(await resolution().locator('code').first().innerText(), sdk.releaseVersion); await checkDrafts(); await capture('writer-release-native', resolution);
  checks.push('The real writer reads SDK issue events; release mutation returns403 and token-only native mutation without cookies returns401. Its assigned mute and chosen-release policies appear after native refresh without losing drafts or URL filters');
  const reopening = page.waitForResponse(response => response.request().method() === 'PATCH' && response.url() === url(nativeIssuePath)); await page.getByRole('button', { name: 'Reopen', exact: true }).click(); assert.equal((await reopening).status(), 200);
  await visible(page.getByRole('button', { name: 'Resolve', exact: true })); await visible(resolution().getByText('This issue is not resolved.', { exact: true })); await checkDrafts();
  await consume('PUT', orgIssuePath, { assignedTo: null }); const restored = await refresh(); assert.equal(restored.issue.status, 'unresolved'); assert.equal(restored.issue.assignedTo, null); assert.equal(restored.issue.ignore, null); assert.equal(restored.issue.resolution, null); modified = false; await checkDrafts();
  checks.push('Native Reopen handles the API-created release policy, and writer unassignment restores the SDK issue baseline while every unsent draft survives');
  await page.goto(destination('api-tokens'), { waitUntil: 'networkidle' }); const row = region('API token metadata').getByRole('row').filter({ hasText: tokenName }); await visible(row);
  await row.getByRole('button', { name: 'Revoke', exact: true }).click(); const revoking = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url() === url(`${tokenPath}/${issuedID}`));
  await row.getByRole('button', { name: 'Confirm revoke', exact: true }).click(); assert.equal((await revoking).status(), 204); revoked = true; await visible(row.getByText('Revoked', { exact: true }));
  await consume('PUT', globalIssuePath, {}, 401); await consume('GET', globalIssuePath, undefined, 401);
  await page.reload({ waitUntil: 'networkidle' }); await visible(region('API token metadata').getByRole('row').filter({ hasText: tokenName }).getByText('Revoked', { exact: true })); assert.equal(await region('New API token').count(), 0); await capture('writer-revoked');
  checks.push('Persistent visible revocation denies both compatible reads and writes despite the live session cookie');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []); assert.deepEqual(badResponses.filter(response => !response.expected), []); assert.deepEqual(observed, expected);
  assert.deepEqual(consoleProblems.filter(value => ![...expected].some(key => key.includes(` ${value.url} `) && value.text.includes(key.split(' ').at(-1)))), []);
} catch (error) { failure = error; console.error(safe(error.stack || error)); }
finally {
  if (modified) try { const response = await native('PATCH', nativeIssuePath, { status: 'unresolved', assignedTo: null }); assert.equal(response.status(), 200); } catch (error) { failure ||= error; }
  if (issuedID && !revoked) try { const response = await native('DELETE', `${tokenPath}/${issuedID}`); assert.equal(response.status(), 204); } catch (error) { failure ||= error; }
  await writeFile(resolve(output, 'report.json'), `${JSON.stringify({ passed: !failure, projectID: sdk.projectID, issueID: sdk.issueID, eventIDs: sdk.eventIDs, checks, screenshots, dimensions, requests, pageErrors, requestFailures, badResponses, consoleProblems, expectedResponses: [...expected], observedExpectedResponses: [...observed], failure: failure && safe(failure.message) }, null, 2)}\n`);
  await browser.close();
}
if (failure) throw new Error(safe(failure.message));
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
