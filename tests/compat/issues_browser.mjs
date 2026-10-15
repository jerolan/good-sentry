// Browser owner: actual issue controls, literal tag navigation and URL/history.
// Parser, SQL predicates and authorization matrices have their own boundary tests.
// The primary supplies SDK-ingested cohorts and explicit raw pair-tag controls.
import assert from 'node:assert/strict';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_ISSUES_FIXTURE;
assert.ok(fixturePath, 'Provide the isolated private issue-browser fixture');
const metadata = await stat(fixturePath);
assert.ok(metadata.isFile() && (metadata.mode & 0o777) === 0o600, 'Use a regular mode0600 fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const { cohort, project, orgSlug } = fixture;
assert.equal(cohort.total, 26, 'This UI fixture deliberately exercises25+1 pages');
for (const order of ['date', 'freq', 'new']) assert.equal(cohort.orders[order].length, 26);
assert.equal(cohort.issueIdsByIndex.length, 26);
assert.ok(fixture.tagCases.length >= 2, 'Provide independent object and pair-tag controls');
const base = new URL(fixture.baseURL);
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/issues-browser');
await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage(); page.setDefaultTimeout(20000);
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], consoleProblems = [], badResponses = [];
let failure;
const invalidQuery = 'region:[';
const query = cohort.query;
const origin = { org: orgSlug, view: 'issues', project: project.id, query, environment: cohort.environment, release: cohort.release, status: 'all', sort: 'date' };
const route = values => { const url = new URL(base); url.search = new URLSearchParams(values).toString(); return url.href; };
const params = () => Object.fromEntries(new URL(page.url()).searchParams);
const region = () => page.getByRole('region', { name: 'Issues', exact: true });
const rows = () => region().getByRole('row').filter({ has: page.getByRole('cell') });
const expected400 = value => {
  const url = new URL(value);
  return url.pathname.endsWith(`/organizations/${encodeURIComponent(orgSlug)}/issues`) && url.searchParams.get('query') === invalidQuery;
};
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: message.text(), url: message.location().url }); });
page.on('response', response => { if (response.status() >= 400) badResponses.push({ status: response.status(), url: response.url() }); });
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
function responseForIssues() {
  return page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === new URL(`api/v1/organizations/${encodeURIComponent(orgSlug)}/issues`, base).pathname);
}
async function apply(status = 200) {
  const response = responseForIssues();
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  const result = await response;
  assert.equal(result.status(), status);
  return result;
}
async function expectPage(expectedIDs, pageNumber, total = cohort.total) {
  await visible(page.getByText(`${total} ${total === 1 ? 'issue' : 'issues'} · Page ${pageNumber}`, { exact: true }));
  assert.equal(await rows().count(), expectedIDs.length);
  const ids = await rows().getByRole('link').evaluateAll(links => links.map(link => new URL(link.href).searchParams.get('issue')));
  assert.deepEqual(ids, expectedIDs.map(String));
}
async function capture(name, anchor) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    if (anchor) await anchor.evaluate(element => element.scrollIntoView({ block: 'start' }));
    else { await page.getByRole('heading', { level: 1 }).focus(); await page.keyboard.press('Control+Home'); }
    await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, width, `${name} fits${width}px`);
    const path = resolve(output, `${name}-${width}.png`); await page.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path);
  }
}
async function expectInputs(values) {
  for (const [label, value] of Object.entries(values)) assert.equal(await page.getByLabel(label, { exact: true }).inputValue(), value, `${label} stays represented in the form`);
}

try {
  const login = await context.request.post(new URL('api/v1/login', base).href, {
    headers: { Origin: base.origin }, data: { email: fixture.ownerEmail, password: fixture.ownerPassword },
  });
  assert.equal(login.status(), 200, 'The real fixture owner signs in');
  await page.goto(route({ org: orgSlug, view: 'issues' }), { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  await expectInputs({ Status: 'unresolved', 'Sort by': 'date', Release: '' });
  await page.getByLabel('Search issues', { exact: true }).fill(query);
  await page.getByLabel('Project', { exact: true }).selectOption(project.id);
  await page.getByLabel('Status', { exact: true }).selectOption('all');
  await page.getByLabel('Environment', { exact: true }).fill(cohort.environment);
  await page.getByLabel('Release', { exact: true }).fill(cohort.release);
  await apply(); await expectPage(cohort.orders.date.slice(0, 25), 1);
  assert.equal(params().release, cohort.release); assert.equal(params().status, 'all');
  await visible(page.getByText('Environment, release, tags and text must match the same stored event. Counts, first seen and last seen cover the issue’s full history.', { exact: true }));
  const historyID = String(cohort.issueIdsByIndex[0]);
  const historyIndex = cohort.orders.date.slice(0, 25).map(String).indexOf(historyID);
  assert.ok(historyIndex >= 0, 'The independently seeded most-recent history sample appears on the first date page');
  const historyRow = rows().nth(historyIndex);
  assert.equal((await historyRow.getByRole('cell').allTextContents())[3], '2', 'Historical match keeps its lifetime two-event count');
  await capture('combined-historical-filters'); await capture('combined-historical-table', region());
  const selectedList = params(); await historyRow.getByRole('link').click();
  await visible(page.getByRole('heading', { name: 'Stack trace', exact: true }));
  assert.equal(params().release, cohort.release, 'Opening latest detail preserves the historical selector');
  const original = page.locator('details').filter({ has: page.getByText('Original event JSON', { exact: true }) });
  await original.locator('summary').click(); const event = JSON.parse(await original.locator('pre').innerText());
  assert.equal(event.environment, 'staging'); assert.equal(event.release, 'filters@2', 'The selected historical issue has a newer sample from another release');
  await page.getByRole('link', { name: '← All issues', exact: true }).click();
  await expectPage(cohort.orders.date.slice(0, 25), 1); assert.deepEqual(params(), selectedList);
  await page.getByRole('button', { name: 'Next', exact: true }).click(); await expectPage(cohort.orders.date.slice(25), 2);
  const pageTwo = params(); await page.reload({ waitUntil: 'networkidle' }); await expectPage(cohort.orders.date.slice(25), 2); assert.deepEqual(params(), pageTwo);
  await expectInputs({ Release: cohort.release, Environment: cohort.environment, 'Search issues': query, Project: project.id, Status: 'all' });
  await capture('historical-page-two');
  await page.getByRole('button', { name: 'Previous', exact: true }).click(); await expectPage(cohort.orders.date.slice(0, 25), 1);
  for (const order of ['freq', 'new']) {
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await visible(page.getByText('26 issues · Page 2', { exact: true }));
    await page.getByLabel('Sort by', { exact: true }).selectOption(order); await apply();
    await expectPage(cohort.orders[order].slice(0, 25), 1); assert.equal(params().sort, order); assert.equal(params().page, undefined);
    await page.reload({ waitUntil: 'networkidle' }); await expectPage(cohort.orders[order].slice(0, 25), 1);
  }
  checks.push('Combined tag/environment/release historical search returns26 real issues; two-event lifetime count,25+1 pages, all three orders and detail/back/reload preserve URL state');

  for (const [index, tag] of fixture.tagCases.entries()) {
    await page.goto(route({ ...origin, sort: 'new', page: '2', issue: tag.issueId }), { waitUntil: 'networkidle' });
    const tags = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Tags', exact: true }) });
    await visible(tags);
    const tagLinks = tags.getByRole('link');
    const tagIndex = (await tagLinks.allTextContents()).indexOf(tag.value || '""');
    assert.ok(tagIndex >= 0, 'The exact recorded tag value has a native search link');
    const link = tagLinks.nth(tagIndex); await visible(link);
    assert.equal(await link.innerText(), tag.value || '""', 'Recorded tag text is preserved safely');
    const term = `tags[${JSON.stringify(tag.key)}]:${JSON.stringify(tag.value)}`;
    assert.equal(new URL(await link.getAttribute('href'), base).searchParams.get('query'), term);
    assert.equal(await page.locator('#main img, #main script').count(), 0, 'Telemetry creates no executable HTML');
    await capture(`tag-${index + 1}-detail`, tags);
    await link.click(); await expectPage([String(tag.issueId)], 1, tag.expectedTotal);
    assert.equal(params().query, term); assert.equal(params().project, String(tag.projectId || project.id));
    assert.equal(params().status, 'all'); assert.equal(params().release, cohort.release); assert.equal(params().environment, cohort.environment);
    assert.equal(params().sort, 'new'); assert.equal(params().page, undefined); assert.equal(params().issue, undefined);
    await page.reload({ waitUntil: 'networkidle' }); await expectPage([String(tag.issueId)], 1, tag.expectedTotal);
    await expectInputs({ 'Search issues': term, Release: cohort.release, Status: 'all' });
    await capture(`tag-${index + 1}-exact-results`);
  }
  checks.push('SDK object tags and resolved raw pair tags navigate through explicitly quoted exact keys/values, including a literal star and HTML/quote punctuation, preserving other filters');

  await page.goto(route({ ...origin, sort: 'new' }), { waitUntil: 'networkidle' }); await expectPage(cohort.orders.new.slice(0, 25), 1);
  const spacedRelease = ` ${cohort.release} `;
  await page.getByLabel('Release', { exact: true }).fill(spacedRelease); await apply();
  await visible(page.getByRole('heading', { name: 'No matching issues', exact: true }));
  assert.equal(params().release, spacedRelease, 'Release identity is never trimmed');
  await page.reload({ waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'No matching issues', exact: true }));
  await expectInputs({ Release: spacedRelease }); await capture('exact-release-empty');
  await page.getByLabel('Release', { exact: true }).fill(cohort.release);
  const spacedEnvironment = ` ${cohort.environment} `;
  await page.getByLabel('Environment', { exact: true }).fill(spacedEnvironment);
  const environmentResponse = await apply();
  assert.equal(params().environment, spacedEnvironment, 'Environment identity is never trimmed');
  assert.equal(new URL(environmentResponse.url()).searchParams.get('environment'), spacedEnvironment, 'Native request receives the exact environment selector');
  await visible(page.getByRole('heading', { name: 'No matching issues', exact: true }));
  await page.reload({ waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'No matching issues', exact: true }));
  await expectInputs({ Environment: spacedEnvironment }); await capture('exact-environment-empty');
  await page.getByLabel('Environment', { exact: true }).fill(cohort.environment);
  const releaseInput = page.getByLabel('Release', { exact: true });
  await releaseInput.fill('🌿'.repeat(255)); assert.equal(await releaseInput.evaluate(element => element.checkValidity()), true);
  await releaseInput.fill('x'.repeat(256)); assert.equal(await releaseInput.evaluate(element => element.checkValidity()), false);
  await releaseInput.fill(cohort.release);
  await page.getByLabel('Search issues', { exact: true }).fill(invalidQuery); await apply(400);
  await visible(page.getByRole('alert')); assert.equal(params().query, invalidQuery); assert.equal(params().page, undefined);
  await expectInputs({ 'Search issues': invalidQuery, Release: cohort.release, Environment: cohort.environment, Project: project.id, Status: 'all', 'Sort by': 'new' });
  assert.equal(await region().getByRole('table').count(), 0); assert.equal(await page.getByRole('button', { name: 'Next', exact: true }).count(), 0);
  await capture('invalid-query-editable');
  const retry = responseForIssues(); await page.getByRole('button', { name: 'Try again', exact: true }).click(); assert.equal((await retry).status(), 400);
  await visible(page.getByRole('alert')); await expectInputs({ 'Search issues': invalidQuery, Release: cohort.release });
  await page.getByLabel('Search issues', { exact: true }).fill(query); await apply(); await expectPage(cohort.orders.new.slice(0, 25), 1);
  await page.goBack({ waitUntil: 'networkidle' }); await visible(page.getByRole('alert')); await expectInputs({ 'Search issues': invalidQuery, Release: cohort.release });
  await page.goForward({ waitUntil: 'networkidle' }); await expectPage(cohort.orders.new.slice(0, 25), 1);
  const beforeNavigation = params(); await page.getByRole('link', { name: 'Projects', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Projects', exact: true })); assert.equal(params().release, undefined);
  await page.goBack({ waitUntil: 'networkidle' }); await expectPage(cohort.orders.new.slice(0, 25), 1); assert.deepEqual(params(), beforeNavigation);
  await page.goto(route({ ...origin, view: 'projects', page: '2' }), { waitUntil: 'networkidle' });
  const projectRow = page.getByRole('row').filter({ has: page.getByText(project.name, { exact: true }) });
  const viewIssues = projectRow.getByRole('link', { name: 'View issues', exact: true });
  const projectHref = new URL(await viewIssues.getAttribute('href'), base);
  assert.equal(projectHref.searchParams.get('release'), null, 'A fresh project issue link clears the originating release');
  assert.equal(projectHref.searchParams.get('page'), null, 'A fresh project issue link resets the originating page');
  const projectResponse = responseForIssues(); await viewIssues.click(); const projectIssues = await projectResponse;
  assert.equal(projectIssues.status(), 200); assert.equal(new URL(projectIssues.url()).searchParams.get('release'), null);
  await visible(region().getByRole('table'));
  for (const key of ['query', 'status', 'environment', 'release', 'page']) assert.equal(params()[key], undefined);
  assert.equal(params().project, project.id); await expectInputs({ Project: project.id, Release: '', Environment: '', Status: 'unresolved' });
  await capture('project-fresh-context');
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  for (const key of ['project', 'query', 'status', 'environment', 'release', 'sort', 'page']) assert.equal(params()[key], undefined);
  await expectInputs({ Release: '', Status: 'unresolved', 'Sort by': 'date' });
  checks.push('Exact spaced release/environment yield real empty results;255 Unicode characters validate;400 keeps editable inputs, actual error, retry and browser history without fake rows; clearing/navigation and fresh project issue links reset release/page and restore defaults');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.deepEqual(badResponses.filter(response => response.status !== 400 || !expected400(response.url)), []);
  assert.deepEqual(consoleProblems.filter(problem => !(/400/.test(problem.text) && expected400(problem.url))), []);
  console.log(JSON.stringify({ ok: true, checks, screenshots: screenshots.length }, null, 2));
} catch (error) {
  failure = error; console.error(error.stack || error.message);
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: false }).catch(() => {});
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: base.href, cohortTotal: cohort.total, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
