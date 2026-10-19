# Logs, performance, uptime and alerts

[Documentation](README.md) · [Project home](../README.md)

Inspect telemetry and operate monitors from the same GoodSentry workspace.

## OpenTelemetry logs

Open **Projects** and expand the project's **OpenTelemetry** panel. Copy its
OTLP endpoint and `X-GoodSentry-Key` header. The endpoint is
`https://your-host/v1/logs`, including your configured path prefix, while the
header selects the project. A Sentry DSN is a different address.

Use an OTLP/HTTP logs exporter with `http/protobuf` or `http/json`. Gzip is
supported. The Projects panel includes an executable Python example with the
verified `opentelemetry-sdk==1.45.1` and protobuf HTTP exporter `1.45.1`.
The official JSON HTTP exporter `0.66b1` also passes real network acceptance.

Logs retain resource, scope and record values, exact integer and nanosecond
strings, bytes and trace identifiers. Expand a row to fetch its typed detail.
Repeated OTLP exports create repeated occurrences, including identical records
within a batch. Delivery is at least once. Existing Sentry log deduplication
continues to use the SDK envelope identity. Only the logs signal is exposed
through OTLP; this endpoint does not accept OTLP traces or metrics.

## Time windows and service filters

Transactions and Logs offer past-hour, 24-hour, seven-day and 30-day presets. Applying one writes absolute UTC bounds into the URL. Pagination, other filters and reload reuse those bounds. Transactions default to the last 24 hours; Logs default to all stored history. Service matching is exact, including case and spaces.

The native query APIs accept paired RFC3339 `start` and `end` instants with time zones and a maximum interval of 366 days. They normalize to UTC microseconds, include the start and exclude the end. Logs also accepts `service`. Invalid or incomplete ranges return 400.

Open a transaction name to inspect its latest stored occurrence. Legacy events show their nested spans; standalone segments show spans in the same project and trace, with their recorded parent IDs. Spans use 50-row pages and retain unavailable timing explicitly. Detail and related-log navigation preserve the originating filters; those time bounds do not hide the occurrence's spans.

Choose **Operations** for a transaction group's child-operation counts, average, p95 and accumulated time. The breakdown combines matching legacy transactions and standalone segments within the selected project and interval. Standalone children use their recorded segment association; roots are excluded. Counts include unavailable timing, while duration metrics use valid durations only. Accumulated time includes overlapping spans.

Choose **View trace** from a transaction, occurrence, log or error to inspect its stored spans across projects. The waterfall uses recorded parent IDs and UTC timestamps, with 50-node pages, links to parents on other pages, selected raw JSON and related errors and logs. Missing, ambiguous and cyclic relationships remain visible. Incomplete timing stays unavailable; zero-duration spans appear as points. A trace includes all stored time in its selected project scope, and Back restores the originating filters and page.

On upgrade, GoodSentry indexes historical legacy spans inside the application, one event per transaction. This resumes after restart and preserves original event JSON and operation statistics. Pending history shows an indexing message and retry action. Related trace logs include historical uppercase IDs; ordinary Logs filters retain exact matching.

## Monitor and alert behavior

Monitor intervals range from 1 to 86400 seconds and timeouts from 1 to 60. Consecutive failures confirm a down state; one successful result recovers it. GET/POST expected-body matching uses a literal substring within the first MiB. POST sends an empty request body. Heartbeats accept POST before each deadline; pausing retains history and stops checks. Owners/admins manage monitors; members read their state and history.

Network checks and webhooks reject private, loopback, link-local and multicast targets by default. Explicitly enable their separate private-IP settings for internal services. HTTPS verifies certificates. Webhooks do not follow redirects or use environment proxies.

Frequency alerts count received errors and message events, excluding duplicate retries, transactions and logs. Uptime alerts use the monitor's associated project and optional environment. Editing or deleting a rule cancels pending work while retaining completed history.

`consolemail://` writes MIME messages to the console. SMTP credentials require verified TLS. Generic webhooks receive compact JSON and a resource link. Transient failures retry after 15, 30, 60 seconds and so on, capped at 900 seconds. A crash after remote acceptance can cause a repeat with the same `X-GoodSentry-Delivery-ID`, JSON ID and email Message-ID.
