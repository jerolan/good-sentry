// Chromium acceptance for a real sentry-go issue on a disposable GoodSentry.
// Required: GOODSENTRY_ISSUE_ID. Optional: GOODSENTRY_UI_URL,
// GOODSENTRY_ORGANIZATION, GOODSENTRY_UI_EXPECT (comma-separated),
// GOODSENTRY_UI_WIDTHS, GOODSENTRY_UI_OUTPUT and PLAYWRIGHT_MODULE.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.GOODSENTRY_UI_URL || 'http://127.0.0.1:18092';
const organization = process.env.GOODSENTRY_ORGANIZATION || 'default-organization';
const issueID = process.env.GOODSENTRY_ISSUE_ID;
const expected = (process.env.GOODSENTRY_UI_EXPECT || '').split(',').map(value => value.trim()).filter(Boolean);
const widths = (process.env.GOODSENTRY_UI_WIDTHS || '320,768,1440').split(',').map(value => Number(value.trim())).filter(value => Number.isInteger(value) && value > 0);
const output = resolve(process.env.GOODSENTRY_UI_OUTPUT || 'test-results/errors-go-ui');
assert.ok(issueID, 'GOODSENTRY_ISSUE_ID is required');
assert.ok(widths.length, 'GOODSENTRY_UI_WIDTHS must contain positive integers');

const url = `${base}/?view=issues&issue=${encodeURIComponent(issueID)}&org=${encodeURIComponent(organization)}`;
const report = { passed: false, url, issueID, expected, widths: [], errors: [], failedRequests: [] };

await mkdir(output, { recursive: true });
const browser = await chromium.launch({ args: ['--no-sandbox'] });
try {
  for (const width of widths) {
    const context = await browser.newContext({ viewport: { width, height: 900 } });
    const page = await context.newPage();
    page.on('pageerror', error => report.errors.push({ width, error: String(error) }));
    page.on('requestfailed', request => report.failedRequests.push({ width, url: request.url(), error: request.failure()?.errorText }));
    const response = await page.goto(url, { waitUntil: 'networkidle' });
    assert.equal(response?.status(), 200, `page status at ${width}`);
    if (expected.length) await page.getByText(expected[0], { exact: false }).first().waitFor({ timeout: 15000 });
    const body = await page.locator('body').innerText();
    for (const value of expected) assert.ok(body.includes(value), `UI at ${width} includes ${value}`);
    const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    assert.ok(documentWidth <= width, `no horizontal overflow at ${width}: ${documentWidth}`);
    const screenshot = resolve(output, `issue-${width}.png`);
    await page.screenshot({ path: screenshot, fullPage: true });
    report.widths.push({ width, documentWidth, screenshot });
    await context.close();
  }
  report.passed = true;
} finally {
  await browser.close();
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
}

assert.deepEqual(report.errors, [], 'no page errors');
assert.deepEqual(report.failedRequests, [], 'no failed requests');
console.log(JSON.stringify(report));
