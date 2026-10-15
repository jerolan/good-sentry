// Browser owner: credential form/reveal lifetime, metadata paging and role UI.
// Real native issuance and compatible GET prove that the displayed secret works.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.BASE_URL || process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18005/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL, password = process.env.GOODSENTRY_TEST_PASSWORD;
const memberEmail = process.env.COMPAT_MEMBER_EMAIL, memberPassword = process.env.COMPAT_MEMBER_PASSWORD;
assert.ok(email && password && memberEmail && memberPassword, 'Provide existing owner and member fixture credentials');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-api-tokens');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`;
const unsafeName = `Token ${run} <img src=x onerror="window.__tokenUnsafe=1">`;
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], consoleProblems = [], badResponses = [], createdIDs = [];
let owner, member, page, organization, tokenPath, validationURL, failure;

async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
function href(params) { const url = new URL(base); url.search = new URLSearchParams(params).toString(); return url.href; }
async function signedIn(loginEmail, loginPassword) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
  const response = await context.request.post(new URL('api/v1/login', base).href, { headers: { Origin: base.origin }, data: { email: loginEmail, password: loginPassword } });
  assert.equal(response.status(), 200, 'Existing fixture account signs in');
  const me = await response.json(), tab = await context.newPage(); tab.setDefaultTimeout(20000);
  tab.on('pageerror', error => pageErrors.push(error.message));
  tab.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  tab.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), method: response.request().method(), status: response.status() }); });
  tab.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: message.text(), url: message.location().url }); });
  return { context, me, tab };
}
async function mutate(method, path, data) {
  return owner.context.request.fetch(new URL(path.replace(/^\//, ''), base).href, { method, headers: { Origin: base.origin, 'X-CSRF-Token': owner.me.csrfToken }, ...(data === undefined ? {} : { data }) });
}
async function capture(name, tab = page, section) {
  for (const width of [320, 768, 1440]) {
    await tab.setViewportSize({ width, height: 1000 });
    await tab.getByRole('heading', { level: 1 }).focus(); await tab.keyboard.press('Control+Home'); await tab.mouse.click(5, 5);
    await tab.evaluate(() => new Promise(done => requestAnimationFrame(() => { scrollTo(0, 0); requestAnimationFrame(done); })));
    assert.equal(await tab.evaluate(() => scrollY), 0);
    const size = await tab.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    dimensions.push({ name, ...size }); assert.equal(size.document, size.viewport, `${name} fits ${width}px`);
    const save = async part => { const path = resolve(output, `${name}-${part}-${width}.png`); await tab.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path); };
    await save('overview');
    if (section) { await tab.getByRole('heading', { level: 2, name: section, exact: true }).evaluate(element => element.scrollIntoView({ block: 'start' })); await save('detail'); }
  }
}
async function secretAbsent(secret) {
  assert.equal(await page.getByRole('region', { name: 'New API token', exact: true }).count(), 0);
  const present = await page.evaluate(value => {
    const stored = storage => Array.from({ length: storage.length }, (_, i) => storage.getItem(storage.key(i))).some(item => item?.includes(value));
    return location.href.includes(value) || JSON.stringify(history.state)?.includes(value) || stored(localStorage) || stored(sessionStorage) || document.body.textContent.includes(value);
  }, secret);
  assert.ok(!present, 'The issued secret is absent from DOM, URL/history and browser storage');
}
const tokenRows = () => page.getByRole('region', { name: 'API token metadata', exact: true }).getByRole('row').filter({ has: page.getByRole('cell') });
const row = name => tokenRows().filter({ hasText: name });
async function createInBrowser(name) {
  const form = page.getByRole('form', { name: 'Create API token', exact: true });
  await form.getByLabel('Token name', { exact: true }).fill(name);
  const response = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === tokenPath);
  await form.getByRole('button', { name: 'Create token', exact: true }).click();
  const result = await response; assert.equal(result.status(), 201);
  const record = await result.json(); createdIDs.push(record.id);
  const reveal = await visible(page.getByRole('region', { name: 'New API token', exact: true }));
  const secret = await reveal.getByLabel('API token secret', { exact: true }).innerText();
  assert.ok(/^gs_[0-9a-f]{64}$/.test(secret), 'The one-time display contains an issued credential');
  assert.ok(secret === record.token, 'The visible secret is the credential issued by the server');
  return { record, secret, reveal };
}

try {
  owner = await signedIn(email, password); page = owner.tab; organization = owner.me.organizations[0].slug;
  tokenPath = `/api/v1/organizations/${encodeURIComponent(organization)}/api-tokens`;
  const projects = [];
  for (const suffix of ['A', 'B']) {
    const response = await mutate('POST', `/api/v1/organizations/${organization}/projects`, { name: `Credential ${suffix} ${run}`, platform: 'javascript' });
    assert.equal(response.status(), 201); projects.push(await response.json());
  }
  for (let i = 0; i < 26; i++) {
    const response = await mutate('POST', tokenPath, { name: i === 0 ? unsafeName : `Pagination ${run} #${String(i).padStart(2, '0')}` });
    assert.equal(response.status(), 201); createdIDs.push((await response.json()).id);
  }
  await page.goto(href({ org: organization, view: 'issues' }), { waitUntil: 'networkidle' });
  await page.getByRole('link', { name: 'API tokens', exact: true }).click();
  await visible(page.getByRole('heading', { name: 'API tokens', exact: true, level: 1 }));
  await visible(row(`Pagination ${run} #25`)); assert.equal(await tokenRows().count(), 25);
  await page.getByRole('button', { name: 'Next', exact: true }).click(); await visible(row(unsafeName));
  assert.equal(new URL(page.url()).searchParams.get('page'), '2');
  await page.reload({ waitUntil: 'networkidle' }); await visible(row(unsafeName));
  await visible(row(unsafeName).getByText('Active', { exact: true })); await visible(row(unsafeName).getByText('All current and future projects', { exact: true }));
  assert.equal(await row(unsafeName).locator('img').count(), 0); assert.equal(await page.evaluate(() => typeof window.__tokenUnsafe), 'undefined');
  await capture('metadata-page2', page, 'Issued tokens');
  checks.push('Owner navigation,25-row metadata pages/reload, all-current-and-future labels, active status and unsafe names render as actual text');

  await page.getByRole('button', { name: 'Previous', exact: true }).click(); await visible(row(`Pagination ${run} #25`));
  const form = page.getByRole('form', { name: 'Create API token', exact: true });
  const invalidName = `Invalid\u0007 ${run}`;
  await form.getByLabel('Token name', { exact: true }).fill(invalidName);
  await form.getByLabel('Lifetime (days)', { exact: true }).fill('17');
  await form.getByLabel('org:read', { exact: true }).uncheck();
  await form.getByLabel('Project access', { exact: true }).selectOption('selected');
  await form.getByLabel(projects[0].name, { exact: true }).check();
  validationURL = new URL(tokenPath.replace(/^\//, ''), base).href;
  const rejected = page.waitForResponse(response => response.request().method() === 'POST' && response.url() === validationURL);
  await form.getByRole('button', { name: 'Create token', exact: true }).click(); assert.equal((await rejected).status(), 400);
  await visible(form.getByRole('alert')); assert.equal(await form.getByLabel('Token name', { exact: true }).inputValue(), invalidName);
  assert.equal(await form.getByLabel('Lifetime (days)', { exact: true }).inputValue(), '17');
  assert.equal(await form.getByLabel('Project access', { exact: true }).inputValue(), 'selected');
  assert.equal(await form.getByLabel('org:read', { exact: true }).isChecked(), false); assert.equal(await form.getByLabel(projects[0].name, { exact: true }).isChecked(), true);
  const readerName = `Automation reader ${run}`, first = await createInBrowser(readerName);
  assert.deepEqual(first.record.scopes, ['project:read']); assert.equal(first.record.allProjects, false); assert.deepEqual(first.record.projects.map(project => project.id), [projects[0].id]);
  await visible(row(readerName)); await visible(row(readerName).getByText(projects[0].name, { exact: false }));
  await first.reveal.getByRole('button', { name: 'Copy token', exact: true }).click();
  const pasteTarget = page.getByRole('form', { name: 'Create API token', exact: true }).getByLabel('Token name', { exact: true });
  await pasteTarget.focus(); await page.keyboard.press('Control+V');
  assert.ok(await pasteTarget.inputValue() === first.secret, 'Copy writes the actual issued credential to the browser clipboard');
  await pasteTarget.fill('');
  const detailURL = new URL(`api/0/projects/${organization}/${projects[0].slug}/`, base).href;
  const usable = await owner.context.request.get(detailURL, { headers: { Authorization: `Bearer ${first.secret}` } });
  assert.equal(usable.status(), 200); assert.equal((await usable.json()).id, projects[0].id);
  await capture('one-time-token', page, 'Issued tokens');
  await page.getByRole('link', { name: 'Projects', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Projects', exact: true, level: 1 })); await secretAbsent(first.secret);
  await page.goBack({ waitUntil: 'networkidle' }); await visible(row(readerName)); await secretAbsent(first.secret);
  await row(readerName).getByRole('button', { name: 'Revoke', exact: true }).click();
  await row(readerName).getByRole('button', { name: 'Confirm revoke', exact: true }).click(); await visible(row(readerName).getByText('Revoked', { exact: true }));
  assert.equal(new URL(page.url()).searchParams.get('page'), '1');
  assert.equal((await owner.context.request.get(detailURL, { headers: { Authorization: `Bearer ${first.secret}` } })).status(), 401);
  await page.reload({ waitUntil: 'networkidle' }); await visible(row(readerName).getByText('Revoked', { exact: true })); await secretAbsent(first.secret);
  checks.push('Real400 retains name/lifetime/scope/project input; scoped UI credential copies correctly, consumes actual compatible project GET, disappears after leaving/Back and is rejected after visible persistent revocation');

  const dismissed = await createInBrowser(`Dismiss reader ${run}`);
  await dismissed.reveal.getByRole('button', { name: 'Dismiss token', exact: true }).click(); await secretAbsent(dismissed.secret);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click(); await visible(row(dismissed.record.name)); await secretAbsent(dismissed.secret);
  const metadata = await owner.context.request.get(new URL(tokenPath.replace(/^\//, ''), base).href);
  assert.equal(metadata.status(), 200); assert.ok(!(await metadata.text()).includes(dismissed.secret), 'Metadata never returns the secret');
  await capture('revoked-metadata', page, 'Issued tokens');
  member = await signedIn(memberEmail, memberPassword);
  await member.tab.goto(href({ org: organization, view: 'issues' }), { waitUntil: 'networkidle' });
  assert.equal(await member.tab.getByRole('link', { name: 'API tokens', exact: true }).count(), 0);
  await member.tab.goto(href({ org: organization, view: 'api-tokens' }), { waitUntil: 'networkidle' });
  await visible(member.tab.getByRole('heading', { name: 'Permission required', exact: true }));
  assert.equal(await member.tab.getByRole('form', { name: 'Create API token', exact: true }).count(), 0);
  assert.equal(await member.tab.getByRole('region', { name: 'API token metadata', exact: true }).count(), 0);
  assert.equal((await member.context.request.get(new URL(tokenPath.replace(/^\//, ''), base).href)).status(), 403);
  await capture('member-permission', member.tab);
  checks.push('Dismiss/refresh cannot retrieve the secret; real member has no credential navigation, metadata or controls and direct navigation shows permission guidance');
  assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.deepEqual(badResponses.filter(item => !(item.status === 400 && item.method === 'POST' && item.url === validationURL)), []);
  assert.deepEqual(consoleProblems.filter(item => !(item.url === validationURL && /400/.test(item.text))), []);
  console.log(JSON.stringify({ ok: true, checks, screenshots: screenshots.length }, null, 2));
} catch (error) { failure = error; console.error(error); }
finally {
  const cleanupFailures = [];
  if (owner && tokenPath) for (const id of createdIDs) {
    try { const response = await mutate('DELETE', `${tokenPath}/${encodeURIComponent(id)}`); if (response.status() !== 204) cleanupFailures.push({ id, status: response.status() }); }
    catch (_) { cleanupFailures.push({ id, status: 'request failed' }); }
  }
  if (cleanupFailures.length && !failure) failure = new Error('Browser-owned credential revocation cleanup failed');
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: base.href, checks, screenshots, dimensions, pageErrors, requestFailures, consoleProblems, badResponses, cleanupFailures, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) throw failure;
