import path from 'node:path';
import fs from 'node:fs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_EXECUTABLE });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(5000);
const errors = [];
page.on('pageerror', error => errors.push(String(error)));
let result = { stage: 'starting', errors };
try {
  const login = await context.request.post(process.env.ACCOUNT_UI_BASE + '/api/v1/login', { data: { email: 'account-ui@example.test', password: 'account-ui-password' } });
  if (login.status() !== 200) throw new Error('login=' + login.status());
  await page.goto(process.env.ACCOUNT_UI_BASE + '/?view=account&org=account-ui-org', { waitUntil: 'domcontentloaded' });
  result.stage = 'account-navigation';
  await page.getByRole('heading', { name: 'Account', exact: true }).waitFor();
  const secondary = page.locator('.release-project').filter({ hasText: 'secondary@example.test' });
  const promote = secondary.getByRole('button', { name: 'Make primary', exact: true });
  if (await promote.count()) await promote.click();
  await page.locator('.account-identity').getByText('secondary@example.test', { exact: true }).waitFor();
  await page.screenshot({ path: path.resolve('test-results/acceptance-account-emails-ui-20261021/account-emails-primary-1440.png'), fullPage: true });
  const oldPrimary = page.locator('.release-project').filter({ hasText: 'account-ui@example.test' });
  await oldPrimary.getByRole('button', { name: 'Remove', exact: true }).click();
  await page.getByRole('button', { name: 'Remove email', exact: true }).click();
  await oldPrimary.waitFor({ state: 'detached' });
  await page.screenshot({ path: path.resolve('test-results/acceptance-account-emails-ui-20261021/account-emails-removed-1440.png'), fullPage: true });
  result = { stage: 'pass', errors, primaryUpdated: true, secondaryRemoved: true };
} catch (error) {
  result.error = String(error);
  result.buttons = await page.getByRole('button').allTextContents().catch(() => []);
  result.dialog = await page.locator('dialog').evaluateAll(nodes => nodes.map(node => node.outerHTML)).catch(() => []);
  await page.screenshot({ path: path.resolve('test-results/acceptance-account-emails-ui-20261021/account-emails-phase2-failure.png'), fullPage: true }).catch(() => {});
}
fs.writeFileSync('test-results/acceptance-account-emails-ui-20261021/phase2.json', JSON.stringify(result, null, 2));
await context.close();
await browser.close();
