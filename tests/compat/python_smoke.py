#!/usr/bin/env python3
"""Run with pinned python-requirements.txt against a disposable GoodSentry server."""

import http.cookiejar
from importlib.metadata import version
import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

import sentry_sdk


class API:
    def __init__(self):
        self.base = os.environ.get("GOODSENTRY_URL", "http://127.0.0.1:18000").rstrip("/")
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
        )
        self.csrf = ""

    def call(self, method, path, payload=None, expected=200):
        headers = {"Accept": "application/json"}
        body = None
        if payload is not None:
            body = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        if method != "GET" and self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        request = urllib.request.Request(self.base + path, body, headers, method=method)
        try:
            with self.opener.open(request, timeout=15) as response:
                if response.status != expected:
                    raise AssertionError(f"{method} {path}: HTTP {response.status}, expected {expected}")
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise AssertionError(f"{method} {path}: HTTP {error.code}: {error.read().decode()}") from error


def eventually(check):
    for _ in range(25):
        result = check()
        if result:
            return result
        time.sleep(0.2)
    raise AssertionError("captured SDK telemetry was not visible through the authenticated API")


def main():
    installed = version("sentry-sdk")
    if installed != "2.72.0":
        raise AssertionError(f"install pinned sentry-sdk 2.72.0, found {installed}")
    api = API()
    me = api.call("POST", "/api/v1/login", {
        "email": os.environ.get("COMPAT_EMAIL", "compat@example.test"),
        "password": os.environ.get("COMPAT_PASSWORD", "long test pw"),
    })
    api.csrf = me["csrfToken"]
    org = urllib.parse.quote(me["organizations"][0]["slug"], safe="")
    org_path = f"/api/v1/organizations/{org}"
    run_id = secrets.token_hex(8)
    project = api.call("POST", org_path + "/projects", {
        "name": "Python SDK " + run_id, "platform": "python",
    }, expected=201)
    message = "GoodSentry Python SDK acceptance " + run_id
    transaction_name = "GoodSentry Python SDK transaction " + run_id
    log_body = "GoodSentry Python SDK log " + run_id
    sentry_sdk.init(
        dsn=project["dsn"], traces_sample_rate=1.0, attach_stacktrace=True,
        environment="sdk-compat", release="goodsentry-compat@1",
        debug=bool(os.environ.get("COMPAT_DEBUG")),
    )
    try:
        raise RuntimeError(message)
    except RuntimeError as error:
        event_id = sentry_sdk.capture_exception(error)
    if not event_id:
        raise AssertionError("official Python SDK dropped the exception")
    with sentry_sdk.start_transaction(name=transaction_name, op="compat.python") as transaction:
        trace_id = transaction.trace_id
        with sentry_sdk.start_span(op="compat.child", description="SDK acceptance"):
            sentry_sdk.logger.info(log_body, attributes={
                "compat.run": run_id, "compat.attempt": 7,
                "compat.ready": True, "compat.ratio": 1.5,
            })
    sentry_sdk.flush(timeout=10)

    def find_issue():
        rows = api.call("GET", org_path + "/issues?query=" + urllib.parse.quote(message))["data"]
        for row in rows:
            detail = api.call("GET", "/api/v1/issues/" + urllib.parse.quote(row["id"]))
            event = detail["event"]
            if event.get("event_id") != event_id:
                continue
            if message not in detail["issue"]["title"]:
                raise AssertionError("stored issue lost the actual Python exception title")
            exceptions = event.get("exception", [])
            if isinstance(exceptions, dict):
                exceptions = exceptions.get("values", [])
            if not any(value.get("value") == message for value in exceptions):
                raise AssertionError("stored event lost the actual Python exception value")
            if not any(value["eventID"] == event_id for value in detail["events"]):
                raise AssertionError("Python SDK event is missing from the issue's event list")
            return True
        return False

    eventually(find_issue)
    print(f"PASS: sentry-sdk {installed} exception {event_id} visible in project {project['slug']}")

    def find_transaction():
        for row in api.call("GET", org_path + "/transactions")["data"]:
            if row["transaction"] != transaction_name:
                continue
            if (row["latestTraceId"], row["op"], row["count"], row["errorCount"]) != (trace_id, "compat.python", 1, 0):
                raise AssertionError(f"Python SDK transaction lost trace, operation or counts: {row}")
            if not (row["avgDuration"] > 0 and row["p50"] > 0 and row["p95"] >= row["p50"]):
                raise AssertionError(f"Python SDK transaction has invalid duration metrics: {row}")
            return True
        return False

    eventually(find_transaction)
    print(f"PASS: Python SDK transaction {transaction_name!r} aggregated with trace {trace_id}")
    expected_attributes = {
        "compat.run": {"type": "string", "value": run_id},
        "compat.attempt": {"type": "integer", "value": 7},
        "compat.ready": {"type": "boolean", "value": True},
        "compat.ratio": {"type": "double", "value": 1.5},
    }

    def find_log():
        rows = api.call("GET", org_path + "/logs?query=" + urllib.parse.quote(log_body))["data"]
        for row in rows:
            if row["body"] != log_body:
                continue
            if (row["traceId"], row["level"]) != (trace_id, "info"):
                raise AssertionError(f"Python SDK log lost trace correlation or severity: {row}")
            for key, expected in expected_attributes.items():
                actual = row["attributes"].get(key)
                if actual != expected or type(actual["value"]) is not type(expected["value"]):
                    raise AssertionError(f"Python SDK log attribute {key} changed: {actual}, expected {expected}")
            return True
        return False

    eventually(find_log)
    print(f"PASS: Python SDK log {log_body!r} retained trace {trace_id} and typed attributes")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"FAIL: {error}", file=sys.stderr)
        sys.exit(1)
