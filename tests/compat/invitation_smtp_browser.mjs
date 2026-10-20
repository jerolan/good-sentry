// Requires an isolated initialized instance connected to the test SMTP receiver.
import assert from 'node:assert/strict';
import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18165/');
const mailDir = process.env.GOODSENTRY_SMTP_OUT;
assert.ok(mailDir, 'Provide the isolated SMTP output directory');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/invitation-smtp-browser');
await mkdir(output, { recursive: true });
const runID = Date.now().toString(36);
const email = `invitee-${runID}-${'x'.repeat(32)}@${'a'.repeat(60)}.${'b'.repeat(60)}.example.test`;
assert.ok(email.length > 120 && email.length <= 254);
const password = 'invitation-browser-password';
const browser = await chromium.launch({ headless: true });
const owner = await browser.newContext();
const invitee = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await invitee.newPage();
page.setDefaultTimeout(15000);
const report = { checks: [], screenshots: [], pageErrors: [], emailLength: email.length };
page.on('pageerror', error => report.pageErrors.push(error.message));
try {
  const login = await owner.request.post(new URL('api/v1/login', base).href, { data: {
    email: process.env.COMPAT_EMAIL || 'recovery-browser@example.test',
    password: process.env.COMPAT_PASSWORD || 'new-recovery-password',
  } });
  assert.equal(login.status(), 200);
  const me = await login.json();
  const org = me.organizations[0];
  const creation = await owner.request.post(new URL(`api/v1/organizations/${org.slug}/invitations`, base).href, {
    headers: { 'X-CSRF-Token': me.csrfToken }, data: { email, role: 'member' },
  });
  assert.equal(creation.status(), 201);
  report.invitationID = (await creation.json()).id;
  let token;
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline && !token) {
    for (const file of (await readdir(mailDir)).filter(name => name.endsWith('.eml'))) {
      const raw = await readFile(resolve(mailDir, file), 'utf8');
      if (!raw.includes(`To: ${email}\r\n`)) continue;
      const decoded = raw.replace(/=\r?\n/g, '').replace(/=([a-f0-9]{2})/gi, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
      token = decoded.match(/inviteToken=([0-9a-f]{64})/)?.[1];
    }
    if (!token) await new Promise(done => setTimeout(done, 100));
  }
  assert.ok(token, 'The running invitation worker must deliver SMTP mail for a valid long email');
  report.checks.push('Application worker delivers a real SMTP invitation for a valid email longer than 120 characters');
  await page.goto(new URL(`?inviteToken=${token}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: `Join ${org.name}`, exact: true }).waitFor();
  await page.getByLabel('Your name', { exact: true }).fill('Invited browser member');
  await page.getByLabel('Choose a password', { exact: true }).fill(password);
  for (const width of [320, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const size = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(size.document <= size.viewport, `Invitation form overflows at ${width}px`);
    const path = resolve(output, `invitation-${width}.png`);
    await page.screenshot({ path, fullPage: true });
    report.screenshots.push(path);
  }
  const response = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/api/v1/invitations/accept'));
  await page.getByRole('button', { name: 'Create account and join', exact: true }).click();
  assert.equal((await response).status(), 200);
  const joined = await (await invitee.request.get(new URL('api/v1/me', base).href)).json();
  assert.equal(joined.user.email, email);
  assert.equal(joined.organizations[0].slug, org.slug);
  assert.equal(joined.organizations[0].role, 'member');
  const replay = await invitee.request.post(new URL('api/v1/invitations/accept', base).href, { data: { token, name: 'Replay', password } });
  assert.equal(replay.status(), 400);
  report.checks.push('Email link creates a signed-in member account and rejects replay');
  assert.deepEqual(report.pageErrors, []);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  await browser.close();
}
