// Browser owner: module loading, retained rule inputs, truthful queue/attempt
// presentation, URL filters and role restrictions. All API/worker data is real.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { randomBytes } from 'node:crypto';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const baseURL = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18001/');
if (!baseURL.pathname.endsWith('/')) baseURL.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL;
const password = process.env.GOODSENTRY_TEST_PASSWORD;
const webhook = process.env.GOODSENTRY_ALERT_WEBHOOK_TARGET;
assert.ok(email && password && webhook, 'Provide existing owner credentials and a bounded local webhook target');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-alerts');
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US' });
const page = await context.newPage(); page.setDefaultTimeout(15000);
const runID = Date.now().toString(36);
const checks = [], screenshots = [], pageErrors = [], requestFailures = [], consoleProblems = [];
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', message => { if (['error', 'warning'].includes(message.type())) consoleProblems.push({ text: message.text(), location: message.location().url }); });
const visible = async locator => { await locator.waitFor({ state: 'visible' }); return locator; };
let me, project, org, failure;
const url = path => new URL(`api/v1/organizations/${encodeURIComponent(org)}/projects/${encodeURIComponent(project.id)}/${path}`, baseURL).href;
async function mutate(path, method, data) {
  return context.request.fetch(path, { method, data, headers: { 'X-CSRF-Token': me.csrfToken, Origin: baseURL.origin } });
}
async function login(loginEmail = email, loginPassword = password, navigate = false) {
  if (navigate) await page.goto(baseURL.href, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Sign in to GoodSentry', exact: true }));
  await page.getByLabel('Email', { exact: true }).fill(loginEmail);
  await page.getByLabel('Password', { exact: true }).fill(loginPassword);
  const signedIn = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/v1/login'));
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  assert.equal((await signedIn).status(), 200);
  await visible(page.getByRole('navigation', { name: 'Main navigation' }));
  me = await (await context.request.get(new URL('api/v1/me', baseURL).href)).json();
}
async function open(changes = {}) {
  const route = new URL(baseURL);
  route.search = new URLSearchParams({ view: 'alerts', alertProject: project.slug, ...changes }).toString();
  await page.goto(route.href, { waitUntil: 'networkidle' });
  await visible(page.getByRole('heading', { name: 'Alerts', exact: true, level: 1 }));
}
async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dialog = page.getByRole('dialog');
    await (await dialog.count() ? dialog.getByRole('button', { name: 'Close', exact: true }) : page.getByRole('heading', { level: 1 })).focus();
    await page.keyboard.press('Control+Home');
    const dimensions = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(dimensions.document <= dimensions.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' }); screenshots.push(path);
  }
}
async function createRule(kind, name) {
  await page.getByRole('button', { name: 'Create alert rule', exact: true }).first().click();
  const dialog = await visible(page.getByRole('dialog', { name: 'Create alert rule', exact: true }));
  await dialog.getByLabel('Rule name', { exact: true }).fill(name);
  await dialog.getByLabel('Alert kind', { exact: true }).selectOption(kind);
  return dialog;
}
async function saveRule(dialog) {
  const response = page.waitForResponse(response => response.request().method() === 'POST' && /\/alerts$/.test(response.url()));
  await dialog.getByRole('button', { name: 'Create alert rule', exact: true }).click();
  const created = await response; assert.equal(created.status(), 201);
  const rule = await created.json();
  await visible(page.getByRole('region', { name: 'Selected alert rule' }).getByRole('heading', { name: rule.name, exact: true }));
  return rule;
}
async function queueTest() {
  const response = page.waitForResponse(response => response.request().method() === 'POST' && /\/alerts\/[^/]+\/test$/.test(response.url()));
  await page.getByRole('button', { name: 'Test alert rule', exact: true }).click();
  const queued = await response; assert.equal(queued.status(), 202);
  assert.equal((await queued.json()).queued, 1, 'One explicit webhook recipient queues one delivery');
  await visible(page.getByRole('region', { name: 'Selected alert rule' }));
  await visible(page.getByRole('button', { name: 'Test alert rule', exact: true }));
}
async function deliveries(rule) {
  const response = await context.request.get(url(`alert-deliveries?rule=${encodeURIComponent(rule.id)}`));
  assert.equal(response.status(), 200); return (await response.json()).data;
}
async function waitDelivery(rule, predicate) {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const found = (await deliveries(rule)).find(predicate);
    if (found) return found;
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  throw new Error('Expected real delivery record did not appear');
}
async function sentDelivery(rule, predicate) {
  const delivery = await waitDelivery(rule, item => predicate(item) && item.status === 'sent');
  const response = await context.request.get(url(`alert-deliveries/${encodeURIComponent(delivery.id)}`));
  assert.equal(response.status(), 200);
  const detail = await response.json();
  assert.equal(detail.delivery.status, 'sent');
  assert.equal(detail.delivery.lastStatusCode, 204, 'Actual local receiver success is persisted');
  assert.ok(detail.delivery.sentAt && detail.delivery.attempts >= 1);
  assert.ok(detail.attempts.some(attempt => attempt.status === 'delivered' && attempt.statusCode === 204 && attempt.completedAt && attempt.elapsedMs >= 0), 'A completed successful worker attempt is persisted');
  if (process.env.GOODSENTRY_ALERT_RECEIPTS) {
    const receipts = (await readFile(process.env.GOODSENTRY_ALERT_RECEIPTS, 'utf8')).trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
    const receipt = receipts.find(item => item.deliveryID === delivery.id);
    assert.ok(receipt, 'Test-owned HTTP receiver actually received this delivery');
    assert.equal(receipt.payload.id, delivery.id);
    assert.equal(receipt.event, delivery.payload.kind);
  }
  return delivery;
}
async function showSentDelivery(rule, delivery) {
  await open({ rule: rule.id, delivery: delivery.id });
  const selected = await visible(page.getByRole('region', { name: 'Selected delivery' }));
  await visible(selected.getByText('sent', { exact: true }));
  await visible(selected.getByText('204', { exact: true }).first());
  const attempts = selected.getByRole('region', { name: 'Delivery attempts', exact: true });
  await visible(attempts.getByRole('row').filter({ hasText: 'delivered' }).filter({ hasText: '204' }).first());
  assert.equal(await attempts.getByText('No worker attempt has started.', { exact: true }).count(), 0);
  return selected;
}
async function sendEvent(environment) {
  const dsn = new URL(project.dsn);
  const eventID = randomBytes(16).toString('hex');
  const payload = JSON.stringify({ event_id: eventID, platform: 'javascript', level: 'error', message: `Browser alert ${runID}`, environment });
  const envelope = `${JSON.stringify({ event_id: eventID, dsn: project.dsn })}\n${JSON.stringify({ type: 'event', length: Buffer.byteLength(payload) })}\n${payload}\n`;
  const response = await context.request.post(new URL(`api/${encodeURIComponent(project.id)}/envelope/`, baseURL).href, {
    headers: { 'Content-Type': 'application/x-sentry-envelope', 'X-Sentry-Auth': `Sentry sentry_version=7,sentry_key=${decodeURIComponent(dsn.username)},sentry_client=goodsentry-alert-browser/1.0` }, data: envelope,
  });
  assert.equal(response.status(), 200); return eventID;
}
async function removeRule(rule) {
  await open({ rule: rule.id });
  await visible(page.getByRole('button', { name: 'Delete alert rule', exact: true }));
  await page.getByRole('button', { name: 'Delete alert rule', exact: true }).click();
  await visible(page.getByText(`Delete ${rule.name}? Pending deliveries will be canceled. Completed history remains.`, { exact: true }));
  await page.getByRole('button', { name: 'Cancel rule deletion', exact: true }).click();
  assert.equal(await page.getByRole('button', { name: 'Delete rule permanently', exact: true }).count(), 0);
  await page.getByRole('button', { name: 'Delete alert rule', exact: true }).click();
  const response = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().endsWith(`/alerts/${rule.id}`));
  await page.getByRole('button', { name: 'Delete rule permanently', exact: true }).click();
  assert.equal((await response).status(), 204);
  await visible(page.getByRole('region', { name: 'Project alert rules' }));
  assert.equal(await page.getByRole('link', { name: rule.name, exact: true }).count(), 0);
}
try {
  await login(email, password, true); org = me.organizations[0].slug;
  assert.equal(me.features.alerts, true);
  await visible(page.getByRole('link', { name: 'Alerts', exact: true }));
  const response = await mutate(new URL(`api/v1/organizations/${encodeURIComponent(org)}/projects`, baseURL).href, 'POST', { name: `Browser alerts ${runID}`, platform: 'javascript' });
  assert.equal(response.status(), 201); project = await response.json();
  await open();
  if (me.features.alertDelivery !== true) await visible(page.getByRole('heading', { name: 'Automatic delivery is not running', exact: true }));
  else assert.equal(await page.getByRole('heading', { name: 'Automatic delivery is not running', exact: true }).count(), 0, 'Active dispatcher has no disabled banner');
  const name = `Frequency ${runID}`;
  const form = await createRule('error_frequency', name);
  for (const [label, expected] of [['Event threshold', '1'], ['Window (minutes)', '5'], ['Cooldown (seconds)', '900']]) assert.equal(await form.getByLabel(label, { exact: true }).inputValue(), expected);
  assert.equal(await form.getByLabel('Enabled', { exact: true }).isChecked(), true);
  assert.equal(await form.getByLabel('Recipient type 1', { exact: true }).inputValue(), 'team_email');
  await form.getByLabel('Environments', { exact: true }).fill('browser-alerts');
  await form.getByLabel('Cooldown (seconds)', { exact: true }).fill('0');
  await form.getByLabel('Recipient type 1', { exact: true }).selectOption('email');
  await form.getByLabel('Email address 1', { exact: true }).fill('invalid address');
  const invalid = page.waitForResponse(response => response.request().method() === 'POST' && /\/alerts$/.test(response.url()));
  await form.getByRole('button', { name: 'Create alert rule', exact: true }).click();
  assert.equal((await invalid).status(), 400);
  await visible(form.getByRole('alert'));
  assert.equal(await form.getByLabel('Rule name', { exact: true }).inputValue(), name);
  assert.equal(await form.getByLabel('Email address 1', { exact: true }).inputValue(), 'invalid address');
  await capture('alert-create-form');
  await form.getByLabel('Recipient type 1', { exact: true }).selectOption('webhook');
  await form.getByLabel('Webhook URL 1', { exact: true }).fill(webhook);
  await form.getByRole('button', { name: 'Add recipient', exact: true }).click();
  await visible(form.getByLabel('Email address 2', { exact: true }));
  await form.getByLabel('Recipient type 2', { exact: true }).selectOption('team_email');
  assert.equal(await form.getByLabel('Webhook URL 2', { exact: true }).isVisible(), false);
  await form.getByRole('button', { name: 'Remove recipient 2', exact: true }).click();
  const frequency = await saveRule(form);
  await page.reload({ waitUntil: 'networkidle' });
  await visible(page.getByRole('region', { name: 'Selected alert rule' }));
  assert.equal(await page.getByLabel('Alert project', { exact: true }).inputValue(), project.slug);
  assert.equal(await page.getByLabel('Alert rule', { exact: true }).inputValue(), frequency.id);
  await capture('alert-rule');
  checks.push('Module loads; documented defaults, recipient controls, retained validation and project/rule URL filters work');

  await queueTest();
  await visible(page.getByRole('status').filter({ hasText: '1 test deliveries queued' }));
  const activeDelivery = me.features.alertDelivery === true;
  const testDelivery = activeDelivery ? await sentDelivery(frequency, () => true) : await waitDelivery(frequency, () => true);
  if (me.features.alertDelivery !== true) {
    assert.equal(testDelivery.status, 'pending', 'No dispatcher means queued, not sent');
    assert.equal(testDelivery.attempts, 0, 'No invented worker attempt');
  }
  let selectedDelivery;
  if (activeDelivery) selectedDelivery = await showSentDelivery(frequency, testDelivery);
  else {
    await open({ rule: frequency.id, delivery: testDelivery.id });
    selectedDelivery = await visible(page.getByRole('region', { name: 'Selected delivery' }));
  }
  await visible(selectedDelivery.getByText(testDelivery.status, { exact: true }));
  await selectedDelivery.getByText('Compact delivery payload', { exact: true }).click();
  await visible(selectedDelivery.locator('pre'));
  const returnedPayload = JSON.parse(await selectedDelivery.locator('pre').innerText());
  assert.equal(returnedPayload.id, testDelivery.id);
  assert.equal(returnedPayload.version, 1);
  const link = selectedDelivery.getByRole('link').filter({ hasText: testDelivery.payload.url });
  assert.equal(await link.getAttribute('href'), testDelivery.payload.url, 'Related resource uses API absolute URL');
  await capture('alert-delivery');
  checks.push(activeDelivery ? 'Test action queues normally; real204 delivery, completed attempt and sent status are visible' : 'Test action reports durable queue count; delivery/payload view shows actual pending state without invented success');

  const before = (await deliveries(frequency)).length;
  await sendEvent('other-environment');
  assert.equal((await deliveries(frequency)).length, before, 'Environment filter excludes unrelated captures');
  const eventID = await sendEvent('browser-alerts');
  const eventDelivery = activeDelivery ? await sentDelivery(frequency, item => item.payload.eventId === eventID) : await waitDelivery(frequency, item => item.payload.eventId === eventID);
  assert.ok(eventDelivery.payload.issue?.id, 'Real capture queues an issue-linked alert');
  assert.ok(eventDelivery.payload.url.startsWith(baseURL.origin), 'HTTP API resolves its public resource URL');
  if (activeDelivery) { await showSentDelivery(frequency, eventDelivery); await capture('alert-capture-sent'); }
  await open({ rule: frequency.id });
  for (let index = 0; index < 25; index++) await queueTest();
  await visible(page.getByText('25 deliveries on this page · Page 1', { exact: true }));
  await page.getByRole('button', { name: 'Next deliveries', exact: true }).click();
  await visible(page.getByText(/deliveries on this page · Page 2/));
  await page.reload({ waitUntil: 'networkidle' });
  await visible(page.getByText(/deliveries on this page · Page 2/));
  await page.getByRole('button', { name: 'Previous deliveries', exact: true }).click();
  await visible(page.getByText('25 deliveries on this page · Page 1', { exact: true }));
  await capture('alert-history');
  checks.push('Real capture/environment matching and 25-row delivery pagination persist through reload');

  if (process.env.GOODSENTRY_MONITOR_TARGET && process.env.GOODSENTRY_MONITOR_CONTROL_URL) {
    assert.equal(me.features.uptime, true);
    async function probeControl(status) {
      assert.ok((await context.request.post(process.env.GOODSENTRY_MONITOR_CONTROL_URL, { data: { status, body: 'uptime alert browser proof' } })).ok(), 'Local probe fixture accepts its state change');
    }
    async function confirmed(status) {
      const deadline = Date.now() + 30000;
      while (Date.now() < deadline) {
        const current = await visible(page.getByRole('region', { name: 'Current monitor state', exact: true }));
        if (await current.getByRole('group', { name: 'Confirmed monitor status', exact: true }).getByText(status, { exact: true }).isVisible()) return;
        await new Promise(resolve => setTimeout(resolve, 500));
        await page.getByRole('button', { name: 'Refresh', exact: true }).click();
      }
      throw new Error(`Real monitor did not reach ${status}`);
    }
    await probeControl(200);
    const uptimeForm = await createRule('uptime', `Uptime ${runID}`);
    assert.equal(await uptimeForm.getByLabel('Event threshold', { exact: true }).isVisible(), false);
    assert.equal(await uptimeForm.getByLabel('Cooldown (seconds)', { exact: true }).isVisible(), false);
    await uptimeForm.getByLabel('Environments', { exact: true }).fill('browser-alerts');
    await uptimeForm.getByLabel('Recipient type 1', { exact: true }).selectOption('webhook');
    await uptimeForm.getByLabel('Webhook URL 1', { exact: true }).fill(webhook);
    const uptimeRule = await saveRule(uptimeForm);
    await page.getByRole('link', { name: 'Uptime', exact: true }).click();
    await page.getByRole('button', { name: 'Create monitor', exact: true }).first().click();
    const monitorForm = await visible(page.getByRole('dialog', { name: 'Create monitor', exact: true }));
    await monitorForm.getByLabel('Monitor name', { exact: true }).fill(`Alert monitor ${runID}`);
    await monitorForm.getByLabel('Monitor type', { exact: true }).selectOption('GET');
    await monitorForm.getByLabel('Target URL', { exact: true }).fill(process.env.GOODSENTRY_MONITOR_TARGET);
    await monitorForm.getByLabel('Project', { exact: true }).selectOption(project.slug);
    await monitorForm.getByLabel('Monitor environment', { exact: true }).fill('browser-alerts');
    await monitorForm.getByLabel('Interval (seconds)', { exact: true }).fill('1');
    await monitorForm.getByLabel('Timeout (seconds)', { exact: true }).fill('2');
    const monitorResponse = page.waitForResponse(response => response.request().method() === 'POST' && /\/monitors$/.test(response.url()));
    await monitorForm.getByRole('button', { name: 'Create monitor', exact: true }).click();
    const created = await monitorResponse; assert.equal(created.status(), 201);
    const monitor = await created.json(); assert.equal(monitor.environment, 'browser-alerts');
    await confirmed('up');
    await page.getByRole('button', { name: 'Pause monitor', exact: true }).click();
    await visible(page.getByRole('button', { name: 'Resume monitor', exact: true }));
    const resumedResponse = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().endsWith(`/monitors/${monitor.id}`));
    await page.getByRole('button', { name: 'Resume monitor', exact: true }).click();
    assert.equal((await (await resumedResponse).json()).environment, 'browser-alerts', 'Monitor pause/resume preserves its environment');
    await confirmed('up');
    await probeControl(503); await confirmed('down');
    const down = activeDelivery ? await sentDelivery(uptimeRule, item => item.payload.monitor?.status === 'down') : await waitDelivery(uptimeRule, item => item.payload.monitor?.status === 'down');
    assert.equal(down.payload.monitor.id, monitor.id); assert.equal(down.payload.environment, 'browser-alerts');
    await probeControl(200); await confirmed('up');
    const recoveryPredicate = item => item.payload.monitor?.status === 'up' && item.payload.monitor?.previousStatus === 'down';
    const recovery = activeDelivery ? await sentDelivery(uptimeRule, recoveryPredicate) : await waitDelivery(uptimeRule, recoveryPredicate);
    await page.getByRole('button', { name: 'Delete monitor', exact: true }).click();
    const removed = page.waitForResponse(response => response.request().method() === 'DELETE' && response.url().endsWith(`/monitors/${monitor.id}`));
    await page.getByRole('button', { name: 'Delete permanently', exact: true }).click(); assert.equal((await removed).status(), 204);
    await page.waitForLoadState('networkidle');
    if (activeDelivery) {
      await showSentDelivery(uptimeRule, down); await capture('alert-uptime-down-sent');
      await showSentDelivery(uptimeRule, recovery); await capture('alert-uptime-recovery-sent');
    } else {
      await open({ rule: uptimeRule.id, delivery: down.id });
      await visible(page.getByRole('region', { name: 'Selected delivery' })); await capture('alert-uptime');
    }
    await removeRule(uptimeRule); await open({ rule: frequency.id });
    checks.push(activeDelivery ? 'Real monitor down/recovery sends both matching environment alerts with visible completed204 attempt rows' : 'Monitor environment survives pause/resume and real down/recovery transitions queue matching project alert payloads');
  }

  await page.getByRole('button', { name: 'Pause alert rule', exact: true }).click();
  await visible(page.getByRole('button', { name: 'Enable alert rule', exact: true }));
  await page.getByRole('button', { name: 'Enable alert rule', exact: true }).click();
  await visible(page.getByRole('button', { name: 'Pause alert rule', exact: true }));
  await page.getByRole('button', { name: 'Edit alert rule', exact: true }).click();
  const edit = await visible(page.getByRole('dialog', { name: 'Edit alert rule', exact: true }));
  assert.equal(await edit.getByLabel('Alert kind', { exact: true }).isDisabled(), true);
  const updatedName = `${name} edited`;
  await edit.getByLabel('Rule name', { exact: true }).fill(updatedName);
  const updated = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().endsWith(`/alerts/${frequency.id}`));
  await edit.getByRole('button', { name: 'Save alert rule', exact: true }).click();
  assert.equal((await updated).status(), 200);
  frequency.name = updatedName;
  await visible(page.getByRole('region', { name: 'Selected alert rule' }).getByRole('heading', { name: updatedName, exact: true }));
  await removeRule(frequency);
  checks.push('Pause/enable, immutable-kind edit and concrete delete confirmation work');

  if (process.env.COMPAT_MEMBER_EMAIL && process.env.COMPAT_MEMBER_PASSWORD) {
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await login(process.env.COMPAT_MEMBER_EMAIL, process.env.COMPAT_MEMBER_PASSWORD);
    assert.equal(await page.getByRole('link', { name: 'Alerts', exact: true }).count(), 0);
    await open();
    await visible(page.getByText('Only organization owners and admins can manage alert rules and inspect deliveries.', { exact: true }));
    assert.equal(await page.getByRole('button', { name: 'Create alert rule', exact: true }).count(), 0);
    assert.equal(await page.getByText(webhook, { exact: true }).count(), 0, 'No recipient destination leaks after sign-out/member login');
    await capture('alert-member');
    checks.push('Real member has no alert navigation, controls or destination metadata on direct URL');
  }
  // This driver performs full-document navigations; a request still in flight for
  // the route being replaced is canceled by the browser (net::ERR_ABORTED). Any
  // other transport failure stays a hard failure.
  assert.deepEqual(pageErrors, []);
  assert.deepEqual(requestFailures.filter(entry => entry.error !== 'net::ERR_ABORTED'), []);
  const unexpected = consoleProblems.filter(item => !(/\/api\/v1\/me$/.test(item.location) && /401/.test(item.text)) && !(/\/alerts$/.test(item.location) && /400/.test(item.text)));
  assert.deepEqual(unexpected, [], 'No unexpected console messages');
  console.log(JSON.stringify({ passed: checks, screenshots, expectedConsoleMessages: consoleProblems.length }, null, 2));
} catch (error) {
  failure = error; console.error(error.stack || error.message);
  await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }).catch(() => {});
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, checks, screenshots, pageErrors, requestFailures, consoleProblems, failure: failure?.message }, null, 2));
  if (process.env.GOODSENTRY_MONITOR_CONTROL_URL) await context.request.post(process.env.GOODSENTRY_MONITOR_CONTROL_URL, { data: { status: 200, body: 'uptime alert browser proof' } }).catch(() => {});
  await browser.close();
}
if (failure) process.exitCode = 1;
