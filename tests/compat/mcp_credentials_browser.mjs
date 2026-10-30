import assert from 'node:assert/strict';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE);
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
const errors = [];

async function visit(base, expectedLogScope, showGrant, revoke, width, featureState) {
  const context = await browser.newContext({ viewport: { width, height: 900 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`${base}/?view=api-tokens`, { waitUntil: 'networkidle' });
    await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
    assert.equal(await page.getByText(/MCP: (on|off)/).count(), 0);
    await page.getByLabel('Email', { exact: true }).fill('owner@resolution.test');
    await page.getByLabel('Password', { exact: true }).fill('resolution-http-password');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByRole('heading', { name: 'API tokens', level: 1, exact: true }).waitFor();
    assert.equal(await page.getByRole('checkbox', { name: 'log:read', exact: true }).count(), expectedLogScope ? 1 : 0);
    const defaults = await page.getByRole('form', { name: 'Create API token' }).locator('input[name="scopes"]:checked').evaluateAll(inputs => inputs.map(input => input.value).sort());
    assert.deepEqual(defaults, ['org:read', 'project:read']);
    if (expectedLogScope) assert.equal(await page.getByRole('checkbox', { name: 'log:read', exact: true }).isChecked(), false);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, `page overflows at ${width}px`);
    if (showGrant) {
      const row = page.getByRole('row').filter({ hasText: 'OAuth Browser MCP client' });
      await row.getByText('OAuth Browser MCP client', { exact: true }).waitFor();
      await row.getByText('org:read', { exact: true }).waitFor();
      await row.getByText('Resolution project', { exact: false }).waitFor();
      await row.getByText('Active', { exact: true }).waitFor();
      assert.equal(await page.getByLabel('API token secret', { exact: true }).count(), 0);
      assert.equal(await page.getByText(/refresh token/i).count(), 0);
      if (revoke) {
        await row.getByRole('button', { name: 'Revoke', exact: true }).click();
        await row.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
        await page.getByRole('row').filter({ hasText: 'OAuth Browser MCP client' }).getByText('Revoked', { exact: true }).waitFor();
      }
    }
    if (featureState) {
      await page.goto(`${base}/?view=admin`, { waitUntil: 'networkidle' });
      await page.getByRole('heading', { name: 'Administration', level: 1, exact: true }).waitFor();
      await page.getByText(`MCP: ${featureState}`, { exact: true }).waitFor();
    }
  } finally {
    await context.close();
  }
}

try {
  for (const width of [320, 768, 1024, 1440]) {
    await visit(process.env.MCP_BROWSER_ENABLED, true, true, width === 1440, width, width === 1440 ? 'on' : null);
  }
  await visit(process.env.MCP_BROWSER_NO_LOGS, false, false, false, 1024);
  await visit(process.env.MCP_BROWSER_DISABLED, false, false, false, 1024, 'off');
  assert.deepEqual(errors, []);
  console.log('MCP credential interface: enabled, disabled, grant metadata and revocation passed');
} finally {
  await browser.close();
}
