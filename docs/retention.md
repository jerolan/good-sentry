# Retention

[Documentation](README.md) · [Project home](../README.md)

Lifetimes are configured independently for telemetry, files and release metadata. Cleanup runs in the application process; restoring deleted data requires an operator backup.

## Error event retention

Errors and message events expire by server receipt time. Set
`GOODSENTRY_EVENT_RETENTION_DAYS`, or its `GLITCHTIP_EVENT_RETENTION_DAYS`
alias, to override the master lifetime. The default is 90 days; valid values
are whole days from 1 to 3650.

Cleanup removes at most 250 events per transaction, together with their owned
attachments and opaque envelope items. Issue counts and summaries reflect the
remaining events. Empty issues keep their last known metadata, active grouping
signatures, comments, assignment, policies and activity. The UI shows **No
retained events** and keeps triage available. Conditional mute progress remains
independent of stored event counts.

New captures can return to the same active issue. A physically expired event
identity can receive a fresh lifetime; retained retries stay idempotent.
Selected event and original JSON reads return 404 after removal. Cleanup runs
inside the application, including with logs or uptime disabled. Recovery
requires an operator backup; increasing the lifetime does not restore data.

## Log retention

GoodSentry removes expired Sentry and OTLP logs in bounded PostgreSQL
transactions. Log age starts at the server's receipt time, so an application
timestamp does not shorten or extend its storage lifetime. The default is
90 days. The application runs cleanup at startup and after each idle interval,
including when new log ingestion is disabled.

`GOODSENTRY_LOG_RETENTION_DAYS` overrides `GLITCHTIP_LOG_RETENTION_DAYS`, then
inherits the master lifetime. The master precedence is
`GOODSENTRY_RETENTION_DAYS`, `GLITCHTIP_RETENTION_DAYS`, `RETENTION_DAYS`, then
90. Lifetimes must be whole days from 1 to 3650.
`GOODSENTRY_RETENTION_INTERVAL_SECONDS` defaults to 60 and accepts 1 to 3600.

Cleanup removes at most 250 log rows and 250 unreferenced OTLP batches per
transaction, then continues while work remains. Shared canonical batch bytes
remain until their last log expires. Deleted rows no longer appear in lists or
detail reads. Eligible rows can remain visible until the maintenance pass.
Recovery requires restoring an operator backup; increasing the lifetime does
not recover deleted logs. Expiration does not archive logs.

## Transaction and span retention

Legacy transaction events and standalone spans expire by server receipt time.
`GOODSENTRY_TRANSACTION_RETENTION_DAYS` overrides
`GLITCHTIP_TRANSACTION_RETENTION_DAYS`, then inherits the master lifetime above.
The default is 90 days; valid values are whole days from 1 to 3650.

Each PostgreSQL transaction removes at most 250 legacy transactions and 250
standalone spans. A legacy transaction's attachments, opaque envelope items and
span-operation projections leave with its receipt. Errors, logs and releases
keep their independent lifetimes, including when they share a trace. Standalone
opaque items without a selected parent follow the independent file lifetime.

Cleanup runs at startup and after the configured retention interval, including
when logs or uptime are disabled. Locked receipts wait for a later pass.
Transaction metrics and trace nodes reflect the surviving data; a trace can
still show related errors or logs after its last span expires. Retrying a
physically expired SDK identity can create a new receipt. Raising the lifetime
does not restore deleted data; recovery requires an operator backup.

## Uptime history retention

Completed monitor results expire after 90 days by default.
`GOODSENTRY_UPTIME_RETENTION_DAYS` overrides
`GLITCHTIP_UPTIME_RETENTION_DAYS`, then inherits the master lifetime. Values
must be whole days from 1 to 3650. Age starts when PostgreSQL records the
completed result; an earlier probe start does not shorten its lifetime.

Each cleanup transaction removes at most 250 expired check results. Cleanup
runs at startup and after the retention interval, including when uptime or
logs are disabled. Locked results wait for a later pass.

Status, failure counts, schedules, heartbeat URLs and alert history survive
result expiry. New checks continue from the last known state. The dashboard
shows **No retained result** when a previously checked monitor has no stored
results, and **Not checked yet** for a monitor that has never run. An empty
history filter does not hide a retained latest result. Increasing the lifetime
does not recover deleted history; recovery requires an operator backup.

## Completed file retention

Source maps, event attachments and stored opaque envelope items expire after
90 days by default. `GOODSENTRY_FILE_RETENTION_DAYS` overrides
`GLITCHTIP_FILE_RETENTION_DAYS`, then inherits the master lifetime. Values must
be whole days from 1 to 3650. PostgreSQL receipt time starts each file's age;
SDK timestamps do not shorten it.

Each transaction removes at most 250 receipts per file class. Cleanup runs at
startup and after the retention interval, including when logs or uptime are
disabled. Readers can finish their existing snapshot; later reads reflect
physical deletion.

File expiry preserves retained events, recorded stack frames, release
metadata, issue comments, assignments, mute progress and independent telemetry.
Native attachment lists become empty and selected downloads return 404 while
the parent event remains readable. Future captures cannot use an expired
source map. Uploading the same build again restores mapping for subsequent
captures; stored historical events keep their original frames.

Repeating a retained event can restore its expired attachment and opaque
children with fresh file receipts without incrementing the parent event count.
An opaque orphan can also start a fresh lifetime after deletion. Increasing
retention does not restore bytes; recovery requires an operator backup.

## Release metadata retention

Release identities expire 365 days after PostgreSQL first records their
organization and version. `GOODSENTRY_RELEASE_RETENTION_DAYS` overrides
`GLITCHTIP_RELEASE_RETENTION_DAYS`, then defaults to 365 independently of the
master and file lifetimes. Values must be whole days from 1 to 3650.

Each pass removes at most 250 release identities and their owned project
associations, commits and deployments. A shared release expires across all
its projects. Open and archived releases follow the same lifetime. Fresh
captures, added projects, finalization and client-supplied release dates do
not refresh the first receipt. Cleanup runs at startup and after the retention
interval, including with logs or uptime disabled.

Automatic expiry keeps source maps and other files on their independent
lifetimes. Events, stored stack frames, issue comments, assignments, mute
progress, saved resolution references and alert history survive. Missing
release metadata disappears from lists and selected metadata reads return
404. The issue's recorded release reference remains readable.

A fresh capture, native source-map upload or explicit CLI creation can record
the same version as a new identity with a fresh lifetime. Retrying a retained
event does not recreate metadata or increase its count. A retained source map
continues mapping new captures after release metadata expires. Explicit manual
release deletion keeps its existing behavior of removing associated release
files. Recovery of expired metadata requires an operator backup.
