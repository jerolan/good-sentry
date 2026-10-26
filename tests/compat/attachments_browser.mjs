// Browser owner: native event attachment panel (metadata, bytes, paging, empty
// states, truncation, hostile text, retry and detached-load guards).
// The provider matrix lives in the Go store/HTTP owners; this script drives the
// real server, real downloads and a real PostgreSQL fixture.
import assert from 'node:assert/strict';
import { execSync } from 'node:child_process';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_ATTACH_FIXTURE;
assert.ok(fixturePath, 'Provide the isolated attachment fixture');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8'));
const base = new URL(fixture.baseURL);
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/attachment-browser');
await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce', acceptDownloads: true });
const page = await context.newPage();
page.setDefaultTimeout(20000);
const screenshots = [], checks = [], pageErrors = [], requestFailures = [], badResponses = [];
const attachmentRequests = [];
let expectedBad = 0, failure;

page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('response', response => {
  if (response.status() >= 400) badResponses.push({ status: response.status(), url: response.url(), expected: expectedBad > 0 });
});
page.on('request', request => { if (new URL(request.url()).pathname.includes('/attachments')) attachmentRequests.push(new URL(request.url()).pathname + new URL(request.url()).search); });

const panel = () => page.getByRole('region', { name: 'Event attachments', exact: true });
const bodyRows = () => panel().locator('tbody tr');
const pageLabel = () => panel().getByText(/^Page \d+$/).first();
const visible = async locator => { await locator.waitFor({ state: 'visible' }); return locator; };
const until = async (read, predicate, message) => {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) { if (predicate(await read())) return; await new Promise(done => setTimeout(done, 100)); }
  assert.fail(message);
};
const issueURL = id => { const url = new URL(base); url.search = new URLSearchParams({ org: fixture.orgSlug, view: 'issues', issue: String(id), status: 'all', project: fixture.project.id }).toString(); return url.href; };

async function capture(name, anchor = panel(), checkBox = anchor === panel()) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    if (anchor) await anchor.evaluate(element => element.scrollIntoView({ block: 'start' }));
    await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, name + ' does not overflow at ' + width + 'px');
    if (checkBox) {
      const box = await anchor.evaluate(element => ({ left: element.getBoundingClientRect().left, right: element.getBoundingClientRect().right }));
      assert.ok(box.left >= -1 && box.right <= width + 1, name + ' stays inside the viewport at ' + width + 'px');
    }
    const scroller = await anchor.locator('.table-scroll').first().evaluate(element => ({ overflow: getComputedStyle(element).overflowX, tabindex: element.getAttribute('tabindex') })).catch(() => null);
    if (scroller) assert.ok(scroller.overflow === 'auto' || scroller.overflow === 'scroll', 'wide metadata scrolls inside its own container');
    const filename = resolve(output, name + '-' + width + '.png');
    await page.screenshot({ path: filename, fullPage: false, animations: 'disabled' });
    screenshots.push(filename);
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
}

try {
  // A real failed load keeps an actionable retry.
  let failOnce = true;
  const failRoute = async route => {
    if (failOnce) { failOnce = false; expectedBad += 1; await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ detail: 'Attachment read failed.' }) }); }
    else await route.continue();
  };
  await page.route(/\/attachments\?page=1$/, failRoute);
  await page.goto(issueURL(fixture.issue.id), { waitUntil: 'networkidle' });
  const alert = await visible(panel().getByRole('alert'));
  assert.match(await alert.innerText(), /Could not load this page/);
  assert.match(await alert.innerText(), /Attachment read failed/);
  await capture('load-failure', alert);
  await page.unroute(/\/attachments\?page=1$/, failRoute);
  await panel().getByRole('button', { name: 'Try again', exact: true }).click();
  await visible(bodyRows().first());
  checks.push('A real failed metadata read shows the actionable retry and recovers on Try again');

  assert.equal(await bodyRows().count(), 25, 'page one holds 25 rows');
  assert.equal(await pageLabel().innerText(), 'Page 1');
  assert.equal(await panel().getByRole('button', { name: 'Previous attachments', exact: true }).isDisabled(), true);
  assert.equal(await panel().getByRole('button', { name: 'Next attachments', exact: true }).isEnabled(), true);
  const headers = await panel().getByRole('columnheader').allInnerTexts();
  assert.deepEqual(headers, ['Name', 'Type', 'Size', 'Uploaded', 'Download']);
  const first = bodyRows().filter({ hasText: fixture.filenames[0] });
  await visible(first);
  const expected = Buffer.from(fixture.expected[fixture.filenames[0]], 'base64');
  assert.match(await first.innerText(), /application\/octet-stream/);
  assert.match(await first.innerText(), new RegExp(expected.length + ' B'));
  assert.equal(await first.locator('time[datetime]').count(), 1, 'Uploaded renders a real instant');
  await capture('page-one');
  checks.push('25 metadata rows render name, type, size, receipt instant and a per-file download control');

  // Real bytes compare with the stored PostgreSQL payload.
  const downloading = page.waitForEvent('download');
  await first.getByRole('button', { name: /^Download / }).click();
  const download = await downloading;
  assert.equal(download.suggestedFilename(), fixture.filenames[0].split('/').pop());
  const saved = await download.path();
  assert.deepEqual(await readFile(saved), expected, 'the downloaded bytes equal the stored blob');
  await visible(panel().getByText('Download started.', { exact: true }));
  checks.push('A download returns the exact stored bytes with the bounded basename');

  // A missing file keeps an actionable panel error.
  let missing = true;
  const missingRoute = async route => {
    if (missing) { missing = false; expectedBad += 1; await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ detail: 'Not found' }) }); }
    else await route.continue();
  };
  await page.route(/\/attachments\/[0-9a-f]{64}$/, missingRoute);
  await bodyRows().filter({ hasText: fixture.filenames[1] }).getByRole('button', { name: /^Download / }).click();
  const missingAlert = await visible(panel().getByRole('alert'));
  assert.match(await missingAlert.innerText(), /This attachment is no longer stored or accessible/);
  await capture('missing-file-error', missingAlert);
  await page.unroute(/\/attachments\/[0-9a-f]{64}$/, missingRoute);
  checks.push('A missing file leaves an actionable panel error and no partial download');

  await panel().getByRole('button', { name: 'Next attachments', exact: true }).click();
  await until(() => bodyRows().count(), count => count === 25, 'page two holds the next 25 rows');
  assert.equal(await pageLabel().innerText(), 'Page 2');
  assert.equal(await panel().getByRole('button', { name: 'Next attachments', exact: true }).isEnabled(), true);
  await panel().getByRole('button', { name: 'Next attachments', exact: true }).click();
  await until(() => bodyRows().count(), count => count === 1, 'page three holds the final row');
  assert.equal(await pageLabel().innerText(), 'Page 3');
  assert.equal(await panel().getByRole('button', { name: 'Next attachments', exact: true }).isDisabled(), true);
  const longRow = bodyRows().filter({ hasText: /^long-n/ });
  await visible(longRow);
  const summary = longRow.locator('summary');
  const text = await summary.innerText();
  assert.ok(Array.from(text).length <= 101, 'the compact summary is bounded');
  assert.ok(text.endsWith('…'));
  assert.equal((await summary.getAttribute('tabindex')) || '0', '0', 'the summary stays keyboard reachable');
  assert.equal(await longRow.getByText(fixture.longName, { exact: true }).isVisible(), false, 'the full name starts collapsed');
  await summary.focus(); await page.keyboard.press('Enter');
  await visible(longRow.getByText(fixture.longName, { exact: true }));
  await visible(longRow.getByText('Name or type shortened for display.', { exact: true }));
  assert.equal(await page.locator('#main img, #main script').count(), 0, 'attachment text stays inert');
  await capture('page-three-long-name', longRow);
  checks.push('A long hostile name keeps a 100-character keyboard disclosure and the truncation notice is disclosed');

  if (process.env.GOODSENTRY_ATTACH_PRUNE_PAGE2) {
    await panel().getByRole('button', { name: 'Previous attachments', exact: true }).click();
    await until(() => bodyRows().count(), count => count === 25, 'page two reloads before the files are pruned');
    execSync(process.env.GOODSENTRY_ATTACH_PRUNE_PAGE2, { stdio: 'ignore' });
    await panel().getByRole('button', { name: 'Next attachments', exact: true }).click();
    await visible(panel().getByText('No attachments on this page.', { exact: true }));
    await capture('page-three-empty');
    checks.push('A later page that lost its files shows the distinct empty-page message');
  }
  if (process.env.GOODSENTRY_ATTACH_PRUNE_ALL) {
    execSync(process.env.GOODSENTRY_ATTACH_PRUNE_ALL, { stdio: 'ignore' });
    await panel().getByRole('button', { name: 'Refresh attachments', exact: true }).click();
    await visible(panel().getByText('No attachments stored for this event.', { exact: true }));
    await capture('page-one-empty');
    checks.push('A refreshed first page after physical expiry shows the empty-event message');
  }

  await page.goto(issueURL(fixture.emptyAttachmentsIssue.id), { waitUntil: 'networkidle' });
  await visible(panel().getByText('No attachments stored for this event.', { exact: true }));
  assert.equal(await panel().getByRole('table').count(), 0);
  await capture('event-without-attachments');
  checks.push('A retained event without attachments reports the empty state and no table');

  attachmentRequests.length = 0;
  await page.goto(issueURL(fixture.noEventIssue.id), { waitUntil: 'networkidle' });
  await page.getByRole('heading', { level: 1 }).waitFor();
  assert.equal(await panel().count(), 0, 'no attachment region without a retained event');
  assert.deepEqual(attachmentRequests, [], 'no attachment request without a retained event');
  checks.push('An issue without a retained event never requests attachments');

  // A delayed load that outlives navigation never rewrites the new screen.
  let delayed = 0;
  const delayRoute = async route => { delayed += 1; await new Promise(done => setTimeout(done, 600)); await route.continue(); };
  await page.goto(issueURL(fixture.emptyAttachmentsIssue.id), { waitUntil: 'networkidle' });
  await visible(panel());
  await page.route(/\/attachments\?page=1$/, delayRoute);
  await panel().getByRole('button', { name: 'Refresh attachments', exact: true }).click();
  await page.getByRole('navigation').getByRole('link', { name: 'Issues', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
  await page.waitForTimeout(900);
  assert.equal(delayed, 1, 'the delayed load really ran');
  assert.equal(await panel().count(), 0, 'the detached panel did not render into the list');
  assert.equal(new URL(page.url()).searchParams.has('issue'), false);
  await page.unroute(/\/attachments\?page=1$/, delayRoute);
  await page.goBack({ waitUntil: 'networkidle' });
  await visible(panel().getByText('No attachments stored for this event.', { exact: true }));
  checks.push('A detached attachment load cannot replace the current route and Back restores the issue panel');

  assert.deepEqual(pageErrors, []);
  assert.deepEqual(requestFailures, []);
  assert.equal(badResponses.filter(entry => entry.expected).length, expectedBad);
  assert.deepEqual(badResponses.filter(entry => !entry.expected), []);
} catch (error) {
  failure = error;
  const filename = resolve(output, 'failure.png');
  await page.screenshot({ path: filename, fullPage: true }).catch(() => {});
  screenshots.push(filename);
} finally {
  await context.close(); await browser.close();
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ passed: !failure, fixtureType: 'real PostgreSQL fixture and real downloads', checks, screenshots, pageErrors, requestFailures, badResponses, failure: failure?.message }, null, 2));
}
if (failure) throw failure;
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));

