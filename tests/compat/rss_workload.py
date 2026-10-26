#!/usr/bin/env python3
"""Repeatable GoodSentry ingestion workload for application RSS measurements.

The workload uses only the public envelope boundary. It does not read server
source or use internal packages. Set GOODSENTRY_LOAD_DSN to a project DSN.
"""

import json
import os
import random
import string
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed


def event_id():
    return "".join(random.choice("0123456789abcdef") for _ in range(32))


def trace_id():
    return event_id()


def span_id():
    return event_id()[:16]


def error_event(index):
    return {
        "event_id": event_id(),
        "message": f"RSS workload failure {index}",
        "level": "error",
        "platform": "python",
        "timestamp": time.time(),
        "environment": "rss-workload",
        "release": "rss-workload@1.0.0",
        "tags": {"workload": "rss", "worker": str(index % 16)},
    }


def transaction_event(index):
    started = time.time() - random.random() * 5
    duration = 0.05 + random.random() * 0.5
    tid = trace_id()
    spans = []
    for child in range(5):
        child_start = started + duration * child / 5
        spans.append(
            {
                "span_id": span_id(),
                "trace_id": tid,
                "op": "db.query",
                "description": f"load child {child}",
                "start_timestamp": child_start,
                "timestamp": child_start + duration / 5,
                "status": "ok",
            }
        )
    return {
        "type": "transaction",
        "event_id": event_id(),
        "transaction": f"GET /rss-workload/{index % 10}",
        "platform": "python",
        "level": "info",
        "start_timestamp": started,
        "timestamp": started + duration,
        "environment": "rss-workload",
        "release": "rss-workload@1.0.0",
        "contexts": {
            "trace": {"trace_id": tid, "span_id": span_id(), "op": "http.server", "status": "ok"}
        },
        "spans": spans,
    }


def log_items(start):
    return {
        "items": [
            {
                "timestamp": time.time() - random.random() * 60,
                "level": random.choice(["info", "warn", "error"]),
                "body": f"RSS workload log {start + i}",
                "attributes": {
                    "service.name": {"type": "string", "value": "rss-workload"},
                    "environment": {"type": "string", "value": "rss-workload"},
                    "sequence": {"type": "integer", "value": start + i},
                },
            }
            for i in range(10)
        ]
    }


def envelope(header, items):
    parts = [json.dumps(header, separators=(",", ":"))]
    for item_type, payload, extra in items:
        raw = json.dumps(payload, separators=(",", ":"))
        parts.append(json.dumps({"type": item_type, "length": len(raw.encode()), **extra}, separators=(",", ":")))
        parts.append(raw)
    return "\n".join(parts) + "\n"


def main():
    dsn = os.environ.get("GOODSENTRY_LOAD_DSN", "")
    if not dsn:
        print("GOODSENTRY_LOAD_DSN is required", file=sys.stderr)
        return 2
    parsed = urllib.parse.urlsplit(dsn)
    segments = parsed.path.strip("/").split("/")
    project = segments[-1]
    if not parsed.hostname or not project:
        print("DSN must include a host and project id", file=sys.stderr)
        return 2
    prefix = "/".join(segments[:-1])
    host = parsed.hostname or ""
    if ":" in host:
        host = f"[{host}]"
    netloc = host + (f":{parsed.port}" if parsed.port else "")
    base = f"{parsed.scheme}://{netloc}" + (f"/{prefix}" if prefix else "")
    endpoint = f"{base}/api/{project}/envelope/?sentry_version=7&sentry_key={urllib.parse.quote(parsed.username or '')}"

    errors = int(os.environ.get("GS_LOAD_ERRORS", "1600"))
    logs = int(os.environ.get("GS_LOAD_LOGS", "4000"))
    transactions = int(os.environ.get("GS_LOAD_TRANSACTIONS", "100"))
    workers = int(os.environ.get("GS_LOAD_WORKERS", "16"))

    jobs = []
    for i in range(errors):
        jobs.append(("error", i))
    for i in range(0, logs, 10):
        jobs.append(("log", i))
    for i in range(transactions):
        jobs.append(("transaction", i))
    random.shuffle(jobs)

    statuses = {}
    failures = []

    def send(job):
        kind, index = job
        if kind == "error":
            body = envelope({"dsn": dsn}, [("event", error_event(index), {})])
        elif kind == "transaction":
            body = envelope({"dsn": dsn}, [("transaction", transaction_event(index), {})])
        else:
            body = envelope(
                {"dsn": dsn},
                [("log", log_items(index), {"content_type": "application/vnd.sentry.items.log+json", "item_count": 10})],
            )
        request = urllib.request.Request(
            endpoint,
            data=body.encode(),
            method="POST",
            headers={
                "Content-Type": "application/x-sentry-envelope",
                "X-Sentry-Auth": f"Sentry sentry_version=7,sentry_key={urllib.parse.quote(parsed.username or '')},sentry_client=goodsentry-rss-workload/1.0",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return kind, response.status, ""
        except urllib.error.HTTPError as error:
            return kind, error.code, error.read().decode(errors="replace")[:200]
        except Exception as error:  # noqa: BLE001 - report the transport failure
            return kind, 0, str(error)[:200]

    started = time.monotonic()
    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(send, job) for job in jobs]
        for future in as_completed(futures):
            kind, status, detail = future.result()
            statuses[(kind, status)] = statuses.get((kind, status), 0) + 1
            if status != 200:
                failures.append({"kind": kind, "status": status, "detail": detail})
    elapsed = time.monotonic() - started
    report = {
        "requests": len(jobs),
        "errors": errors,
        "logs": logs,
        "transactions": transactions,
        "workers": workers,
        "elapsed_seconds": round(elapsed, 3),
        "requests_per_second": round(len(jobs) / elapsed, 1),
        "statuses": {f"{kind}:{status}": count for (kind, status), count in sorted(statuses.items())},
        "failures": failures[:20],
        "failure_count": len(failures),
    }
    print(json.dumps(report, indent=2))
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
