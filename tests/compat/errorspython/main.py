"""Official sentry-sdk 2.72.0 error acceptance driver.

Run against a disposable GoodSentry with a project DSN:
GOODSENTRY_DSN=<dsn> GOODSENTRY_RUN=<run> python main.py

The driver uses the SDK's default transport. It prints the exact event IDs and
supplied metadata so a separate controller can verify API/UI receipts.
"""

import json
import os
import sys
import time

import sentry_sdk


def env(name, fallback):
    value = os.environ.get(name, "").strip()
    return value or fallback


def main():
    dsn = os.environ.get("GOODSENTRY_DSN", "").strip()
    if not dsn:
        raise SystemExit("GOODSENTRY_DSN is required")
    run_id = env("GOODSENTRY_RUN", str(time.time_ns()))
    mode = env("GOODSENTRY_ERRORS_MODE", "single")
    message = env("GOODSENTRY_ERRORS_MESSAGE", "Python SDK error " + run_id)
    environment = "errors-python-acceptance"
    release = "errors-python@1.0.0"
    tag = "errors-python-" + run_id
    user_id = "errors-python-user-" + run_id

    sentry_sdk.init(dsn=dsn, environment=environment, release=release, traces_sample_rate=0, send_default_pii=False)

    result = {
        "sdkVersion": sentry_sdk.VERSION,
        "mode": mode,
        "run": run_id,
        "message": message,
        "environment": environment,
        "release": release,
        "tag": tag,
        "userID": user_id,
        "eventIDs": [],
    }
    try:
        if mode == "single":
            with sentry_sdk.new_scope() as scope:
                scope.set_tag("acceptance.run", tag)
                scope.set_tag("component", "errors-python")
                scope.set_context("acceptance", {"run": run_id, "kind": "error"})
                scope.set_user({"id": user_id})
                event_id = sentry_sdk.capture_exception(RuntimeError(message))
            if not event_id:
                raise RuntimeError("official sentry-sdk dropped the exception")
            sentry_sdk.flush(timeout=10)
            result["eventIDs"] = [event_id]
        elif mode == "group":
            count = int(env("GOODSENTRY_ERRORS_COUNT", "3"))
            if not 1 <= count <= 61:
                raise RuntimeError("GOODSENTRY_ERRORS_COUNT must be 1..61")
            fingerprint = ["errors-python-group", run_id]
            result["fingerprint"] = fingerprint
            for index in range(count):
                with sentry_sdk.new_scope() as scope:
                    scope.fingerprint = fingerprint
                    scope.set_tag("acceptance.run", tag)
                    scope.set_tag("component", "errors-python")
                    scope.set_context("acceptance", {"run": run_id, "kind": "error", "occurrence": index})
                    scope.set_user({"id": user_id})
                    event_id = sentry_sdk.capture_exception(RuntimeError(message))
                if not event_id:
                    raise RuntimeError("official sentry-sdk dropped grouping capture %d" % index)
                sentry_sdk.flush(timeout=10)
                result["eventIDs"].append(event_id)
        elif mode == "retry":
            fixed = os.environ.get("GOODSENTRY_ERRORS_EVENT_ID", "").strip()
            if len(fixed) != 32:
                raise RuntimeError("GOODSENTRY_ERRORS_EVENT_ID must be 32 hex characters")
            fingerprint = ["errors-python-retry", run_id]
            result["fingerprint"] = fingerprint
            for attempt in range(2):
                event_id = sentry_sdk.capture_event({
                    "event_id": fixed,
                    "message": message,
                    "level": "error",
                    "platform": "python",
                    "timestamp": time.time(),
                    "environment": environment,
                    "release": release,
                    "fingerprint": fingerprint,
                    "tags": {"acceptance.run": tag, "component": "errors-python"},
                    "contexts": {"acceptance": {"run": run_id, "kind": "error", "attempt": attempt}},
                    "user": {"id": user_id},
                    "exception": {"values": [{"type": "RuntimeError", "value": message, "mechanism": {"type": "generic", "handled": False}}]},
                })
                if not event_id:
                    raise RuntimeError("official sentry-sdk dropped retry attempt %d" % attempt)
                sentry_sdk.flush(timeout=10)
                result["eventIDs"].append(event_id)
        else:
            raise RuntimeError("unknown GOODSENTRY_ERRORS_MODE %r" % mode)
    finally:
        sentry_sdk.flush(timeout=10)

    encoded = json.dumps(result)
    output = os.environ.get("GOODSENTRY_ERRORS_OUTPUT", "").strip()
    if output:
        with open(output, "w", encoding="utf-8") as handle:
            handle.write(encoded)
    print(encoded)


if __name__ == "__main__":
    main()
