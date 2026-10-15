// Browser owner: exact release routes, mutation forms, raw metadata and independent
// history pages. Storage/HTTP suites own concurrency, scope and alias matrices.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = new URL(process.env.BASE_URL || process.env.GOODSENTRY_TEST_URL || 'http://127.0.0.1:18005/');
if (!base.pathname.endsWith('/')) base.pathname += '/';
const email = process.env.GOODSENTRY_TEST_EMAIL, password = process.env.GOODSENTRY_TEST_PASSWORD;
const memberEmail = process.env.COMPAT_MEMBER_EMAIL, memberPassword = process.env.COMPAT_MEMBER_PASSWORD;
assert.ok(email && password && memberEmail && memberPassword, 'Existing owner and member fixture credentials are required');
const output = resolve(process.env.GOODSENTRY_TEST_OUTPUT || 'test-results/browser-releases'); await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const run = `${Date.now().toString(36)}-${randomBytes(3).toString('hex')}`, unsafe = '<img src=x onerror="window.__releaseUnsafe=1">';
const version = `  Release / ${run} ${unsafe} 雪  `, unicodeVersion = ` ${'🌿'.repeat(252)}  `;
const checks = [], screenshots = [], dimensions = [], pageErrors = [], requestFailures = [], consoleProblems = [], badResponses = [];
let owner, member, page, organization, projects = [], failure;
const params = (tab = page) => Object.fromEntries(new URL(tab.url()).searchParams);
const route = values => { const url = new URL(base); url.search = new URLSearchParams(values).toString(); return url.href; };
const compatible = value => `api/0/organizations/${encodeURIComponent(organization)}/releases/${value === undefined ? '' : `${encodeURIComponent(value)}/`}`;
const native = value => `api/v1/organizations/${encodeURIComponent(organization)}/releases${value === undefined ? '' : `/${encodeURIComponent(value)}`}`;
async function visible(locator) { await locator.waitFor({ state: 'visible' }); return locator; }
async function session(loginEmail, loginPassword) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'en-US', reducedMotion: 'reduce' });
  const login = await context.request.post(new URL('api/v1/login', base).href, { headers: { Origin: base.origin }, data: { email: loginEmail, password: loginPassword } });
  assert.equal(login.status(), 200); const me = await login.json(), tab = await context.newPage(); tab.setDefaultTimeout(20000);
  tab.on('pageerror', error => pageErrors.push(error.message)); tab.on('requestfailed', request => requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  tab.on('response', response => { if (response.status() >= 400) badResponses.push({ url: response.url(), status: response.status() }); });
  tab.on('console', message => { if (['warning', 'error'].includes(message.type())) consoleProblems.push({ text: message.text(), url: message.location().url }); });
  return { context, me, tab };
}
async function mutate(method, path, data) { return owner.context.request.fetch(new URL(path, base).href, { method, headers: { Origin: base.origin, 'X-CSRF-Token': owner.me.csrfToken }, ...(data === undefined ? {} : { data }) }); }
function responseFor(method, suffix = '') { return page.waitForResponse(response => response.request().method() === method && new URL(response.url()).pathname === new URL(compatible(suffix === 'create' ? undefined : version) + (suffix === 'create' ? '' : suffix), base).pathname); }
const region = (label, tab = page) => tab.getByRole('region', { name: label, exact: true });
const listRows = () => region('Releases').getByRole('row').filter({ has: page.getByRole('cell') });
const listRow = name => listRows().filter({ hasText: name });
async function detail(tab = page) { await visible(region('Release context', tab)); assert.equal(await tab.getByRole('heading', { level: 1 }).textContent(), version); }
async function capture(name, tab = page, section) {
  for (const width of [320, 768, 1440]) {
    await tab.setViewportSize({ width, height: 1000 });
    const openDialog = tab.getByRole('dialog');
    if (await openDialog.count()) await openDialog.evaluate(dialog => { dialog.scrollTop = 0; });
    else { await tab.getByRole('heading', { level: 1 }).focus(); await tab.keyboard.press('Control+Home'); await tab.mouse.click(5, 5); }
    await tab.evaluate(() => new Promise(done => requestAnimationFrame(() => { scrollTo(0, 0); requestAnimationFrame(done); })));
    assert.equal(await tab.evaluate(() => scrollY), 0);
    const size = await tab.evaluate(() => ({ viewport: innerWidth, document: document.documentElement.scrollWidth })); dimensions.push({ name, ...size }); assert.equal(size.viewport, size.document, `${name} fits ${width}px`);
    const save = async part => { const path = resolve(output, `${name}-${part}-${width}.png`); await tab.screenshot({ path, fullPage: false, animations: 'disabled' }); screenshots.push(path); };
    await save('overview'); if (section) { await tab.getByRole('heading', { name: section, exact: true, level: 2 }).evaluate(element => element.scrollIntoView({ block: 'start' })); await save('detail'); }
  }
}
async function uploadMap(project, release, artifactName) {
  const maps = page.getByLabel(`Source maps for ${project.name}`, { exact: true });
  if (await maps.getAttribute('open') === null) await maps.locator('summary').click();
  const form = await visible(maps.getByRole('form', { name: `Upload source map for ${project.name}`, exact: true }));
  await form.getByLabel('Artifact name or URL', { exact: true }).fill(artifactName);
  await form.getByLabel('Release', { exact: true }).fill(release);
  assert.equal(await form.getByLabel('Release', { exact: true }).inputValue(), release);
  await form.getByLabel('Source map file', { exact: true }).setInputFiles({ name: 'release.map', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ version: 3, sources: ['release.js'], names: [], mappings: 'AAAA', sourcesContent: ['throw new Error("release fixture");'] })) });
  const response = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith(`/projects/${project.id}/sourcemaps`));
  await form.getByRole('button', { name: 'Upload source map', exact: true }).click(); const result = await response; assert.equal(result.status(), 201); assert.equal((await result.json()).artifact.release, release);
  await visible(maps.getByRole('row').filter({ hasText: artifactName })); return maps;
}

try {
  owner = await session(email, password); page = owner.tab; organization = owner.me.organizations[0].slug;
  for (const suffix of ['A', 'B']) { const response = await mutate('POST', `api/v1/organizations/${organization}/projects`, { name: `Release ${suffix} ${run}`, platform: 'javascript' }); assert.equal(response.status(), 201); projects.push(await response.json()); }
  const shadowResponse = await mutate('POST', `api/v1/organizations/${organization}/projects`, { name: projects[0].id, platform: 'javascript' }); assert.equal(shadowResponse.status(), 201); const shadow = await shadowResponse.json(); assert.equal(shadow.slug, projects[0].id, 'The real fixture shadows A’s numeric id with a different project slug');
  for (let i = 0; i < 26; i++) { const response = await mutate('POST', compatible(), { version: `List ${run} #${String(i).padStart(2, '0')}`, projects: [projects[0].slug] }); assert.equal(response.status(), 201); assert.deepEqual((await response.json()).projects.map(project => project.id), [projects[0].id]); }
  await page.goto(route({ org: organization, view: 'issues' }), { waitUntil: 'networkidle' }); await page.getByRole('link', { name: 'Releases', exact: true }).click();
  await page.getByRole('button', { name: 'Create release', exact: true }).click();
  await page.getByRole('dialog', { name: 'Create unreleased version', exact: true }).getByLabel('Release version', { exact: true }).fill('unsaved-navigation-probe');
  await page.goBack({ waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'Issues', exact: true, level: 1 })); assert.equal(await page.getByRole('dialog').count(), 0);
  await page.goForward({ waitUntil: 'networkidle' }); await visible(page.getByRole('heading', { name: 'Releases', exact: true, level: 1 })); assert.equal(await page.getByRole('dialog').count(), 0);
  await page.getByRole('button', { name: 'Create release', exact: true }).click(); const reopenedCreate = page.getByRole('dialog', { name: 'Create unreleased version', exact: true }); assert.equal(await reopenedCreate.getByLabel('Release version', { exact: true }).inputValue(), ''); await reopenedCreate.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByLabel('Search versions', { exact: true }).fill(run); await page.getByLabel('Release project', { exact: true }).selectOption({ value: projects[0].id }); await page.getByRole('button', { name: 'Apply release filters', exact: true }).click();
  await visible(listRow(`List ${run} #25`)); assert.equal(await listRows().count(), 25);
  await page.getByRole('button', { name: 'Next', exact: true }).click(); await visible(listRow(`List ${run} #00`)); await page.reload({ waitUntil: 'networkidle' }); await visible(listRow(`List ${run} #00`));
  const originalList = params(); await listRow(`List ${run} #00`).getByRole('link').click(); await visible(region('Release context')); await visible(region('Release commits').getByText('No commits on this page', { exact: true })); await visible(region('Release deployments').getByText('No deployments on this page', { exact: true }));
  await page.getByRole('link', { name: '← Back to releases', exact: true }).click(); await visible(listRow(`List ${run} #00`)); assert.deepEqual(params(), originalList);
  await page.getByRole('button', { name: 'Create release', exact: true }).click(); let dialog = page.getByRole('dialog', { name: 'Create unreleased version', exact: true });
  await dialog.getByLabel('Release version', { exact: true }).fill('.'); for (const project of projects) await dialog.getByLabel(project.name, { exact: true }).check();
  let response = responseFor('POST', 'create'); await dialog.getByRole('button', { name: 'Create release', exact: true }).click(); assert.equal((await response).status(), 400); await visible(dialog.getByRole('alert'));
  assert.equal(await dialog.getByLabel('Release version', { exact: true }).inputValue(), '.'); for (const project of projects) assert.equal(await dialog.getByLabel(project.name, { exact: true }).isChecked(), true);
  await dialog.getByLabel('Release version', { exact: true }).fill(version); response = responseFor('POST', 'create'); await dialog.getByRole('button', { name: 'Create release', exact: true }).click(); assert.equal((await response).status(), 201); await detail();
  await visible(region('Release context').getByText(projects[0].name, { exact: true })); await visible(region('Release context').getByText(projects[1].name, { exact: true })); assert.equal(await region('Release context').getByText(shadow.name, { exact: true }).count(), 0);
  assert.equal(params().version, version); assert.equal(params().page, '2'); assert.equal(params().project, projects[0].id); await visible(region('Release context').getByText('Unreleased', { exact: true }));
  const selectedBeforeEdit = params(); await page.getByRole('button', { name: 'Edit release', exact: true }).click(); await page.getByRole('dialog', { name: 'Edit release metadata', exact: true }).getByLabel('Ref', { exact: true }).fill('unsaved-edit-navigation');
  await page.goBack({ waitUntil: 'networkidle' }); await visible(listRow(`List ${run} #00`)); assert.equal(await page.getByRole('dialog').count(), 0);
  await page.goForward({ waitUntil: 'networkidle' }); await detail(); assert.deepEqual(params(), selectedBeforeEdit); assert.equal(await page.getByRole('dialog').count(), 0);
  await page.getByRole('button', { name: 'Edit release', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'Edit release metadata', exact: true }); assert.equal(await dialog.getByLabel('Ref', { exact: true }).inputValue(), '');
  await dialog.getByLabel('Ref', { exact: true }).fill(unsafe); await dialog.getByLabel('Metadata URL', { exact: true }).fill('javascript:window.__releaseUnsafe=2'); await dialog.getByLabel('Release status', { exact: true }).selectOption('archived');
  response = responseFor('PUT'); await dialog.getByRole('button', { name: 'Save release', exact: true }).click(); assert.equal((await response).status(), 200); await detail(); await visible(region('Release context').getByText('archived', { exact: true }));
  assert.equal(await page.locator('a[href^="javascript:"]').count(), 0); assert.equal(await page.getByRole('main').locator('img').count(), 0); assert.equal(await page.evaluate(() => typeof window.__releaseUnsafe), 'undefined');
  const finalizedAfter = Date.now() - 1000; await page.getByRole('button', { name: 'Finalize now (UTC)', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'Finalize release', exact: true });
  response = responseFor('PUT'); await dialog.getByRole('button', { name: 'Finalize now', exact: true }).click(); const finalized = await response; assert.equal(finalized.status(), 200); const released = await finalized.json(); assert.ok(Date.parse(released.dateReleased) >= finalizedAfter && Date.parse(released.dateReleased) <= Date.now() + 1000); await detail(); await visible(region('Release context').getByText('Released', { exact: true }));
  const commits = Array.from({ length: 26 }, (_, i) => ({ id: `commit-${String(i).padStart(2, '0')}-${run}`, repository: `Manual ${run}`, message: `${i === 0 ? unsafe : 'Recorded change'} ${i}`, authorName: `Author ${unsafe}`, authorEmail: 'release-fixture@example.invalid', timestamp: '2025-02-03T05:06:07.123456+02:00', receipt: 'INT64_LITERAL', hugeExponent: 'EXPONENT_LITERAL', extra: { note: unsafe } }));
  const commitText = JSON.stringify(commits, null, 2).replaceAll('"INT64_LITERAL"', '9223372036854775807').replaceAll('"EXPONENT_LITERAL"', '1e1000000');
  await page.getByRole('button', { name: 'Replace commit set', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'Replace commit set', exact: true }); assert.equal(await dialog.getByLabel('Complete commit JSON array', { exact: true }).inputValue(), '');
  await dialog.getByLabel('Complete commit JSON array', { exact: true }).fill('[{"id":""}]'); response = responseFor('POST', 'commits/'); await dialog.getByRole('button', { name: 'Replace commits', exact: true }).click(); assert.equal((await response).status(), 400); await visible(dialog.getByRole('alert')); assert.equal(await dialog.getByLabel('Complete commit JSON array', { exact: true }).inputValue(), '[{"id":""}]');
  await dialog.getByLabel('Complete commit JSON array', { exact: true }).fill(commitText); response = responseFor('POST', 'commits/'); await dialog.getByRole('button', { name: 'Replace commits', exact: true }).click(); assert.equal((await response).status(), 200); await detail();
  const commitRow = region('Release commits').getByRole('row').filter({ hasText: commits[0].id }); await visible(commitRow); await visible(commitRow.getByText('2025-02-03T03:06:07.123456Z', { exact: true })); await commitRow.getByText('Inspect metadata', { exact: true }).click(); const raw = await commitRow.locator('pre').innerText(); assert.ok(raw.includes('9223372036854775807') && raw.includes('"hugeExponent": "1e1000000"') && raw.includes(JSON.stringify(unsafe).slice(1, -1)));
  await page.getByRole('button', { name: 'Record deployment', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'Record completed deployment', exact: true });
  await dialog.getByLabel('Deployment environment', { exact: true }).fill('invalid\u0007'); await dialog.getByLabel('Deployment name', { exact: true }).fill(`Browser deploy ${run}`); await dialog.getByLabel(projects[0].name, { exact: true }).check();
  response = responseFor('POST', 'deploys/'); await dialog.getByRole('button', { name: 'Record deployment', exact: true }).click(); assert.equal((await response).status(), 400); await visible(dialog.getByRole('alert')); assert.equal(await dialog.getByLabel('Deployment name', { exact: true }).inputValue(), `Browser deploy ${run}`); assert.equal(await dialog.getByLabel(projects[0].name, { exact: true }).isChecked(), true);
  await dialog.getByLabel('Deployment environment', { exact: true }).fill('production'); response = responseFor('POST', 'deploys/'); await dialog.getByRole('button', { name: 'Record deployment', exact: true }).click(); const recordedDeploy = await response; assert.equal(recordedDeploy.status(), 201); assert.deepEqual((await recordedDeploy.json()).projects, [projects[0].slug]); await detail();
  for (let i = 0; i < 25; i++) { const deploy = await mutate('POST', `${compatible(version)}deploys/`, { environment: 'production', name: `Fixture deploy ${run} #${i}`, projects: [projects[0].slug] }); assert.equal(deploy.status(), 201); }
  await page.getByRole('button', { name: 'Refresh release', exact: true }).click(); await detail(); await visible(region('Release deployments').getByText(`Fixture deploy ${run} #24`, { exact: true }));
  await capture('owner-release', page, 'Commits'); await page.getByRole('button', { name: 'Next commits', exact: true }).click(); await detail(); await visible(region('Release commits').getByText(commits[25].id, { exact: true }));
  await page.getByRole('button', { name: 'Next deployments', exact: true }).click(); await detail(); await visible(region('Release deployments').getByText(`Browser deploy ${run}`, { exact: true }));
  const selected = params(); assert.equal(selected.commitPage, '2'); assert.equal(selected.deployPage, '2'); assert.equal(selected.page, '2'); assert.equal(selected.version, version);
  await page.reload({ waitUntil: 'networkidle' }); await detail(); assert.deepEqual(params(), selected); await visible(region('Release commits').getByText(commits[25].id, { exact: true })); await visible(region('Release deployments').getByText(`Browser deploy ${run}`, { exact: true }));
  await capture('independent-pages', page, 'Deployments'); await page.getByRole('link', { name: '← Back to releases', exact: true }).click(); await visible(listRow(`List ${run} #00`)); assert.deepEqual(params(), originalList);
  await page.getByLabel('Release environment', { exact: true }).fill('production'); await page.getByRole('button', { name: 'Apply release filters', exact: true }).click(); await visible(listRow(version)); assert.equal(await listRows().count(), 1); await page.reload({ waitUntil: 'networkidle' }); await visible(listRow(version)); await listRow(version).getByRole('link').click(); await detail();
  checks.push('Create/edit dialogs close on real Back, stay closed on Forward and reopen without unsaved input; ordinary server400 retains useful fields. Exact spaced/slashed version, safe ref/URL/status, UTC finalize, explicit int64/extreme-exponent commits and independent25+1 history pages work');

  const ownerDetail = page.url(); member = await session(memberEmail, memberPassword); await member.tab.goto(route({ org: organization, view: 'releases', version, commitPage: '2', deployPage: '2' }), { waitUntil: 'networkidle' }); await detail(member.tab);
  await visible(region('Release commits', member.tab).getByText(commits[25].id, { exact: true })); await visible(region('Release deployments', member.tab).getByText(`Browser deploy ${run}`, { exact: true }));
  for (const label of ['Create release', 'Edit release', 'Finalize now (UTC)', 'Replace commit set', 'Record deployment', 'Remove project', 'Delete release']) assert.equal(await member.tab.getByRole('button', { name: label, exact: true }).count(), 0);
  await capture('member-release', member.tab, 'Deployments');
  const projectAContext = region('Release context').getByText(projects[0].name, { exact: true }).locator('..').locator('..'); await projectAContext.getByRole('link', { name: 'Project and source maps', exact: true }).click();
  assert.equal(params().sourceMaps, projects[0].id); const artifact = `https://assets.example.invalid/${run}/release.min.js`; await uploadMap(projects[0], version, artifact);
  await uploadMap(projects[0], unicodeVersion, `https://assets.example.invalid/${run}/unicode.min.js`); assert.equal(Array.from(unicodeVersion).length, 255);
  const unicodeRelease = await owner.context.request.get(new URL(compatible(unicodeVersion), base).href); assert.equal(unicodeRelease.status(), 200); assert.equal((await unicodeRelease.json()).version, unicodeVersion);
  await page.goto(ownerDetail, { waitUntil: 'networkidle' }); await detail();
  await region('Release context').getByText(projects[0].name, { exact: true }).locator('..').locator('..').getByRole('button', { name: 'Remove project', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'Remove project association', exact: true }); await visible(dialog.getByText(/release-specific source maps/)); const removed = page.waitForResponse(response => response.request().method() === 'DELETE'); await dialog.getByRole('button', { name: 'Confirm removal', exact: true }).click(); assert.equal((await removed).status(), 204); await dialog.waitFor({ state: 'hidden' }); await region('Release context').getByText(projects[0].name, { exact: true }).waitFor({ state: 'hidden' }); await detail();
  assert.equal(await region('Release context').getByText(projects[0].name, { exact: true }).count(), 0); await visible(region('Release context').getByText(projects[1].name, { exact: true }));
  const maps = await owner.context.request.get(new URL(`api/v1/organizations/${organization}/projects/${projects[0].id}/sourcemaps`, base).href); assert.equal(maps.status(), 200); const artifacts = (await maps.json()).data; assert.ok(!artifacts.some(item => item.release === version)); assert.ok(artifacts.some(item => item.release === unicodeVersion));
  await page.getByRole('button', { name: 'Delete release', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'Delete release', exact: true }); await visible(dialog.getByText(/release-specific source maps/)); await capture('delete-confirmation', page); await dialog.getByRole('button', { name: 'Confirm delete', exact: true }).click(); await visible(page.getByRole('heading', { name: 'Releases', exact: true, level: 1 })); assert.equal(params().version, undefined);
  assert.equal((await owner.context.request.get(new URL(compatible(version), base).href)).status(), 404);
  await page.getByRole('link', { name: 'API tokens', exact: true }).click(); await visible(page.getByRole('heading', { name: 'API tokens', exact: true, level: 1 })); assert.equal(await page.getByLabel('project:releases', { exact: true }).isChecked(), false); assert.equal(await page.getByLabel('org:read', { exact: true }).isChecked(), true); assert.equal(await page.getByLabel('project:read', { exact: true }).isChecked(), true);
  for (const key of ['version', 'commitPage', 'deployPage', 'sourceMaps']) assert.equal(params()[key], undefined);
  checks.push('Real member reads metadata and both history pages without write controls; project/source-map navigation accepts exact255 Unicode identities, removal deletes only matching maps, confirmed whole deletion returns to preserved filters, and release-write credentials remain opt-in');
  assert.equal(await page.evaluate(() => typeof window.__releaseUnsafe), 'undefined'); assert.deepEqual(pageErrors, []); assert.deepEqual(requestFailures, []);
  assert.deepEqual(badResponses.filter(item => item.status !== 400), []); assert.deepEqual(consoleProblems.filter(item => !(/400/.test(item.text) && item.url.includes('/releases/'))), []);
  console.log(JSON.stringify({ ok: true, checks, screenshots: screenshots.length }, null, 2));
} catch (error) { failure = error; console.error(error); }
finally { await writeFile(resolve(output, 'report.json'), JSON.stringify({ ok: !failure, baseURL: base.href, organization, projectIds: projects.map(project => project.id), version, checks, screenshots, dimensions, pageErrors, requestFailures, consoleProblems, badResponses, failure: failure?.message }, null, 2)); await browser.close(); }
if (failure) throw failure;
