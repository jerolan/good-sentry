"""Run the official OTLP protobuf/HTTP Python exporter against a disposable instance.

GOODSENTRY_OTLP_ENDPOINT=http://localhost:18127/v1/logs GOODSENTRY_KEY=<public key> python otlp_logs_exporter.py
"""
import os
import sys
import time

from opentelemetry._logs import SeverityNumber, set_logger_provider
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs._internal import LogRecord
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import set_span_in_context


def main() -> int:
    endpoint = os.environ.get("GOODSENTRY_OTLP_ENDPOINT")
    key = os.environ.get("GOODSENTRY_KEY")
    if not endpoint or not key:
        print("GOODSENTRY_OTLP_ENDPOINT and GOODSENTRY_KEY are required", file=sys.stderr)
        return 2
    run = os.environ.get("GOODSENTRY_RUN", "run1")
    resource = Resource.create({
        "service.name": "otlp-exporter-service",
        "deployment.environment.name": "otlp-exporter-acceptance",
    })
    provider = LoggerProvider(resource=resource)
    exporter = OTLPLogExporter(endpoint=endpoint, headers={"X-GoodSentry-Key": key})
    provider.add_log_record_processor(BatchLogRecordProcessor(exporter))
    set_logger_provider(provider)

    tracer_provider = TracerProvider(resource=resource)
    tracer_provider.add_span_processor(SimpleSpanProcessor(InMemorySpanExporter()))
    tracer = tracer_provider.get_tracer("otlp-exporter")
    records = [
        (SeverityNumber.INFO, "INFO", "Exporter info log " + run, {"component": "otlp-exporter", "attempt": 21, "healthy": False, "ratio": 1.5}),
        (SeverityNumber.WARN, "WARN", "Exporter warn log " + run, {"reason": "filter-target"}),
        (SeverityNumber.ERROR, "ERROR", "Exporter error log " + run, {"attempts": 5}),
    ]
    with tracer.start_as_current_span("otlp-logs-acceptance") as span:
        logger = provider.get_logger("otlp-exporter")
        context = span.get_span_context()
        for severity_number, severity_text, body, attributes in records:
            logger.emit(LogRecord(
                timestamp=time.time_ns(),
                observed_timestamp=time.time_ns(),
                severity_number=severity_number,
                severity_text=severity_text,
                body=body,
                attributes=attributes,
                trace_id=context.trace_id,
                span_id=context.span_id,
            ))
        trace_id = format(context.trace_id, "032x")
        span_id = format(context.span_id, "016x")
    provider.shutdown()
    tracer_provider.shutdown()
    print("trace", trace_id, "span", span_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
