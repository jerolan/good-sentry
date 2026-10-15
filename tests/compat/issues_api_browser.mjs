// Test-audit gate: native snapshot-to-DOM analytics, accessible UTC bucket data,
// responsive layout and preservation through actual triage/refresh own this
// browser proof. Store tests own identities/bucketing/snapshots; no SQL matrix,
// mocked response, synthetic telemetry or production-only testing seam.
import assert from 'node:assert/strict';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { isAbsolute, resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_ISSUES_API_FIXTURE, sdkReportPath = process.env.GOODSENTRY_ISSUES_API_SDK_REPORT;
assert.ok(fixturePath && isAbsolute(fixturePath) && sdkReportPath && isAbsolute(sdkReportPath), 'Provide absolute private fixture and successful SDK report');
assert.equal((await stat(fixturePath)).mode & 0o077, 0, 'Fixture must be private');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8')), sdk = JSON.parse(await readFile(sdkReportPath, 'utf8'));
assert.equal(sdk.passed, true); assert.equal(sdk.orgSlug, fixture.orgSlug);
assert.ok(sdk.projectID && sdk.issueID && Number.isInteger(sdk.userCount) && Number.isInteger(sdk.expected24hTotal), 'SDK report supplies its own issue and known identity/event totals');
const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
assert.equal(base.protocol, 'http:'); assert.equal(base.port, '18083');
assert.ok(['localhost', '127.0.0.1', '[::1]', 'host.docker.internal'].includes(base.hostname), 'Use the new isolated image runtime');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/issues-api-browser'); await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true }), context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const checks = [], screenshots = [], dimensions = [], barChecks = [], layoutChecks = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [], nativeReads = [], api0Reads = [];
const secrets = [fixture.cookieHeader, fixture.csrfToken].filter(Boolean);
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const url = path => new URL(path, base).href, issuePath = id => `api/v1/issues/${encodeURIComponent(id)}`;
let page, failure, initialStatus, restoreStatus = false;
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
const analytics = () => page.getByRole('region', { name: 'Issue event analytics', exact: true });
const summary = () => page.getByRole('region', { name: 'Issue summary', exact: true });
const issueResponse = id => page.waitForResponse(response => response.request().method() === 'GET' && response.url() === url(issuePath(id)));
function destination(id) {
  const target = new URL(base); target.search = new URLSearchParams({ org: fixture.orgSlug, view: 'issues', issue: id, project: sdk.projectID, status: 'all', environment: 'browser-filter-does-not-limit-analytics' }).toString(); return target.href;
}
async function open(id) {
  const response = issueResponse(id); await page.goto(destination(id), { waitUntil: 'networkidle' });
  const actual = await response; assert.equal(actual.status(), 200); const detail = await actual.json();
  await visible(analytics()); return detail;
}
async function checkAnalytics(detail, users, total) {
  assert.equal(detail.analytics.userCount, users);
  assert.equal(detail.analytics.stats['24h'].length, 24);
  assert.equal(detail.analytics.stats['24h'].reduce((sum, [, count]) => sum + count, 0), total);
  await page.waitForFunction(expected => document.querySelector('[aria-label="Issue event analytics"] figcaption time')?.dateTime === expected,
    new Date(detail.analytics.stats['24h'][0][0] * 1000).toISOString());
  const metric = summary().locator('dt').filter({ hasText: /^Affected users$/ }).locator('..');
  assert.equal((await metric.locator('dd').innerText()).replace(/,/g, ''), String(users));
  await visible(metric.getByText('Identified retained users', { exact: true }));
  await visible(analytics().getByText(`${total.toLocaleString('en-US')} ${total === 1 ? 'event' : 'events'}`, { exact: true }));
  await visible(analytics().getByText('All environments. Each bar is one hour; times are UTC.', { exact: true }));
  assert.equal(await analytics().getByRole('img').count(), 1);
  const rendered = await analytics().getByRole('img').evaluate(plot => ({
    tag: plot.tagName.toLowerCase(), namespace: plot.namespaceURI,
    bars: [...plot.querySelectorAll('rect')].map(rect => {
      const bounds = rect.getBoundingClientRect(), style = getComputedStyle(rect);
      return { namespace: rect.namespaceURI, width: bounds.width, height: bounds.height, fill: style.fill, opacity: Number(style.opacity), fillOpacity: Number(style.fillOpacity), visibility: style.visibility };
    }),
  }));
  assert.equal(rendered.tag, 'svg'); assert.equal(rendered.namespace, 'http://www.w3.org/2000/svg');
  assert.equal(rendered.bars.length, 24); assert.ok(rendered.bars.every(bar => bar.namespace === 'http://www.w3.org/2000/svg'));
  const painted = rendered.bars.filter(bar => bar.width > 0 && bar.height > 0 && bar.opacity > 0 && bar.fillOpacity > 0 && bar.visibility === 'visible'
    && !['none', 'transparent'].includes(bar.fill) && !/^rgba\([^)]*,\s*0(?:\.0*)?\)$/.test(bar.fill) && !/\/\s*0(?:\.0*)?%?\)$/.test(bar.fill));
  if (total > 0) assert.ok(painted.length > 0, 'A real nonempty series paints a visible bar');
  else assert.ok(rendered.bars.every(bar => bar.height === 0), 'A real zero series paints no false event bars');
  barChecks.push({ total, rectangles: rendered.bars.length, painted: painted.length, zeroHeight: rendered.bars.filter(bar => bar.height === 0).length });
  const toggle = analytics().getByText('Hourly event data', { exact: true });
  if (!(await toggle.evaluate(element => element.parentElement.open))) await toggle.click();
  const table = await visible(analytics().getByRole('table', { name: 'Hourly event counts (UTC)', exact: true }));
  assert.equal(await table.getByRole('row').count(), 25);
  const rows = await table.evaluate(element => [...element.querySelectorAll('tbody tr')].map(row => ({ times: [...row.querySelectorAll('time')].map(time => time.dateTime), count: row.lastElementChild.textContent.replace(/,/g, '') })));
  assert.deepEqual(rows, detail.analytics.stats['24h'].map(([start, count]) => ({ times: [new Date(start * 1000).toISOString(), new Date((start + 3600) * 1000).toISOString()], count: String(count) })));
  if (total === 0) await visible(analytics().getByText('No events in the last 24 hours.', { exact: true }));
  await toggle.click();
}
async function capture(name, anchor = analytics) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 }); await anchor().scrollIntoViewIfNeeded();
    await anchor().evaluate(element => element.scrollIntoView({ block: 'start' }));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `${name} fits ${width}px`); dimensions.push({ name, ...size });
    const layout = await analytics().getByRole('figure').evaluate(figure => {
      const plot = figure.querySelector('svg[role="img"]'), caption = figure.querySelector('figcaption');
      if (!plot || !caption) return null;
      const frame = figure.getBoundingClientRect(), image = plot.getBoundingClientRect(), label = caption.getBoundingClientRect();
      return { figureWidth: frame.width, widthRatio: image.width / frame.width, captionBelow: label.top >= image.bottom };
    });
    assert.ok(layout && layout.figureWidth > 0, `${name} has a visible chart at ${width}px`);
    assert.ok(layout.widthRatio >= 0.95 && layout.widthRatio <= 1.05, `${name} chart uses its figure width at ${width}px`);
    assert.equal(layout.captionBelow, true, `${name} UTC range sits below the chart at ${width}px`);
    layoutChecks.push({ name, viewport: width, ...layout });
    const file = resolve(output, `${name}-${width}.png`); await page.screenshot({ path: file, fullPage: false, animations: 'disabled' }); screenshots.push(file);
  }
}
try {
  assert.ok(fixture.cookieHeader && fixture.csrfToken);
  await context.addCookies(fixture.cookieHeader.split(/;\s*/).filter(Boolean).map(pair => { const split = pair.indexOf('='); assert.ok(split > 0); return { name: pair.slice(0, split), value: pair.slice(split + 1), url: base.href, httpOnly: true, sameSite: 'Lax' }; }));
  const session = await context.request.get(url('api/v1/me')); assert.equal(session.status(), 200);
  page = await context.newPage(); page.setDefaultTimeout(20000);
  page.on('pageerror', error => pageErrors.push(safe(error.message)));
  page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  page.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), status: response.status() }); });
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: safe(message.text()), url: message.location().url }); });
  page.on('request', request => { const path = new URL(request.url()).pathname; if (path.includes('/api/0/')) api0Reads.push(path); if (request.method() === 'GET' && path === new URL(issuePath(sdk.issueID), base).pathname) nativeReads.push(path); });
  const detail = await open(sdk.issueID); initialStatus = detail.issue.status;
  await checkAnalytics(detail, sdk.userCount, sdk.expected24hTotal);
  await capture('identified-users-summary', summary); await capture('all-environment-events');
  checks.push('Real native detail supplies identified retained users and24buckets; displayed totals, accessible table and UTC interval times match its single snapshot at320/768/1440');
  assert.equal(initialStatus, 'unresolved', 'Use the SDK-owned unresolved issue for reversible UI triage');
  const commentDraft = 'Unsent analytics browser note', releaseDraft = 'analytics / unsent release';
  await page.getByRole('region', { name: 'Comments', exact: true }).getByLabel('Add a comment', { exact: true }).fill(commentDraft);
  await page.getByRole('region', { name: 'Resolution', exact: true }).getByLabel('Chosen release version', { exact: true }).fill(releaseDraft);
  const patched = page.waitForResponse(response => response.request().method() === 'PATCH' && response.url() === url(issuePath(sdk.issueID)));
  await page.getByRole('button', { name: 'Resolve', exact: true }).click(); assert.equal((await patched).status(), 200); restoreStatus = true;
  await visible(page.getByRole('button', { name: 'Reopen', exact: true })); await checkAnalytics(detail, sdk.userCount, sdk.expected24hTotal);
  assert.equal(await page.getByRole('region', { name: 'Comments', exact: true }).getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  assert.equal(await page.getByRole('region', { name: 'Resolution', exact: true }).getByLabel('Chosen release version', { exact: true }).inputValue(), releaseDraft);
  const refreshed = issueResponse(sdk.issueID); await page.getByRole('button', { name: 'Refresh issue', exact: true }).click();
  const current = await (await refreshed).json(); await checkAnalytics(current, sdk.userCount, sdk.expected24hTotal);
  assert.equal(await page.getByRole('region', { name: 'Comments', exact: true }).getByLabel('Add a comment', { exact: true }).inputValue(), commentDraft);
  assert.equal(new URL(page.url()).searchParams.get('environment'), 'browser-filter-does-not-limit-analytics');
  const reopened = page.waitForResponse(response => response.request().method() === 'PATCH' && response.url() === url(issuePath(sdk.issueID)));
  await page.getByRole('button', { name: 'Reopen', exact: true }).click(); assert.equal((await reopened).status(), 200); restoreStatus = false;
  await visible(page.getByRole('button', { name: 'Resolve', exact: true }));
  checks.push('Real Resolve/Reopen rerenders preserve analytics and unsent drafts; Refresh issue replaces analytics from native detail while retaining navigation filters and the comment draft');
  const emptyID = sdk.emptyIssueID || fixture.emptyIssueID;
  assert.ok(emptyID, 'Provide an actual SDK-owned or approved fixture issue with zero captures in the last24h');
  const empty = await open(emptyID); await checkAnalytics(empty, empty.analytics.userCount, 0); await capture('empty-hourly-events');
  checks.push('An actual empty24-hour interval shows zero events, its real retained-user count and a zero-filled accessible series');
  assert.deepEqual(api0Reads, [], 'Native UI never joins an independently timed compatible API read');
  assert.ok(nativeReads.length >= 2); assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []); assert.deepEqual(badResponses, []); assert.deepEqual(consoleProblems, []);
} catch (error) { failure = error; console.error(safe(error.stack || error)); }
finally {
  if (restoreStatus) try { const response = await context.request.patch(url(issuePath(sdk.issueID)), { headers: { Origin: base.origin, 'X-CSRF-Token': fixture.csrfToken }, data: { status: initialStatus } }); assert.equal(response.status(), 200); } catch (error) { failure ||= error; }
  await writeFile(resolve(output, 'report.json'), `${JSON.stringify({ passed: !failure, projectID: sdk.projectID, issueID: sdk.issueID, userCount: sdk.userCount, expected24hTotal: sdk.expected24hTotal, checks, screenshots, dimensions, barChecks, layoutChecks, nativeReads, api0Reads, pageErrors, requestFailures, badResponses, consoleProblems, failure: failure && safe(failure.message), limitations: ['No simulated missing-analytics response; unavailable rendering is guarded in source.', 'Grouping/resolution concurrency and provider identity/bucket matrices belong to their existing owners.'] }, null, 2)}\n`);
  await browser.close();
}
if (failure) throw new Error(safe(failure.message));
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
