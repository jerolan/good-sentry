import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18152/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/people-confirmation');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH || undefined });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
const page = await context.newPage();
if (process.env.GOODSENTRY_PEOPLE_JS) {
  const peopleSource = await readFile(process.env.GOODSENTRY_PEOPLE_JS);
  await page.route('**/people.js*', route => route.fulfill({ status: 200, contentType: 'application/javascript', body: peopleSource }));
}
page.setDefaultTimeout(5000);
const checks = [], pageErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));
const email = 'confirmation-owner@example.test', password = 'confirmation-owner-password';
async function captureResponsive(name) {
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${name} overflows at ${width}`);
    await page.screenshot({ path: resolve(output, `${name}-${width}.png`), fullPage: true, animations: 'disabled' });
  }
}
try {
  const setup = await context.request.post(new URL('api/v1/setup', base).href, {
    headers: { Origin: base.origin }, data: { email, password, name: 'Confirmation owner', organization: 'Confirmation fixture' },
  });
  assert.equal(setup.status(), 201, 'This test requires a fresh disposable installation');
  const { organizations } = await setup.json();
  const org = organizations[0].slug;
  assert.ok(process.env.GOODSENTRY_TEST_PG, 'This test requires a disposable PostgreSQL container');
  execFileSync('docker', ['exec', process.env.GOODSENTRY_TEST_PG, 'psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1',
    '-U', 'goodsentry', '-d', 'goodsentry', '-c', `
      INSERT INTO users(email,name,password_hash) VALUES('confirmation-member@example.test','Confirmation member',decode('00','hex'));
      INSERT INTO memberships(organization_id,user_id,role)
        SELECT o.id,u.id,'member' FROM organizations o CROSS JOIN users u WHERE u.email='confirmation-member@example.test';
    `]);
  await page.goto(new URL(`?view=people&org=${encodeURIComponent(org)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'People and teams', exact: true }).waitFor();
  const owner = page.locator('tr').filter({ hasText: email });
  await owner.getByLabel('Role for Confirmation owner').selectOption('member');
  const lastOwnerResponse = page.waitForResponse(r => r.request().method() === 'PATCH' && /\/members\/\d+$/.test(r.url()));
  await owner.getByRole('button', { name: 'Save role' }).click();
  assert.equal((await lastOwnerResponse).status(), 409);
  const lastOwnerError = page.getByRole('alert').filter({ hasText: 'An organization must retain at least one owner' });
  await lastOwnerError.waitFor();
  assert.equal(await lastOwnerError.evaluate(e => document.activeElement === e), true);
  checks.push('Last-owner error is displayed and focused');

  const teamName = page.getByLabel('Team name', { exact: true }).first();
  await teamName.fill('Unsaved team draft');
  const ownerRemove = owner.getByRole('button', { name: 'Remove', exact: true });
  let organizationDeletes = 0;
  page.on('request', request => { if (request.method() === 'DELETE' && /\/organizations\/[^/]+\/members\/\d+$/.test(request.url())) organizationDeletes++; });
  await ownerRemove.focus();
  await page.keyboard.press('Enter');
  const ownerWarning = page.locator('tr').filter({ hasText: 'Remove Confirmation owner from this organization?' });
  await ownerWarning.waitFor();
  assert.equal(await lastOwnerError.isVisible(), false);
  assert.match(await ownerWarning.innerText(), /tokens, assignments and team memberships/);
  const ownerCancel = ownerWarning.getByRole('button', { name: 'Cancel', exact: true });
  assert.equal(await ownerCancel.evaluate(e => document.activeElement === e), true);
  await captureResponsive('organization-confirm');
  await ownerCancel.click();
  assert.equal(organizationDeletes, 0);
  assert.equal(await ownerRemove.evaluate(e => document.activeElement === e), true);
  assert.equal(await teamName.inputValue(), 'Unsaved team draft');
  await ownerRemove.click();
  let allowOwnerFailure, sawOwnerRequest;
  const ownerHeld = new Promise(resolve => { allowOwnerFailure = resolve; });
  const ownerIntercepted = new Promise(resolve => { sawOwnerRequest = resolve; });
  const ownerPattern = '**/api/v1/organizations/*/members/*';
  await page.route(ownerPattern, async route => {
    if (route.request().method() !== 'DELETE') return route.continue();
    sawOwnerRequest();
    await ownerHeld;
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ detail: 'Storage is temporarily unavailable' }) });
  });
  await ownerWarning.getByRole('button', { name: 'Confirm removal', exact: true }).click();
  await ownerIntercepted;
  assert.equal(await ownerWarning.getByRole('button', { name: 'Confirm removal', exact: true }).isDisabled(), true);
  assert.equal(await ownerWarning.getByRole('button', { name: 'Cancel', exact: true }).isDisabled(), true);
  assert.equal(await page.getByRole('button', { name: 'Refresh', exact: true }).isDisabled(), true);
  allowOwnerFailure();
  const ownerFailure = ownerWarning.getByRole('alert').filter({ hasText: 'Storage is temporarily unavailable' });
  await ownerFailure.waitFor();
  assert.equal(await ownerFailure.evaluate(e => document.activeElement === e), true);
  await ownerWarning.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(await ownerRemove.evaluate(e => document.activeElement === e), true);
  assert.equal(await teamName.inputValue(), 'Unsaved team draft');
  await page.unroute(ownerPattern);
  checks.push('Organization removal explains consequences; cancel and failure preserve focus and drafts; pending controls are disabled');
  const otherMember = page.locator('tr').filter({ hasText: 'confirmation-member@example.test' });
  await otherMember.getByRole('button', { name: 'Remove', exact: true }).click();
  const otherWarning = page.locator('tr').filter({ hasText: 'Remove Confirmation member from this organization?' });
  const otherRemoved = page.waitForResponse(r => r.request().method() === 'DELETE' && /\/organizations\/[^/]+\/members\/\d+$/.test(r.url()));
  await otherWarning.getByRole('button', { name: 'Confirm removal', exact: true }).click();
  assert.equal((await otherRemoved).status(), 204);
  await page.getByText('confirmation-member@example.test', { exact: true }).waitFor({ state: 'detached' });
  await page.getByText(email, { exact: true }).first().waitFor();
  checks.push('Confirmed organization removal reaches the real API and removes only the selected member');

  await page.getByLabel('Team name', { exact: true }).first().fill('Incident team');
  let allowCreate, sawCreate;
  const createHeld = new Promise(resolve => { allowCreate = resolve; });
  const createIntercepted = new Promise(resolve => { sawCreate = resolve; });
  const createPattern = '**/api/v1/organizations/*/teams';
  await page.route(createPattern, async route => {
    if (route.request().method() !== 'POST') return route.continue();
    sawCreate();
    await createHeld;
    await route.continue();
  });
  let created = page.waitForResponse(r => r.request().method() === 'POST' && /\/teams$/.test(r.url()));
  await page.getByRole('button', { name: 'Create team' }).click();
  await createIntercepted;
  assert.equal(await ownerRemove.isDisabled(), true);
  assert.equal(await page.getByRole('button', { name: 'Refresh', exact: true }).isDisabled(), true);
  allowCreate();
  assert.equal((await created).status(), 201);
  await page.unroute(createPattern);
  const card = page.locator('article').filter({ hasText: 'Incident team' });
  await card.getByRole('heading', { name: 'Incident team' }).waitFor();
  await page.getByLabel('Team name', { exact: true }).first().fill('Incident team');
  const conflictResponse = page.waitForResponse(r => r.request().method() === 'POST' && /\/teams$/.test(r.url()));
  await page.getByRole('button', { name: 'Create team' }).click();
  assert.equal((await conflictResponse).status(), 409);
  const conflict = page.getByRole('alert').filter({ hasText: 'Resource already exists' });
  await conflict.waitFor();
  assert.equal(await conflict.evaluate(e => document.activeElement === e), true);
  checks.push('Team conflict is displayed and focused');

  await card.getByLabel('Member to add').selectOption({ label: `Confirmation owner · ${email}` });
  const added = page.waitForResponse(r => r.request().method() === 'POST' && /\/members\/\d+$/.test(r.url()));
  await card.getByRole('button', { name: 'Add member' }).click();
  assert.equal((await added).status(), 204);
  const teamMember = card.locator('tr').filter({ hasText: email });
  await teamMember.getByRole('button', { name: 'Remove', exact: true }).waitFor();
  let deletes = 0;
  page.on('request', request => { if (request.method() === 'DELETE' && /\/teams\/\d+\/members\/\d+$/.test(request.url())) deletes++; });
  const remove = teamMember.getByRole('button', { name: 'Remove', exact: true });
  await remove.focus();
  await page.keyboard.press('Enter');
  const confirm = teamMember.getByRole('button', { name: 'Confirm removal', exact: true });
  await confirm.waitFor();
  assert.equal(deletes, 0, 'Opening confirmation must not remove the membership');
  assert.match(await teamMember.innerText(), /team/i);
  assert.equal(await teamMember.getByRole('button', { name: 'Cancel', exact: true }).evaluate(e => document.activeElement === e), true);
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Page overflows at ${width}`);
    assert.ok(await confirm.evaluate(e => { const r = e.getBoundingClientRect(); return r.left >= 0 && r.right <= innerWidth; }), `Confirmation is clipped at ${width}`);
    await page.screenshot({ path: resolve(output, `team-confirm-${width}.png`), fullPage: true, animations: 'disabled' });
  }
  await teamMember.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(deletes, 0, 'Cancel must preserve the membership');
  assert.equal(await remove.evaluate(e => document.activeElement === e), true);
  await teamMember.getByRole('button', { name: 'Remove', exact: true }).click();
  let allowFailure, observed;
  const held = new Promise(resolve => { allowFailure = resolve; });
  const intercepted = new Promise(resolve => { observed = resolve; });
  const routePattern = '**/api/v1/organizations/*/teams/*/members/*';
  await page.route(routePattern, async route => {
    if (route.request().method() !== 'DELETE') return route.continue();
    observed();
    await held;
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ detail: 'Storage is temporarily unavailable' }) });
  });
  await teamMember.getByRole('button', { name: 'Confirm removal', exact: true }).click();
  await intercepted;
  assert.equal(await teamMember.getByRole('button', { name: 'Confirm removal', exact: true }).isDisabled(), true);
  assert.equal(await teamMember.getByRole('button', { name: 'Cancel', exact: true }).isDisabled(), true);
  assert.equal(await page.getByRole('button', { name: 'Refresh', exact: true }).isDisabled(), true);
  allowFailure();
  const failure = card.getByRole('alert').filter({ hasText: 'Storage is temporarily unavailable' });
  await failure.waitFor();
  assert.equal(await failure.evaluate(e => document.activeElement === e), true);
  await teamMember.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(await failure.isVisible(), false);
  assert.equal(await remove.evaluate(e => document.activeElement === e), true);
  await page.unroute(routePattern);
  await teamMember.getByRole('button', { name: 'Remove', exact: true }).click();
  const removed = page.waitForResponse(r => r.request().method() === 'DELETE' && /\/teams\/\d+\/members\/\d+$/.test(r.url()));
  await teamMember.getByRole('button', { name: 'Confirm removal', exact: true }).click();
  assert.equal((await removed).status(), 204);
  assert.equal(deletes, 2);
  await card.getByText('This team has no members.').waitFor();
  checks.push('Team removal requires confirmation, supports cancel and error recovery, prevents cancel while pending, and works with keyboard and responsive layouts');

  const rename = card.getByLabel('Team name', { exact: true });
  await rename.fill('Unsaved rename');
  const deleteTeam = card.getByRole('button', { name: 'Delete team', exact: true });
  let teamDeletes = 0;
  page.on('request', request => { if (request.method() === 'DELETE' && /\/teams\/\d+$/.test(request.url())) teamDeletes++; });
  await deleteTeam.focus();
  await page.keyboard.press('Enter');
  const teamWarning = card.locator('.people-team-detail').filter({ hasText: 'Delete this team?' });
  await teamWarning.waitFor();
  assert.match(await teamWarning.innerText(), /memberships/i);
  assert.match(await teamWarning.innerText(), /project associations/i);
  const teamCancel = teamWarning.getByRole('button', { name: 'Cancel', exact: true });
  assert.equal(await teamCancel.evaluate(e => document.activeElement === e), true);
  await captureResponsive('team-delete-confirm');
  await teamCancel.click();
  assert.equal(teamDeletes, 0);
  assert.equal(await deleteTeam.evaluate(e => document.activeElement === e), true);
  assert.equal(await rename.inputValue(), 'Unsaved rename');
  await deleteTeam.click();
  let allowTeamFailure, sawTeamRequest;
  const teamHeld = new Promise(resolve => { allowTeamFailure = resolve; });
  const teamIntercepted = new Promise(resolve => { sawTeamRequest = resolve; });
  const teamPattern = '**/api/v1/organizations/*/teams/*';
  await page.route(teamPattern, async route => {
    if (route.request().method() !== 'DELETE') return route.continue();
    sawTeamRequest();
    await teamHeld;
    await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ detail: 'Storage is temporarily unavailable' }) });
  });
  await teamWarning.getByRole('button', { name: 'Confirm deletion', exact: true }).click();
  await teamIntercepted;
  assert.equal(await teamWarning.getByRole('button', { name: 'Confirm deletion', exact: true }).isDisabled(), true);
  assert.equal(await teamWarning.getByRole('button', { name: 'Cancel', exact: true }).isDisabled(), true);
  assert.equal(await page.getByRole('button', { name: 'Refresh', exact: true }).isDisabled(), true);
  allowTeamFailure();
  const teamFailure = teamWarning.getByRole('alert').filter({ hasText: 'Storage is temporarily unavailable' });
  await teamFailure.waitFor();
  assert.equal(await teamFailure.evaluate(e => document.activeElement === e), true);
  await teamWarning.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal(await deleteTeam.evaluate(e => document.activeElement === e), true);
  assert.equal(await rename.inputValue(), 'Unsaved rename');
  await page.unroute(teamPattern);
  await deleteTeam.click();
  const teamDeleted = page.waitForResponse(r => r.request().method() === 'DELETE' && /\/teams\/\d+$/.test(r.url()));
  await teamWarning.getByRole('button', { name: 'Confirm deletion', exact: true }).click();
  assert.equal((await teamDeleted).status(), 204);
  assert.equal(teamDeletes, 2);
  await page.getByRole('heading', { name: 'Incident team', exact: true }).waitFor({ state: 'detached' });
  checks.push('Team deletion explains membership and project effects, preserves focus and drafts, handles errors and pending state, and deletes only after confirmation');
  assert.deepEqual(pageErrors, []);
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ checks, pageErrors, peopleSourceOverlay: Boolean(process.env.GOODSENTRY_PEOPLE_JS) }, null, 2) + '\n');
} finally {
  await browser.close();
}
