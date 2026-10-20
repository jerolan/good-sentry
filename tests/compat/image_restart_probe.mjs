import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';

const base = (process.env.GOODSENTRY_URL || 'http://127.0.0.1:18160').replace(/\/$/, '');
const cookies = new Map();
let csrf = '';

async function api(method, path, payload, expected = 200) {
  const headers = { Accept: 'application/json' };
  if (cookies.size) headers.Cookie = [...cookies].map(([key, value]) => `${key}=${value}`).join('; ');
  if (payload !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && csrf) headers['X-CSRF-Token'] = csrf;
  const response = await fetch(base + path, {
    method, headers, body: payload === undefined ? undefined : JSON.stringify(payload),
    signal: AbortSignal.timeout(15000),
  });
  for (const cookie of response.headers.getSetCookie()) {
    const pair = cookie.split(';', 1)[0];
    const split = pair.indexOf('=');
    cookies.set(pair.slice(0, split), pair.slice(split + 1));
  }
  const body = await response.text();
  assert.equal(response.status, expected, `${method} ${path}: HTTP ${response.status}: ${body}`);
  return JSON.parse(body);
}

assert.ok(process.env.COMPAT_EMAIL && process.env.COMPAT_PASSWORD && process.env.COMPAT_EVENT_ID, 'Provide owner credentials and event ID');
const me = await api('POST', '/api/v1/login', { email: process.env.COMPAT_EMAIL, password: process.env.COMPAT_PASSWORD });
csrf = me.csrfToken;
const org = me.organizations[0];
const projects = await api('GET', `/api/v1/organizations/${encodeURIComponent(org.slug)}/projects`);
const search = await api('GET', `/api/v1/organizations/${encodeURIComponent(org.slug)}/issues?query=${encodeURIComponent('GoodSentry Node SDK acceptance')}`);
let matched;
for (const issue of search.data) {
  const detail = await api('GET', `/api/v1/issues/${encodeURIComponent(issue.id)}`);
  if (detail.event?.event_id === process.env.COMPAT_EVENT_ID) {
    matched = { issue: detail.issue, event: detail.event };
    break;
  }
}
assert.ok(matched, 'SDK event missing');
const project = projects.data.find(item => item.id === matched.issue.projectId);
assert.ok(project, 'SDK event project missing');
const snapshot = {
  organization: { id: org.id, slug: org.slug, name: org.name },
  project: { id: project.id, slug: project.slug, name: project.name },
  issue: {
    id: matched.issue.id, title: matched.issue.title, count: matched.issue.count,
    firstSeen: matched.issue.firstSeen, lastSeen: matched.issue.lastSeen,
  },
  event: {
    id: matched.event.id, eventID: matched.event.event_id, timestamp: matched.event.timestamp,
    message: matched.event.message, exception: matched.event.exception,
  },
};
if (process.env.COMPAT_OUTPUT) await writeFile(process.env.COMPAT_OUTPUT, JSON.stringify(snapshot, null, 2));
console.log(JSON.stringify(snapshot, null, 2));
