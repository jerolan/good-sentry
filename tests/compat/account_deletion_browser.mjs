// Run against an isolated installation with one sole owner and one member.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:8000/');
const owner = {
  email: process.env.ACCOUNT_DELETE_OWNER_EMAIL || 'owner@account-delete.test',
  password: process.env.ACCOUNT_DELETE_OWNER_PASSWORD || 'account-delete-password',
};
const member = {
  email: process.env.ACCOUNT_DELETE_MEMBER_EMAIL || 'member@account-delete.test',
  password: process.env.ACCOUNT_DELETE_MEMBER_PASSWORD || 'account-delete-password',
};
const org = process.env.ACCOUNT_DELETE_ORG || 'account-delete-org';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/account-deletion-browser');
await mkdir(output, { recursive: true });

const browser = await chromium.launch({ headless: true });
const report = { checks: [], screenshots: [], pageErrors: [] };

async function login(context, account) {
  return context.request.post(new URL('api/v1/login', base).href, { data: account });
}

async function openAccount(context, account) {
  const response = await login(context, account);
  assert.equal(response.status(), 200, `login ${account.email}=${response.status()}`);
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on('pageerror', error => report.pageErrors.push(error.message));
  await page.goto(new URL(`?view=account&org=${encodeURIComponent(org)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Account', exact: true, level: 1 }).waitFor();
  return page;
}

async function openDeleteDialog(page) {
  const control = page.getByRole('button', { name: 'Delete account', exact: true });
  await control.click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('heading', { name: 'Delete account?', exact: true }).waitFor();
  return { control, dialog };
}

try {
  const ownerContext = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
  try {
    const page = await openAccount(ownerContext, owner);
    const { dialog } = await openDeleteDialog(page);
    const confirm = dialog.getByRole('button', { name: 'Delete account permanently', exact: true });
    assert.equal(await confirm.isDisabled(), true);
    await dialog.getByLabel('Type your email address', { exact: true }).fill(owner.email);
    assert.equal(await confirm.isEnabled(), true);
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await dialog.waitFor({ state: 'detached' });
    assert.equal((await ownerContext.request.get(new URL('api/v1/me', base).href)).status(), 200);
    report.checks.push('Cancel and typed confirmation keep the account available');

    await openDeleteDialog(page);
    const responsePromise = page.waitForResponse(response => response.request().method() === 'DELETE' && new URL(response.url()).pathname.endsWith('/api/0/users/me/'));
    await page.getByRole('dialog').getByLabel('Type your email address', { exact: true }).fill(owner.email);
    await page.getByRole('dialog').getByRole('button', { name: 'Delete account permanently', exact: true }).click();
    assert.equal((await responsePromise).status(), 409);
    await page.getByRole('dialog').getByRole('alert').filter({ hasText: 'must retain at least one owner' }).waitFor();
    assert.equal((await ownerContext.request.get(new URL('api/v1/me', base).href)).status(), 200);
    const ownerShot = resolve(output, 'account-delete-owner-blocked-1440.png');
    await page.screenshot({ path: ownerShot, fullPage: true });
    report.screenshots.push(ownerShot);
    report.checks.push('Sole owner receives 409, keeps the dialog and remains signed in');
  } finally {
    await ownerContext.close();
  }

  const memberContext = await browser.newContext({ viewport: { width: 390, height: 1000 }, reducedMotion: 'reduce' });
  try {
    const page = await openAccount(memberContext, member);
    const { dialog } = await openDeleteDialog(page);
    await dialog.getByLabel('Type your email address', { exact: true }).fill(member.email);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
    assert.equal(overflow, false, 'Delete account dialog overflows at 390px');
    const dialogShot = resolve(output, 'account-delete-dialog-390.png');
    await page.screenshot({ path: dialogShot, fullPage: true });
    report.screenshots.push(dialogShot);
    const responsePromise = page.waitForResponse(response => response.request().method() === 'DELETE' && new URL(response.url()).pathname.endsWith('/api/0/users/me/'));
    await dialog.getByRole('button', { name: 'Delete account permanently', exact: true }).click();
    assert.equal((await responsePromise).status(), 204);
    await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
    await page.getByText('Your account has been deleted.', { exact: true }).waitFor();
    assert.equal((await memberContext.request.get(new URL('api/v1/me', base).href)).status(), 401);
    assert.equal((await login(memberContext, member)).status(), 401);
    report.checks.push('Member deletion clears the session and prevents a later sign-in');
    assert.deepEqual(report.pageErrors, []);
  } finally {
    await memberContext.close();
  }
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  await browser.close();
}
