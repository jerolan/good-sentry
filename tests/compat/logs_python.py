"""Run against a disposable GoodSentry instance:
GOODSENTRY_DSN=<project dsn> /path/to/venv/bin/python logs_python.py
"""
import os
import sys

import sentry_sdk
from sentry_sdk import logger, start_transaction


def main() -> int:
    dsn = os.environ.get("GOODSENTRY_DSN")
    if not dsn:
        print("GOODSENTRY_DSN is required", file=sys.stderr)
        return 2
    run = os.environ.get("GOODSENTRY_RUN", "run1")
    sentry_sdk.init(
        dsn=dsn,
        environment="logs-python-acceptance",
        release="logs-python@2.72.0",
        traces_sample_rate=1.0,
        enable_logs=True,
    )
    with start_transaction(name="logs-python-acceptance") as transaction:
        logger.info(
            "Python SDK info log " + run,
            attributes={
                "service.name": "logs-python-service",
                "component": "logs-python",
                "extreme": 9223372036854775807,
                "healthy": False,
                "ratio": 1.5,
            },
        )
        logger.warning("Python SDK warn log " + run, attributes={"reason": "filter-target", "service.name": "logs-python-service"})
        logger.error("Python SDK error log " + run, attributes={"attempt": 3, "service.name": "logs-python-service"})
        trace_id = str(transaction.trace_id)
        span_id = str(transaction.span_id)
    sentry_sdk.flush(timeout=10)
    print("trace", trace_id, "span", span_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
