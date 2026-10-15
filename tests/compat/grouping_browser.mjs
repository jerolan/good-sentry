// Browser owner: native group selection, aliases, drafts and comment controls.
// Independently authored wire captures seed actual HTTP storage; SDK proof is separate.
// Request routing below only delays a real request, never supplies a response.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_GROUPS_FIXTURE || process.env.GOODSENTRY_ISSUES_FIXTURE;
assert.ok(fixturePath, 'Provide the new isolated private grouping fixture');
const metadata = await stat(fixturePath);
assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const base = new URL(fixture.baseURL); if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/grouping-browser');
await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true }), contexts = [];
const run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const prefix = `Grouping browser ${run}`, unsafe = '<img src=x onerror="window.__groupingUnsafe=1">';
const checks = [], screenshots = [], dimensions = [], events = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [];
const expectedResponses = new Set(), observedExpected = [];
let owner, member, page, project, otherProject, failure, releaseHeld;
let originalIssues = [], mergedIDs = [], rootID, staleIDs = [];
const path = id => `api/v1/issues/${encodeURIComponent(id)}`;
const mergePath = `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues/merge`;
const url = relative => new URL(relative, base).href;
const sortIDs = values => [...values].sort((a, b) => BigInt(a) < BigInt(b) ? -1 : BigInt(a) > BigInt(b) ? 1 : 0);
const route = values => { const target = new URL(base); target.search = new URLSearchParams(values).toString(); return target.href; };
const params = tab => Object.fromEntries(new URL(tab.url()).searchParams);
const panel = (name, tab = page) => tab.getByRole('region', { name, exact: true });
const origins = (tab = page) => panel('Original groups', tab);
const comments = (tab = page) => panel('Comments', tab);
const mute = () => panel('Mute notifications');
const resolution = () => panel('Resolution');
const issues = () => panel('Issues');
const selection = () => page.getByRole('group', { name: 'Merge issues', exact: true });
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function until(read, accepts, message) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) { const value = await read(); if (accepts(value)) return value; await new Promise(done => setTimeout(done, 100)); }
  assert.fail(message);
}
async function session(email, password, label) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' }); contexts.push(context);
  const response = await context.request.post(url('api/v1/login'), { headers: { Origin: base.origin }, data: { email, password } });
  assert.equal(response.status(), 200, `${label} signs into the actual isolated server`);
  const me = await response.json(), tab = await context.newPage(); tab.setDefaultTimeout(20000);
  tab.on('pageerror', error => pageErrors.push({ label, text: error.message }));
  tab.on('requestfailed', request => requestFailures.push({ label, url: request.url(), error: request.failure()?.errorText }));
  tab.on('response', response => {
    if (response.status() < 400) return;
    const key = `${response.request().method()} ${response.url()} ${response.status()}`, expected = expectedResponses.has(key);
    badResponses.push({ label, url: response.url(), status: response.status(), expected });
    if (expected) observedExpected.push(key);
  });
  tab.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ label, text: message.text(), url: message.location().url }); });
  return { context, page: tab, csrf: me.csrfToken, userID: me.user.id };
}
async function api(method, relative, data, actor = owner) {
  const response = await actor.context.request.fetch(url(relative), { method, headers: { Origin: base.origin, 'X-CSRF-Token': actor.csrf }, ...(data === undefined ? {} : { data }) });
  assert.ok(response.ok(), `Actual ${method} ${relative} succeeds (${response.status()})`);
  return response.status() === 204 ? null : response.json();
}
async function capture(target, index, cohort) {
  const eventID = randomBytes(16).toString('hex'), dsn = new URL(target.dsn);
  const message = `${prefix} original ${String(index).padStart(3, '0')} ${index === 0 ? unsafe : ''}`;
  const event = { event_id: eventID, timestamp: Date.now() / 1000 - 3600 + index, platform: 'javascript', level: 'error', message,
    fingerprint: [`grouping-browser-${run}`, String(index)], environment: 'qa', release: '1.0.0', tags: { cohort },
    exception: { values: [{ type: 'GroupingBrowserError', value: message, stacktrace: { frames: [{ filename: 'src/grouping-browser.js', function: 'ownedCapture', lineno: 7, colno: 1, in_app: true }] } }] } };
  const payload = JSON.stringify(event);
  const envelope = `${JSON.stringify({ dsn: target.dsn, event_id: eventID })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const response = await owner.context.request.post(url(`api/${encodeURIComponent(target.id)}/envelope/`), { headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-grouping-browser/1.0` }, data: envelope });
  assert.equal(response.status(), 200, 'Owned wire capture is acknowledged by the actual ingestion endpoint'); events.push(eventID);
}
async function list(target, query = JSON.stringify(prefix), pageNumber = 1) {
  return api('GET', `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/issues?${new URLSearchParams({ project: target.id, query, status: 'all', sort: 'new', page: String(pageNumber) })}`);
}
async function expectIssuePage(total, pageNumber) { await visible(issues().getByText(`${total} issues · Page ${pageNumber}`, { exact: true })); }
async function applyFilters(values) {
  for (const [label, value] of Object.entries(values)) {
    const control = page.getByLabel(label, { exact: true });
    if (['Project', 'Status', 'Sort by'].includes(label)) await control.selectOption({ value }); else await control.fill(value);
  }
  const response = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === new URL(`api/v1/organizations/${fixture.orgSlug}/issues`, base).pathname);
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click(); assert.equal((await response).status(), 200);
}
function responseFor(tab, method, relative) { return tab.waitForResponse(response => response.url() === url(relative) && response.request().method() === method); }
async function clickRequest(control, method, relative, status = 200, tab = page) {
  const pending = responseFor(tab, method, relative); await control.click(); const response = await pending;
  assert.equal(response.status(), status); return response;
}
async function captureUI(name, anchor, tab = page) {
  for (const width of [320, 768, 1440]) {
    await tab.setViewportSize({ width, height: 1000 });
    await anchor.evaluate(element => element.scrollIntoView({ block: 'start' }));
    await tab.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
    const size = await tab.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, width, `${name} fits${width}px`);
    const file = resolve(output, `${name}-${width}.png`); await tab.screenshot({ path: file, fullPage: false, animations: 'disabled' }); screenshots.push(file);
  }
}
async function openIssue(record) { await page.getByRole('link', { name: record.title, exact: true }).click(); await visible(origins()); await visible(page.getByRole('heading', { name: 'Stack trace', exact: true })); }
async function searchOrigins(query, total) {
  await origins().getByLabel('Search original groups', { exact: true }).fill(query);
  const response = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname.endsWith('/origins') && new URL(response.url()).searchParams.get('query') === query);
  await origins().getByRole('button', { name: 'Search original groups', exact: true }).click(); assert.equal((await response).status(), 200);
  await visible(origins().getByText(`${total} original ${total === 1 ? 'group' : 'groups'} · Page 1`, { exact: true }));
}
async function drafts(expectedComment, minutes, version) {
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), expectedComment);
  assert.equal(await mute().getByLabel('Minutes (optional)', { exact: true }).inputValue(), minutes);
  assert.equal(await resolution().getByLabel('Chosen release version', { exact: true }).inputValue(), version);
}

try {
  owner = await session(fixture.ownerEmail, fixture.ownerPassword, 'owner'); page = owner.page;
  member = await session(fixture.memberEmail, fixture.memberPassword, 'member');
  project = await api('POST', `api/v1/organizations/${fixture.orgSlug}/projects`, { name: `Grouping browser ${run}`, platform: 'javascript' });
  otherProject = await api('POST', `api/v1/organizations/${fixture.orgSlug}/projects`, { name: `Grouping control ${run}`, platform: 'javascript' });
  for (let index = 0; index < 102; index++) await capture(project, index, index < 26 ? 'merge' : 'selection');
  await capture(otherProject, 999, 'control');
  const first = await until(() => list(project), result => result.total === 102, 'All102 owned original groups become searchable');
  for (let number = 1; number <= 5; number++) originalIssues.push(...(number === 1 ? first : await list(project, JSON.stringify(prefix), number)).data);
  const byIndex = new Map(originalIssues.map(issue => [Number(/original (\d{3})/.exec(issue.title)[1]), issue]));
  const crossProject = (await list(otherProject)).data[0];
  mergedIDs = sortIDs(Array.from({ length: 26 }, (_, index) => byIndex.get(index).id)); rootID = mergedIDs[0];
  const root = originalIssues.find(issue => issue.id === rootID), source = byIndex.get(1);
  await api('PATCH', path(rootID), { status: 'ignored', ignore: { count: 5 }, assignedTo: { type: 'user', id: owner.userID } });
  await api('PATCH', path(byIndex.get(25).id), { status: 'resolved', resolution: null });
  const rootComment = await api('POST', path(rootID) + '/comments', { text: `${prefix} root owner comment` });
  const sourceComment = await api('POST', path(source.id) + '/comments', { text: `${prefix} source member comment` }, member);
  const filters = { org: fixture.orgSlug, view: 'issues', project: project.id, query: JSON.stringify(prefix), status: 'all', sort: 'new', environment: 'qa', release: '1.0.0' };
  await page.goto(route(filters), { waitUntil: 'networkidle' }); await expectIssuePage(102, 1);
  for (let number = 1; number <= 4; number++) {
    await issues().getByRole('checkbox', { name: 'Select all issues on this page', exact: true }).check();
    await visible(selection().getByText(new RegExp(`^${number * 25} selected`)));
    await issues().getByRole('button', { name: 'Next', exact: true }).click(); await expectIssuePage(102, number + 1);
  }
  const allBoxes = await issues().getByRole('checkbox').all(); assert.equal(allBoxes.length, 3);
  assert.ok(await allBoxes[1].isDisabled()); assert.ok(await allBoxes[2].isDisabled());
  await allBoxes[0].click(); await visible(selection().getByRole('alert').filter({ hasText: 'Select at most 100 issues.' }));
  await page.getByRole('button', { name: 'Refresh', exact: true }).click(); await expectIssuePage(102, 5); await visible(selection().getByText(/^100 selected/));
  await captureUI('selection-limit', selection());
  await selection().getByRole('button', { name: 'Clear selection', exact: true }).click();
  await applyFilters({ Project: '' }); await expectIssuePage(103, 1);
  await issues().getByRole('checkbox', { name: `Select issue ${crossProject.shortId || crossProject.id}`, exact: true }).check();
  const newest = byIndex.get(101); await issues().getByRole('checkbox', { name: `Select issue ${newest.shortId || newest.id}`, exact: true }).check();
  assert.ok(await selection().getByRole('button', { name: 'Merge selected', exact: true }).isDisabled());
  await visible(selection().getByText(/different projects/)); await captureUI('mixed-project-selection', selection());
  await applyFilters({ Project: project.id, 'Search issues': `${JSON.stringify(prefix)} cohort:merge` }); await expectIssuePage(26, 1);
  await visible(selection().getByText(/^0 selected/)); assert.equal(params(page).page, undefined);
  checks.push('Real list selection survives four page changes and Refresh, stops at100, blocks mixed projects and clears when filter/project scope changes');

  await issues().getByRole('checkbox', { name: 'Select all issues on this page', exact: true }).check();
  await issues().getByRole('button', { name: 'Next', exact: true }).click(); await expectIssuePage(26, 2);
  await issues().getByRole('checkbox', { name: 'Select all issues on this page', exact: true }).check();
  let mergeRequests = 0; const trackMerge = request => { if (request.method() === 'POST' && request.url() === url(mergePath)) mergeRequests++; }; page.on('request', trackMerge);
  await selection().getByRole('button', { name: 'Merge selected', exact: true }).click();
  await visible(selection().getByText(`Merge 26 selected issues into issue ${rootID}?`, { exact: true })); assert.equal(mergeRequests, 0);
  await selection().getByRole('button', { name: 'Cancel merge', exact: true }).click(); assert.equal(mergeRequests, 0);
  await selection().getByRole('button', { name: 'Merge selected', exact: true }).click(); await captureUI('merge-confirmation', selection());
  const merged = await clickRequest(selection().getByRole('button', { name: 'Confirm merge', exact: true }), 'POST', mergePath);
  assert.deepEqual(merged.request().postDataJSON(), { issueIds: mergedIDs }); assert.equal((await merged.json()).issue.id, rootID); page.off('request', trackMerge);
  await visible(origins().getByText('26 original groups · Page 1', { exact: true })); await visible(page.getByText('26 original groups', { exact: true }));
  assert.equal(params(page).issue, rootID); assert.equal(params(page).page, '2'); assert.equal(params(page).release, '1.0.0');
  const combined = await api('GET', path(rootID)); assert.equal(combined.issue.count, 26); assert.equal(combined.issue.status, 'ignored');
  await visible(mute().getByText('5 additional errors remaining.', { exact: true }));
  await visible(origins().getByText(root.title, { exact: true }));
  await visible(panel('Activity').getByText(/Previous status: resolved.*Simple resolution/));
  assert.equal(await page.locator('#main img, #main script').count(), 0); assert.equal(await page.evaluate(() => window.__groupingUnsafe), undefined);
  await captureUI('merged-original-groups', origins());
  await searchOrigins('original 000', 1); await visible(origins().getByText('Current root', { exact: true })); await searchOrigins('', 26);
  await origins().getByRole('checkbox', { name: `Select original group ${rootID}`, exact: true }).check();
  await origins().getByRole('button', { name: 'Next origins', exact: true }).click(); await visible(origins().getByText('26 original groups · Page 2', { exact: true }));
  const lastID = mergedIDs[25]; await origins().getByRole('checkbox', { name: `Select original group ${lastID}`, exact: true }).check(); await visible(origins().getByText(/^2 selected/));
  await captureUI('origins-second-page', origins());
  await origins().getByRole('button', { name: 'Previous origins', exact: true }).click(); await visible(origins().getByText('26 original groups · Page 1', { exact: true }));
  assert.ok(await origins().getByRole('checkbox', { name: `Select original group ${rootID}`, exact: true }).isChecked());
  await origins().getByRole('button', { name: 'Select all original groups', exact: true }).click(); await visible(origins().getByText(/^26 selected/));
  checks.push('Confirmed merge opens the lowest-ID group with26 origins and retained root state; safe titles, literal search,25+1 origin pages and cross-page/root/all selection work');

  await member.page.goto(route({ ...filters, issue: rootID }), { waitUntil: 'networkidle' });
  const memberRow = comments(member.page).getByRole('listitem').filter({ hasText: sourceComment.text });
  const ownerRow = comments(member.page).getByRole('listitem').filter({ hasText: rootComment.text });
  await visible(memberRow); assert.equal(await memberRow.getByRole('button', { name: 'Edit comment', exact: true }).count(), 1);
  assert.equal(await ownerRow.getByRole('button', { name: 'Edit comment', exact: true }).count(), 0); await captureUI('member-group-comments', comments(member.page), member.page);
  const moderate = comments().getByRole('listitem').filter({ hasText: sourceComment.text }); await moderate.getByRole('button', { name: 'Edit comment', exact: true }).click();
  const moderatorEditor = comments().getByRole('form', { name: 'Edit comment', exact: true });
  const moderated = `${prefix} moderated original-member comment`; await moderatorEditor.getByLabel('Edit comment text', { exact: true }).fill(moderated);
  await clickRequest(moderatorEditor.getByRole('button', { name: 'Save comment', exact: true }), 'PATCH', `${path(rootID)}/comments/${sourceComment.id}`);
  await visible(comments().getByText(moderated, { exact: true }));
  const combinedDraft = `${prefix} composer survives separation`; await comments().getByLabel('Add a comment', { exact: true }).fill(combinedDraft);
  await mute().getByLabel('Minutes (optional)', { exact: true }).fill('7'); await resolution().getByLabel('Chosen release version', { exact: true }).fill('separation / draft');
  await origins().getByRole('button', { name: 'Separate selected', exact: true }).click();
  await clickRequest(origins().getByRole('button', { name: 'Confirm separation', exact: true }), 'POST', path(rootID) + '/unmerge');
  await visible(page.getByText('1 original group', { exact: true })); await visible(origins().getByRole('link', { name: `Open issue ${source.id}`, exact: true }));
  await visible(panel('Activity').getByText('Separated original groups', { exact: true }).first());
  await drafts(combinedDraft, '7', 'separation / draft');
  const separatedSource = await api('GET', path(source.id)); assert.ok(separatedSource.comments.some(comment => comment.id === sourceComment.id && comment.text === moderated));
  assert.ok(!(await api('GET', path(rootID))).comments.some(comment => comment.id === sourceComment.id)); await captureUI('separation-result-links', origins());
  checks.push('Real member comment permissions survive merging; owner moderation keeps the source comment with its separated original group, and all/root separation retains draft inputs and links to actual results');

  const target = byIndex.get(26), alias = byIndex.get(27); staleIDs = [target.id, alias.id];
  const aliasComment = await api('POST', path(alias.id) + '/comments', { text: `${prefix} alias comment before merge` });
  await page.getByRole('link', { name: '← All issues', exact: true }).click();
  await applyFilters({ 'Search issues': `${JSON.stringify(prefix)} original 026` }); await openIssue(target);
  const rootDraft = `${prefix} independent root draft`; await comments().getByLabel('Add a comment', { exact: true }).fill(rootDraft);
  await page.getByRole('link', { name: '← All issues', exact: true }).click();
  await applyFilters({ 'Search issues': `${JSON.stringify(prefix)} original 027` }); await openIssue(alias);
  const aliasParams = params(page), aliasDraft = `${prefix} alias composer draft`, editDraft = `${prefix} unsaved edit follows its comment`;
  await comments().getByLabel('Add a comment', { exact: true }).fill(aliasDraft); await mute().getByLabel('Minutes (optional)', { exact: true }).fill('11');
  await resolution().getByLabel('Chosen release version', { exact: true }).fill('alias / resolution draft');
  const aliasRow = comments().getByRole('listitem').filter({ hasText: aliasComment.text }); await aliasRow.getByRole('button', { name: 'Edit comment', exact: true }).click();
  const aliasEditor = comments().getByRole('form', { name: 'Edit comment', exact: true });
  await aliasEditor.getByLabel('Edit comment text', { exact: true }).fill(editDraft);
  await api('POST', mergePath, { issueIds: staleIDs });
  const stalePath = `${path(alias.id)}/comments/${aliasComment.id}`; expectedResponses.add(`PATCH ${url(stalePath)} 409`);
  const conflict = await clickRequest(aliasEditor.getByRole('button', { name: 'Save comment', exact: true }), 'PATCH', stalePath, 409);
  assert.equal((await conflict.json()).mergedInto, target.id); await drafts(aliasDraft, '11', 'alias / resolution draft');
  await visible(aliasEditor.getByRole('link', { name: 'Open current group', exact: true })); await captureUI('stale-alias-drafts-409', comments());
  assert.equal((await api('GET', path(target.id))).comments.find(comment => comment.id === aliasComment.id).text, aliasComment.text);
  await aliasEditor.getByRole('link', { name: 'Open current group', exact: true }).click(); await visible(origins().getByText('2 original groups · Page 1', { exact: true }));
  assert.deepEqual(params(page), { ...aliasParams, issue: target.id }); await drafts(aliasDraft, '11', 'alias / resolution draft');
  const recoveredEditor = comments().getByRole('form', { name: 'Edit comment', exact: true }); assert.equal(await recoveredEditor.getByLabel('Edit comment text', { exact: true }).inputValue(), editDraft);
  await captureUI('current-group-preserved-drafts', comments());
  await clickRequest(recoveredEditor.getByRole('button', { name: 'Save comment', exact: true }), 'PATCH', `${path(target.id)}/comments/${aliasComment.id}`); await visible(comments().getByText(editDraft, { exact: true }));
  await page.getByRole('link', { name: '← All issues', exact: true }).click(); await applyFilters({ 'Search issues': `${JSON.stringify(prefix)} original 026` });
  // Historical search still selects the combined root, whose latest title belongs to its other original group.
  await page.getByRole('link', { name: alias.title, exact: true }).click(); await visible(origins());
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), rootDraft);
  checks.push('A genuine stale-alias409 keeps composer, mute, resolution and edit drafts; its explicit canonical-group link preserves filters and recovers the edit without overwriting the independent root composer');

  const memberListURL = route(filters); await member.page.goto(memberListURL, { waitUntil: 'networkidle' });
  const aliasURL = route({ ...filters, issue: alias.id, page: '2' }); await member.page.goto(aliasURL, { waitUntil: 'networkidle' });
  await visible(origins(member.page).getByText('2 original groups · Page 1', { exact: true }));
  assert.deepEqual(params(member.page), { ...filters, issue: target.id, page: '2' }); await captureUI('canonical-alias-member', origins(member.page), member.page);
  await member.page.goBack({ waitUntil: 'networkidle' }); assert.equal(member.page.url(), memberListURL, 'Alias canonicalization replaces rather than adding a browser-history entry');
  await member.page.goForward({ waitUntil: 'networkidle' }); await visible(origins(member.page));
  await origins(member.page).getByRole('checkbox', { name: `Select original group ${alias.id}`, exact: true }).check();
  await origins(member.page).getByRole('button', { name: 'Separate selected', exact: true }).click();
  await clickRequest(origins(member.page).getByRole('button', { name: 'Confirm separation', exact: true }), 'POST', path(target.id) + '/unmerge', 200, member.page);
  await visible(origins(member.page).getByRole('link', { name: `Open issue ${alias.id}`, exact: true }));
  checks.push('An ordinary member reads a canonical alias with exact filter/history preservation and separates an original group through the actual controls');

  const lateRoot = byIndex.get(28), lateSource = byIndex.get(29); await api('POST', mergePath, { issueIds: [lateRoot.id, lateSource.id] });
  await page.goto(route({ ...filters, issue: lateRoot.id }), { waitUntil: 'networkidle' }); await visible(origins().getByText('2 original groups · Page 1', { exact: true }));
  const navigationDraft = `${prefix} composer after late response`; await comments().getByLabel('Add a comment', { exact: true }).fill(navigationDraft);
  await origins().getByRole('checkbox', { name: `Select original group ${lateSource.id}`, exact: true }).check(); await origins().getByRole('button', { name: 'Separate selected', exact: true }).click();
  let entered; const held = new Promise(done => { entered = done; }), gate = new Promise(done => { releaseHeld = done; });
  const latePath = path(lateRoot.id) + '/unmerge';
  const handler = async routed => { if (routed.request().method() !== 'POST') { await routed.continue(); return; } entered(); await gate; await routed.continue(); };
  await page.route(url(latePath), handler); const lateResponse = responseFor(page, 'POST', latePath);
  await origins().getByRole('button', { name: 'Confirm separation', exact: true }).click();
  await Promise.race([held, new Promise((_, reject) => setTimeout(() => reject(new Error('The actual separation request did not reach the delay gate')), 15000))]);
  await page.getByRole('navigation').getByRole('link', { name: 'Issues', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Issues', exact: true }));
  releaseHeld(); releaseHeld = null; assert.equal((await lateResponse).status(), 200); await page.unroute(url(latePath), handler); await page.waitForLoadState('networkidle');
  assert.equal(params(page).issue, undefined); await visible(page.getByRole('heading', { name: 'Issues', exact: true })); await captureUI('late-separation-navigation', page.getByRole('heading', { name: 'Issues', exact: true }));
  await page.goBack({ waitUntil: 'networkidle' }); await visible(origins().getByText('1 original group · Page 1', { exact: true }));
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), navigationDraft);
  checks.push('A delayed real separation response cannot replace sidebar navigation; Back reads the actual separated group and restores its composer draft');

  assert.equal(observedExpected.length, 1, 'Exactly the deliberately stale mutation returns409');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []); assert.deepEqual(badResponses.filter(value => !value.expected), []);
  const unexpectedConsole = consoleProblems.filter(value => !(/409/.test(value.text) && value.url === url(stalePath)));
  assert.deepEqual(unexpectedConsole, []);
  console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
} catch (error) {
  failure = error; console.error(error.stack || error.message);
  await page?.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  releaseHeld?.();
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ passed: !failure, fixtureType: 'owned HTTP/wire fixtures; no SDK-compatibility claim', projectIDs: [project?.id, otherProject?.id], rootID, mergedIDs, staleIDs, eventIDs: events, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure?.message }, null, 2));
  await Promise.all(contexts.map(context => context.close())); await browser.close();
}
if (failure) process.exitCode = 1;
