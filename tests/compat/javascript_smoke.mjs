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
  const finishedSpans = new Map();
  const options = {
    dsn: project.dsn, tracesSampleRate: 1, attachStacktrace: true,
    environment: "sdk-compat", release: "goodsentry-compat@1",
    debug: Boolean(process.env.COMPAT_DEBUG),
    beforeSendSpan(span) {
      finishedSpans.set(span.span_id, {
        start_timestamp: span.start_timestamp, end_timestamp: span.end_timestamp,
      });
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
  let traceID, rootSpanID, childSpanID;
  await Sentry.startSpan({ name: transactionName, op: "compat.node" }, async (span) => {
    traceID = span.spanContext().traceId;
    rootSpanID = span.spanContext().spanId;
    await Sentry.startSpan({ name: "SDK acceptance child", op: "compat.child" }, async (child) => {
      childSpanID = child.spanContext().spanId;
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
    const reference = row.latestSample;
    assert.equal(reference.source, "span", "current Node SDK must select a standalone segment occurrence");
    assert.equal(reference.projectId, project.id, "aggregate linked an occurrence from another project");
    assert.match(reference.id, /^[1-9][0-9]*$/, "occurrence ID must be a positive decimal row identity");
    const detail = await api("GET", `${orgPath}/transactions/${reference.source}/${reference.id}?page=1`);
    assert.deepEqual(detail.sample, reference);
    assert.equal(detail.traceId, traceID);
    assert.equal(detail.spanId, rootSpanID);
    assert.equal(detail.parentSpanId, "");
    assert.equal(detail.name, transactionName);
    assert.equal(detail.op, "compat.node");
    assert.equal(detail.environment, "sdk-compat");
    assert.equal(detail.release, "goodsentry-compat@1");
    assert.equal(detail.duration, row.avgDuration, "detail selected a different duration than the aggregate occurrence");
    const emittedRoot = finishedSpans.get(rootSpanID);
    assert.ok(emittedRoot, "official SDK did not finish the expected root span");
    assert.ok(Number.isFinite(emittedRoot.start_timestamp) && Number.isFinite(emittedRoot.end_timestamp), "streaming SDK root timing must be finite Unix seconds");
    assert.ok(Math.abs(detail.duration - (emittedRoot.end_timestamp - emittedRoot.start_timestamp) * 1000) <= 0.002, `occurrence changed SDK root duration: detail=${detail.duration}, SDK start=${emittedRoot.start_timestamp} end=${emittedRoot.end_timestamp}`);
    assert.equal(detail.spanScope, "projectTrace");
    assert.equal(detail.spanCount, 2);
    assert.equal(detail.spans.length, 2);
    assert.equal(detail.page, 1);
    assert.equal(detail.pageSize, 50);
    assert.equal(detail.hasMore, false);
    assert.equal(detail.data.is_segment, true);
    assert.equal(detail.data.trace_id, traceID);
    assert.equal(detail.data.span_id, rootSpanID);
    assert.equal(detail.data.name, transactionName);
    for (const [key, value] of Object.entries({
      "sentry.op": "compat.node", "sentry.environment": "sdk-compat", "sentry.release": "goodsentry-compat@1",
    })) assert.deepEqual(detail.data.attributes[key], { type: "string", value }, `raw root attribute ${key} changed`);
    const child = detail.spans.find((value) => value.spanId === childSpanID);
    assert.ok(child, "occurrence lost the SDK child span");
    assert.equal(child.parentSpanId, rootSpanID);
    assert.equal(child.name, "SDK acceptance child");
    assert.equal(child.op, "compat.child");
    assert.equal(child.data.is_segment, false);
    assert.equal(child.data.trace_id, traceID);
    assert.equal(child.data.span_id, childSpanID);
    assert.equal(child.data.parent_span_id, rootSpanID);
    assert.equal(child.data.name, "SDK acceptance child");
    assert.deepEqual(child.data.attributes["sentry.op"], { type: "string", value: "compat.child" });
    assert.deepEqual(child.data.attributes["sentry.segment.id"], { type: "string", value: rootSpanID });
    const emittedChild = finishedSpans.get(childSpanID);
    assert.ok(emittedChild, "official SDK did not finish the expected child span");
    assert.ok(Number.isFinite(emittedChild.start_timestamp) && Number.isFinite(emittedChild.end_timestamp), "streaming SDK child timing must be finite Unix seconds");
    assert.ok(child.duration !== null && child.duration >= 0 && Math.abs(child.duration - (emittedChild.end_timestamp - emittedChild.start_timestamp) * 1000) <= 0.002, "occurrence changed SDK child duration");
    for (const value of [detail, child]) {
      assert.ok(value.startTimestamp?.endsWith("Z") && value.endTimestamp?.endsWith("Z"), "normalized SDK timing must remain available in UTC");
      assert.ok(Date.parse(value.endTimestamp) >= Date.parse(value.startTimestamp), "normalized SDK timing reversed");
    }
    const operations = await api("GET", `${orgPath}/transactions/${reference.source}/${reference.id}/span-groups?project=${project.id}`);
    assert.deepEqual(operations.sample, reference);
    assert.equal(operations.transactionCount, 1);
    assert.equal(operations.spanCount, 1, "segment roots must not inflate child-operation counts");
    assert.equal(operations.timedSpanCount, 1);
    assert.equal(operations.data.length, 1);
    const operation = operations.data[0];
    assert.equal(operation.op, "compat.child");
    assert.equal(operation.description, "SDK acceptance child");
    assert.equal(operation.count, 1);
    assert.equal(operation.timedCount, 1);
    const expectedDuration = (emittedChild.end_timestamp - emittedChild.start_timestamp) * 1000;
    for (const metric of [operation.avgDuration, operation.p95Duration, operation.totalTime]) {
      assert.ok(typeof metric === "number" && Math.abs(metric - expectedDuration) <= 0.002, `child-operation metric ${metric} differs from emitted SDK duration ${expectedDuration}`);
    }
    const tracePath = `${orgPath}/traces/${traceID}`;
    const trace = await api("GET", `${tracePath}?project=${project.id}`);
    assert.equal(trace.traceId, traceID);
    assert.equal(trace.projectId, project.id);
    assert.equal(trace.nodeCount, 2);
    assert.equal(trace.projectCount, 1);
    assert.equal(trace.logsEnabled, true);
    assert.ok(trace.logCount >= 1, "trace lost its official SDK log correlation");
    assert.equal(trace.data.length, 2);
    const [rootNode, childNode] = trace.data;
    assert.deepEqual(trace.data.map((node) => node.id), [`span-${reference.id}`, `span-${child.id}`], "trace forest lost concrete SDK root/child order");
    assert.equal(rootNode.kind, "segment");
    assert.equal(rootNode.recordId, reference.id);
    assert.equal(rootNode.parentSpanId, "");
    assert.equal(rootNode.parentStatus, "none");
    assert.equal(rootNode.parentId, "");
    assert.equal(rootNode.parentPage, 0);
    assert.equal(rootNode.depth, 0);
    assert.equal(rootNode.childCount, 1);
    assert.equal(childNode.kind, "span");
    assert.equal(childNode.recordId, child.id);
    assert.equal(childNode.parentSpanId, rootSpanID);
    assert.equal(childNode.parentStatus, "resolved");
    assert.equal(childNode.parentId, rootNode.id);
    assert.equal(childNode.parentPage, 1);
    assert.equal(childNode.depth, 1);
    assert.equal(childNode.childCount, 0);
    const utcMilliseconds = (value) => {
      assert.equal(typeof value, "string", "trace timing must be an available UTC instant");
      const parts = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/.exec(value);
      assert.ok(parts, `trace timing must be UTC RFC3339: ${value}`);
      return Date.parse(`${parts[1]}Z`) + Number(`0.${parts[2] || "0"}`) * 1000;
    };
    assert.ok(Math.abs(utcMilliseconds(trace.startTimestamp) - emittedRoot.start_timestamp * 1000) <= 0.002, "trace start changed emitted SDK timing");
    assert.ok(Math.abs(utcMilliseconds(trace.endTimestamp) - emittedRoot.end_timestamp * 1000) <= 0.002, "trace end changed emitted SDK timing");
    assert.ok(typeof trace.duration === "number" && Math.abs(trace.duration - (emittedRoot.end_timestamp - emittedRoot.start_timestamp) * 1000) <= 0.002, "trace duration changed emitted SDK root interval");
    for (const [node, emitted, spanId, name, op] of [
      [rootNode, emittedRoot, rootSpanID, transactionName, "compat.node"],
      [childNode, emittedChild, childSpanID, "SDK acceptance child", "compat.child"],
    ]) {
      assert.equal(node.source, "span");
      assert.equal(node.ordinal, 0);
      assert.equal(node.projectId, project.id);
      assert.equal(node.spanId, spanId);
      assert.equal(node.spanIdStatus, "valid");
      assert.equal(node.name, name);
      assert.equal(node.op, op);
      assert.equal(node.environment, "sdk-compat");
      assert.equal(node.release, "goodsentry-compat@1");
      assert.ok(Math.abs(utcMilliseconds(node.startTimestamp) - emitted.start_timestamp * 1000) <= 0.002, "trace node start changed emitted SDK timing");
      assert.ok(Math.abs(utcMilliseconds(node.endTimestamp) - emitted.end_timestamp * 1000) <= 0.002, "trace node end changed emitted SDK timing");
      assert.ok(typeof node.duration === "number" && Math.abs(node.duration - (emitted.end_timestamp - emitted.start_timestamp) * 1000) <= 0.002, "trace node duration changed emitted SDK interval");
      assert.ok(typeof node.offset === "number" && Math.abs(node.offset - (emitted.start_timestamp - emittedRoot.start_timestamp) * 1000) <= 0.002, "trace node offset changed emitted SDK position");
    }
    const raw = await api("GET", `${tracePath}/nodes/${childNode.id}?project=${project.id}`);
    assert.equal(raw.id, childNode.id);
    assert.equal(raw.source, "span");
    assert.equal(raw.recordId, child.id);
    assert.equal(raw.ordinal, 0);
    assert.equal(raw.projectId, project.id);
    assert.equal(raw.data.trace_id, traceID);
    assert.equal(raw.data.span_id, childSpanID);
    assert.equal(raw.data.parent_span_id, rootSpanID);
    assert.equal(raw.data.name, "SDK acceptance child");
    assert.equal(raw.data.is_segment, false);
    assert.deepEqual(raw.data.attributes["sentry.op"], { type: "string", value: "compat.child" });
    assert.deepEqual(raw.data.attributes["sentry.segment.id"], { type: "string", value: rootSpanID });
    return true;
  });
  console.log(`PASS: Node SDK transaction ${JSON.stringify(transactionName)} retained occurrence context, child-operation metrics and trace ancestry/timing for trace ${traceID}`);

}

try {
  await main();
} catch (error) {
  console.error(`FAIL: ${error.message}`);
  process.exitCode = 1;
} finally {
  await Sentry.close(2000);
}
