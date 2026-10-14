// Browser contract: onboarding, DSN connection and issue workflow must work against
// the real server. API tests cannot detect broken assets, CSP, focus or layout.
// This harness uses public HTTP/browser boundaries and adds no production seam.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseURL = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18001/');
if (!baseURL.pathname.endsWith('/')) baseURL.pathname += '/';
const runID = Date.now().toString(36);
const email = process.env.GOODSENTRY_TEST_EMAIL || `browser-${runID}@example.invalid`;
const password = process.env.GOODSENTRY_TEST_PASSWORD || randomBytes(24).toString('hex');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
const consoleProblems = [];
const pageErrors = [];
const requestFailures = [];
const checks = [];
const screenshots = [];
let expectedAuthProbe = false;
page.on('console', message => {
  if (['warning', 'error'].includes(message.type())) consoleProblems.push({ type: message.type(), text: message.text(), location: message.location().url });
});
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));

async function visible(locator) {
  await locator.waitFor({ state: 'visible' });
  return locator;
}

async function capture(name, width) {
  await page.setViewportSize({ width, height: 1000 });
  const measurements = await page.evaluate(() => ({ width: innerWidth, documentWidth: document.documentElement.scrollWidth }));
  assert.ok(measurements.documentWidth <= measurements.width, `${name}: document overflows at ${width}px (${measurements.documentWidth}px)`);
  const path = resolve(output, `${name}-${width}.png`);
  await page.screenshot({ path, fullPage: true, animations: 'disabled' });
  screenshots.push(path);
}

async function signIn() {
  await visible(page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }));
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  const response = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/login'));
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  assert.equal((await response).status(), 200, 'Password login succeeds');
  await visible(page.getByRole('navigation', { name: 'Main navigation', exact: true }));
  await page.getByRole('link', { name: 'Issues', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
}

let failure;
try {
  const setupResponse = await context.request.get(new URL('api/v1/setup', baseURL).href);
  assert.equal(setupResponse.status(), 200, 'Setup discovery responds');
  const setup = await setupResponse.json();
  if (!setup.required && (!process.env.GOODSENTRY_TEST_EMAIL || !process.env.GOODSENTRY_TEST_PASSWORD)) {
    throw new Error('The server is already initialized. Use an isolated empty database or supply GOODSENTRY_TEST_EMAIL and GOODSENTRY_TEST_PASSWORD.');
  }
  expectedAuthProbe = !setup.required;
  await page.goto(baseURL.href, { waitUntil: 'networkidle' });
  if (setup.required) {
    await visible(page.getByRole('heading', { name: 'Welcome to GoodSentry', exact: true }));
    for (const width of [320, 768, 1440]) await capture('onboarding', width);
    await page.getByLabel('Your name', { exact: true }).fill('Browser verifier');
    await page.getByLabel('Organization name', { exact: true }).fill(`Browser workspace ${runID}`);
    await page.getByLabel('Email', { exact: true }).fill(email);
    await page.getByLabel('Password', { exact: true }).fill(password);
    const response = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/setup'));
    await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
    assert.equal((await response).status(), 201, 'Owner setup succeeds');
    await visible(page.getByRole('heading', { level: 1, name: 'Issues', exact: true }));
    checks.push('First owner and workspace created through browser onboarding');
  } else {
    await signIn();
    checks.push('Existing owner signed in through browser');
  }

  await page.getByRole('button', { name: 'Create project', exact: true }).first().click();
  const dialog = await visible(page.getByRole('dialog', { name: 'Create project', exact: true }));
  await dialog.getByLabel('Project name', { exact: true }).fill(`Browser project ${runID}`);
  await dialog.getByLabel('Platform', { exact: true }).selectOption('javascript');
  const createdResponse = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/projects'));
  await dialog.getByRole('button', { name: 'Create project', exact: true }).click();
  const projectResponse = await createdResponse;
  assert.equal(projectResponse.status(), 201, 'Project creation succeeds');
  const project = await projectResponse.json();
  await visible(page.getByRole('heading', { level: 1, name: 'Projects', exact: true }));
  const projectRow = page.getByRole('row').filter({ hasText: `Browser project ${runID}` });
  assert.equal(await projectRow.locator('code').innerText(), project.dsn, 'Project DSN is shown');
  const dsn = new URL(project.dsn);
  assert.ok(dsn.username, 'The DSN includes an SDK key');
  assert.ok(dsn.pathname.endsWith(`/${project.id}`), 'The DSN identifies the created project');
  await projectRow.getByRole('button', { name: 'Copy', exact: true }).click();
  await visible(page.getByRole('status').filter({ hasText: /DSN copied|Copy unavailable/ }));
  for (const width of [320, 768, 1440]) await capture('projects', width);
  checks.push('Project created with a visible DSN and working copy control');

  const eventID = randomBytes(16).toString('hex');
  const unsafe = '<img src=x onerror="window.__goodSentryUnsafe=1">';
  const value = `Browser acceptance failure ${runID} ${unsafe}`;
  const event = {
    event_id: eventID, timestamp: Date.now() / 1000, platform: 'javascript', level: 'error',
    message: value, environment: 'browser-test', release: `browser-${runID}`,
    fingerprint: [`browser-acceptance-${runID}`], tags: { surface: 'checkout', unsafe },
    user: { id: 'acceptance-user', email: 'buyer@example.invalid' },
    contexts: { runtime: { name: 'Chromium', version: 'acceptance' } },
    exception: [{ type: 'CheckoutError', value, stacktrace: { frames: [{ filename: 'src/checkout.js', function: 'submitCheckout', lineno: 42, colno: 9, in_app: true, pre_context: ['const cart = loadCart();'], context_line: `throw new Error('${unsafe}');`, post_context: ['sendReceipt();'] }] } }],
    breadcrumbs: { values: [{ category: 'checkout', type: 'user', timestamp: Date.now() / 1000, message: 'Payment button selected', data: { basket: 'acceptance' } }] },
  };
  const payload = JSON.stringify(event);
  const envelope = `${JSON.stringify({ event_id: eventID, dsn: project.dsn })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const ingestion = await context.request.post(new URL(`api/${encodeURIComponent(project.id)}/envelope/`, baseURL).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-browser-acceptance/1.0` }, data: envelope,
  });
  assert.equal(ingestion.status(), 200, 'Real envelope is accepted');
  await projectRow.getByRole('link', { name: 'View issues', exact: true }).click();
  const issueLink = await visible(page.getByRole('link').filter({ hasText: `Browser acceptance failure ${runID}` }).first());
  await page.getByLabel('Search issues', { exact: true }).fill(`Browser acceptance failure ${runID}`);
  await page.getByLabel('Environment', { exact: true }).fill('browser-test');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await visible(issueLink);
  assert.equal(new URL(page.url()).searchParams.get('environment'), 'browser-test', 'Environment filter is represented in URL');
  await page.reload({ waitUntil: 'networkidle' });
  await visible(issueLink);
  assert.equal(await page.getByLabel('Search issues', { exact: true }).inputValue(), `Browser acceptance failure ${runID}`, 'Search filter survives reload');
  for (const width of [320, 768, 1440]) await capture('issues', width);
  await issueLink.click();
  await visible(page.getByRole('heading', { name: 'Stack trace', exact: true }));
  await visible(page.getByText('submitCheckout', { exact: true }));
  await visible(page.getByText('src/checkout.js:42:9', { exact: true }));
  await visible(page.getByText('Payment button selected', { exact: true }));
  await visible(page.getByRole('heading', { name: 'Tags', exact: true }));
  await visible(page.getByText(eventID, { exact: true }));
  assert.equal(await page.locator('#main img').count(), 0, 'Telemetry HTML stays text');
  assert.equal(await page.evaluate(() => window.__goodSentryUnsafe), undefined, 'Telemetry cannot execute script');
  for (const width of [320, 768, 1440]) await capture('issue-detail', width);
  const unnamedControls = await page.evaluate(() => Array.from(document.querySelectorAll('button, a[href], input:not([type=hidden]), select, textarea')).filter(element => {
    if (!element.getClientRects().length) return false;
    const labels = Array.from(element.labels || []).map(label => label.textContent).join(' ');
    const name = element.getAttribute('aria-label') || element.getAttribute('aria-labelledby') || labels || (['BUTTON', 'A'].includes(element.tagName) ? element.textContent : '');
    return !name.trim();
  }).map(element => element.tagName));
  assert.deepEqual(unnamedControls, [], 'Visible interactive controls have accessible names');
  checks.push('Visible interactive controls have accessible names');
  checks.push('Real ingested error appears with source, breadcrumbs, tags and event ID; telemetry stays text');
  checks.push('Issue search and environment filters survive a reload');

  await page.getByRole('button', { name: 'Resolve', exact: true }).click();
  await visible(page.getByRole('button', { name: 'Reopen', exact: true }));
  const comment = `Investigating release browser-${runID}.`;
  await page.getByLabel('Add a comment', { exact: true }).fill(comment);
  await page.getByRole('button', { name: 'Post comment', exact: true }).click();
  await visible(page.getByText(comment, { exact: true }));
  await page.reload({ waitUntil: 'networkidle' });
  await visible(page.getByRole('button', { name: 'Reopen', exact: true }));
  await visible(page.getByText(comment, { exact: true }));
  await page.getByRole('button', { name: 'Reopen', exact: true }).click();
  await visible(page.getByRole('button', { name: 'Resolve', exact: true }));
  checks.push('Resolve/reopen and comments persist across reload');

  // Seed telemetry through the public envelope boundary. These payloads are
  // independently authored fixtures, not SDK/server source or mocked API data.
  const traceID = randomBytes(16).toString('hex');
  const transactionName = `GET /browser-checkout-${runID}`;
  async function sendItem(type, item, extra = {}) {
    const payload = typeof item === 'string' ? item : JSON.stringify(item);
    const body = `${JSON.stringify({ dsn: project.dsn })}\n${JSON.stringify({ type, length: Buffer.byteLength(payload), ...extra })}\n${payload}\n`;
    const response = await context.request.post(new URL(`api/${encodeURIComponent(project.id)}/envelope/`, baseURL).href, {
      headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-browser-acceptance/1.0` }, data: body,
    });
    assert.equal(response.status(), 200, `${type} envelope is accepted`);
  }
  const transactionStart = Math.floor(Date.now() / 1000) - 120;
  for (const [index, duration] of [100, 200, 300, 400, 1000].entries()) {
    await sendItem('transaction', {
      type: 'transaction', event_id: randomBytes(16).toString('hex'), transaction: transactionName,
      platform: 'javascript', level: 'info', start_timestamp: transactionStart + index, timestamp: transactionStart + index + duration / 1000,
      contexts: { trace: { trace_id: traceID, span_id: randomBytes(8).toString('hex'), op: 'http.server', status: index === 4 ? 'internal_error' : 'ok' } },
    });
  }
  const logMessage = `Browser typed log ${runID} ${unsafe}`;
  const logTime = Date.now() / 1000 - 30;
  const logItems = Array.from({ length: 26 }, (_, index) => ({
    timestamp: logTime + index / 1000, trace_id: traceID, span_id: '0123456789abcdef', level: 'warn', body: logMessage,
    attributes: {
      'service.name': { type: 'string', value: 'checkout-service' }, environment: { type: 'string', value: 'browser-test' },
      attempts: { type: 'integer', value: index }, maxInt64: { type: 'integer', value: '__goodsentry_browser_int64_max__' }, retryable: { type: 'boolean', value: false },
      latency: { type: 'double', value: 3.25 }, unsafe: { type: 'string', value: unsafe },
    },
  }));
  const rawLogs = JSON.stringify({ items: logItems }).replaceAll('"__goodsentry_browser_int64_max__"', '9223372036854775807');
  await sendItem('log', rawLogs, { item_count: logItems.length, content_type: 'application/vnd.sentry.items.log+json' });
  await page.getByRole('link', { name: 'Transactions', exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Transactions', exact: true }));
  await page.getByLabel('Project', { exact: true }).selectOption(project.id);
  await page.getByRole('button', { name: 'Apply filter', exact: true }).click();
  const transactionRow = await visible(page.getByRole('row').filter({ hasText: transactionName }));
  const transactionCells = await transactionRow.getByRole('cell').allTextContents();
  assert.deepEqual(transactionCells.slice(2, 7), ['5', '1', '400 ms', '300 ms', '880 ms'], 'Visible counts and percentiles use milliseconds');
  await visible(page.getByText('Last 24 hours', { exact: true }));
  await page.reload({ waitUntil: 'networkidle' });
  await visible(transactionRow);
  assert.equal(await page.getByLabel('Project', { exact: true }).inputValue(), project.id, 'Transaction project filter survives reload');
  for (const width of [320, 768, 1440]) await capture('transactions', width);
  checks.push('Transaction project filter, counts, errors and average/p50/p95 are visible and persistent');
  await transactionRow.getByRole('button', { name: 'Copy trace ID', exact: true }).click();
  await visible(page.getByRole('status').filter({ hasText: /Trace ID copied|Copy unavailable/ }));
  await transactionRow.getByRole('link', { name: traceID, exact: true }).click();
  await visible(page.getByRole('heading', { level: 1, name: 'Logs', exact: true }));
  await visible(page.getByText('25 logs on this page · Page 1', { exact: true }));
  assert.equal(await page.getByLabel('Trace ID', { exact: true }).inputValue(), traceID, 'Trace link applies log correlation filter');
  assert.equal(await page.getByRole('row').count(), 26, 'First page contains 25 logs and a header');
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await visible(page.getByText('1 log on this page · Page 2', { exact: true }));
  await page.getByRole('button', { name: 'Previous', exact: true }).click();
  await visible(page.getByText('25 logs on this page · Page 1', { exact: true }));
  await page.getByLabel('Search logs', { exact: true }).fill(`Browser typed log ${runID}`);
  await page.getByLabel('Level', { exact: true }).selectOption('warn');
  await page.getByLabel('Environment', { exact: true }).fill('browser-test');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await visible(page.getByText('25 logs on this page · Page 1', { exact: true }));
  await page.reload({ waitUntil: 'networkidle' });
  await visible(page.getByText('25 logs on this page · Page 1', { exact: true }));
  for (const [label, expected] of [['Search logs', `Browser typed log ${runID}`], ['Level', 'warn'], ['Environment', 'browser-test'], ['Trace ID', traceID], ['Project', project.id]]) {
    assert.equal(await page.getByLabel(label, { exact: true }).inputValue(), expected, `${label} log filter survives reload`);
  }
  const firstLog = page.getByRole('row').filter({ hasText: logMessage }).first();
  await firstLog.locator('summary').click();
  await visible(firstLog.getByRole('heading', { name: 'Log attributes', exact: true }));
  const attributes = firstLog.getByLabel('Log attributes', { exact: true });
  for (const [name, expectedType, expectedValue] of [['attempts', 'integer', '25'], ['maxInt64', 'integer', '9223372036854775807'], ['retryable', 'boolean', 'false'], ['latency', 'double', '3.25'], ['unsafe', 'string', unsafe]]) {
    const value = attributes.locator('dt').filter({ hasText: new RegExp(`^${name}$`) }).locator('xpath=following-sibling::dd[1]');
    await visible(value.getByText(expectedType, { exact: true }));
    const displayed = await visible(value.locator('pre'));
    assert.equal(await displayed.innerText(), expectedValue, `${name} preserves its visible value after reload`);
  }
  assert.equal(await page.locator('#main img').count(), 0, 'Log HTML stays text');
  assert.equal(await page.evaluate(() => window.__goodSentryUnsafe), undefined, 'Log attributes cannot execute script');
  for (const width of [320, 768, 1440]) await capture('logs', width);
  checks.push('Trace correlation, trace copy, 25-row log pagination, typed attributes and URL filters work');
  checks.push('Transaction and expanded-log views remain bounded at 320, 768 and 1440 pixels');

  // Keyboard navigation uses native controls; evaluation below only inspects focus.
  await page.keyboard.press('Tab');
  const focused = await page.evaluate(() => ({ tag: document.activeElement?.tagName, name: document.activeElement?.textContent?.trim() }));
  assert.ok(['A', 'BUTTON', 'INPUT', 'SELECT', 'TEXTAREA'].includes(focused.tag), `Tab reaches a native control, received ${focused.tag}`);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await signIn();
  checks.push('Sign out and subsequent password login work');
  checks.push('No document overflow at 320, 768 or 1440 pixels');
  assert.deepEqual(pageErrors, [], 'No uncaught browser JavaScript errors');
  assert.deepEqual(requestFailures, [], 'No failed transport requests');
  const unexpected = consoleProblems.filter(problem => !(expectedAuthProbe && problem.location.endsWith('/api/v1/me') && /401/.test(problem.text)));
  assert.deepEqual(unexpected, [], 'No unexpected console errors or warnings');
  console.log(JSON.stringify({ passed: checks, screenshots, expectedAuthenticationProbeMessages: consoleProblems.length - unexpected.length }, null, 2));
} catch (error) {
  failure = error;
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
  console.error(error.stack || error.message);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: baseURL.href, checks, screenshots, consoleProblems, pageErrors, requestFailures, failure: failure?.message }, null, 2));
  await browser.close();
}
if (failure) process.exitCode = 1;
