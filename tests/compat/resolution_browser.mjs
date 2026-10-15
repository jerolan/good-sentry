// Browser owner: resolution selection, exact identities, lifecycle and drafts.
// Independent wire fixtures exercise the real API; SDK compatibility is separate.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_RESOLUTION_FIXTURE || process.env.GOODSENTRY_ISSUES_FIXTURE;
assert.ok(fixturePath, 'Provide the private isolated resolution fixture');
const metadata = await stat(fixturePath); assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const base = new URL(fixture.baseURL); if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/resolution-browser'); await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true }); const contexts = [];
const run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const family = `browser-${run}`, releaseVersion = value => `${family}@${value}`;
const fingerprint = [`resolution-browser-${run}`], unsafe = '<img src=x onerror="window.__resolutionUnsafe=1">';
const message = `Resolution browser ${run} ${unsafe}`;
const prefix = ` Release / ${run} ${unsafe} `, exactVersion = prefix + '🌿'.repeat(254 - Array.from(prefix).length) + ' ';
assert.equal(Array.from(exactVersion).length, 255);
const checks = [], screenshots = [], dimensions = [], events = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [];
let owner, member, page, project, otherProject, issueID, failure, expected400 = false, releaseHeld;
const issuePath = () => `api/v1/issues/${encodeURIComponent(issueID)}`;
const issueURL = () => new URL(issuePath(), base).href;
const releasePath = version => `api/0/organizations/${encodeURIComponent(fixture.orgSlug)}/releases/${version === undefined ? '' : `${encodeURIComponent(version)}/`}`;
const resolution = (tab = page) => tab.getByRole('region', { name: 'Resolution', exact: true });
const chosen = (tab = page) => resolution(tab).getByLabel('Chosen release version', { exact: true });
const candidates = () => resolution().getByRole('list', { name: 'Project release candidates', exact: true }).getByRole('listitem');
const comments = () => page.getByRole('region', { name: 'Comments', exact: true });
const mute = () => page.getByRole('region', { name: 'Mute notifications', exact: true });
const activity = () => page.getByRole('region', { name: 'Activity', exact: true });
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function until(read, accepts, message) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) { const value = await read(); if (accepts(value)) return value; await new Promise(done => setTimeout(done, 100)); }
  assert.fail(message);
}
async function session(email, password, label) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' }); contexts.push(context);
  const login = await context.request.post(new URL('api/v1/login', base).href, { headers: { Origin: base.origin }, data: { email, password } }); assert.equal(login.status(), 200);
  const me = await login.json(), tab = await context.newPage(); tab.setDefaultTimeout(20000);
  tab.on('pageerror', error => pageErrors.push({ label, text: error.message }));
  tab.on('requestfailed', request => requestFailures.push({ label, url: request.url(), error: request.failure()?.errorText }));
  tab.on('response', response => { if (response.status() >= 400) badResponses.push({ label, url: response.url(), status: response.status(), expected: expected400 && response.status() === 400 && response.request().method() === 'PATCH' && response.url() === issueURL() }); });
  tab.on('console', value => { if (['error', 'warning'].includes(value.type())) consoleProblems.push({ label, text: value.text(), expected: expected400 && /400/.test(value.text()) }); });
  return { context, page: tab, csrf: me.csrfToken };
}
async function api(method, path, data, actor = owner) {
  const response = await actor.context.request.fetch(new URL(path, base).href, { method, headers: { Origin: base.origin, 'X-CSRF-Token': actor.csrf }, ...(data === undefined ? {} : { data }) });
  assert.ok(response.ok(), `Actual ${method} request succeeds (${response.status()})`); return response.status() === 204 ? null : response.json();
}
async function captureError(release) {
  const eventID = randomBytes(16).toString('hex'), dsn = new URL(project.dsn);
  const event = { event_id: eventID, timestamp: Date.now() / 1000, platform: 'javascript', level: 'error', message, fingerprint, release,
    exception: { values: [{ type: 'ResolutionBrowserError', value: message, stacktrace: { frames: [{ filename: 'src/resolution-browser.js', function: 'captureProof', lineno: 7, colno: 1, in_app: true }] } }] } };
  const payload = JSON.stringify(event);
  const envelope = `${JSON.stringify({ event_id: eventID, dsn: project.dsn })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const response = await owner.context.request.post(new URL(`api/${encodeURIComponent(project.id)}/envelope/`, base).href, { headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-resolution-browser/1.0` }, data: envelope });
  assert.equal(response.status(), 200); events.push(eventID); return eventID;
}
function patchResponse(tab = page) { return tab.waitForResponse(response => response.url() === issueURL() && response.request().method() === 'PATCH'); }
async function clickPatch(control, status = 200, tab = page) { const pending = patchResponse(tab); await control.click(); const response = await pending; assert.equal(response.status(), status); return response; }
async function browse() { const details = resolution().locator('details'); if (!(await details.getAttribute('open') !== null)) await details.locator('summary').click(); await visible(resolution().getByRole('button', { name: 'Next releases', exact: true })); }
async function search(query) {
  await browse(); await resolution().getByLabel('Search project releases', { exact: true }).fill(query);
  const pending = page.waitForResponse(response => new URL(response.url()).pathname === new URL(issuePath() + '/releases', base).pathname && new URL(response.url()).searchParams.get('query') === query);
  await resolution().getByRole('button', { name: 'Search releases', exact: true }).click(); assert.equal((await pending).status(), 200);
  await visible(resolution().getByRole('button', { name: 'Next releases', exact: true }));
}
async function capture(name, tab = page, anchor = resolution(tab)) {
  for (const width of [320, 768, 1440]) {
    await tab.setViewportSize({ width, height: 1000 }); await anchor.evaluate(element => element.scrollIntoView({ block: 'start' }));
    await tab.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
    const size = await tab.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, width, `${name} fits${width}px`);
    const path = resolve(output, `${name}-${width}.png`); await tab.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path);
  }
}
async function expectDrafts(commentDraft, muteDraft) {
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  assert.equal(await mute().getByLabel('Minutes (optional)', { exact: true }).inputValue(), muteDraft);
}

try {
  owner = await session(fixture.ownerEmail, fixture.ownerPassword, 'owner'); page = owner.page;
  project = await api('POST', `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/projects`, { name: `Resolution browser ${run}`, platform: 'javascript' });
  otherProject = await api('POST', `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/projects`, { name: `Resolution control ${run}`, platform: 'javascript' });
  await captureError(releaseVersion('1.0.0'));
  for (let index = 1; index <= 25; index++) await api('POST', releasePath(), { version: releaseVersion(`1.0.${index}`), projects: [project.slug], dateReleased: null });
  await api('POST', releasePath(), { version: releaseVersion('1.0.99'), projects: [otherProject.slug], dateReleased: null });
  await api('PUT', releasePath(releaseVersion('1.0.7')), { status: 'archived' });
  const list = new URL(`api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues`, base); list.search = new URLSearchParams({ project: project.id, status: 'all', query: JSON.stringify(`Resolution browser ${run}`) }).toString();
  const found = await until(async () => { const response = await owner.context.request.get(list.href); assert.equal(response.status(), 200); return response.json(); }, result => result.total === 1, 'The owned capture produces one searchable issue'); issueID = String(found.data[0].id);
  const origin = new URL(base); origin.search = new URLSearchParams({ org: fixture.orgSlug, view: 'issues', issue: issueID, project: otherProject.id, query: `retained ${run}`, environment: 'production', release: ' filter / identity ', status: 'all', sort: 'new', page: '2' }).toString();
  await page.goto(origin.href, { waitUntil: 'networkidle' }); await visible(resolution());
  await visible(resolution().getByText('This issue is not resolved.', { exact: true }));
  await search(`${family}@1.0.`); await visible(resolution().getByText('26 releases · Page 1', { exact: true })); assert.equal(await candidates().count(), 25);
  assert.deepEqual(await candidates().locator('code').allTextContents(), Array.from({ length: 25 }, (_, index) => releaseVersion(`1.0.${25 - index}`)));
  await resolution().getByRole('button', { name: 'Next releases', exact: true }).click(); await visible(resolution().getByText('26 releases · Page 2', { exact: true })); assert.deepEqual(await candidates().locator('code').allTextContents(), [releaseVersion('1.0.0')]);
  await capture('candidates-page-two');
  await search(releaseVersion('1.0.7')); assert.equal(await candidates().count(), 1); await visible(candidates().getByText('archived', { exact: true })); await candidates().getByRole('button').click(); assert.equal(await chosen().inputValue(), releaseVersion('1.0.7'));
  const commentDraft = `Resolution unsent draft ${run}`; await comments().getByLabel('Add a comment', { exact: true }).fill(commentDraft); await mute().getByLabel('Minutes (optional)', { exact: true }).fill('12');
  let response = await clickPatch(resolution().getByRole('button', { name: 'Resolve in release', exact: true })); const archived = await response.json(); assert.equal(archived.resolution.release.version, releaseVersion('1.0.7'));
  await visible(resolution().getByText('Resolved in a chosen release.', { exact: true })); await expectDrafts(commentDraft, '12'); assert.equal(page.url(), origin.href);
  await resolution().getByRole('button', { name: 'Use observed release', exact: true }).click(); assert.equal(await chosen().inputValue(), releaseVersion('1.0.0')); assert.equal((await api('GET', issuePath())).issue.resolution.release.version, releaseVersion('1.0.7'));
  await capture('archived-chosen-release'); checks.push('Issue-scoped candidates ignore an unrelated URL project, paginate25+1 and search archived choices; choosing an observed version edits the draft without changing the saved policy');

  const target = await api('POST', releasePath(), { version: exactVersion, projects: [project.slug], dateReleased: null });
  await search(`Release / ${run}`); assert.equal(await candidates().count(), 1); await candidates().getByRole('button').click(); assert.equal(await chosen().inputValue(), exactVersion);
  response = await clickPatch(resolution().getByRole('button', { name: 'Resolve in release', exact: true })); const exact = await response.json(); assert.equal(exact.resolution.release.version, exactVersion);
  await visible(resolution().getByText('Resolved in a chosen release.', { exact: true })); assert.equal(await resolution().locator('code').first().innerText(), exactVersion);
  assert.equal(await resolution().locator('time').first().getAttribute('datetime'), target.dateCreated); await expectDrafts(commentDraft, '12');
  assert.equal(await page.locator('#main img, #main script').count(), 0); await capture('exact-unicode-chosen-release');
  expected400 = true; await chosen().fill('.'); await clickPatch(resolution().getByRole('button', { name: 'Resolve in release', exact: true }), 400);
  await visible(resolution().getByRole('alert').filter({ hasText: /\S/ })); assert.equal(await chosen().inputValue(), '.'); await expectDrafts(commentDraft, '12');
  await capture('invalid-version-draft'); expected400 = false;
  checks.push('A255-codepoint version containing astral Unicode, exact spaces/slashes/case and HTML text reaches the real API unchanged; a natural400 retains editable version, mute and comment drafts');

  await chosen().fill(releaseVersion('1.0.8')); let enter;
  const entered = new Promise(done => { enter = done; }), held = new Promise(done => { releaseHeld = done; });
  const handler = async route => { if (route.request().method() !== 'PATCH') { await route.continue(); return; } assert.equal(route.request().postDataJSON().resolution.version, releaseVersion('1.0.8')); enter(); await held; await route.continue(); };
  await page.route(issueURL(), handler); const saving = patchResponse(); await resolution().getByRole('button', { name: 'Resolve in release', exact: true }).click();
  let timeout; try { await Promise.race([entered, new Promise((_, reject) => { timeout = setTimeout(() => reject(new Error('The actual resolution request was not held')), 15000); })]); } finally { clearTimeout(timeout); }
  await chosen().fill(releaseVersion('1.0.9')); releaseHeld(); releaseHeld = null; assert.equal((await saving).status(), 200); await page.unroute(issueURL(), handler);
  await until(() => resolution().locator('code').first().innerText(), value => value === releaseVersion('1.0.8'), 'The saved policy is visible after the response'); assert.equal(await chosen().inputValue(), releaseVersion('1.0.9'), 'Newer version input survives the response');
  await visible(resolution().getByRole('button', { name: 'Resolve in release', exact: true })); assert.equal((await api('GET', issuePath())).issue.resolution.release.version, releaseVersion('1.0.8')); await expectDrafts(commentDraft, '12');
  await capture('pending-save-newer-draft');
  const simple = await resolution().getByRole('button', { name: 'Resolve without release', exact: true }).elementHandle(), issues = await page.getByRole('navigation').getByRole('link', { name: 'Issues', exact: true }).elementHandle();
  const leaving = patchResponse(); await page.evaluate(({ simple, issues }) => { simple.click(); issues.click(); }, { simple, issues }); assert.equal((await leaving).status(), 200);
  await visible(page.getByRole('heading', { name: 'Issues', level: 1, exact: true })); await page.waitForLoadState('networkidle'); assert.equal(await resolution().count(), 0);
  await page.goBack({ waitUntil: 'networkidle' }); await visible(resolution().getByText('Simple resolution. Any new error reopens this issue.', { exact: true })); assert.equal(page.url(), origin.href);
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  checks.push('A held real PATCH preserves newer input and other drafts; a response after sidebar navigation cannot replace the new view, and Back shows actual simple resolution with original URL filters');

  response = await clickPatch(resolution().getByRole('button', { name: 'Resolve in next release', exact: true })); const pending = await response.json(); assert.equal(pending.resolution.mode, 'next_release'); assert.equal(pending.resolution.release, null); assert.equal(pending.resolution.baseline.version, releaseVersion('1.0.0'));
  await visible(resolution().getByText('Waiting for the next eligible project release.', { exact: true })); await capture('next-release-pending');
  await api('POST', releasePath(), { version: releaseVersion('2.0.0'), projects: [project.slug], dateReleased: null });
  const bound = await until(() => api('GET', issuePath()), detail => detail.issue.resolution?.release?.version === releaseVersion('2.0.0'), 'Real maintenance binds the metadata-only release');
  await page.getByRole('button', { name: 'Refresh issue', exact: true }).click(); await visible(resolution().getByText('The next release has been selected.', { exact: true }));
  assert.equal(await resolution().locator('code').first().innerText(), releaseVersion('2.0.0')); assert.ok((await resolution().locator('code').allTextContents()).includes(releaseVersion('1.0.0'))); await capture('next-release-bound');
  const binding = activity().getByRole('listitem').filter({ hasText: 'Next release selected' }).filter({ hasText: `Next release: ${releaseVersion('2.0.0')}` }); await visible(binding.getByText('System', { exact: true }));
  await api('DELETE', releasePath(releaseVersion('2.0.0'))); await page.getByRole('button', { name: 'Refresh issue', exact: true }).click(); await visible(resolution().getByText('The next release has been selected.', { exact: true }));
  assert.equal(await resolution().locator('code').first().innerText(), releaseVersion('2.0.0')); assert.equal(await resolution().locator('time').first().getAttribute('datetime'), bound.issue.resolution.release.dateCreated); await capture('saved-reference-after-removal');
  const regressionEvent = await captureError(releaseVersion('2.0.0')); await until(() => api('GET', issuePath()), detail => detail.issue.status === 'unresolved' && detail.issue.resolution === null, 'A real recurrence reaches the selected next release');
  await page.getByRole('button', { name: 'Refresh issue', exact: true }).click(); await visible(resolution().getByText('This issue is not resolved.', { exact: true })); await visible(page.getByText('Regressed', { exact: true }));
  const regression = activity().getByRole('listitem').filter({ hasText: 'Issue regressed' }).filter({ hasText: regressionEvent }); await visible(regression.getByText('System', { exact: true })); await visible(regression.getByText(/Next release reached/)); assert.ok((await regression.innerText()).includes(`Next release: ${releaseVersion('2.0.0')}`));
  await capture('next-release-regression-activity', page, activity()); checks.push('Pending next policy binds through actual metadata-only maintenance; saved references survive metadata deletion, then real recurrence displays System binding and reasoned regression activity');

  if (fixture.memberEmail && fixture.memberPassword) {
    member = await session(fixture.memberEmail, fixture.memberPassword, 'member'); await member.page.goto(origin.href, { waitUntil: 'networkidle' }); await visible(resolution(member.page));
    await chosen(member.page).fill(releaseVersion('1.0.7')); await clickPatch(resolution(member.page).getByRole('button', { name: 'Resolve in release', exact: true }), 200, member.page);
    await visible(resolution(member.page).getByText('Resolved in a chosen release.', { exact: true })); await capture('member-release-resolution', member.page, resolution(member.page));
    checks.push('An actual ordinary organization member can use the native resolution controls with an archived project release');
  }
  assert.equal(await page.evaluate(() => typeof window.__resolutionUnsafe), 'undefined'); assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.equal(badResponses.filter(value => value.expected).length, 1); assert.deepEqual(badResponses.filter(value => !value.expected), []); assert.deepEqual(consoleProblems.filter(value => !value.expected), []);
} catch (error) {
  failure = error; releaseHeld?.(); if (page) { const path = resolve(output, 'failure.png'); await page.screenshot({ path }).catch(() => {}); screenshots.push(path); }
} finally {
  if (owner && issueID) await api('PATCH', issuePath(), { status: 'unresolved' }).catch(() => {});
  await Promise.all(contexts.map(context => context.close())); await browser.close();
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ passed: !failure, fixtureType: 'independently authored wire captures; SDK acceptance separate', issueID, projectID: project?.id, otherProjectID: otherProject?.id, events, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure?.message }, null, 2));
}
if (failure) throw failure;
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
