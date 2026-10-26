import assert from 'node:assert/strict';
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18153/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const mailDir = process.env.GOODSENTRY_SMTP_OUT || '/tmp/goodsentry-smtp-messages';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/goal-browser-recovery');
await mkdir(output, { recursive: true });
const email = 'recovery-browser@example.test';
const oldPassword = 'old-recovery-password';
const newPassword = 'new-recovery-password';
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const checks = [], screenshots = [], pageErrors = [], requestFailures = [], consoleErrors = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', message => { if (message.type() === 'error') consoleErrors.push({ text: message.text(), location: message.location().url }); });

async function mailFiles() {
  return (await readdir(mailDir)).filter(name => name.endsWith('.eml')).sort();
}
async function resetTokenFromMail() {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const files = await mailFiles();
    for (const file of files.reverse()) {
      const encoded = (await readFile(resolve(mailDir, file), 'utf8')).replace(/=\r?\n/g, '');
      const raw = encoded.replace(/=([0-9A-Fa-f]{2})/g, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
      const match = raw.match(/resetToken=([0-9a-f]{64})/);
      if (match) return match[1];
    }
    await new Promise(done => setTimeout(done, 100));
  }
  throw new Error('password reset email did not arrive');
}
async function capture(name) {
  for (const width of [320, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.locator('h1').focus();
    await page.evaluate(() => new Promise(done => { scrollTo(0, 0); requestAnimationFrame(done); }));
    const dimensions = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(dimensions.document <= dimensions.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
}
async function login(password) {
  return fetch(new URL('api/v1/login', base), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password }) });
}

try {
  const state = await context.request.get(new URL('api/v1/setup', base).href);
  if ((await state.json()).required) {
    assert.equal((await context.request.post(new URL('api/v1/setup', base).href, { data: { email, password: oldPassword, name: 'Recovery browser owner', organization: `Recovery browser ${Date.now().toString(36)}` } })).status(), 201);
  } else {
    assert.equal((await login(oldPassword)).status, 200);
  }
  const filesBefore = await mailFiles();
  const requestReset = () => fetch(new URL('api/v1/account/password/reset/request', base), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email }) });
  assert.equal((await requestReset()).status, 202);
  const token = await resetTokenFromMail();
  assert.match(token, /^[0-9a-f]{64}$/);
  const filesAfter = await mailFiles();
  assert.equal(filesAfter.length, filesBefore.length + 1);
  const resetURL = new URL(`?resetToken=${token}`, base).href;
  await page.goto(resetURL, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Choose a new password', exact: true }).waitFor();
  await page.getByLabel('New password', { exact: true }).fill(newPassword);
  await page.getByLabel('Confirm new password', { exact: true }).fill(newPassword);
  await capture('recovery-reset-form');
  const resetResponse = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/api/v1/account/password/reset'));
  await page.getByRole('button', { name: 'Set new password', exact: true }).click();
  assert.equal((await resetResponse).status(), 204);
  await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
  await page.getByText('Password reset. Sign in with your new password.', { exact: true }).waitFor();
  checks.push('Real SMTP email link resets the password through the browser and returns to sign-in');
  await capture('recovery-success');
  assert.equal((await login(oldPassword)).status, 401);
  assert.equal((await login(newPassword)).status, 200);
  const replay = await fetch(new URL('api/v1/account/password/reset', base), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token, newPassword: 'replay-password' }) });
  assert.equal(replay.status, 400);
  assert.equal((await requestReset()).status, 202);
  await new Promise(done => setTimeout(done, 500));
  assert.equal((await mailFiles()).length, filesAfter.length, '60-second request floor must not emit a second email');
  checks.push('Consumed token replay is rejected and the request floor suppresses a second email');
  assert.deepEqual(pageErrors, []);
  assert.deepEqual(requestFailures.filter(item => item.error !== 'net::ERR_ABORTED'), []);
  assert.deepEqual(consoleErrors, []);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ base: base.href, checks, screenshots, pageErrors, requestFailures, consoleErrors }, null, 2));
  await browser.close();
}
