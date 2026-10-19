// Test-audit authoring gate (four answers):
// 1. Owns observable modern-feedback browser contracts: actual list/detail/category,
//    safe text and binary rendering, focus, draft preservation, and stale-request behavior.
// 2. Credible regressions include selecting the wrong receipt (especially >50 old),
//    unsafe URL/text handling, attachment misrouting, category leakage, lost drafts/focus,
//    error-only controls on feedback, and delayed page/inspection/module responses painting
//    after selection, page, mode, category, or SPA route changes.
// 3. Existing user_reports_browser.mjs owns legacy reports only; storage/HTTP tests cannot
//    observe DOM, keyboard focus, layout, draft maps, and browser lifecycle. This modern
//    suite exercises those distinct UI risks at the real application boundary.
// 4. No production seam: a real authenticated browser drives the app; route interception
//    only delays/fails real requests and passes successful responses through route.fetch().
//
// A private receipt-controller fixture supplies the real 61-ID capture order, issue
// object, special text/binary and linked/pending/unavailable/empty-history cases.
// Its ordinary-member session reads actual issue, event and attachment endpoints.
// See SPEC.md for the fixture and runtime environment contract.
//
// This is a browser-owned contract test. It creates no database state and seeds no rows.
import assert from 'node:assert/strict';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { basename, isAbsolute, resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_MODERN_FIXTURE;
assert.ok(fixturePath && isAbsolute(fixturePath), 'GOODSENTRY_MODERN_FIXTURE must be an absolute private JSON path');
assert.ok(resolve(fixturePath).length > 0 && (await stat(fixturePath)).isFile(), 'Use a regular private fixture file');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
assert.equal(fixture.sdkNetwork?.passed, true);
assert.equal(fixture.sdkNetwork?.serverAcceptance, true);
assert.equal(fixture.sdkNetwork?.ids?.length, 61);
assert.deepEqual(fixture.sdkNetwork.ids, fixture.main?.eventIDs);
assert.equal(fixture.main?.eventIDs?.length, 61, 'Fixture must retain all three expected issue pages');
assert.equal(fixture.main.eventIDs[0], fixture.main.special.eventID, 'Fixture eventIDs are in ascending capture order and the special submission is oldest');
assert.ok(fixture.main.issue?.id && typeof fixture.main.issue.title === 'string' && fixture.project?.id && fixture.project?.slug);
assert.ok(fixture.main.special?.attachment?.filename && fixture.main.special.attachment.dataBase64);
assert.ok(fixture.main.linked?.feedbackEventID && fixture.main.linked.errorEventID && fixture.main.linked.currentIssueID);
assert.ok(fixture.main.pendingID && fixture.main.unavailableID && fixture.main.noReferenceID && fixture.main.nonErrorReferenceID && fixture.main.zeroIssueID && fixture.errorIssueID);

const outputValue = process.env.GOODSENTRY_TEST_OUTPUT;
assert.ok(outputValue && isAbsolute(outputValue), 'GOODSENTRY_TEST_OUTPUT must be an absolute directory');
const output = resolve(outputValue);
const phase = process.env.GOODSENTRY_TEST_PHASE;
assert.ok(phase && /^[A-Za-z0-9._-]+$/.test(phase) && phase !== '.' && phase !== '..' && basename(phase) === phase, 'GOODSENTRY_TEST_PHASE must be a fresh filename-safe phase');
await mkdir(output, { recursive: true });
const evidencePath = resolve(output, `${phase}.json`);
try {
  await stat(evidencePath);
  throw new Error(`Evidence already exists at ${evidencePath}; choose a new phase`);
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
for (const suffix of ['320-page1.png', '768-page1.png', '1024-page1.png', '1440-page1.png', '320-selected.png', '768-selected.png', '1024-selected.png', '1440-selected.png', 'failure.png']) {
  try { await stat(resolve(output, `${phase}-${suffix}`)); throw new Error(`Screenshot already exists for phase ${phase}: ${suffix}`); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
}

const base = new URL(fixture.baseURL);
const evidence = { phase, passed: false, widths: [], checks: [], errors: [], console: [], failedRequests: [], unexpected: [], outside: [] };
const redact = value => String(value || '').replace(/https?:\/\/[^\s"']+/g, raw => {
  try { const url = new URL(raw); return `${url.origin}${url.pathname}`; } catch { return '[url]'; }
}).replace(/\b(cookie|authorization|token|secret)\b\s*[:=]\s*[^,\s;]*/gi, '$1=[redacted]');
const safeURL = raw => { try { const url = new URL(raw); return `${url.origin}${url.pathname}`; } catch { return '[invalid-url]'; } };
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_EXECUTABLE ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE } : {}) });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce', locale: 'en-US', acceptDownloads: true });
const addCookies = async target => target.addCookies(fixture.cookieHeader.split('; ').map(value => {
  const at = value.indexOf('=');
  return { name: value.slice(0, at), value: value.slice(at + 1), url: base.origin };
}));
await addCookies(context);
const page = await context.newPage();
let failure;
const expectedStatuses = new Map();
expectedStatuses.set(new URL('favicon.ico', base).href, 404);
const watch = target => {
  target.on('pageerror', error => evidence.errors.push(redact(error.message)));
  target.on('console', message => {
    const expectedStatus = expectedStatuses.get(message.location().url);
    if (['error', 'warning'].includes(message.type())) evidence.console.push({ message: redact(message.text()), url: safeURL(message.location().url), expected: expectedStatus !== undefined && message.text().includes(String(expectedStatus)) });
  });
  target.on('requestfailed', request => evidence.failedRequests.push({ url: safeURL(request.url()), error: redact(request.failure()?.errorText), expected: expectedStatuses.get(request.url()) === 503 && request.failure()?.errorText === 'net::ERR_ABORTED' }));
  target.on('request', request => { if (new URL(request.url()).origin !== base.origin) evidence.outside.push(safeURL(request.url())); });
  target.on('response', response => {
    if (response.status() >= 400) evidence.unexpected.push({ url: safeURL(response.url()), status: response.status(), expected: expectedStatuses.get(response.url()) === response.status() });
  });
};
watch(page);
const queryTarget = (issue, view = 'feedback', extra = {}) => {
  const url = new URL(base);
  url.search = new URLSearchParams({ org: fixture.orgSlug, view, project: String(fixture.project.id), issue, ...extra });
  return url.href;
};
const feedbackRegion = (target = page) => target.getByRole('region', { name: 'Feedback submissions', exact: true });
const feedbackRow = (target, id) => feedbackRegion(target).getByRole('listitem').filter({ has: target.getByRole('button', { name: `Inspect feedback submission ${id}`, exact: true }) });
const detailRegion = (target, name, id) => target.getByRole('region', { name: `${name} ${id}`, exact: true });
const feedbackReady = async (target, total, pageNo = 1) => {
  await feedbackRegion(target).getByText(`${total} feedback submissions · Page ${pageNo}`, { exact: true }).waitFor();
};
const feedbackIssueReady = target => feedbackRegion(target).getByRole('heading', { name: 'Feedback', exact: true }).waitFor();
const selectedSubmission = async (target, id) => {
  const detail = detailRegion(target, 'Feedback submission', id);
  await detail.getByText('Submission JSON', { exact: true }).waitFor();
  return detail;
};
const dimensionsAndFit = async target => {
  const dims = await target.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
  assert.equal(dims.document, dims.viewport, `Document width at ${dims.viewport}px`);
  const clipped = await feedbackRegion(target).evaluate(region => [region, ...region.querySelectorAll('*')].filter(element => {
    const box = element.getBoundingClientRect();
    return box.width > 0 && !element.closest('.table-scroll,.event-json,.source-context') &&
      (box.right > innerWidth + 1 || box.left < -1 || element.scrollWidth > element.clientWidth + 2);
  }).map(element => ({ tag: element.tagName, className: String(element.className), right: element.getBoundingClientRect().right, scroll: element.scrollWidth, client: element.clientWidth })));
  assert.deepEqual(clipped, [], `Feedback content fits at ${dims.viewport}px`);
  return dims;
};
const routeFailOnce = async (pattern, status, body) => {
  let first = true;
  const seen = [];
  await page.route(pattern, async route => {
    seen.push(route.request().url());
    if (first) {
      first = false;
      expectedStatuses.set(route.request().url(), status);
      await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    } else await route.continue();
  });
  return { seen, clear: () => page.unroute(pattern) };
};
const activeGates = new Set();
const waitForControlledRequest = async pending => {
  let timer;
  try {
    return await Promise.race([pending, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('Controlled real request did not arrive within 15 seconds')), 15000); })]);
  } finally {
    clearTimeout(timer);
  }
};
const gateRealRequest = async pattern => {
  let release;
  const gate = new Promise(resolveGate => { release = resolveGate; });
  let entered;
  const arrived = new Promise(resolveEntered => { entered = resolveEntered; });
  let settled;
  let started = false;
  const completed = new Promise(resolveCompleted => { settled = resolveCompleted; });
  const handler = async route => {
    started = true;
    try {
      const response = await route.fetch();
      entered(route.request().url());
      await gate;
      await route.fulfill({ response });
    } catch (error) {
      evidence.errors.push(`Controlled route failed: ${redact(error.message)}`);
    } finally {
      settled();
    }
  };
  const controlled = { get arrived() { return waitForControlledRequest(arrived); }, release, clear: async () => {
    if (started) await completed;
    await page.unroute(pattern, handler);
    activeGates.delete(controlled);
  } };
  await page.route(pattern, handler);
  activeGates.add(controlled);
  return controlled;
};
const commentLabel = page.getByLabel('Add a comment', { exact: true });
let currentFeedbackPage = 1;
const goFeedbackPage = async number => {
  while (currentFeedbackPage < number) {
    await feedbackRegion().getByRole('button', { name: 'Next feedback', exact: true }).click();
    currentFeedbackPage++;
    await feedbackReady(page, 61, currentFeedbackPage);
  }
  while (currentFeedbackPage > number) {
    await feedbackRegion().getByRole('button', { name: 'Previous feedback', exact: true }).click();
    currentFeedbackPage--;
    await feedbackReady(page, 61, currentFeedbackPage);
  }
};
const pageForReceipt = id => {
  const index = fixture.main.eventIDs.indexOf(id);
  assert.notEqual(index, -1, 'Fixture eventIDs must include the requested receipt');
  return Math.ceil((fixture.main.eventIDs.length - index) / 25);
};

try {
  const special = fixture.main.special;
  const hostile = special.message;
  assert.equal([...hostile].length, 10000);
  assert.equal([...special.name].length, 128);
  assert.equal([...special.email].length, 254);
  assert.equal([...special.url].length, 2048);
  const url = queryTarget(fixture.main.issue.id, 'feedback', { query: `message-${fixture.runID}`, status: 'all', environment: 'production', release: `feedback-${fixture.runID}`, sort: 'date', page: '1' });
  await page.goto(url, { waitUntil: 'networkidle' });
  await feedbackIssueReady(page);
  await page.getByRole('heading', { name: fixture.main.issue.title, exact: true }).waitFor();
  await feedbackReady(page, 61);
  assert.equal(await feedbackRegion().getByRole('listitem').count(), 25, 'IssueDetail first snapshot may supply page 1');
  assert.equal(new URL(page.url()).searchParams.get('view'), 'feedback');
  assert.equal(await page.getByRole('button', { name: 'Ignore indefinitely', exact: true }).count(), 1);
  assert.equal(await page.getByRole('button', { name: 'Resolve', exact: true }).count(), 1);
  assert.equal(await page.getByRole('button', { name: 'Reopen', exact: true }).count(), 0);
  assert.equal(await page.getByLabel('Additional errors (optional)', { exact: true }).count(), 0);
  assert.equal(await page.getByLabel('Chosen release version', { exact: true }).count(), 0);

  const commentDraft = `Unsent feedback comment ${fixture.runID}`;
  await commentLabel.fill(commentDraft);
  const hostileName = special.name;
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dims = await dimensionsAndFit(page);
    evidence.widths.push({ state: 'page1', ...dims });
    await page.screenshot({ path: resolve(output, `${phase}-${width}-page1.png`), fullPage: true, animations: 'disabled' });
  }

  await goFeedbackPage(3);
  const row = feedbackRow(page, special.eventID);
  const rowText = await row.innerText();
  for (const value of [hostile, hostileName, special.email, special.url]) assert.ok(rowText.includes(value), 'The list renders each supplied field as text');
  assert.equal(await row.locator('a[href]').count(), 0, 'Supplied source URL and contact text never become links');
  assert.equal(await row.locator('script,iframe,img').count(), 0, 'Supplied values never create executable or fetched content');

  await goFeedbackPage(1);
  await goFeedbackPage(2);
  assert.equal(await feedbackRegion().getByRole('listitem').count(), 25);
  await goFeedbackPage(3);
  assert.equal(await feedbackRegion().getByRole('listitem').count(), 11);
  assert.equal(await feedbackRegion().getByRole('button', { name: 'Next feedback', exact: true }).isDisabled(), true);
  await goFeedbackPage(2);
  await goFeedbackPage(1);
  await goFeedbackPage(3);
  const inspected = feedbackRow(page, special.eventID).getByRole('button', { name: `Inspect feedback submission ${special.eventID}`, exact: true });
  await inspected.click();
  let detail = await selectedSubmission(page, special.eventID);
  const selectedText = await detail.innerText();
  for (const value of [hostile, hostileName, special.email, special.url]) assert.ok(selectedText.includes(value), 'Selected raw feedback renders supplied fields');
  assert.equal(selectedText.includes(`Feedback ${fixture.runID} submission 60`), false, 'Selected detail never substitutes the newest submission');
  await detail.getByRole('region', { name: 'Event attachments', exact: true }).getByRole('button', { name: `Download ${special.attachment.filename}`, exact: true }).waitFor();
  const downloadWait = page.waitForEvent('download');
  await detail.getByRole('button', { name: `Download ${special.attachment.filename}`, exact: true }).click();
  const download = await downloadWait;
  assert.deepEqual([...await readFile(await download.path())], [...Buffer.from(special.attachment.dataBase64, 'base64')]);
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dims = await dimensionsAndFit(page);
    evidence.widths.push({ state: 'selectedSubmission', ...dims });
    await detail.getByRole('heading', { name: 'Feedback submission', exact: true }).scrollIntoViewIfNeeded();
    await page.screenshot({ path: resolve(output, `${phase}-${width}-selected.png`), animations: 'disabled' });
  }
  await detail.getByRole('button', { name: 'Close feedback detail', exact: true }).click();
  assert.equal(await inspected.evaluate(element => element === document.activeElement), true, 'Close restores focus to the selecting submission');
  await inspected.press('Enter');
  detail = await selectedSubmission(page, special.eventID);
  assert.equal(await page.getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);

  // Association readers must use the selected receipt: linked remains exact and reaches its current issue.
  const linked = fixture.main.linked;
  await goFeedbackPage(pageForReceipt(linked.feedbackEventID));
  const linkedRow = feedbackRow(page, linked.feedbackEventID);
  await linkedRow.getByText('Associated error linked', { exact: true }).waitFor();
  await linkedRow.getByRole('button', { name: `Inspect associated error for feedback ${linked.feedbackEventID}`, exact: true }).click();
  const associated = detailRegion(page, 'Associated error', linked.feedbackEventID);
  await associated.getByText(linked.errorMessage, { exact: true }).waitFor();
  await associated.getByRole('link', { name: 'Open associated issue', exact: true }).waitFor();
  const associatedLink = new URL(await associated.getByRole('link', { name: 'Open associated issue', exact: true }).getAttribute('href'), base);
  assert.equal(associatedLink.searchParams.get('view'), 'issues');
  assert.equal(associatedLink.searchParams.get('issue'), linked.currentIssueID);
  assert.equal(await associated.getByRole('region', { name: 'Event attachments', exact: true }).count(), 0);
  await associated.getByRole('button', { name: 'Close feedback detail', exact: true }).click();
  const linkedJSON = `**/events/${encodeURIComponent(linked.feedbackEventID)}/json/`;
  const modeGate = await gateRealRequest(linkedJSON);
  await linkedRow.getByRole('button', { name: `Inspect feedback submission ${linked.feedbackEventID}`, exact: true }).click();
  await modeGate.arrived;
  await linkedRow.getByRole('button', { name: `Inspect associated error for feedback ${linked.feedbackEventID}`, exact: true }).click();
  await detailRegion(page, 'Associated error', linked.feedbackEventID).getByText(linked.errorMessage, { exact: true }).waitFor();
  modeGate.release(); await modeGate.clear();
  await page.waitForLoadState('networkidle');
  assert.equal(await detailRegion(page, 'Feedback submission', linked.feedbackEventID).count(), 0);
  assert.equal(await detailRegion(page, 'Associated error', linked.feedbackEventID).count(), 1, 'A delayed submission response cannot overwrite the selected associated mode');

  await detailRegion(page, 'Associated error', linked.feedbackEventID).getByRole('button', { name: 'Close feedback detail', exact: true }).click();
  const associatedPath = `**/feedback/${encodeURIComponent(linked.feedbackEventID)}/associated-event`;
  const associatedFailure = await routeFailOnce(associatedPath, 404, { detail: 'Not found' });
  await linkedRow.getByRole('button', { name: `Inspect associated error for feedback ${linked.feedbackEventID}`, exact: true }).click();
  const failedAssociated = detailRegion(page, 'Associated error', linked.feedbackEventID);
  await failedAssociated.getByRole('button', { name: 'Retry feedback detail', exact: true }).waitFor();
  await failedAssociated.getByRole('button', { name: 'Retry feedback detail', exact: true }).click();
  await detailRegion(page, 'Associated error', linked.feedbackEventID).getByText(linked.errorMessage, { exact: true }).waitFor();
  assert.equal(associatedFailure.seen.length, 2);
  await associatedFailure.clear();

  const pending = fixture.main.pendingID;
  await goFeedbackPage(pageForReceipt(pending));
  const pendingRow = feedbackRow(page, pending);
  await pendingRow.getByText('Waiting for associated error', { exact: true }).waitFor();
  assert.equal(await pendingRow.getByRole('button', { name: `Inspect associated error for feedback ${pending}`, exact: true }).isDisabled(), true);
  const unavailable = fixture.main.unavailableID;
  await goFeedbackPage(pageForReceipt(unavailable));
  const unavailableRow = feedbackRow(page, unavailable);
  await unavailableRow.getByText('Associated error unavailable', { exact: true }).waitFor();
  assert.equal(await unavailableRow.getByRole('button', { name: `Inspect associated error for feedback ${unavailable}`, exact: true }).isDisabled(), true);
  const nonError = fixture.main.nonErrorReferenceID;
  await goFeedbackPage(pageForReceipt(nonError));
  const nonErrorRow = feedbackRow(page, nonError);
  await nonErrorRow.getByText('Associated error unavailable', { exact: true }).waitFor();
  assert.equal(await nonErrorRow.getByRole('button', { name: `Inspect associated error for feedback ${nonError}`, exact: true }).isDisabled(), true);
  const noReference = fixture.main.noReferenceID;
  await goFeedbackPage(pageForReceipt(noReference));
  await feedbackRow(page, noReference).getByText('No associated error', { exact: true }).waitFor();

  // Refresh keeps a selected event and all issue-level drafts; triage refresh must not leak error policy forms.
  await goFeedbackPage(1);
  await commentLabel.fill(commentDraft);
  const assigneeSearch = page.getByLabel('Search people and teams', { exact: true });
  await assigneeSearch.fill('draft-assignee-' + fixture.runID);
  await feedbackRegion().getByRole('button', { name: 'Refresh feedback', exact: true }).focus();
  await page.keyboard.press('Enter');
  await feedbackReady(page, 61, 1);
  assert.equal((await page.evaluate(() => ({ tag: document.activeElement?.tagName, text: document.activeElement?.textContent }))).tag, 'H2');
  assert.equal(await commentLabel.inputValue(), commentDraft);
  assert.equal(await page.getByLabel('Search people and teams', { exact: true }).inputValue(), 'draft-assignee-' + fixture.runID);
  assert.equal(await page.getByLabel('Additional errors (optional)', { exact: true }).count(), 0);
  assert.equal(await page.getByLabel('Chosen release version', { exact: true }).count(), 0);

  const ignored = page.getByRole('button', { name: 'Ignore indefinitely', exact: true });
  await ignored.click();
  await page.getByRole('button', { name: 'Reopen', exact: true }).waitFor();
  assert.equal(await page.getByRole('button', { name: 'Ignore indefinitely', exact: true }).count(), 0);
  assert.equal(await commentLabel.inputValue(), commentDraft);
  await page.getByRole('button', { name: 'Reopen', exact: true }).click();
  await page.getByRole('button', { name: 'Ignore indefinitely', exact: true }).waitFor();
  await page.getByRole('button', { name: 'Resolve', exact: true }).click();
  await page.getByRole('button', { name: 'Reopen', exact: true }).waitFor();
  assert.equal(await commentLabel.inputValue(), commentDraft);
  await page.getByRole('button', { name: 'Reopen', exact: true }).click();
  await page.getByRole('button', { name: 'Resolve', exact: true }).waitFor();

  // Direct detail URL without view canonicalizes from durable category and keeps filter state.
  const direct = await context.newPage(); watch(direct);
  const directTarget = new URL(queryTarget(fixture.main.issue.id, 'issues', { query: `keep-${fixture.runID}`, status: 'all', project: String(fixture.project.id), environment: 'production', release: 'stable', sort: 'date', page: '3' }));
  directTarget.searchParams.delete('view');
  await direct.goto(directTarget.href, { waitUntil: 'networkidle' });
  await feedbackIssueReady(direct);
  const directURL = new URL(direct.url());
  assert.equal(directURL.searchParams.get('view'), 'feedback');
  for (const key of ['query', 'status', 'project', 'environment', 'release', 'sort', 'page']) assert.equal(directURL.searchParams.get(key), key === 'query' ? `keep-${fixture.runID}` : key === 'project' ? String(fixture.project.id) : key === 'page' ? '3' : key === 'status' ? 'all' : key === 'environment' ? 'production' : key === 'release' ? 'stable' : 'date');
  assert.equal(await direct.getByRole('button', { name: 'Resolve', exact: true }).count(), 1);

  // Category nav and list API stay isolated; switching view clears issue selection/page.
  const categoryRequests = [];
  await page.route('**/api/v1/organizations/**/issues?**', async route => { categoryRequests.push(new URL(route.request().url())); await route.continue(); });
  const feedbackNav = page.getByRole('link', { name: 'Feedback', exact: true });
  await feedbackNav.click();
  await page.getByRole('heading', { name: 'Feedback', exact: true }).waitFor();
  const categoryURL = new URL(page.url());
  assert.equal(categoryURL.searchParams.has('issue'), false);
  assert.ok([null, '1'].includes(categoryURL.searchParams.get('page')));
  assert.ok(categoryRequests.some(request => request.searchParams.get('category') === 'feedback'));
  for (const key of ['query', 'status', 'project', 'environment', 'release', 'sort']) assert.equal(categoryURL.searchParams.get(key), new URL(url).searchParams.get(key));
  await page.getByLabel('Search feedback', { exact: true }).fill('');
  await page.getByLabel('Environment', { exact: true }).fill('');
  await page.getByLabel('Release', { exact: true }).fill('');
  await page.getByLabel('Project', { exact: true }).selectOption(String(fixture.project.id));
  await page.getByLabel('Status', { exact: true }).selectOption('all');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await page.getByRole('link', { name: fixture.main.issue.title, exact: true }).click();
  await feedbackReady(page, 61);
  currentFeedbackPage = 1;
  assert.equal(await commentLabel.inputValue(), commentDraft);

  // Empty retained history still has a durable feedback category and an explicit empty state.
  const zeroPage = await context.newPage(); watch(zeroPage);
  await zeroPage.goto(queryTarget(fixture.main.zeroIssueID, 'issues'), { waitUntil: 'networkidle' });
  await feedbackIssueReady(zeroPage);
  await zeroPage.getByText('This issue has no stored event payloads. Comments, activity and triage remain available.', { exact: true }).waitFor();
  await feedbackRegion(zeroPage).getByText('No feedback submissions for this issue.', { exact: true }).waitFor();
  assert.equal(await feedbackRegion(zeroPage).getByRole('listitem').count(), 0);

  // Error category stays error and issue list requests carry category=error.
  const errorPage = await context.newPage(); watch(errorPage);
  const errorRequests = [];
  await errorPage.route('**/api/v1/organizations/**/issues?**', async route => { errorRequests.push(new URL(route.request().url())); await route.continue(); });
  await errorPage.goto(queryTarget(fixture.errorIssueID, 'issues'), { waitUntil: 'networkidle' });
  await errorPage.getByRole('link', { name: '← All issues', exact: true }).waitFor();
  assert.equal(new URL(errorPage.url()).searchParams.get('view'), 'issues');
  await errorPage.getByRole('link', { name: '← All issues', exact: true }).click();
  await errorPage.getByRole('heading', { name: 'Issues', exact: true }).waitFor();
  assert.ok(errorRequests.some(request => request.searchParams.get('category') === 'error'));

  // Real feedback page failure then retry keeps the issue and unsent comment.
  await goFeedbackPage(1);
  const feedbackPageURL = `**/api/v1/issues/${encodeURIComponent(fixture.main.issue.id)}/feedback?page=2`;
  const pageFailure = await routeFailOnce(feedbackPageURL, 503, { detail: 'Controlled feedback read failure' });
  await feedbackRegion().getByRole('button', { name: 'Next feedback', exact: true }).click();
  await feedbackRegion().getByRole('button', { name: 'Retry feedback', exact: true }).waitFor();
  await feedbackRegion().getByRole('button', { name: 'Retry feedback', exact: true }).click();
  await feedbackReady(page, 61, 2);
  assert.equal(await commentLabel.inputValue(), commentDraft);
  currentFeedbackPage = 2;
  assert.equal(pageFailure.seen.length, 2);
  await pageFailure.clear();

  // A missing selected JSON event is retryable; it must never substitute latest event content.
  await goFeedbackPage(3);
  const eventPattern = `**/events/${encodeURIComponent(special.eventID)}/json/`;
  const eventFailure = await routeFailOnce(eventPattern, 404, { detail: 'Not found' });
  await feedbackRow(page, special.eventID).getByRole('button', { name: `Inspect feedback submission ${special.eventID}`, exact: true }).click();
  const failedDetail = detailRegion(page, 'Feedback submission', special.eventID);
  await failedDetail.getByText('This feedback detail is no longer available. Refresh feedback to check for changes.', { exact: true }).waitFor();
  assert.equal((await failedDetail.innerText()).includes(`Feedback ${fixture.runID} submission 60`), false);
  await failedDetail.getByRole('button', { name: 'Retry feedback detail', exact: true }).click();
  await selectedSubmission(page, special.eventID);
  assert.equal(eventFailure.seen.length, 2);
  await eventFailure.clear();

  // Delayed inspection response cannot replace a different selection or a changed mode.
  const inspectionGate = await gateRealRequest(eventPattern);
  await feedbackRow(page, special.eventID).getByRole('button', { name: `Inspect feedback submission ${special.eventID}`, exact: true }).click();
  await inspectionGate.arrived;
  const nextID = fixture.main.eventIDs[10];
  await feedbackRow(page, nextID).getByRole('button', { name: `Inspect feedback submission ${nextID}`, exact: true }).click();
  await selectedSubmission(page, nextID);
  inspectionGate.release(); await inspectionGate.clear();
  await page.waitForLoadState('networkidle');
  assert.equal(await detailRegion(page, 'Feedback submission', special.eventID).count(), 0);
  assert.equal(await detailRegion(page, 'Feedback submission', nextID).count(), 1);

  const pageChangeGate = await gateRealRequest(eventPattern);
  await feedbackRow(page, special.eventID).getByRole('button', { name: `Inspect feedback submission ${special.eventID}`, exact: true }).click();
  await pageChangeGate.arrived;
  await goFeedbackPage(2);
  pageChangeGate.release(); await pageChangeGate.clear();
  await page.waitForLoadState('networkidle');
  assert.equal(await detailRegion(page, 'Feedback submission', special.eventID).count(), 0, 'Changing feedback page invalidates pending inspection');

  // Delayed page response cannot repaint after category/route change.
  await goFeedbackPage(2);
  const pageGate = await gateRealRequest('**/feedback?page=3');
  await feedbackRegion().getByRole('button', { name: 'Next feedback', exact: true }).click();
  await pageGate.arrived;
  await page.getByRole('link', { name: '← All feedback', exact: true }).click();
  await page.getByRole('heading', { name: 'Feedback', exact: true }).waitFor();
  assert.equal(new URL(page.url()).searchParams.get('view'), 'feedback');
  assert.equal(new URL(page.url()).searchParams.has('issue'), false);
  await page.locator(`a[href*="issue=${fixture.main.zeroIssueID}"]`).click();
  await feedbackIssueReady(page);
  await feedbackRegion().getByText('No feedback submissions for this issue.', { exact: true }).waitFor();
  assert.equal(new URL(page.url()).searchParams.get('issue'), fixture.main.zeroIssueID);
  pageGate.release(); await pageGate.clear();
  await page.waitForLoadState('networkidle');
  assert.equal(new URL(page.url()).searchParams.get('issue'), fixture.main.zeroIssueID, 'Late page data cannot replace another issue selected through the SPA list');

  // Dynamic module request failure/retry occurs in a fresh real browser context.
  const moduleContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await addCookies(moduleContext);
  const modulePage = await moduleContext.newPage(); watch(modulePage);
  let firstModule = true;
  const moduleRequests = [];
  await modulePage.route('**/feedback.js*', async route => {
    moduleRequests.push(route.request().url());
    if (firstModule) {
      firstModule = false;
      expectedStatuses.set(route.request().url(), 503);
      await route.fulfill({ status: 503, contentType: 'text/javascript', body: '/* controlled unavailable module */' });
    } else await route.continue();
  });
  await modulePage.goto(queryTarget(fixture.main.issue.id), { waitUntil: 'networkidle' });
  await modulePage.getByRole('button', { name: 'Retry feedback module', exact: true }).waitFor();
  await modulePage.getByLabel('Add a comment', { exact: true }).fill(commentDraft);
  await modulePage.getByRole('button', { name: 'Retry feedback module', exact: true }).click();
  await feedbackReady(modulePage, 61);
  assert.equal(moduleRequests.length, 2);
  assert.equal(new URL(moduleRequests[1]).search, '?retry=1');
  assert.equal(await modulePage.getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  await moduleContext.close();

  assert.deepEqual(evidence.errors, []);
  assert.deepEqual(evidence.console.filter(item => !item.expected), []);
  assert.deepEqual(evidence.failedRequests.filter(item => !item.expected), []);
  assert.deepEqual(evidence.unexpected.filter(item => !item.expected), []);
  assert.deepEqual(evidence.outside, []);
  evidence.checks.push('61 receipt pages (25/25/11), oldest selected submission and exact binary, linked current issue and no-fallback states, safe hostile text, triage/drafts/focus, category/direct-link behavior, empty history, retries, stale async invalidation, and 320/768/1024/1440 layouts');
  evidence.passed = true;
} catch (error) {
  failure = error;
  evidence.error = redact(error.stack || error.message);
  try { await page.screenshot({ path: resolve(output, `${phase}-failure.png`), fullPage: true }); } catch { /* retain original failure */ }
} finally {
  for (const gate of activeGates) {
    gate.release();
    await gate.clear();
  }
  await writeFile(evidencePath, JSON.stringify(evidence, null, 2), { flag: 'wx' });
  await browser.close();
}
console.log(JSON.stringify({ phase, passed: evidence.passed, widths: evidence.widths, checks: evidence.checks, error: evidence.error }));
if (failure) throw failure;










