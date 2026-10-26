import { execSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { writeFile } from 'node:fs/promises';

const run = randomBytes(4).toString('hex');
const base = new URL('http://127.0.0.1:18126/observe/');
const db = 'goodsentry_log_retention_acceptance';
const psql = (sql, database = db) => execSync("docker exec goodsentry-otlp-pg psql -U goodsentry -d " + database + " -t -A -c " + JSON.stringify(sql), { encoding: 'utf8' }).trim();

const jar = new Map();
const cookie = () => [...jar].map(([k, v]) => k + '=' + v).join('; ');
async function raw(path, { method, body, headers = {} } = {}) {
  method = method || (body === undefined ? 'GET' : 'POST');
  const next = { Origin: base.origin, Accept: 'application/json', ...headers };
  const c = cookie(); if (c) next.Cookie = c;
  if (body !== undefined) next['Content-Type'] = 'application/json';
  const response = await fetch(new URL(path, base), { method, headers: next, body: body === undefined ? undefined : JSON.stringify(body) });
  for (const value of response.headers.getSetCookie()) { const pair = value.split(';')[0]; const i = pair.indexOf('='); if (i > 0) jar.set(pair.slice(0, i), pair.slice(i + 1)); }
  const text = await response.text();
  return { status: response.status, data: text ? (() => { try { return JSON.parse(text); } catch { return text; } })() : null };
}
const email = 'log-retention-' + run + '@example.test';
const password = 'log-retention-password-' + run;
await raw('api/v1/setup', { body: { email, password, name: 'Log Retention Owner', organization: 'Log retention ' + run } });
const me = await raw('api/v1/me');
const csrf = me.data.csrfToken;
const org = me.data.organizations[0].slug;
const project = (await raw('api/v1/organizations/' + org + '/projects', { method: 'POST', body: { name: 'Log retention acceptance', platform: 'go' }, headers: { 'X-CSRF-Token': csrf } })).data;
const publicKey = new URL(project.dsn).username;

const record = (body, traceId, spanId, nano) => ({ timeUnixNano: String(nano), severityNumber: 9, severityText: 'INFO', body: { stringValue: body }, ...(traceId ? { traceId, spanId } : {}) });
const traceOld = 'aa'.repeat(16), spanOld = 'bb'.repeat(8);
const baseNano = BigInt(Date.now()) * 1000000n;
const exportBatch = async (service, records) => {
  const payload = { resourceLogs: [{ resource: { attributes: [{ key: 'service.name', value: { stringValue: service } }] }, scopeLogs: [{ scope: { name: 'log-retention' }, logRecords: records }] }] };
  const response = await fetch(new URL('v1/logs', base), { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-GoodSentry-Key': publicKey }, body: JSON.stringify(payload) });
  if (response.status !== 200) throw new Error('otlp ' + response.status + ' ' + (await response.text()).slice(0, 200));
};

const oldA = 'Expire batch A record one ' + run;
const oldB = 'Expire batch A record two ' + run;
const oldC = 'Expire sibling batch old ' + run;
const keepD = 'Keep sibling batch fresh ' + run;
await exportBatch('retention-service', [record(oldA, traceOld, spanOld, baseNano), record(oldB, traceOld, spanOld, baseNano + 1n)]);
await exportBatch('retention-service', [record(oldC, '', '', baseNano + 2n), record(keepD, '', '', baseNano + 3n)]);

const dsn = new URL(project.dsn);
const sendSDK = async body => {
  const item = JSON.stringify({ items: [{ timestamp: Math.floor(Date.now() / 1000), level: 'info', body }] });
  const envelope = JSON.stringify({ dsn: project.dsn }) + '\n' + JSON.stringify({ type: 'log', length: Buffer.byteLength(item), content_type: 'application/vnd.sentry.items.log+json', item_count: 1 }) + '\n' + item + '\n';
  const response = await fetch(new URL('api/' + project.id + '/envelope/?sentry_version=7&sentry_key=' + encodeURIComponent(dsn.username), base), { method: 'POST', headers: { 'Content-Type': 'application/x-sentry-envelope' }, body: envelope });
  if (response.status !== 200) throw new Error('sdk log ' + response.status);
};
const sdkKeep = 'Keep SDK receipt ' + run;
const sdkOld = 'Expire SDK receipt ' + run;
await sendSDK(sdkKeep);
await sendSDK(sdkOld);

const idOf = body => psql("select id from logs where body = '" + body + "'");
const ids = { oldA: idOf(oldA), oldB: idOf(oldB), oldC: idOf(oldC), keepD: idOf(keepD), sdkKeep: idOf(sdkKeep), sdkOld: idOf(sdkOld) };
const batches = { a: psql("select otlp_batch_id from logs where id = " + ids.oldA), sibling: psql("select otlp_batch_id from logs where id = " + ids.oldC) };
const before = { logs: psql('select count(*) from logs'), batches: psql('select count(*) from otlp_log_batches'), hash: psql("select item_hash from logs where id = " + ids.sdkKeep) };

psql("update logs set created_at = now() - interval '2 days' where id in (" + [ids.oldA, ids.oldB, ids.oldC, ids.sdkOld].join(',') + ")");
const dumped = execSync("docker exec goodsentry-otlp-pg pg_dump -U goodsentry -d " + db + " --no-owner --no-privileges > /tmp/gs-log-retention-backup.dump && echo ok", { encoding: 'utf8' }).trim();
psql('drop database if exists goodsentry_log_retention_restore;', 'postgres');
psql('create database goodsentry_log_retention_restore;', 'postgres');
execSync("docker exec -i goodsentry-otlp-pg psql -U goodsentry -d goodsentry_log_retention_restore -q < /tmp/gs-log-retention-backup.dump", { encoding: 'utf8' });

execSync('docker restart goodsentry-logs-accept >/dev/null', { encoding: 'utf8' });
for (let attempt = 0; attempt < 60; attempt++) {
  try { const ready = await fetch(new URL('healthz', base)); if (ready.status === 200) break; } catch {}
  await new Promise(done => setTimeout(done, 500));
}
let expired = false;
for (let attempt = 0; attempt < 90; attempt++) {
  if (psql('select count(*) from logs where id in (' + [ids.oldA, ids.oldB, ids.oldC, ids.sdkOld].join(',') + ')') === '0') { expired = true; break; }
  await new Promise(done => setTimeout(done, 1000));
}
const after = {
  logs: psql('select count(*) from logs'),
  batches: psql('select count(*) from otlp_log_batches'),
  oldPresent: psql('select count(*) from logs where id in (' + [ids.oldA, ids.oldB, ids.oldC, ids.sdkOld].join(',') + ')'),
  batchARemoved: psql("select count(*) from otlp_log_batches where id = " + batches.a) === '0',
  siblingBatchKept: psql("select count(*) from otlp_log_batches where id = " + batches.sibling) === '1',
  keepDPresent: psql('select count(*) from logs where id = ' + ids.keepD) === '1',
  sdkKeepPresent: psql('select count(*) from logs where id = ' + ids.sdkKeep) === '1',
  sdkHashUnchanged: psql('select item_hash from logs where id = ' + ids.sdkKeep) === before.hash,
  restoredOldRows: psql('select count(*) from logs where id in (' + [ids.oldA, ids.oldB, ids.oldC, ids.sdkOld].join(',') + ')', 'goodsentry_log_retention_restore'),
};
const detail = async id => (await raw('api/v1/organizations/' + org + '/logs/' + id)).status;
const compatible = await raw('api/0/organizations/' + org + '/logs/?project=' + encodeURIComponent(project.id));
const compatibleRows = Array.isArray(compatible.data) ? compatible.data : (compatible.data?.data || []);
const compatibleIds = compatibleRows.map(item => String(item.id));
const result = {
  run, org, projectId: String(project.id), ids, batches, before, after, expired,
  detailOld: await detail(ids.oldA),
  detailKeep: await detail(ids.keepD),
  compatibleStatus: compatible.status,
  compatibleHasOld: compatibleIds.includes(String(ids.oldA)),
  compatibleHasKeep: compatibleIds.includes(String(ids.keepD)),
  compatibleTotal: compatibleIds.length,
  compatibleSample: compatibleIds.slice(0, 5),
  compatibleBody: JSON.stringify(compatible.data).slice(0, 400),
};
await writeFile('/tmp/gs-log-retention-result.json', JSON.stringify(result, null, 2));
console.log(JSON.stringify(result));

