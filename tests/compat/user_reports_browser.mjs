// Browser owns actual rendering, keyboard/drafts and stale asynchronous UI responses.
// Existing SQL/HTTP tests cannot detect a wrong displayed event or detached focus.
// Route controls below only delay/fail real requests; they do not implement reports.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir,stat} from 'node:fs/promises';
import {resolve} from 'node:path';
const fixturePath=process.env.GOODSENTRY_REPORTS_FIXTURE;
assert.ok(fixturePath,'Provide the private isolated report fixture');
assert.ok((await stat(fixturePath)).isFile(),'Use a regular private fixture file');
const f=JSON.parse(await readFile(fixturePath,'utf8'));
const sdk=f.sdkEvents,reports=f.sdkReports;
assert.equal(sdk?.passed,true);assert.equal(reports?.passed,true);
assert.equal(sdk.ids.length,60);assert.equal(reports.targets.length,29);
const out=resolve(process.env.GOODSENTRY_TEST_OUTPUT||'test-results/user-reports-browser');await mkdir(out,{recursive:true});
const base=new URL(f.baseURL),phase=process.env.GOODSENTRY_TEST_PHASE||'verification-'+Date.now();
const evidence={phase,passed:false,widths:[],checks:[],errors:[],unexpected:[],outside:[],console:[],failedRequests:[]};
try{await stat(resolve(out,phase+'.json'));throw new Error('Preserve existing evidence; use a new phase');}catch(e){if(e.code!=='ENOENT')throw e;}
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_EXECUTABLE?{executablePath:process.env.PLAYWRIGHT_EXECUTABLE}:{})});
const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce',locale:'en-US',acceptDownloads:true});
const addCookies=async(c,header=f.cookieHeader)=>c.addCookies(header.split('; ').map(value=>{const at=value.indexOf('=');return {name:value.slice(0,at),value:value.slice(at+1),url:base.origin};}));
await addCookies(context);const page=await context.newPage();let failure,expectedFailure=false;
const controlledErrors=new Map();
// Existing shell has no favicon; keep this unrelated request visible in evidence.
controlledErrors.set(new URL('favicon.ico',base).href,404);
const watch=p=>{p.on('pageerror',e=>evidence.errors.push(e.message));p.on('console',value=>{if(['error','warning'].includes(value.type()))evidence.console.push({message:value.text(),url:value.location().url,expected:controlledErrors.has(value.location().url)&&value.text().includes(String(controlledErrors.get(value.location().url)))});});p.on('requestfailed',request=>evidence.failedRequests.push({url:request.url(),error:request.failure()?.errorText,expected:expectedFailure}));p.on('request',r=>{if(!r.url().startsWith(base.origin+'/'))evidence.outside.push(r.url());});p.on('response',r=>{if(r.status()>=400&&!expectedFailure)evidence.unexpected.push({url:r.url(),status:r.status()});});};watch(page);
const target=issue=>{const url=new URL(base);url.search=new URLSearchParams({org:f.orgSlug,view:'issues',project:f.project.id,issue});return url.href;};
const panel=p=>p.getByRole('region',{name:'User reports',exact:true});
const stable=async(p,count=27,number=1)=>{await panel(p).getByRole('button',{name:'Refresh user reports',exact:true}).and(p.locator(':enabled')).waitFor();await panel(p).getByText(`${count} user reports · Page ${number} of ${Math.max(1,Math.ceil(count/25))}`,{exact:true}).waitFor();};
const active=p=>p.evaluate(()=>({tag:document.activeElement?.tagName,text:document.activeElement?.textContent,level:document.activeElement?.tagName}));
const currentEvent=(p,id)=>panel(p).getByRole('region',{name:`Reported event ${id}`,exact:true});
const fit=async p=>{
 const dimensions=await p.evaluate(()=>({viewport:innerWidth,document:document.documentElement.scrollWidth}));assert.equal(dimensions.viewport,dimensions.document,'Document fits '+dimensions.viewport);
 const clipped=await panel(p).evaluate(panel=>[panel,...panel.querySelectorAll('*')].filter(e=>{const b=e.getBoundingClientRect();return b.width>0&&!e.closest('.table-scroll,.event-json,.source-context')&&(b.right>innerWidth+1||b.left< -1||e.scrollWidth>e.clientWidth+2);}).map(e=>({tag:e.tagName,class:e.className,width:e.getBoundingClientRect().width,right:e.getBoundingClientRect().right,scroll:e.scrollWidth,client:e.clientWidth,text:e.textContent.slice(0,60)})));
 assert.deepEqual(clipped,[],'Report controls and text fit inside the card at '+dimensions.viewport);return dimensions;
};
const open=async(p,id)=>{await panel(p).getByRole('button',{name:`View reported event ${id}`,exact:true}).click();const result=currentEvent(p,id);await result.getByRole('heading',{name:'Stack trace',exact:true}).waitFor();return result;};
try{
 await page.goto(target(f.issue),{waitUntil:'networkidle'});await stable(page);
 assert.equal(await panel(page).getByRole('listitem').count(),25);
 const draft='Unsent feedback investigation '+f.runID;
 await page.getByLabel('Add a comment',{exact:true}).fill(draft);await page.getByLabel('Additional errors (optional)',{exact:true}).fill('99');await page.getByLabel('Chosen release version',{exact:true}).fill('draft-feedback@'+f.runID);
 await panel(page).getByRole('button',{name:'Refresh user reports',exact:true}).focus();await page.keyboard.press('Enter');
 assert.equal((await active(page)).tag,'H2');await stable(page);assert.equal((await active(page)).text,'User reports');
 evidence.checks.push('refresh retains keyboard focus');
 for(const width of [320,768,1024,1440]){
  await page.setViewportSize({width,height:1000});const dimensions=await fit(page);
  evidence.widths.push({page:1,...dimensions});await page.screenshot({path:resolve(out,`${phase}-${width}-list.png`),fullPage:true,animations:'disabled'});
 }
 await panel(page).getByRole('button',{name:'Next user reports',exact:true}).click();await stable(page,27,2);assert.equal((await active(page)).text,'User reports');assert.equal(await panel(page).getByRole('listitem').count(),2);
 const special=reports.targets[0],specialRow=panel(page).getByRole('listitem').filter({has:page.getByRole('button',{name:`View reported event ${special.event_id}`,exact:true})});
 assert.equal(await specialRow.getByText(special.name,{exact:true}).textContent(),special.name);assert.equal(await specialRow.getByText(special.email,{exact:true}).textContent(),special.email);assert.equal(await specialRow.getByText(special.comments,{exact:true}).textContent(),special.comments);
 let selected=await open(page,special.event_id);
 await selected.getByText('Original reported checkout failure '+f.runID,{exact:true}).waitFor();assert.equal(await selected.getByText('Latest unreported checkout failure '+f.runID,{exact:true}).count(),0);
 assert.equal((await active(page)).tag,'H3');assert.equal((await active(page)).text,'Reported event');
 const attachment=selected.getByRole('region',{name:'Event attachments',exact:true});await attachment.getByRole('button',{name:'Download reported-proof.bin',exact:true}).waitFor();
 const downloading=page.waitForEvent('download');await attachment.getByRole('button',{name:'Download reported-proof.bin',exact:true}).click();const download=await downloading;assert.deepEqual([...await readFile(await download.path())],[0,255,1,254,2,0,128,42]);
 for(const width of [320,768,1024,1440]){
  await page.setViewportSize({width,height:1000});const dimensions=await fit(page);evidence.widths.push({page:2,...dimensions});
  await selected.getByRole('heading',{name:'Reported event',exact:true}).scrollIntoViewIfNeeded();await page.screenshot({path:resolve(out,`${phase}-${width}-selected.png`),animations:'disabled'});
  await page.screenshot({path:resolve(out,`${phase}-${width}-long-report.png`),fullPage:true,animations:'disabled'});
 }
 assert.equal(await page.evaluate(()=>window.reportUnsafe),undefined);assert.equal(await panel(page).locator('script,img,svg').count(),0);
 assert.equal(await page.getByLabel('Add a comment',{exact:true}).inputValue(),draft);assert.equal(await page.getByLabel('Additional errors (optional)',{exact:true}).inputValue(),'99');
 await selected.getByRole('button',{name:'Close reported event',exact:true}).click();assert.equal(await panel(page).getByRole('button',{name:`View reported event ${special.event_id}`,exact:true}).evaluate(e=>e===document.activeElement),true);
 await page.keyboard.press('Enter');selected=await currentEvent(page,special.event_id);await selected.getByRole('heading',{name:'Stack trace',exact:true}).waitFor();
 {
  const action=await page.getByRole('button',{name:'Resolve',exact:true}).count()?'Resolve':'Reopen';
  await page.getByRole('button',{name:action,exact:true}).click();await stable(page,27,2);await currentEvent(page,special.event_id).getByRole('heading',{name:'Stack trace',exact:true}).waitFor();
  assert.equal(await page.getByLabel('Add a comment',{exact:true}).inputValue(),draft);assert.equal(await page.getByLabel('Additional errors (optional)',{exact:true}).inputValue(),'99');assert.equal(await page.getByLabel('Chosen release version',{exact:true}).inputValue(),'draft-feedback@'+f.runID);
  if(action==='Resolve'){await page.getByRole('button',{name:'Reopen',exact:true}).click();await stable(page,27,2);}
 }
 evidence.checks.push('27 reports paged; maximum hostile text exact; actual older-than50 event and binary download; drafts and triage preserved at four widths');
 // A failed current page can be retried without navigating away or losing drafts.
 let failPage=true;expectedFailure=true;
 await page.route('**/user-reports?page=2',async route=>{if(failPage){failPage=false;controlledErrors.set(route.request().url(),503);await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({detail:'Controlled report read failure'})});}else await route.continue();});
 await panel(page).getByRole('button',{name:'Refresh user reports',exact:true}).click();await panel(page).getByRole('button',{name:'Retry user reports',exact:true}).waitFor();await panel(page).getByRole('button',{name:'Retry user reports',exact:true}).click();await stable(page,27,2);await page.unroute('**/user-reports?page=2');expectedFailure=false;
 assert.equal(await page.getByLabel('Add a comment',{exact:true}).inputValue(),draft);
 // A missing selected event has a factual error and a retry, never latest fallback.
 let failEvent=true;const eventURL=`**/events/${special.event_id}/json/`;expectedFailure=true;
 await page.route(eventURL,async route=>{if(failEvent){failEvent=false;controlledErrors.set(route.request().url(),404);await route.fulfill({status:404,contentType:'application/json',body:JSON.stringify({detail:'Not found'})});}else await route.continue();});
 await panel(page).getByRole('button',{name:`View reported event ${special.event_id}`,exact:true}).click();await currentEvent(page,special.event_id).getByText('This reported event is no longer available. Refresh user reports to check for changes.',{exact:true}).waitFor();
 await currentEvent(page,special.event_id).getByRole('button',{name:'Retry reported event',exact:true}).click();await currentEvent(page,special.event_id).getByRole('heading',{name:'Stack trace',exact:true}).waitFor();await page.unroute(eventURL);expectedFailure=false;
 evidence.checks.push('page and missing-event failure/retry preserve context');
 // Delayed old event cannot replace a newer selected event.
 let entered,release;const requested=new Promise(r=>entered=r),gate=new Promise(r=>release=r);
 await page.route(eventURL,async route=>{const response=await route.fetch();entered();await gate;await route.fulfill({response});});
 await panel(page).getByRole('button',{name:`View reported event ${special.event_id}`,exact:true}).click();await requested;
 const another=sdk.ids[1];await open(page,another);release();await page.unrouteAll({behavior:'wait'});await page.waitForLoadState('networkidle');assert.equal(await currentEvent(page,special.event_id).count(),0);assert.equal(await currentEvent(page,another).count(),1);
 // A pending event response must also be invalidated when changing report page.
 let enteredAgain,releaseAgain;const requestedAgain=new Promise(r=>enteredAgain=r),gateAgain=new Promise(r=>releaseAgain=r);
 await page.route(eventURL,async route=>{const response=await route.fetch();enteredAgain();await gateAgain;await route.fulfill({response});});
 await panel(page).getByRole('button',{name:`View reported event ${special.event_id}`,exact:true}).click();await requestedAgain;
 await panel(page).getByRole('button',{name:'Previous user reports',exact:true}).click();await stable(page);releaseAgain();await page.unrouteAll({behavior:'wait'});await page.waitForLoadState('networkidle');assert.equal(await panel(page).getByRole('heading',{name:'Reported event',exact:true}).count(),0);
 // A delayed page response cannot paint a different route.
 let pageEntered,pageRelease;const pageRequested=new Promise(r=>pageEntered=r),pageGate=new Promise(r=>pageRelease=r);
 await page.route('**/user-reports?page=2',async route=>{const response=await route.fetch();pageEntered();await pageGate;await route.fulfill({response});});
 await panel(page).getByRole('button',{name:'Next user reports',exact:true}).click();await pageRequested;
 await page.getByRole('link',{name:'← All issues',exact:true}).click();await page.getByRole('link',{name:'Error: No feedback submitted '+f.runID,exact:true}).click();await stable(page,0);pageRelease();await page.unrouteAll({behavior:'wait'});await page.waitForLoadState('networkidle');
 await panel(page).getByText('No user reports for this issue.',{exact:true}).waitFor();assert.equal(await panel(page).getByRole('listitem').count(),0);
 evidence.checks.push('stale event selection, page changes and route responses ignored; actual empty issue');
 // Module network failure must leave triage intact and retry the same issue.
 const moduleContext=await browser.newContext();await addCookies(moduleContext);const modulePage=await moduleContext.newPage();watch(modulePage);let failModule=true;expectedFailure=true;
 const moduleRequests=[];await modulePage.route('**/reports.js*',async route=>{moduleRequests.push(route.request().url());if(failModule){failModule=false;controlledErrors.set(route.request().url(),503);await route.fulfill({status:503,contentType:'text/plain',body:'Controlled module outage'});}else await route.continue();});
 await modulePage.goto(target(f.issue),{waitUntil:'networkidle'});await modulePage.getByRole('button',{name:'Retry user reports',exact:true}).waitFor();await modulePage.getByLabel('Add a comment',{exact:true}).fill(draft);
 assert.equal(await modulePage.getByRole('button',{name:'Resolve',exact:true}).isEnabled(),true);
 await modulePage.getByRole('button',{name:'Retry user reports',exact:true}).click();await stable(modulePage);assert.equal(moduleRequests.length,2);assert.equal(new URL(moduleRequests[1]).search,'?retry=1');assert.equal((await active(modulePage)).text,'User reports');assert.equal(await modulePage.getByLabel('Add a comment',{exact:true}).inputValue(),draft);await moduleContext.close();expectedFailure=false;
 evidence.checks.push('actual module outage and retry preserve issue and draft');
 assert.deepEqual(evidence.console.filter(value=>!value.expected),[]);assert.deepEqual(evidence.failedRequests.filter(value=>!value.expected),[]);assert.deepEqual(evidence.errors,[]);assert.deepEqual(evidence.unexpected,[]);assert.deepEqual(evidence.outside,[]);evidence.passed=true;
}catch(e){failure=e;evidence.error=e.stack;await page.screenshot({path:resolve(out,phase+'-failure.png'),fullPage:true});}
await writeFile(resolve(out,phase+'.json'),JSON.stringify(evidence,null,2));await browser.close();console.log(JSON.stringify({phase,passed:evidence.passed,widths:evidence.widths,checks:evidence.checks,error:evidence.error}));if(failure)throw failure;
