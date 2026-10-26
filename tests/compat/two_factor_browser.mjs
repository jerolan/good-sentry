// Browser owner: TOTP enrollment, challenge login, recovery code and disable.
import assert from 'node:assert/strict';
import { createHmac } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18099/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL;
const password = process.env.GOODSENTRY_TEST_PASSWORD;
assert.ok(email && password, 'Provide an existing owner account without 2FA enabled');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-two-factor');
await mkdir(output, { recursive: true });

function base32Decode(value) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = 0, buffer = 0, out = [];
  for (const char of value.replace(/=+$/, '').toUpperCase()) {
    const index = alphabet.indexOf(char);
    assert.ok(index >= 0, `invalid base32 character ${char}`);
    buffer = (buffer << 5) | index; bits += 5;
    if (bits >= 8) { bits -= 8; out.push((buffer >>> bits) & 0xff); buffer &= (1 << bits) - 1; }
  }
  return Buffer.from(out);
}

function totp(secret) {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const sum = createHmac('sha1', base32Decode(secret)).update(counter).digest();
  const offset = sum[sum.length - 1] & 0x0f;
  const value = sum.readUInt32BE(offset) & 0x7fffffff;
  return String(value % 1_000_000).padStart(6, '0');
}

async function waitNextStep(step) {
  const target = (step + 1) * 30000 + 200;
  await new Promise(done => setTimeout(done, Math.max(0, target - Date.now())));
}

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const pageErrors = [], screenshots = [], checks = [], requestFailures = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));

async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.equal(size.document, size.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
}

async function signIn(emailValue, passwordValue) {
  await page.goto(base.href, { waitUntil: 'networkidle' });
  await page.getByLabel('Email', { exact: true }).fill(emailValue);
  await page.getByLabel('Password', { exact: true }).fill(passwordValue);
  const response = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/login'));
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  assert.equal((await response).status(), 200);
}

async function challenge(code) {
  await page.getByRole('heading', { name: 'Confirm it is you', exact: true }).waitFor();
  await page.getByLabel('Authentication code', { exact: true }).fill(code);
  const response = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/login/two-factor'));
  await page.getByRole('button', { name: 'Verify and sign in', exact: true }).click();
  assert.equal((await response).status(), 200);
  await page.getByRole('navigation', { name: 'Main navigation', exact: true }).waitFor();
}

try {
  await signIn(email, password);
  await page.goto(new URL('?view=account', base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Account', exact: true, level: 1 }).waitFor();
  await page.getByRole('button', { name: 'Enable two-factor authentication', exact: true }).click();
  const secret = await page.locator('.two-factor-value code').first().innerText();
  assert.match(secret, /^[A-Z2-7]{32}$/);
  const enrolledStep = Math.floor(Date.now() / 30000);
  await page.getByLabel('Authentication code', { exact: true }).fill(totp(secret));
  await page.getByRole('button', { name: 'Confirm and enable', exact: true }).click();
  await page.getByText('Two-factor authentication is on for this account.', { exact: true }).waitFor();
  const panel = page.locator('section[aria-label="Two-factor authentication"]');
  const codes = await panel.getByRole('listitem').locator('code').allTextContents();
  assert.ok(codes.length >= 10, `expected recovery codes, got ${codes.length}`);
  await capture('account-two-factor');
  checks.push('Enrollment shows the secret/otpauth, confirms a current code and displays one-time recovery codes');

  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
  await signIn(email, password);
  await waitNextStep(enrolledStep);
  await challenge(totp(secret));
  await page.goto(new URL('?view=account', base).href, { waitUntil: 'networkidle' });
  await capture('account-two-factor-enabled');
  checks.push('Password-only sign-in requires the TOTP step and opens the workspace');

  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
  await capture('signin-challenge');
  await signIn(email, password);
  await challenge(codes[0]);
  checks.push('A recovery code completes the challenge once');

  await page.goto(new URL('?view=account', base).href, { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: 'Disable two-factor authentication', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Current password', { exact: true }).fill(password);
  await dialog.getByRole('button', { name: 'Disable two-factor authentication', exact: true }).click();
  await page.getByRole('button', { name: 'Enable two-factor authentication', exact: true }).waitFor();
  checks.push('Disable requires the current password and returns the account to password-only sign-in');
  assert.deepEqual(pageErrors, []);
  const report = { ok: true, checks, screenshots, pageErrors, requestFailures };
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
