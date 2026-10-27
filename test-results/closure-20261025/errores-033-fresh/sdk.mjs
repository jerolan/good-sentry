import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { randomBytes } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
const require = createRequire(import.meta.url), sdk = require('/tmp/goodsentry-sdk/node_modules/@sentry/node');
assert.equal(sdk.SDK_VERSION, '11.7.0');
const base='http://127.0.0.1:18533/', run=randomBytes(6).toString('hex'), checks=[];
async function request(path, {method='GET',body,token,csrf}={}) {
  const headers={Accept:'application/json'};
  if (body !== undefined) headers['Content-Type']='application/json';
  if (token) headers.Authorization=`Bearer ${token}`;
  if (csrf) { headers['X-CSRF-Token']=csrf; headers.Origin=new URL(base).origin; }
  const response=await fetch(new URL(path,base),{method,headers,...(body!==undefined?{body:JSON.stringify(body)}:{})});
  const text=await response.text(); let data; try { data=JSON.parse(text); } catch { data=null; }
  return {status:response.status,data};
}
const me=await request('api/v1/me'); assert.equal(me.status,200); const org=me.data.organizations[0].slug, csrf=me.data.csrfToken;
const project=await request(`api/v1/organizations/${org}/projects`,{method:'POST',csrf,body:{name:`Closure 033 ${run}`,platform:'node'}}); assert.equal(project.status,201);
const p=project.data;
const token=await request(`api/v1/organizations/${org}/api-tokens`,{method:'POST',csrf,body:{name:`Closure 033 ${run}`,scopes:['event:write'],projectIds:[p.id],expiresInDays:1}}); assert.equal(token.status,201); const bearer=token.data.token;
assert.deepEqual(token.data.scopes,['event:write']); assert.equal(token.data.allProjects,false); checks.push('A project-restricted event:write token is issued');
const acknowledgements=[]; sdk.init({dsn:p.dsn,defaultIntegrations:false,autoSessionTracking:false,transport(options){const actual=sdk.makeNodeTransport(options);return {flush:t=>actual.flush(t),async send(envelope){const r=await actual.send(envelope);acknowledgements.push(r.statusCode);return r;}}}});
async function capture(){const id=sdk.withScope(scope=>{scope.setFingerprint([`closure033-${run}`]);scope.addAttachment({filename:'proof.bin',data:Buffer.from([0,1,127,255])});return sdk.captureException(new Error(`closure033 ${run}`));});assert.ok(await sdk.flush(10000));return id;}
const first=await capture(); assert.deepEqual(acknowledgements,[200]);
const issues=await request(`api/v1/organizations/${org}/issues?project=${p.id}&status=all`); assert.equal(issues.status,200); assert.equal(issues.data.total,1);const issue=issues.data.data[0].id;
const before=await request(`api/v1/issues/${issue}`); assert.equal(before.status,200); assert.equal(before.data.event.event_id,first);checks.push('Official Node 11.7.0 capture is acknowledged and readable');
const attachmentPath=`api/v1/issues/${issue}/events/${first}/attachments`; const attachments=await request(attachmentPath); assert.equal(attachments.status,200); assert.equal(attachments.data.data.length,1); assert.equal(attachments.data.data[0].filename,'proof.bin'); const download=await fetch(new URL(`${attachmentPath}/${attachments.data.data[0].id}`,base)); assert.equal(download.status,200); assert.deepEqual(Buffer.from(await download.arrayBuffer()),Buffer.from([0,1,127,255])); checks.push('The SDK attachment is listed and downloaded byte for byte');
const discarded=await request(`api/0/organizations/${org}/issues/${issue}/`,{method:'PUT',token:bearer,body:{discard:true}}); assert.equal(discarded.status,204); checks.push('Restricted writer permanently discards the captured issue');
const second=await capture(); assert.notEqual(first,second);assert.deepEqual(acknowledgements,[200,200]);
const after=await request(`api/v1/organizations/${org}/issues?project=${p.id}&status=all`);assert.equal(after.status,200);assert.equal(after.data.total,0);checks.push('A new matching SDK capture is acknowledged and remains absent after discard');
await sdk.close(10000);
const report={passed:true,sdkVersion:sdk.SDK_VERSION,source:'b77a2cc',projectId:p.id,issueId:issue,firstEventId:first,secondEventId:second,acknowledgements,checks,attachmentSent:'proof.bin: 4 bytes'};
await writeFile('/tmp/goodsentry-closure-033/sdk-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
