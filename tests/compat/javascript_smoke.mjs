// Run with the pinned package.json dependencies against a disposable server.
// COMPAT_SENTRY_NODE may name a temporary installed package directory.
// COMPAT_ENVELOPE_CAPTURE records only our generated span envelopes for diagnostics.
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { createRequire } from "node:module";
import { writeFileSync } from "node:fs";
import { setTimeout as delay } from "node:timers/promises";

const require = createRequire(import.meta.url);
const sdkPackage = process.env.COMPAT_SENTRY_NODE || "@sentry/node";
const Sentry = require(sdkPackage);
const sdkVersion = require(`${sdkPackage}/package.json`).version;
const base = (process.env.GOODSENTRY_URL || "http://127.0.0.1:18000").replace(/\/$/, "");
const cookies = new Map();
let csrf = "";

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

async function eventually(check) {
  for (let attempt = 0; attempt < 25; attempt++) {
    if (await check()) return;
    await delay(200);
  }
  throw new Error("captured SDK telemetry was not visible through the authenticated API");
}

async function main() {
  assert.equal(sdkVersion, "11.7.0", "install the pinned official Node SDK");
  const me = await api("POST", "/api/v1/login", {
    email: process.env.COMPAT_EMAIL || "compat@example.test",
    password: process.env.COMPAT_PASSWORD || "long test pw",
  });
  csrf = me.csrfToken;
  assert.ok(csrf, "login must return a CSRF token");
  const orgPath = `/api/v1/organizations/${encodeURIComponent(me.organizations[0].slug)}`;
  const runID = randomBytes(8).toString("hex");
  const project = await api("POST", `${orgPath}/projects`, {
    name: `Node SDK ${runID}`, platform: "node",
  }, 201);
  const message = `GoodSentry Node SDK acceptance ${runID}`;
  const transactionName = `GoodSentry Node SDK transaction ${runID}`;
  const logBody = `GoodSentry Node SDK log ${runID}`;
  const options = {
    dsn: project.dsn, tracesSampleRate: 1, attachStacktrace: true,
    environment: "sdk-compat", release: "goodsentry-compat@1",
    debug: Boolean(process.env.COMPAT_DEBUG),
    beforeSendSpan(span) {
      if (process.env.COMPAT_DEBUG && JSON.stringify(span).includes(runID)) {
        console.error("SDK root span:", JSON.stringify(span));
      }
      return span;
    },
  };
  if (process.env.COMPAT_ENVELOPE_CAPTURE) {
    options.transport = (transportOptions) => {
      const transport = Sentry.makeNodeTransport(transportOptions);
      return {
        send(envelope) {
          if (envelope[1].some(([header]) => header.type === "span")) {
            writeFileSync(process.env.COMPAT_ENVELOPE_CAPTURE, JSON.stringify(envelope, (_, value) =>
              value instanceof Uint8Array ? { bytesBase64: Buffer.from(value).toString("base64") } : value, 2), { mode: 0o600 });
          }
          return transport.send(envelope);
        },
        flush(timeout) { return transport.flush(timeout); },
      };
    };
  }
  Sentry.init(options);
  const eventID = Sentry.captureException(new Error(message));
  assert.ok(eventID, "official Node SDK dropped the exception");
  let traceID;
  await Sentry.startSpan({ name: transactionName, op: "compat.node" }, async (span) => {
    traceID = span.spanContext().traceId;
    await Sentry.startSpan({ name: "SDK acceptance child", op: "compat.child" }, async () => {
      Sentry.logger.info(logBody, {
        "compat.run": runID, "compat.attempt": 7,
        "compat.ready": true, "compat.ratio": 1.5,
      });
      await Promise.resolve();
    });
  });
  assert.ok(await Sentry.flush(10000), "official Node SDK flush timed out");
  assert.ok(await Sentry.close(10000), "official Node SDK close timed out");

  await eventually(async () => {
    const listed = await api("GET", `${orgPath}/issues?query=${encodeURIComponent(message)}`);
    for (const candidate of listed.data) {
      const detail = await api("GET", `/api/v1/issues/${encodeURIComponent(candidate.id)}`);
      if (detail.event.event_id !== eventID) continue;
      assert.ok(detail.issue.title.includes(message), "stored issue lost the Node exception title");
      const exceptions = Array.isArray(detail.event.exception)
        ? detail.event.exception : detail.event.exception?.values || [];
      assert.ok(exceptions.some((value) => value.value === message), "stored event lost the Node exception value");
      assert.ok(detail.events.some((event) => event.eventID === eventID), "Node event is missing from issue event list");
      return true;
    }
    return false;
  });
  console.log(`PASS: @sentry/node ${sdkVersion} exception ${eventID} visible in project ${project.slug}`);

  const expectedAttributes = {
    "compat.run": { type: "string", value: runID },
    "compat.attempt": { type: "integer", value: 7 },
    "compat.ready": { type: "boolean", value: true },
    "compat.ratio": { type: "double", value: 1.5 },
  };
  await eventually(async () => {
    const listed = await api("GET", `${orgPath}/logs?query=${encodeURIComponent(logBody)}`);
    const row = listed.data.find((value) => value.body === logBody);
    if (!row) return false;
    assert.equal(row.traceId, traceID, "log lost the SDK trace ID");
    assert.equal(row.level, "info");
    for (const [key, expected] of Object.entries(expectedAttributes)) {
      assert.deepEqual(row.attributes[key], expected, `typed log attribute ${key} changed`);
    }
    return true;
  });
  console.log(`PASS: Node SDK log ${JSON.stringify(logBody)} retained trace ${traceID} and typed attributes`);

  await eventually(async () => {
    const listed = await api("GET", `${orgPath}/transactions`);
    const row = listed.data.find((value) => value.transaction === transactionName);
    if (!row) return false;
    assert.equal(row.latestTraceId, traceID, "transaction lost the SDK trace ID");
    assert.equal(row.op, "compat.node");
    assert.equal(row.count, 1);
    assert.equal(row.errorCount, 0);
    assert.ok(row.avgDuration > 0 && row.p50 > 0 && row.p95 >= row.p50, "invalid transaction duration metrics");
    return true;
  });
  console.log(`PASS: Node SDK transaction ${JSON.stringify(transactionName)} aggregated with trace ${traceID}`);

}

try {
  await main();
} catch (error) {
  console.error(`FAIL: ${error.message}`);
  process.exitCode = 1;
} finally {
  await Sentry.close(2000);
}
