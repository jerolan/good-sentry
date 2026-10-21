// Run against an isolated installation with one owner, organization and project.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:8000/');
const email = process.env.WIZARD_EMAIL || 'wizard@example.test';
const password = process.env.WIZARD_PASSWORD || 'wizard-browser-password';
const organization = process.env.WIZARD_ORGANIZATION || 'wizard-org';
const projectID = process.env.WIZARD_PROJECT_ID || '1';
const projectSlug = process.env.WIZARD_PROJECT_SLUG || 'wizard-project';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/wizard-browser');
await mkdir(output, { recursive: true });

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(10000);
const report = { checks: [], screenshots: [], pageErrors: [] };
page.on('pageerror', error => report.pageErrors.push(error.message));

try {
  const hashResponse = await context.request.get(new URL('api/0/wizard/', base).href);
  assert.equal(hashResponse.status(), 200, 'wizard hash endpoint');
  const { hash } = await hashResponse.json();
  assert.match(hash, /^[0-9a-f]{64}$/);

  await page.goto(new URL(`account/settings/wizard/${hash}/?org_slug=${encodeURIComponent(organization)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }).waitFor();
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('heading', { name: 'Connect Sentry Wizard', exact: true }).waitFor();
  await page.getByLabel('Organization', { exact: true }).selectOption(organization);

  const desktop = resolve(output, 'wizard-connect-1440.png');
  await page.screenshot({ path: desktop, fullPage: true });
  report.screenshots.push(desktop);
  await page.setViewportSize({ width: 390, height: 1000 });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
  assert.equal(overflow, false, 'wizard page overflows at 390px');
  const mobile = resolve(output, 'wizard-connect-390.png');
  await page.screenshot({ path: mobile, fullPage: true });
  report.screenshots.push(mobile);

  const connect = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/api/0/wizard-set-token/'));
  await page.getByRole('button', { name: 'Connect sentry-wizard', exact: true }).click();
  assert.equal((await connect).status(), 200);
  await page.getByRole('heading', { name: 'Sentry Wizard is connected', exact: true }).waitFor();
  await page.getByText('The terminal should finish setup automatically.', { exact: true }).waitFor();
  report.checks.push('Browser sign-in, organization selection and wizard handoff complete at 1440 and 390');

  const payloadResponse = await context.request.get(new URL(`api/0/wizard/${hash}/`, base).href);
  assert.equal(payloadResponse.status(), 200);
  const payload = await payloadResponse.json();
  const project = payload.projects?.find(item => item.id === projectID && item.slug === projectSlug);
  assert.ok(project, 'wizard payload contains the expected project');
  assert.equal(project.organization.slug, organization);
  const dsnValue = project.keys?.[0]?.dsn?.public;
  assert.match(dsnValue, /^https?:\/\/[0-9a-f]+@/);
  assert.match(payload.apiKeys?.token, /^gs_[0-9a-f]{64}$/);
  assert.equal((await context.request.get(new URL(`api/0/organizations/${organization}/releases/`, base).href, { headers: { Authorization: `Bearer ${payload.apiKeys.token}` } })).status(), 200);

  const dsn = new URL(dsnValue);
  const envelope = '{"event_id":"abababababababababababababababab"}\n{"type":"event"}\n{"message":"Sentry Wizard DSN acceptance"}';
  const projectPath = dsn.pathname.replace(/\/+$/, '');
  const prefixPath = projectPath.slice(0, projectPath.lastIndexOf('/'));
  const dsnProject = projectPath.slice(projectPath.lastIndexOf('/') + 1);
  const endpoint = new URL(`${prefixPath}/api/${dsnProject}/envelope/?sentry_version=7&sentry_key=${encodeURIComponent(dsn.username)}`, base);
  const capture = await context.request.post(endpoint.href, { headers: { 'Content-Type': 'text/plain' }, data: envelope });
  assert.equal(capture.status(), 200);
  report.checks.push('Wizard payload exposes a working DSN and project:releases credential');

  assert.equal((await context.request.delete(new URL(`api/0/wizard/${hash}/`, base).href)).status(), 200);
  assert.equal((await context.request.get(new URL(`api/0/wizard/${hash}/`, base).href)).status(), 404);
  report.checks.push('A completed transfer is deleted and cannot be polled again');
  assert.deepEqual(report.pageErrors, []);
} catch (error) {
  report.error = String(error);
  await page.screenshot({ path: resolve(output, 'wizard-failure.png'), fullPage: true }).catch(() => {});
  throw error;
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  await context.close();
  await browser.close();
}
