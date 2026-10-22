import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18150/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/goal-browser-providers');
await mkdir(output, { recursive: true });
const runID = Date.now().toString(36);
const email = 'provider-browser@example.test';
const password = 'provider-browser-password';
const screenshots = [], checks = [], pageErrors = [], requestFailures = [], consoleErrors = [];
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
const page = await context.newPage();
page.setDefaultTimeout(15000);
page.on('pageerror', error => pageErrors.push(error.message));
page.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
page.on('console', message => { if (message.type() === 'error') consoleErrors.push({ text: message.text(), location: message.location().url }); });

const providers = [
  ['slack', 'Slack incoming webhook', 'Slack webhook URL', 'Use the channel\'s incoming webhook URL. Keep it secret.'],
  ['discord', 'Discord webhook', 'Discord webhook URL', 'Use a Discord channel webhook URL. Keep it secret.'],
  ['teams', 'Microsoft Teams webhook', 'Microsoft Teams webhook URL', 'Use a Teams workflow or incoming webhook URL. Keep it secret.'],
  ['googlechat', 'Google Chat webhook', 'Google Chat webhook URL', 'Use the space\'s Google Chat webhook URL. Keep it secret.'],
  ['feishu', 'Feishu / Lark bot', 'Feishu / Lark bot URL', 'Use a custom bot webhook URL without request signing. Keep it secret.'],
  ['ntfy', 'ntfy topic URL', 'ntfy topic URL', 'Use the topic URL, including its secret topic name.'],
  ['zulip', 'Zulip Slack-compatible webhook', 'Zulip webhook URL', 'Use Zulip\'s Slack-compatible incoming webhook URL. Keep it secret.'],
  ['mattermost', 'Mattermost incoming webhook', 'Mattermost webhook URL', 'Use the incoming webhook URL from Mattermost. Keep it secret.'],
];

async function capture(name) {
  for (const width of [320, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const dialog = page.getByRole('dialog');
    await (await dialog.count() ? dialog.getByRole('button', { name: 'Close', exact: true }) : page.getByRole('heading', { level: 1 })).focus();
    const dimensions = await page.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth }));
    assert.ok(dimensions.document <= dimensions.viewport, `${name} overflows at ${width}px`);
    const path = resolve(output, `${name}-${width}.png`);
    await page.screenshot({ path, fullPage: true, animations: 'disabled' });
    screenshots.push(path);
  }
}

try {
  const state = await context.request.get(new URL('api/v1/setup', base).href);
  assert.equal(state.status(), 200);
  if ((await state.json()).required) {
    const setup = await context.request.post(new URL('api/v1/setup', base).href, {
      data: { email, password, name: 'Provider browser owner', organization: `Provider browser ${runID}` },
    });
    assert.equal(setup.status(), 201);
  } else {
    const login = await context.request.post(new URL('api/v1/login', base).href, { data: { email, password } });
    assert.equal(login.status(), 200);
  }
  const me = await (await context.request.get(new URL('api/v1/me', base).href)).json();
  const org = me.organizations[0].slug;
  const projectResponse = await context.request.post(new URL(`api/v1/organizations/${encodeURIComponent(org)}/projects`, base).href, {
    data: { name: `Provider project ${runID}`, platform: 'go' },
    headers: { 'X-CSRF-Token': me.csrfToken },
  });
  assert.equal(projectResponse.status(), 201);
  const project = await projectResponse.json();
  await page.goto(new URL(`?view=alerts&alertProject=${encodeURIComponent(project.slug)}`, base).href, { waitUntil: 'networkidle' });
  await page.getByRole('heading', { name: 'Alerts', exact: true, level: 1 }).waitFor();
  await page.getByRole('button', { name: 'Create alert rule', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Create alert rule', exact: true });
  await dialog.waitFor();
  await dialog.getByLabel('Rule name', { exact: true }).fill(`Provider rule ${runID}`);
  await dialog.getByLabel('Recipient type 1', { exact: true }).selectOption('slack');
  await dialog.getByLabel('Slack webhook URL 1', { exact: true }).fill(`https://hooks.example.test/slack/${runID}`);
  for (let index = 0; index < providers.length - 1; index++) {
    await dialog.getByRole('button', { name: 'Add recipient', exact: true }).click();
  }
  for (let row = 0; row < providers.length; row++) {
    const [type, option, label, help] = providers[row];
    const number = row + 1;
    await dialog.getByLabel(`Recipient type ${number}`, { exact: true }).selectOption(type);
    assert.equal((await dialog.getByLabel(`Recipient type ${number}`, { exact: true }).locator(`option[value="${type}"]`).textContent()).trim(), option);
    const destination = dialog.getByLabel(`${label} ${number}`, { exact: true });
    await destination.fill(`https://hooks.example.test/${type}/${runID}`);
    await dialog.getByText(help, { exact: true }).waitFor();
  }
  assert.match(await dialog.innerText(), /Provider URLs create native channel messages; generic webhooks receive GoodSentry JSON/);
  await capture('provider-alert-create');
  await page.setViewportSize({ width: 320, height: 1000 });
  const lastDestination = dialog.getByLabel('Mattermost webhook URL 8', { exact: true });
  await lastDestination.scrollIntoViewIfNeeded();
  assert.equal(await lastDestination.isVisible(), true);
  const dialogMetrics = await dialog.evaluate(element => ({ scrollHeight: element.scrollHeight, clientHeight: element.clientHeight }));
  assert.ok(dialogMetrics.scrollHeight > dialogMetrics.clientHeight, 'long provider dialog must scroll internally');
  const bottomPath = resolve(output, 'provider-alert-create-bottom-320.png');
  await page.screenshot({ path: bottomPath, fullPage: true, animations: 'disabled' });
  screenshots.push(bottomPath);
  await dialog.evaluate(element => { element.scrollTop = 0; });
  const responsePromise = page.waitForResponse(response => response.request().method() === 'POST' && /\/alerts$/.test(response.url()));
  await dialog.getByRole('button', { name: 'Create alert rule', exact: true }).click();
  const response = await responsePromise;
  assert.equal(response.status(), 201);
  const rule = await response.json();
  assert.deepEqual(rule.recipients.map(item => item.type), providers.map(item => item[0]));
  await page.getByRole('region', { name: 'Selected alert rule' }).getByRole('heading', { name: rule.name, exact: true }).waitFor();
  const ruleText = await page.getByRole('region', { name: 'Selected alert rule' }).innerText();
  for (const [type] of providers) assert.match(ruleText, new RegExp(`${type}: https://hooks\\.example\\.test/${type}/${runID}`));
  checks.push('Create dialog exposes eight provider choices with distinct URLs, help and accessible labels');
  await page.reload({ waitUntil: 'networkidle' });
  await page.getByRole('region', { name: 'Selected alert rule' }).getByRole('heading', { name: rule.name, exact: true }).waitFor();
  assert.equal(await page.getByLabel('Alert project', { exact: true }).inputValue(), project.slug);
  assert.equal(await page.getByLabel('Alert rule', { exact: true }).inputValue(), rule.id);
  checks.push('Real rule roundtrip preserves project, rule and all eight recipients through reload');
  await capture('provider-alert-rule');
  assert.deepEqual(pageErrors, []);
  assert.deepEqual(requestFailures, []);
  assert.deepEqual(consoleErrors, []);
} finally {
  await writeFile(resolve(output, 'report.json'), JSON.stringify({ base: base.href, runID, checks, screenshots, pageErrors, requestFailures, consoleErrors }, null, 2));
  await browser.close();
}
