// Browser owner: conditional-mute forms, current state, drafts and navigation.
// Wire fixtures are independently authored, not an SDK compatibility claim.
// Request routing only delays/changes a request; every response is real HTTP.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_IGNORE_FIXTURE || process.env.GOODSENTRY_ISSUES_FIXTURE;
assert.ok(fixturePath, 'Provide the private isolated ignore fixture');
const metadata = await stat(fixturePath);
assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const base = new URL(fixture.baseURL); if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/ignore-browser');
await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage(); page.setDefaultTimeout(20000);
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [], events = [];
const run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const message = `Mute browser ${run} <img src=x onerror="window.__muteUnsafe=1">`;
const fingerprint = [`mute-browser-${run}`];
let failure, issueID, csrf, expected400 = false, pendingRelease;
const endpoint = () => new URL(`api/v1/issues/${encodeURIComponent(issueID)}`, base).href;
const mute = () => page.getByRole('region', { name: 'Mute notifications', exact: true });
const minutes = () => mute().getByLabel('Minutes (optional)', { exact: true });
const count = () => mute().getByLabel('Additional errors (optional)', { exact: true });
const comments = () => page.getByRole('region', { name: 'Comments', exact: true });
const activity = () => page.getByRole('region', { name: 'Activity', exact: true });
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', value => { if (['error', 'warning'].includes(value.type())) consoleProblems.push({ text: value.text(), expected: expected400 && /400/.test(value.text()) }); });
page.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), status: response.status(), expected: expected400 && response.status() === 400 && response.request().method() === 'PATCH' && response.url() === endpoint() }); });
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function until(read, accepts, message) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) { const value = await read(); if (accepts(value)) return value; await new Promise(done => setTimeout(done, 100)); }
  assert.fail(message);
}
async function api(suffix = '', method = 'GET', data) {
  const response = await context.request.fetch(endpoint() + suffix, { method, headers: { Origin: base.origin, 'X-CSRF-Token': csrf }, ...(data === undefined ? {} : { data }) });
  assert.ok(response.ok(), `Actual ${method} issue ${suffix} succeeds (${response.status()})`);
  return response.status() === 204 ? null : response.json();
}
async function captureError() {
  const eventID = randomBytes(16).toString('hex');
  const event = { event_id: eventID, platform: 'javascript', level: 'error', timestamp: Date.now() / 1000, message, fingerprint,
    exception: { values: [{ type: 'MuteBrowserError', value: message, stacktrace: { frames: [{ filename: 'src/mute-browser.js', function: 'captureProof', lineno: 7, colno: 1, in_app: true }] } }] } };
  const payload = JSON.stringify(event), dsn = new URL(fixture.project.dsn);
  const envelope = `${JSON.stringify({ dsn: fixture.project.dsn, event_id: eventID })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const response = await context.request.post(new URL(`api/${encodeURIComponent(fixture.project.id)}/envelope/`, base).href, { headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-ignore-browser/1.0` }, data: envelope });
  assert.equal(response.status(), 200, 'The actual capture boundary acknowledges the owned wire fixture'); events.push(eventID); return eventID;
}
function patchResponse() { return page.waitForResponse(response => response.url() === endpoint() && response.request().method() === 'PATCH'); }
async function apply() {
  const pending = patchResponse(); await mute().getByRole('button', { name: 'Apply mute', exact: true }).click();
  const response = await pending; assert.equal(response.status(), 200); await visible(page.getByText('Mute updated.', { exact: true })); return response.json();
}
async function inputs(expectedMinutes, expectedCount) { assert.equal(await minutes().inputValue(), expectedMinutes); assert.equal(await count().inputValue(), expectedCount); }
async function capture(name, anchor = mute()) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await anchor.evaluate(element => element.scrollIntoView({ block: 'start' }));
    await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, width, `${name} fits${width}px`);
    const path = resolve(output, `${name}-${width}.png`); await page.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path);
  }
}
async function heldPatch(invalid = false) {
  let signalEntered; const entered = new Promise(done => { signalEntered = done; });
  const release = new Promise(done => { pendingRelease = done; });
  const handler = async route => {
    if (route.request().method() !== 'PATCH') { await route.continue(); return; }
    signalEntered(); await release;
    await route.continue(invalid ? { postData: JSON.stringify({ status: 'ignored', ignore: { count: 0 } }) } : {});
  };
  await page.route(endpoint(), handler);
  const response = patchResponse(); await mute().getByRole('button', { name: 'Apply mute', exact: true }).click();
  await Promise.race([entered, new Promise((_, reject) => setTimeout(() => reject(new Error('The actual mute request was not held')), 15000))]);
  return { response, release: async () => { pendingRelease(); pendingRelease = null; const result = await response; await page.unroute(endpoint(), handler); return result; } };
}

try {
  const login = await context.request.post(new URL('api/v1/login', base).href, { headers: { Origin: base.origin }, data: { email: fixture.ownerEmail, password: fixture.ownerPassword } });
  assert.equal(login.status(), 200); csrf = (await login.json()).csrfToken;
  await captureError();
  const listURL = new URL(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues`, base);
  listURL.search = new URLSearchParams({ project: fixture.project.id, status: 'all', query: JSON.stringify(`Mute browser ${run}`) }).toString();
  const rows = await until(async () => { const response = await context.request.get(listURL.href); assert.equal(response.status(), 200); return response.json(); }, result => result.total === 1, 'The owned capture creates one searchable issue');
  issueID = String(rows.data[0].id);
  fixture.ignore = { ...fixture.ignore, issueID, message, fingerprint, seedEventID: events[0] };
  await writeFile(fixturePath, JSON.stringify(fixture, null, 2), { mode: 0o600 });
  const url = new URL(base); url.search = new URLSearchParams({ org: fixture.orgSlug, view: 'issues', issue: issueID, project: fixture.project.id, status: 'all' }).toString();
  await page.goto(url.href, { waitUntil: 'networkidle' }); await visible(mute());
  await visible(mute().getByText('No notification mute is active.', { exact: true }));
  assert.equal(await page.locator('#main img, #main script').count(), 0, 'Capture text creates no executable HTML');
  await mute().getByRole('button', { name: 'Apply mute', exact: true }).click();
  await visible(mute().getByRole('alert').filter({ hasText: 'Enter minutes, an additional error count, or both.' }));
  await count().fill('2147483648'); await mute().getByRole('button', { name: 'Apply mute', exact: true }).click();
  await visible(mute().getByRole('alert').filter({ hasText: 'Use whole numbers from 1 to 2,147,483,647' })); await inputs('', '2147483648');
  await capture('client-validation-draft');
  checks.push('Empty/overflow conditions show editable errors, preserve input and safely render the owned capture');

  const commentDraft = `Mute workflow unsent draft ${run}`; await comments().getByLabel('Add a comment', { exact: true }).fill(commentDraft);
  await minutes().fill('5'); await count().fill('3'); const combined = await apply();
  assert.equal(combined.status, 'ignored'); assert.equal(combined.ignore.minutes, 5); assert.equal(combined.ignore.count, 3);
  await visible(mute().getByText('Muted for 5 minutes or 3 additional errors.', { exact: true }));
  await visible(mute().getByText('3 additional errors remaining.', { exact: true }));
  assert.equal(await mute().locator('time').getAttribute('datetime'), combined.ignore.until);
  await visible(mute().getByText(`UTC: ${combined.ignore.until}`, { exact: true }));
  await capture('combined-current-deadline');
  const repeated = await apply(); assert.equal(repeated.ignore.until, combined.ignore.until, 'Retrying unchanged fields preserves the original deadline');
  await minutes().fill('6'); await count().fill('9');
  const assignment = page.getByRole('region', { name: 'Assignment', exact: true });
  await assignment.getByLabel('Search people and teams', { exact: true }).fill(fixture.assigneeUser.email);
  await assignment.getByRole('button', { name: 'Search assignees', exact: true }).click();
  const assign = await visible(assignment.getByRole('button', { name: /^Assign to person / }));
  const assigning = patchResponse(); await assign.click(); assert.equal((await assigning).status(), 200);
  await visible(page.getByText('Assignment updated.', { exact: true })); await inputs('6', '9');
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  assert.equal((await api()).issue.ignore.until, combined.ignore.until);
  checks.push('Combined OR conditions show actual local/UTC deadline and remaining; retry and assignment preserve current policy, mute drafts and comment draft');

  expected400 = true; const rejected = await heldPatch(true);
  await minutes().fill('7'); await count().fill('11');
  assert.equal((await rejected.release()).status(), 400);
  await visible(mute().getByRole('alert').filter({ hasText: /\S/ })); await inputs('7', '11');
  assert.equal(await minutes().isEnabled(), true); assert.equal(await count().isEnabled(), true);
  assert.equal((await api()).issue.ignore.until, combined.ignore.until);
  await capture('real400-newer-draft'); expected400 = false;
  checks.push('An actual server400 from one deliberately invalid outbound PATCH retains the newer editable draft without changing the saved condition');

  await minutes().fill(''); await count().fill('2'); const held = await heldPatch();
  await count().fill('99'); assert.equal((await held.release()).status(), 200);
  await visible(mute().getByText('Muted for 2 additional errors.', { exact: true })); await inputs('', '99');
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  await capture('successful-pending-draft');
  await captureError(); await until(() => api(), detail => detail.issue.ignore?.remaining === 1, 'A new accepted capture advances remaining');
  await page.getByRole('button', { name: 'Refresh issue', exact: true }).click();
  await visible(mute().getByText('1 additional error remaining.', { exact: true })); await capture('remaining-one');
  const unmutingEvent = await captureError(); await until(() => api(), detail => detail.issue.status === 'unresolved' && detail.issue.ignore === null, 'The real final capture ends the mute');
  await page.getByRole('button', { name: 'Refresh issue', exact: true }).click();
  await visible(mute().getByText('No notification mute is active.', { exact: true }));
  const ended = activity().getByRole('listitem').filter({ hasText: 'Mute ended' }).filter({ hasText: unmutingEvent });
  await visible(ended.getByText('System', { exact: true }));
  await visible(ended.getByText(/Additional error count reached · Previous condition: 2 additional errors/));
  assert.equal(await page.getByText('Regressed', { exact: true }).count(), 0);
  await capture('automatic-count-activity', activity());
  checks.push('Newer input survives an actual successful held request; real captures continue, remaining updates and automatic System activity explains count unmute without a regression badge');

  await minutes().fill('1'); await count().fill(''); await apply();
  const clearing = patchResponse(); await mute().getByRole('button', { name: 'Mute indefinitely', exact: true }).click(); assert.equal((await clearing).status(), 200);
  await visible(mute().getByText('Muted indefinitely.', { exact: true }));
  const indefinite = (await api()).issue; assert.equal(indefinite.status, 'ignored'); assert.equal(indefinite.ignore, null);
  assert.equal(await mute().getByRole('button', { name: 'Mute indefinitely', exact: true }).isDisabled(), true);
  await capture('indefinite-mute'); await page.reload({ waitUntil: 'networkidle' });
  await visible(mute().getByText('Muted indefinitely.', { exact: true })); await inputs('', '');
  checks.push('Clearing a saved condition sends explicit null, keeps Ignore indefinitely, disables redundant clear and persists on reload');

  const navigationDraft = `Draft across mute navigation ${run}`; await comments().getByLabel('Add a comment', { exact: true }).fill(navigationDraft);
  await count().fill('4');
  const submit = await mute().getByRole('button', { name: 'Apply mute', exact: true }).elementHandle();
  const issues = await page.getByRole('navigation').getByRole('link', { name: 'Issues', exact: true }).elementHandle();
  const navigating = patchResponse(); await page.evaluate(({ submit, issues }) => { submit.click(); issues.click(); }, { submit, issues }); assert.equal((await navigating).status(), 200);
  await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true })); await page.waitForLoadState('networkidle');
  assert.equal(new URL(page.url()).searchParams.has('issue'), false); assert.equal(await mute().count(), 0);
  await page.goBack({ waitUntil: 'networkidle' }); await visible(mute().getByText('Muted for 4 additional errors.', { exact: true }));
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), navigationDraft);
  await capture('back-after-pending-navigation');
  checks.push('A late mute response leaves sidebar navigation intact; browser Back restores actual saved policy and comment draft');
  if (fixture.ignore.durationIssueID) {
    const durationID = String(fixture.ignore.durationIssueID);
    const durationResponse = await context.request.get(new URL(`api/v1/issues/${encodeURIComponent(durationID)}`, base).href);
    assert.equal(durationResponse.status(), 200);
    const duration = await durationResponse.json(); assert.equal(duration.issue.status, 'unresolved'); assert.equal(duration.issue.ignore, null);
    const durationURL = new URL(base); durationURL.search = new URLSearchParams({ org: fixture.orgSlug, view: 'issues', issue: durationID, project: fixture.project.id, status: 'all' }).toString();
    await page.goto(durationURL.href, { waitUntil: 'networkidle' });
    await visible(mute().getByText('No notification mute is active.', { exact: true }));
    const ended = activity().getByRole('listitem').filter({ hasText: 'Mute ended' }).filter({ hasText: 'Duration reached' }).first();
    await visible(ended.getByText('System', { exact: true }));
    assert.equal((await ended.innerText()).includes('New event'), false, 'Worker expiration does not invent a triggering event');
    await capture('automatic-duration-activity', activity());
    checks.push('The independently supplied real worker-expired issue is read-only; System activity identifies duration with no invented event and the UI shows no active mute');
  }
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.equal(badResponses.filter(value => value.expected).length, 1); assert.deepEqual(badResponses.filter(value => !value.expected), []);
  assert.deepEqual(consoleProblems.filter(value => !value.expected), []);
} catch (error) {
  failure = error; pendingRelease?.();
  const path = resolve(output, 'failure.png'); await page.screenshot({ path }).catch(() => {}); screenshots.push(path);
} finally {
  if (issueID && csrf) await api('', 'PATCH', { status: 'unresolved' }).catch(() => {});
  await context.close(); await browser.close();
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ passed: !failure, fixtureType: 'independently authored wire captures; official SDK acceptance is separate', issueID, durationIssueID: fixture.ignore?.durationIssueID, events, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure?.message }, null, 2));
}
if (failure) throw failure;
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
