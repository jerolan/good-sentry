// Uses an isolated Chromium page, the official pinned CDN bundle, and a real server.
// COMPAT_PLAYWRIGHT may name an existing temporary Playwright package directory.
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import { setTimeout as delay } from "node:timers/promises";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.COMPAT_PLAYWRIGHT || "playwright");
const base = (process.env.GOODSENTRY_URL || "http://127.0.0.1:18000").replace(/\/$/, "");
const bundle = "https://browser.sentry-cdn.com/11.7.0/bundle.tracing.logs.metrics.min.js";
const integrity = "sha384-4hVyRtcJfOjrFnfkkeOAlFOf15CvnDo0eQmbzzGuWjstMU6bsoKrVJWx1YIHYTRX";
const cookies = new Map();
let csrf = "";
let browser;
let fixture;

async function api(method, path, payload, expected = 200) {
  const headers = { Accept: "application/json" };
  if (cookies.size) headers.Cookie = [...cookies].map(([key, value]) => `${key}=${value}`).join("; ");
  if (payload !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && csrf) headers["X-CSRF-Token"] = csrf;
  const response = await fetch(base + path, {
    method, headers, signal: AbortSignal.timeout(15000),
    body: payload === undefined ? undefined : JSON.stringify(payload),
  });
  for (const cookie of response.headers.getSetCookie()) {
    const pair = cookie.split(";", 1)[0];
    const split = pair.indexOf("=");
    cookies.set(pair.slice(0, split), pair.slice(split + 1));
  }
  const body = await response.text();
  assert.equal(response.status, expected, `${method} ${path}: HTTP ${response.status}: ${body}`);
  return JSON.parse(body);
}

async function eventually(description, check) {
  for (let attempt = 0; attempt < 25; attempt++) {
    if (await check()) return;
    await delay(200);
  }
  throw new Error(`${description} was not visible through the authenticated API`);
}

async function main() {
  const me = await api("POST", "/api/v1/login", {
    email: process.env.COMPAT_EMAIL || "compat@example.test",
    password: process.env.COMPAT_PASSWORD || "long test pw",
  });
  csrf = me.csrfToken;
  assert.ok(csrf, "login must return a CSRF token");
  const orgPath = `/api/v1/organizations/${encodeURIComponent(me.organizations[0].slug)}`;
  const runID = randomBytes(8).toString("hex");
  const project = await api("POST", `${orgPath}/projects`, {
    name: `Browser SDK ${runID}`, platform: "javascript",
  }, 201);
  const message = `GoodSentry browser SDK acceptance ${runID}`;
  const transactionName = `GoodSentry browser SDK transaction ${runID}`;
  const logBody = `GoodSentry browser SDK log ${runID}`;

  fixture = createServer((request, response) => {
    response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    response.end(`<!doctype html><html lang="en"><title>SDK acceptance fixture</title>
      <script src="${bundle}" integrity="${integrity}" crossorigin="anonymous"></script>
      <body>GoodSentry browser SDK acceptance fixture</body></html>`);
  });
  await new Promise((resolve, reject) => {
    fixture.once("error", reject);
    fixture.listen(0, "127.0.0.1", resolve);
  });
  const origin = `http://127.0.0.1:${fixture.address().port}`;
  assert.notEqual(origin, new URL(project.dsn).origin, "fixture must use a separate origin");
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  const responses = [];
  const failures = [];
  const browserErrors = [];
  const ingestionPath = `/api/${project.id}/envelope/`;
  page.on("response", (response) => {
    if (new URL(response.url()).pathname === ingestionPath) {
      responses.push(Promise.all([response.request().allHeaders(), response.allHeaders()]).then(([requestHeaders, responseHeaders]) => ({
        status: response.status(), method: response.request().method(),
        origin: requestHeaders.origin,
        allowOrigin: responseHeaders["access-control-allow-origin"],
      })));
    }
  });
  page.on("requestfailed", (request) => {
    if (new URL(request.url()).pathname === ingestionPath) failures.push(request.failure()?.errorText);
  });
  page.on("pageerror", (error) => browserErrors.push(error.message));
  await page.goto(origin, { waitUntil: "load" });
  const captured = await page.evaluate(async ({ dsn, message, transactionName, logBody, runID }) => {
    const sdk = globalThis.Sentry;
    if (!sdk || sdk.SDK_VERSION !== "11.7.0") throw new Error("official pinned browser SDK failed to load");
    if (typeof sdk.logger?.info !== "function") throw new Error("official browser bundle lacks logs");
    sdk.init({
      dsn, tracesSampleRate: 1, attachStacktrace: true,
      environment: "sdk-compat", release: "goodsentry-compat@1",
    });
    const eventID = sdk.captureException(new Error(message));
    let traceID;
    await sdk.startSpan({ name: transactionName, op: "compat.browser" }, async (span) => {
      traceID = span.spanContext().traceId;
      await sdk.startSpan({ name: "SDK acceptance child", op: "compat.child" }, async () => {
        sdk.logger.info(logBody, {
          "compat.run": runID, "compat.attempt": 7,
          "compat.ready": true, "compat.ratio": 1.5,
        });
        await Promise.resolve();
      });
    });
    const flushed = await sdk.flush(10000);
    const closed = await sdk.close(10000);
    return { eventID, traceID, flushed, closed };
  }, { dsn: project.dsn, message, transactionName, logBody, runID });
  assert.ok(captured.eventID && captured.traceID, "browser SDK must produce event and trace IDs");
  assert.ok(captured.flushed && captured.closed, "browser SDK flush/close timed out");
  assert.deepEqual(browserErrors, [], "unexpected browser runtime errors");
  assert.deepEqual(failures, [], "browser ingestion failed, including CORS/network failures");
  const deliveries = (await Promise.all(responses)).filter((response) => response.method === "POST");
  assert.ok(deliveries.length, "no real browser SDK ingestion requests observed");
  for (const response of deliveries) {
    assert.ok(response.status >= 200 && response.status < 300, `browser ingestion HTTP ${response.status}`);
    assert.equal(response.origin, origin, "SDK ingestion must originate from the isolated page");
    assert.ok(response.allowOrigin === "*" || response.allowOrigin === origin, "CORS must allow the isolated client origin");
  }
  console.log(`PASS: Chromium ${browser.version()} browser SDK 11.7.0 delivered from isolated origin ${origin} with CORS`);

  await eventually("browser SDK exception", async () => {
    const listed = await api("GET", `${orgPath}/issues?query=${encodeURIComponent(message)}`);
    for (const candidate of listed.data) {
      const detail = await api("GET", `/api/v1/issues/${encodeURIComponent(candidate.id)}`);
      if (detail.event.event_id !== captured.eventID) continue;
      assert.ok(detail.issue.title.includes(message), "stored issue lost the browser exception title");
      const exceptions = Array.isArray(detail.event.exception)
        ? detail.event.exception : detail.event.exception?.values || [];
      assert.ok(exceptions.some((value) => value.value === message), "stored event lost the browser exception value");
      assert.ok(detail.events.some((event) => event.eventID === captured.eventID), "browser event is missing from issue event list");
      return true;
    }
    return false;
  });
  console.log(`PASS: browser SDK exception ${captured.eventID} visible in project ${project.slug}`);

  const expectedAttributes = {
    "compat.run": { type: "string", value: runID },
    "compat.attempt": { type: "integer", value: 7 },
    "compat.ready": { type: "boolean", value: true },
    "compat.ratio": { type: "double", value: 1.5 },
  };
  await eventually("browser SDK log", async () => {
    const listed = await api("GET", `${orgPath}/logs?query=${encodeURIComponent(logBody)}`);
    const row = listed.data.find((value) => value.body === logBody);
    if (!row) return false;
    assert.equal(row.traceId, captured.traceID, "browser log lost the SDK trace ID");
    assert.equal(row.level, "info");
    for (const [key, expected] of Object.entries(expectedAttributes)) {
      assert.deepEqual(row.attributes[key], expected, `browser typed log attribute ${key} changed`);
    }
    return true;
  });
  console.log(`PASS: browser SDK log ${JSON.stringify(logBody)} retained trace ${captured.traceID} and typed attributes`);

  await eventually("browser SDK transaction", async () => {
    const listed = await api("GET", `${orgPath}/transactions`);
    const row = listed.data.find((value) => value.transaction === transactionName);
    if (!row) return false;
    assert.equal(row.latestTraceId, captured.traceID, "browser transaction lost the SDK trace ID");
    assert.equal(row.op, "compat.browser");
    assert.equal(row.count, 1);
    assert.equal(row.errorCount, 0);
    assert.ok(row.avgDuration > 0 && row.p50 > 0 && row.p95 >= row.p50, "invalid browser transaction durations");
    return true;
  });
  console.log(`PASS: browser SDK transaction ${JSON.stringify(transactionName)} aggregated with trace ${captured.traceID}`);
}

try {
  await main();
} catch (error) {
  console.error(`FAIL: ${error.message}`);
  process.exitCode = 1;
} finally {
  if (browser) await browser.close();
  if (fixture) await new Promise((resolve) => fixture.close(resolve));
}
