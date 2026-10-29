import assert from 'node:assert/strict';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
const errors = [];

async function visit(base, expectedLogScope, revoke) {
  const context = await browser.newContext({ viewport: { width: 1024, height: 900 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`${base}/?view=api-tokens`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
    await page.getByLabel('Email', { exact: true }).fill('owner@resolution.test');
    await page.getByLabel('Password', { exact: true }).fill('resolution-http-password');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByRole('heading', { name: 'API tokens', level: 1, exact: true }).waitFor();
    assert.equal(await page.getByRole('checkbox', { name: 'log:read', exact: true }).count(), expectedLogScope ? 1 : 0);
    if (revoke) {
      const row = page.getByRole('row').filter({ hasText: 'OAuth Browser MCP client' });
      await row.getByText('OAuth Browser MCP client', { exact: true }).waitFor();
      await row.getByText('org:read', { exact: true }).waitFor();
      await row.getByText('Resolution project', { exact: false }).waitFor();
      await row.getByText('Active', { exact: true }).waitFor();
      assert.equal(await page.getByLabel('API token secret', { exact: true }).count(), 0);
      assert.equal(await page.getByText(/refresh token/i).count(), 0);
      await row.getByRole('button', { name: 'Revoke', exact: true }).click();
      await row.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
      await page.getByRole('row').filter({ hasText: 'OAuth Browser MCP client' }).getByText('Revoked', { exact: true }).waitFor();
    }
  } finally {
    await context.close();
  }
}

try {
  await visit(process.env.MCP_BROWSER_ENABLED, true, true);
  await visit(process.env.MCP_BROWSER_NO_LOGS, false, false);
  await visit(process.env.MCP_BROWSER_DISABLED, false, false);
  assert.deepEqual(errors, []);
  console.log('MCP credential interface: enabled, disabled, grant metadata and revocation passed');
} finally {
  await browser.close();
}
