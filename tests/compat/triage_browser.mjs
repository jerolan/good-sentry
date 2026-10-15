// Browser owner: triage controls, independent histories, permissions and drafts.
// Provider/HTTP tests own persistence and policy matrices. No API mocks or seams.
import assert from 'node:assert/strict';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_TRIAGE_FIXTURE || process.env.GOODSENTRY_ISSUES_FIXTURE;
assert.ok(fixturePath, 'Provide the isolated private triage fixture');
const metadata = await stat(fixturePath);
assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const base = new URL(fixture.baseURL);
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/triage-browser');
await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true });
const contexts = [];
const checks = [], screenshots = [], dimensions = [], assignmentBounds = [], pageErrors = [], consoleProblems = [], requestFailures = [], badResponses = [], cleanup = [];
const ownedComments = new Set();
const issueID = String(fixture.issue.id);
const path = `api/v1/issues/${encodeURIComponent(issueID)}`;
const prefix = `Triage browser ${Date.now()}`;
let owner, member, failure, sdkEventID, commentRejectionPending = false;
const route = issue => { const url = new URL(base); url.search = new URLSearchParams({ org: fixture.orgSlug, view: 'issues', issue: String(issue), status: 'all', project: fixture.project.id }).toString(); return url.href; };
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function until(read, predicate, message) {
  const deadline = Date.now() + 20000;
  while (Date.now() < deadline) { const value = await read(); if (predicate(value)) return value; await new Promise(done => setTimeout(done, 100)); }
  assert.fail(message);
}
async function login(email, password, label) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' }); contexts.push(context);
  const response = await context.request.post(new URL('api/v1/login', base).href, { headers: { Origin: base.origin }, data: { email, password } });
  assert.equal(response.status(), 200, `${label} signs in to the actual server`);
  const me = await response.json(); const page = await context.newPage(); page.setDefaultTimeout(20000);
  page.on('pageerror', error => pageErrors.push({ label, text: error.message }));
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ label, text: message.text(), expected: commentRejectionPending && /400/.test(message.text()) }); });
  page.on('requestfailed', request => requestFailures.push({ label, url: request.url(), error: request.failure()?.errorText }));
  page.on('response', response => { if (response.status() >= 400) badResponses.push({ label, status: response.status(), url: response.url(), expected: commentRejectionPending && response.status() === 400 && response.request().method() === 'POST' && new URL(response.url()).pathname === new URL(`${path}/comments`, base).pathname }); });
  return { context, page, csrf: me.csrfToken };
}
async function api(actor, suffix = '', method = 'GET', data) {
  const response = await actor.context.request.fetch(new URL(`${path}${suffix}`, base).href, { method, headers: { Origin: base.origin, 'X-CSRF-Token': actor.csrf }, ...(data === undefined ? {} : { data }) });
  assert.ok(response.ok(), `Actual ${method} ${suffix || 'issue'} succeeds (${response.status()})`);
  return response.status() === 204 ? null : response.json();
}
function panel(page, name) { return page.getByRole('region', { name, exact: true }); }
function commentRows(page) { return panel(page, 'Comments').getByRole('list', { name: 'Comments', exact: true }).getByRole('listitem'); }
function activityRows(page) { return panel(page, 'Activity').getByRole('list', { name: 'Recorded activity', exact: true }).getByRole('listitem'); }
function mutation(page, suffix, method) { return page.waitForResponse(response => response.request().method() === method && new URL(response.url()).pathname === new URL(`${path}${suffix}`, base).pathname); }
async function action(page, control, suffix = '', method = 'PATCH') { const pending = mutation(page, suffix, method); await control.click(); const response = await pending; assert.ok(response.ok(), 'The UI mutation succeeds at the real boundary'); return response; }
async function open(actor) {
  await actor.page.goto(route(issueID), { waitUntil: 'networkidle' });
  await visible(actor.page.getByRole('heading', { name: 'Stack trace', exact: true }));
  await visible(panel(actor.page, 'Assignment').getByRole('button', { name: 'Next assignees', exact: true }));
  await visible(panel(actor.page, 'Activity').getByRole('button', { name: 'Next activity', exact: true }));
}
async function capture(page, name, anchor) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    if (anchor) await anchor.evaluate(element => element.scrollIntoView({ block: 'start' }));
    else { await page.getByRole('heading', { level: 1 }).focus(); await page.keyboard.press('Control+Home'); }
    await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, width, `${name} fits ${width}px`);
    const assignment = panel(page, 'Assignment');
    const bounds = await assignment.evaluate(element => {
      const panel = element.getBoundingClientRect();
      return { left: panel.left, right: panel.right, client: element.clientWidth, scroll: element.scrollWidth,
        names: [...element.querySelectorAll('strong')].map(name => { const range = document.createRange(); range.selectNodeContents(name); return [...range.getClientRects()].map(rect => ({ left: rect.left, right: rect.right })); }) };
    });
    assignmentBounds.push({ name, width, ...bounds });
    assert.ok(bounds.scroll <= bounds.client, `Assignment has no hidden horizontal overflow at${width}px`);
    assert.ok(bounds.names.flat().every(rect => rect.left >= bounds.left && rect.right <= bounds.right), `Every actor name wraps inside Assignment at${width}px`);
    const filename = resolve(output, `${name}-${width}.png`); await page.screenshot({ path: filename, fullPage: false, animations: 'disabled' }); screenshots.push(filename);
  }
}
async function searchAssignees(page, query) {
  const assignment = panel(page, 'Assignment');
  await assignment.getByLabel('Search people and teams', { exact: true }).fill(query);
  const response = page.waitForResponse(response => new URL(response.url()).pathname.endsWith(`/issues/${issueID}/assignees`) && new URL(response.url()).searchParams.get('query') === query);
  await assignment.getByRole('button', { name: 'Search assignees', exact: true }).click(); assert.equal((await response).status(), 200);
  await visible(assignment.getByRole('button', { name: 'Next assignees', exact: true }));
  return assignment;
}
async function postComment(actor, text) {
  const page = actor.page; await panel(page, 'Comments').getByLabel('Add a comment', { exact: true }).fill(text);
  const response = await action(page, panel(page, 'Comments').getByRole('button', { name: 'Post comment', exact: true }), '/comments', 'POST');
  const record = await response.json(); ownedComments.add(record.id);
  await visible(commentRows(page).filter({ hasText: text })); return record;
}

try {
  owner = await login(fixture.ownerEmail, fixture.ownerPassword, 'owner');
  assert.equal((await api(owner, '/comments')).total, 0, 'The supplied isolated issue starts without comments');
  await api(owner, '', 'PATCH', { status: 'ignored', assignedTo: { type: 'team', id: fixture.assigneeTeam.id } });
  const seeded = [];
  for (let index = 0; index < 26; index++) {
    const record = await api(owner, '/comments', 'POST', { text: `${prefix} history ${String(index).padStart(2, '0')}` });
    seeded.push(record); ownedComments.add(record.id);
  }
  await open(owner); const page = owner.page;
  const assignment = () => panel(page, 'Assignment'); const comments = () => panel(page, 'Comments'); const activity = () => panel(page, 'Activity');
  await visible(assignment().getByText(fixture.assigneeTeam.name, { exact: true }).first());
  assert.equal(await assignment().getByRole('listitem').count(), 25);
  await assignment().getByRole('button', { name: 'Next assignees', exact: true }).click();
  await visible(assignment().getByText(`${fixture.assigneeTotal} assignees · Page 2`, { exact: true }));
  assert.equal(await assignment().getByRole('listitem').count(), fixture.assigneeTotal - 25);
  await visible(comments().getByText('26 comments · Page 1', { exact: true }));
  assert.equal(await commentRows(page).count(), 25);
  assert.equal((await commentRows(page).first().innerText()).split('\n').includes('null'), false, 'Unedited comments show no fabricated null text');
  assert.deepEqual(await commentRows(page).locator(':scope > p:last-of-type').allTextContents(), seeded.slice(1).reverse().map(record => record.text));
  await comments().getByRole('button', { name: 'Next comments', exact: true }).click();
  await visible(comments().getByText('26 comments · Page 2', { exact: true }));
  assert.equal(await commentRows(page).count(), 1); await visible(commentRows(page).getByText(seeded[0].text, { exact: true }));
  await activity().getByRole('button', { name: 'Next activity', exact: true }).click();
  await visible(activity().getByText(/activity entries · Page 2$/));
  const composerDraft = `${prefix} unsent <draft> & text`;
  await comments().getByLabel('Add a comment', { exact: true }).fill(composerDraft);
  await action(page, page.getByRole('button', { name: 'Reopen', exact: true }));
  await visible(page.getByRole('button', { name: 'Resolve', exact: true }));
  await visible(comments().getByText('26 comments · Page 2', { exact: true }));
  await visible(activity().getByText(/activity entries · Page 2$/));
  await visible(assignment().getByText(`${fixture.assigneeTotal} assignees · Page 2`, { exact: true }));
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), composerDraft);
  assert.equal(new URL(page.url()).searchParams.has('commentPage'), false, 'Detail history pages stay local');
  await capture(page, 'independent-history-pages', comments());
  checks.push('29 real assignees paginate25+4,26 comments paginate25+1, activity and candidate pages remain independent through status changes with composer draft intact');

  let search = await searchAssignees(page, fixture.assigneeUser.email);
  assert.equal(await search.getByRole('listitem').count(), 1);
  await action(page, search.getByRole('button', { name: /^Assign to person / }));
  await visible(assignment().getByText(fixture.assigneeUser.name, { exact: true }).first());
  assert.deepEqual((await api(owner)).issue.assignedTo.id, fixture.assigneeUser.id);
  await action(page, assignment().getByRole('button', { name: 'Clear assignment', exact: true }));
  await visible(assignment().getByText('Unassigned', { exact: true }));
  search = await searchAssignees(page, fixture.assigneeTeam.slug);
  await action(page, search.getByRole('button', { name: /^Assign to team / }));
  await visible(assignment().getByText(fixture.assigneeTeam.name, { exact: true }).first());
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), composerDraft);
  assert.equal(await page.locator('#main img, #main script').count(), 0, 'Unsafe actor names remain text');
  await capture(page, 'searched-team-assignment', assignment());
  checks.push('Real user/team search, assignment and clear preserve drafts and render unsafe names as text');

  await comments().getByRole('button', { name: 'Previous comments', exact: true }).click();
  await visible(comments().getByText('26 comments · Page 1', { exact: true }));
  const rejectedDraft = '雪'.repeat(4000);
  await comments().getByLabel('Add a comment', { exact: true }).fill(rejectedDraft);
  commentRejectionPending = true;
  const rejectedResponse = mutation(page, '/comments', 'POST');
  await comments().getByRole('button', { name: 'Post comment', exact: true }).click();
  assert.equal((await rejectedResponse).status(), 400, 'The real UTF8 byte budget rejects this draft');
  await visible(comments().getByRole('alert').filter({ hasText: /\S/ }));
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), rejectedDraft);
  await capture(page, 'comment-validation-retains-draft', comments().getByLabel('Add a comment', { exact: true }));
  commentRejectionPending = false;
  checks.push('A real400 for UTF8 comment bytes retains the editable draft and displays the server error; the following corrected post succeeds');
  const unsafe = `${prefix} <img src=x onerror=alert(1)> & review`;
  const posted = await postComment(owner, unsafe);
  const postedRow = () => commentRows(page).filter({ hasText: unsafe });
  await postedRow().getByRole('button', { name: 'Edit comment', exact: true }).click();
  const saved = `${prefix} saved revision before response`, newer = `${prefix} newer unsaved draft typed after Save`;
  await comments().getByLabel('Edit comment text', { exact: true }).fill(saved);
  const save = await comments().getByRole('button', { name: 'Save comment', exact: true }).elementHandle();
  const input = await comments().getByLabel('Edit comment text', { exact: true }).elementHandle();
  const saveResponse = mutation(page, `/comments/${posted.id}`, 'PATCH');
  await page.evaluate(({ save, input, newer }) => { save.click(); input.value = newer; input.dispatchEvent(new Event('input', { bubbles: true })); }, { save, input, newer });
  assert.equal((await saveResponse).status(), 200);
  await until(() => comments().getByLabel('Edit comment text', { exact: true }).inputValue().catch(() => null), value => value === newer, 'Typing after Save remains an unsaved edit draft');
  await visible(page.getByText('Comment updated.', { exact: true }));
  const stored = (await api(owner, '/comments')).data.find(record => record.id === posted.id);
  assert.equal(stored.text, saved); assert.equal(stored.dateCreated, posted.dateCreated); assert.ok(stored.updatedAt);
  await capture(page, 'edit-inflight-draft', comments().getByRole('form', { name: 'Edit comment', exact: true }));
  await comments().getByRole('button', { name: 'Cancel edit', exact: true }).click();
  const savedRow = () => commentRows(page).filter({ hasText: saved });
  await visible(savedRow().getByText(saved, { exact: true }));
  await savedRow().getByRole('button', { name: 'Delete comment', exact: true }).click();
  await visible(savedRow().getByText('Delete this comment? Its text will be removed.', { exact: true }));
  assert.ok((await api(owner, '/comments')).data.some(record => record.id === posted.id));
  await action(page, savedRow().getByRole('button', { name: 'Confirm deletion', exact: true }), `/comments/${posted.id}`, 'DELETE');
  ownedComments.delete(posted.id);
  await visible(comments().getByText('26 comments · Page 1', { exact: true }));
  await activity().getByRole('button', { name: 'Previous activity', exact: true }).click();
  await visible(activityRows(page).filter({ hasText: 'Deleted a comment' }).filter({ hasText: `Comment ${posted.id}` }));
  assert.equal(await page.locator('#main img, #main script').count(), 0);
  await capture(page, 'recorded-comment-activity', activity());
  checks.push('Comments add/edit/delete through real controls, preserve original timestamp, show edited time, require deletion confirmation, retain newer in-flight draft (pre-fix RED) and record IDs without deleted text');

  member = await login(fixture.memberEmail, fixture.memberPassword, 'member'); await open(member);
  const peerRow = commentRows(member.page).filter({ hasText: seeded[25].text }); await visible(peerRow);
  assert.equal(await peerRow.getByRole('button', { name: 'Edit comment', exact: true }).count(), 0);
  assert.equal(await peerRow.getByRole('button', { name: 'Delete comment', exact: true }).count(), 0);
  const memberText = `${prefix} member contribution`;
  const memberRecord = await postComment(member, memberText);
  await commentRows(member.page).filter({ hasText: memberText }).getByRole('button', { name: 'Edit comment', exact: true }).click();
  const memberEdited = `${prefix} member revised`;
  await panel(member.page, 'Comments').getByLabel('Edit comment text', { exact: true }).fill(memberEdited);
  await action(member.page, panel(member.page, 'Comments').getByRole('button', { name: 'Save comment', exact: true }), `/comments/${memberRecord.id}`);
  await visible(commentRows(member.page).getByText(memberEdited, { exact: true }));
  await capture(member.page, 'member-comment-permissions', panel(member.page, 'Comments'));
  await comments().getByRole('button', { name: 'Refresh comments', exact: true }).click();
  const moderation = commentRows(page).filter({ hasText: memberEdited }); await visible(moderation);
  await moderation.getByRole('button', { name: 'Edit comment', exact: true }).click();
  const moderated = `${prefix} owner moderated`;
  await comments().getByLabel('Edit comment text', { exact: true }).fill(moderated);
  await action(page, comments().getByRole('button', { name: 'Save comment', exact: true }), `/comments/${memberRecord.id}`);
  await visible(commentRows(page).getByText(moderated, { exact: true }));
  checks.push('A real ordinary member sees no peer edit/delete controls but can edit their own comment; owner can moderate the member comment');

  await action(page, page.getByRole('button', { name: 'Resolve', exact: true }));
  await visible(page.getByRole('button', { name: 'Reopen', exact: true }));
  const resolved = (await api(owner)).issue; assert.equal(resolved.status, 'resolved'); assert.ok(resolved.resolvedAt);
  const sdk = createRequire(import.meta.url)(process.env.GOODSENTRY_SDK_MODULE || '@sentry/node');
  sdk.init({ dsn: fixture.project.dsn, sampleRate: 1, tracesSampleRate: 0, autoSessionTracking: false, sendDefaultPii: false, beforeSend(event) { event.timestamp = fixture.triage.occurrenceTimestamp; return event; } });
  sdk.withScope(scope => { scope.setFingerprint(fixture.triage.fingerprint); try { throw new Error(fixture.triage.message); } catch (error) { sdkEventID = sdk.captureException(error); } });
  assert.ok(await sdk.flush(10000), 'The official SDK flushes to the real server'); await sdk.close(10000);
  const regressed = await until(() => api(owner), detail => detail.issue.status === 'unresolved' && detail.issue.regressedAt && detail.events.some(event => event.eventID === sdkEventID), 'The actual subsequent SDK error reopens this issue');
  assert.equal(regressed.issue.count, resolved.count + 1); assert.equal(regressed.issue.resolvedAt, null);
  const regressionRecord = (await api(owner, '/activity')).data.find(item => item.type === 'regressed' && item.data.eventID === sdkEventID);
  assert.equal(regressionRecord.data.resolvedAt, resolved.resolvedAt);
  await page.getByRole('button', { name: 'Refresh issue', exact: true }).click();
  await visible(page.getByText('Regressed', { exact: true }));
  await visible(activityRows(page).filter({ hasText: 'Issue regressed' }).filter({ hasText: sdkEventID }));
  assert.equal(await page.locator(`time[datetime="${regressed.issue.regressedAt}"]`).count() >= 1, true);
  await capture(page, 'sdk-regression-overview'); await capture(page, 'sdk-regression-activity', activity());
  checks.push('An actual official Node SDK error with old occurrence time reopens the resolved issue, increments count and displays receipt-time regression badge/activity/eventID');

  const navigationDraft = `${prefix} draft survives navigation`;
  await comments().getByLabel('Add a comment', { exact: true }).fill(navigationDraft);
  const resolveControl = await page.getByRole('button', { name: 'Resolve', exact: true }).elementHandle();
  const issuesControl = await page.getByRole('navigation').getByRole('link', { name: 'Issues', exact: true }).elementHandle();
  const navigating = mutation(page, '', 'PATCH');
  await page.evaluate(({ resolveControl, issuesControl }) => { resolveControl.click(); issuesControl.click(); }, { resolveControl, issuesControl });
  assert.equal((await navigating).status(), 200);
  await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  await page.waitForLoadState('networkidle'); assert.equal(new URL(page.url()).searchParams.has('issue'), false);
  assert.equal(await page.getByRole('heading', { name: 'Assignment', exact: true }).count(), 0, 'A late triage response never replaces the new route');
  await page.goBack({ waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Stack trace', exact: true }));
  await visible(comments().getByLabel('Add a comment', { exact: true }));
  assert.equal(await comments().getByLabel('Add a comment', { exact: true }).inputValue(), navigationDraft);
  assert.equal(await page.getByText('Regressed', { exact: true }).count(), 0, 'Manual status clears the regression marker');
  await capture(page, 'navigation-draft-restored', comments());
  checks.push('Same-turn Resolve and sidebar navigation leaves the new Issues route intact; returning restores composer draft and manual status clears the regression badge');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.equal(badResponses.filter(response => response.expected).length, 1);
  assert.deepEqual(badResponses.filter(response => !response.expected), []);
  assert.deepEqual(consoleProblems.filter(problem => !problem.expected), []);
} catch (error) {
  failure = error;
  if (owner?.page && !owner.page.isClosed()) { const filename = resolve(output, 'failure.png'); await owner.page.screenshot({ path: filename }).catch(() => {}); screenshots.push(filename); }
} finally {
  if (owner) for (const id of ownedComments) {
    try { await api(owner, `/comments/${encodeURIComponent(id)}`, 'DELETE'); cleanup.push({ id, status: 204 }); }
    catch { cleanup.push({ id, status: 'failed' }); }
  }
  await Promise.all(contexts.map(context => context.close())); await browser.close();
  const report = { passed: !failure, checks, sdkEventID, screenshots, dimensions, assignmentBounds, pageErrors, consoleProblems, requestFailures, badResponses, cleanup, failure: failure?.message };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
}
if (failure) throw failure;
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
