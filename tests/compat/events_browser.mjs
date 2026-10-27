// Test-audit gate: actual browser scope defaults/descriptions, keyboard choice,
// issued-secret usability and visible revocation own this UX proof. Provider and
// HTTP matrices cannot detect accidental UI grants or broken credential controls.
// Reuse the SDK-owned project; no mocked responses, telemetry seeding or SQL.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { isAbsolute, resolve } from 'node:path';

const fixturePath = process.env.GOODSENTRY_EVENTS_FIXTURE, sdkReportPath = process.env.GOODSENTRY_EVENTS_SDK_REPORT;
assert.ok(fixturePath && isAbsolute(fixturePath) && sdkReportPath && isAbsolute(sdkReportPath), 'Provide absolute private fixture and successful SDK report paths');
assert.equal((await stat(fixturePath)).mode & 0o077, 0, 'Fixture must be private');
const fixture = JSON.parse(await readFile(fixturePath, 'utf8')), sdkReport = JSON.parse(await readFile(sdkReportPath, 'utf8'));
assert.equal(sdkReport.passed, true); assert.equal(sdkReport.orgSlug, fixture.orgSlug);
const base = new URL(fixture.baseURL); base.pathname = `${base.pathname.replace(/\/$/, '')}/`;
assert.equal(base.protocol, 'http:'); assert.ok(['localhost', '127.0.0.1', '[::1]', 'host.docker.internal'].includes(base.hostname));
assert.ok(!['18080', '18081', '18009'].includes(base.port), 'Protected acceptance runtimes are not browser targets');
assert.equal(base.pathname, sdkReport.basePath, 'Browser uses the same configured public prefix as SDK');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/events-browser'); await mkdir(output, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true }), context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], badResponses = [], consoleProblems = [], expected = new Set(), observed = [];
const secrets = [fixture.ownerPassword, fixture.cookieHeader, fixture.csrfToken].filter(Boolean);
const safe = value => secrets.reduce((text, secret) => text.split(secret).join('[redacted]'), String(value));
const url = path => new URL(path, base).href, run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const tokenPath = `api/v1/organizations/${encodeURIComponent(fixture.orgSlug)}/api-tokens`;
let page, me, issuedID, issuedSecret, revoked = false, failure;
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function native(method, path) { return context.request.fetch(url(path), { method, headers: { Origin: base.origin, 'X-CSRF-Token': me.csrfToken } }); }
async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 }); await page.evaluate(() => scrollTo(0, 0));
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, `${name} fits ${width}px`);
    const file = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path: file, fullPage: false, animations: 'disabled', mask: [page.getByLabel('API token secret', { exact: true })] }); screenshots.push(file);
  }
}
async function consume(path, status) {
  if (status >= 400) expected.add(`GET ${url(path)} ${status}`);
  const result = await page.evaluate(async ({ target, secret }) => {
    const response = await fetch(target, { headers: { Authorization: `Bearer ${secret}`, Accept: 'application/json' } });
    return { status: response.status, body: await response.json() };
  }, { target: url(path), secret: issuedSecret });
  assert.equal(result.status, status); return result.body;
}
try {
  if (fixture.ownerEmail && fixture.ownerPassword) {
    const login = await context.request.post(url('api/v1/login'), { headers: { Origin: base.origin }, data: { email: fixture.ownerEmail, password: fixture.ownerPassword } });
    assert.equal(login.status(), 200); me = await login.json();
  } else {
    assert.ok(fixture.cookieHeader && fixture.csrfToken, 'Provide existing session credentials or login credentials');
    await context.addCookies(fixture.cookieHeader.split(/;\s*/).filter(Boolean).map(pair => {
      const split = pair.indexOf('='); assert.ok(split > 0);
      return { name: pair.slice(0, split), value: pair.slice(split + 1), url: base.href, httpOnly: true, sameSite: 'Lax' };
    }));
    const current = await context.request.get(url('api/v1/me')); assert.equal(current.status(), 200); me = await current.json();
  }
  assert.ok(me.organizations.some(value => value.slug === fixture.orgSlug));
  page = await context.newPage(); page.setDefaultTimeout(20000);
  page.on('pageerror', error => pageErrors.push(safe(error.message)));
  page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  page.on('response', response => { if (response.status() < 400) return; const key = `${response.request().method()} ${response.url()} ${response.status()}`; badResponses.push({ url: response.url(), status: response.status(), expected: expected.has(key) }); if (expected.has(key)) observed.push(key); });
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: safe(message.text()), url: message.location().url }); });
  const destination = new URL(base); destination.search = new URLSearchParams({ org: fixture.orgSlug, view: 'api-tokens' }).toString();
  await page.goto(destination.href, { waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'API tokens', exact: true, level: 1 }));
  const form = page.getByRole('form', { name: 'Create API token', exact: true });
  assert.equal(await form.getByLabel('org:read', { exact: true }).isChecked(), true);
  assert.equal(await form.getByLabel('project:read', { exact: true }).isChecked(), true);
  assert.equal(await form.getByLabel('project:releases', { exact: true }).isChecked(), false);
  assert.equal(await form.getByLabel('event:read', { exact: true }).isChecked(), false);
  assert.equal(await form.getByLabel('event:write', { exact: true }).isChecked(), false);
  const copy = await form.innerText(); assert.ok(copy.includes('project:read reads project metadata, project-scoped event data, attachments and legacy user reports.')); assert.ok(copy.includes('event:read reads issues, issue events and performance telemetry.')); assert.ok(copy.includes('event:write reads issues and issue events, changes issue status, assignment and supported mute or release policies individually or in bulk, merges issue groups, and resets future grouping.')); assert.ok(copy.includes('event:admin deletes whole issue groups and their recorded history; it does not grant read or other write permissions.'));
  await capture('scope-defaults'); checks.push('Existing org/project defaults remain checked; release/event scopes are opt-in with visible distinct purposes at320/768/1440');
  await form.getByLabel('Token name', { exact: true }).fill(`UI issue events ${run}`);
  await form.getByLabel('Lifetime (days)', { exact: true }).fill('1');
  await form.getByLabel('org:read', { exact: true }).uncheck(); await form.getByLabel('project:read', { exact: true }).uncheck();
  await form.getByLabel('event:read', { exact: true }).focus(); await page.keyboard.press('Space');
  assert.equal(await form.getByLabel('event:read', { exact: true }).isChecked(), true);
  await form.getByLabel('Project access', { exact: true }).selectOption('selected');
  await form.getByLabel(sdkReport.projectName, { exact: true }).check();
  await capture('explicit-event-scope');
  const created = page.waitForResponse(response => response.request().method() === 'POST' && response.url() === url(tokenPath));
  await form.getByRole('button', { name: 'Create token', exact: true }).click();
  const response = await created; assert.equal(response.status(), 201); const token = await response.json(); issuedID = token.id; issuedSecret = token.token; secrets.push(issuedSecret);
  assert.deepEqual(token.scopes, ['event:read']); assert.equal(token.allProjects, false); assert.deepEqual(token.projects.map(value => value.id), [sdkReport.projectID]);
  const reveal = await visible(page.getByRole('region', { name: 'New API token', exact: true }));
  assert.equal(await reveal.getByLabel('API token secret', { exact: true }).innerText(), issuedSecret);
  const issuePath = `api/0/organizations/${encodeURIComponent(fixture.orgSlug)}/issues/${sdkReport.issueID}/events/`;
  const listed = await consume(issuePath, 200); assert.deepEqual(new Set(listed.map(value => value.eventID)), new Set(sdkReport.eventIDs));
  await consume(`api/0/projects/${encodeURIComponent(fixture.orgSlug)}/${encodeURIComponent(sdkReport.projectSlug)}/events/`, 403);
  await reveal.getByRole('button', { name: 'Dismiss token', exact: true }).click();
  assert.equal(await page.getByRole('region', { name: 'New API token', exact: true }).count(), 0);
  assert.equal(await page.evaluate(secret => document.body.textContent.includes(secret) || location.href.includes(secret) || JSON.stringify(history.state)?.includes(secret), issuedSecret), false);
  checks.push('Keyboard-selected event-only UI credential grants exactly the selected SDK project, reads actual SDK issue events and cannot read project-scoped events; one-time reveal dismisses');
  const rows = page.getByRole('region', { name: 'API token metadata', exact: true }).getByRole('row');
  const row = rows.filter({ hasText: token.name }); await visible(row);
  await row.getByRole('button', { name: 'Revoke', exact: true }).click();
  const revocation = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url() === url(`${tokenPath}/${issuedID}`));
  await row.getByRole('button', { name: 'Confirm revoke', exact: true }).click(); assert.equal((await revocation).status(), 204); revoked = true;
  await visible(row.getByText('Revoked', { exact: true })); await consume(issuePath, 401);
  await page.reload({ waitUntil: 'networkidle' }); await visible(rows.filter({ hasText: token.name }).getByText('Revoked', { exact: true }));
  assert.equal(await page.getByRole('region', { name: 'New API token', exact: true }).count(), 0); await capture('event-scope-revoked');
  checks.push('Visible persistent revocation invalidates the browser-issued credential even with the live login cookie; responsive metadata and existing controls remain usable');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []); assert.deepEqual(badResponses.filter(value => !value.expected), []);
  assert.deepEqual(new Set(observed), expected);
  assert.deepEqual(consoleProblems.filter(value => ![...expected].some(key => key.includes(` ${value.url} `) && value.text.includes(key.split(' ').at(-1)))), []);
} catch (error) { failure = error; console.error(safe(error.stack || error)); }
finally {
  if (issuedID && !revoked) try { assert.equal((await native('DELETE', `${tokenPath}/${issuedID}`)).status(), 204); } catch (error) { failure ||= error; }
  await writeFile(resolve(output, 'report.json'), `${JSON.stringify({ passed: !failure, projectID: sdkReport.projectID, issueID: sdkReport.issueID, checks, screenshots, dimensions, pageErrors, requestFailures, badResponses, consoleProblems, expectedResponses: [...expected], observedExpectedResponses: observed, failure: failure && safe(failure.message) }, null, 2)}\n`);
  await browser.close();
}
if (failure) throw new Error(safe(failure.message));
console.log(JSON.stringify({ passed: true, checks: checks.length, screenshots: screenshots.length, report: resolve(output, 'report.json') }));
