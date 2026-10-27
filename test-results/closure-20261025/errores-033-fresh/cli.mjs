import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {randomBytes} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {writeFile} from 'node:fs/promises';
const base='http://127.0.0.1:18533/', cli='/tmp/sentry-cli-3.8.0', sdk=createRequire(import.meta.url)('/tmp/goodsentry-sdk/node_modules/@sentry/node');
assert.equal(spawnSync(cli,['--version'],{encoding:'utf8'}).stdout.trim(),'sentry-cli 3.8.0');
async function req(path,method='GET',body,csrf){let headers={Accept:'application/json'};if(body!==undefined)headers['Content-Type']='application/json';if(csrf){headers['X-CSRF-Token']=csrf;headers.Origin=new URL(base).origin}let r=await fetch(new URL(path,base),{method,headers,...(body===undefined?{}:{body:JSON.stringify(body)})});let t=await r.text();let data;try{data=JSON.parse(t)}catch{data=null}return {status:r.status,data}}
const me=await req('api/v1/me'), org=me.data.organizations[0].slug, csrf=me.data.csrfToken, run=randomBytes(5).toString('hex');
const project=await req(`api/v1/organizations/${org}/projects`,'POST',{name:`CLI 033 ${run}`,platform:'node'},csrf);assert.equal(project.status,201);const p=project.data;
async function tok(scope){const r=await req(`api/v1/organizations/${org}/api-tokens`,'POST',{name:`CLI ${scope} ${run}`,scopes:[scope],projectIds:[p.id],expiresInDays:1},csrf);assert.equal(r.status,201);return r.data}
const reader=await tok('event:read'), writer=await tok('event:write');assert.equal(reader.allProjects,false);assert.equal(writer.allProjects,false);
sdk.init({dsn:p.dsn,defaultIntegrations:false,autoSessionTracking:false});const id=sdk.withScope(scope=>{scope.setFingerprint([`cli033-${run}`]);scope.addAttachment({filename:'cli.bin',data:Buffer.from([4,3,2,1])});return sdk.captureException(new Error(`cli033 ${run}`))});assert.ok(await sdk.flush(10000));await sdk.close(10000);
const list=await req(`api/v1/organizations/${org}/issues?project=${p.id}&status=all`);assert.equal(list.data.total,1);const issue=list.data.data[0].id;
async function state(){const r=await req(`api/v1/issues/${issue}`);assert.equal(r.status,200);assert.equal(r.data.event.event_id,id);return r.data.issue.status}
function call(token,command){return spawnSync(cli,['issues','--org',org,'--project',p.slug,'--id',String(issue),command],{encoding:'utf8',env:{...process.env,SENTRY_URL:base,SENTRY_AUTH_TOKEN:token,SENTRY_NO_PROGRESS_BAR:'1'}})}
let a=call(reader.token,'list');assert.equal(a.status,0);assert.ok(a.stdout.includes(String(issue)));
let b=call(reader.token,'resolve');assert.notEqual(b.status,0);assert.match(b.stderr,/403/);assert.equal(await state(),'unresolved');
let c=call(writer.token,'resolve');assert.equal(c.status,0);assert.equal(await state(),'resolved');
let d=call(writer.token,'unresolve');assert.equal(d.status,0);assert.equal(await state(),'unresolved');
let e=call(writer.token,'mute');assert.equal(e.status,0);assert.equal(await state(),'ignored');
let f=call(writer.token,'unresolve');assert.equal(f.status,0);assert.equal(await state(),'unresolved');
const report={passed:true,cliVersion:'3.8.0',sdkVersion:sdk.SDK_VERSION,projectId:p.id,issueId:issue,eventId:id,checks:['restricted reader lists the SDK issue','reader resolve is denied with 403','restricted writer resolves, reopens, mutes and reopens','native reads retain the SDK event after every mutation'],limitation:'CLI 3.8.0 does not provide delete or discard'};await writeFile('/tmp/goodsentry-closure-033/cli-report.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
