import path from 'node:path';
import fs from 'node:fs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE });
const context = await browser.newContext({ viewport: { width: 390, height: 1000 }, reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(5000);
const errors = [];
page.on('pageerror', error => errors.push(String(error)));
let result = { stage: 'starting', errors };
try {
  const login = await context.request.post(process.env.ACCOUNT_UI_BASE + '/api/v1/login', { data: { email: 'secondary@example.test', password: 'account-ui-password' } });
  if (login.status() !== 200) throw new Error('login=' + login.status());
  await page.goto(process.env.ACCOUNT_UI_BASE + '/?view=account&org=account-ui-org', { waitUntil: 'domcontentloaded' });
  await page.getByRole('heading', { name: 'Account', exact: true }).waitFor();
  await page.getByLabel('New email address', { exact: true }).fill('mobile-pending@example.test');
  await page.getByRole('button', { name: 'Add email', exact: true }).click();
  await page.getByText('mobile-pending@example.test', { exact: true }).waitFor();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  if (overflow) throw new Error('horizontal overflow');
  const skipVisible = await page.locator('.skip-link').isVisible();
  if (skipVisible) throw new Error('skip link visible without keyboard focus');
  await page.screenshot({ path: path.resolve('test-results/acceptance-account-emails-ui-20261021/account-emails-mobile-390.png'), fullPage: true });
  result = { stage: 'pass', errors, pendingVisible: true, overflow: false, skipLinkHidden: true };
} catch (error) {
  result.error = String(error);
  await page.screenshot({ path: path.resolve('test-results/acceptance-account-emails-ui-20261021/account-emails-mobile-failure.png'), fullPage: true }).catch(() => {});
}
fs.writeFileSync('test-results/acceptance-account-emails-ui-20261021/phase3.json', JSON.stringify(result, null, 2));
await context.close();
await browser.close();
