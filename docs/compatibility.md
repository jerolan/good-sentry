# Compatibility and project status

[Documentation](README.md) · [Project home](../README.md)

Compatibility is verified per workflow and client version. The project remains in active development and has not reached full GlitchTip feature parity.

## What works today

| Capability | Verified behavior |
| --- | --- |
| Error tracking | Store/envelope ingestion, issue grouping, duplicate suppression, historical tag/environment/release search, sorting/pages, user/team assignment, editable comments and recorded triage activity with regressions. |
| User feedback | Modern SDK submissions with optional error associations, per-submission attachments and paginated triage, plus legacy reports attached to their original error. |
| JavaScript source maps | Official CLI injection/bundle uploads and native uploads, Debug ID or release/distribution selection, original source context and preserved minified stacks. |
| Releases | Shared project versions, observed SDK releases, local commit metadata, completed deploys and compatible CLI lifecycle operations, with a responsive management UI. |
| Performance | Legacy transactions and span v2 segments, latency and error aggregates, time windows, child-operation statistics, stored occurrence detail and a cross-project trace waterfall. |
| Logs | Typed attributes, exact 64-bit integer display, deduplication, pagination and project/text/level/environment/service/time/trace filters. Native OpenTelemetry `/v1/logs` ingestion accepts standard OTLP/HTTP exporters (binary protobuf and JSON, optional gzip) without a Sentry SDK or extra collector. |
| Uptime | HTTP HEAD/GET/POST, TCP and heartbeat monitors, failure thresholds, pause/resume, history and down/recovery transitions. |
| Alerts | Frequency and uptime rules, environment filters, cooldown, SMTP/consolemail and generic webhooks, durable retries and delivery attempt history. |
| Automation credentials | Expiring, revocable tokens limited to an organization and selected projects, one-time reveal, project metadata reads and optional release writes. |
| Deployment | One non-root application image, PostgreSQL-only coordination, embedded Tailwind CSS and persistence across application restarts. |

The browser workflows run at 320, 768 and 1440 pixels. PostgreSQL integration and race tests cover concurrent ingestion, duplicate retries, tenant boundaries, fenced worker leases and restart recovery. Alert acceptance uses actual local HTTP/SMTP receivers, including retries across two application replicas.

## SDK compatibility

The following pinned official clients have passed error, transaction and typed-log capture followed by authenticated retrieval:

| SDK | Verified version |
| --- | --- |
| Go | `v0.50.0` |
| Python | `2.72.0` |
| Node.js | `11.7.0` |
| Browser JavaScript | `11.7.0` |

The Go and Node checks also verify occurrence context, child-operation metrics and trace parent relationships, depth, timing and selected raw records against data emitted by each SDK.

For an existing integration, start by changing its DSN. Keep the SDK's usual sampling and log settings. For example, with the verified Node SDK:

```js
import * as Sentry from "@sentry/node";

Sentry.init({
  dsn: process.env.SENTRY_DSN, // Copy the DSN from your GoodSentry project.
  tracesSampleRate: 1.0,
  enableLogs: true,
});

Sentry.captureException(new Error("Hello from GoodSentry"));
```

These checks establish the listed workflows for those versions. Node SDK `11.7.0` also passes binary attachments with native metadata, pagination and byte-exact downloads for organization owners and members. Additional envelope types and other SDK versions need their own verification. Unknown envelope items are retained and reported through `unsupportedItems`; retention alone does not implement their functionality.

## Browser security reports

Browsers can send Content Security Policy violations directly to GoodSentry,
without an SDK. Use your project ID and the public key from its DSN:

```http
Content-Security-Policy-Report-Only: default-src 'self'; report-uri https://errors.example.com/api/PROJECT_ID/security/?sentry_key=PUBLIC_KEY
```

Both `/security/` and `/csp-report/` accept legacy `application/csp-report`
objects and Reporting API `application/reports+json` arrays. Add your configured
path prefix when applicable. Optional `sentry_release` and
`sentry_environment` query values label the captured issues. Reports retain
the directive, blocked resource, policy, source location and original browser
fields. Only CSP violation reports are supported; other Reporting API report
types are rejected. Batches are limited to 100 reports and the configured
request body limit. Each browser delivery counts as a separate occurrence.

## Current gaps

Remaining work includes automatic repository integration, native debug-file/minidump uploads and symbolication, further conditional policies, invitations and password recovery, SSL certificate monitors, provider-specific alert formats, management API parity, integrations, MCP, OAuth/OIDC and cold archival.

The verified modern upload protocol is the JavaScript SYSB v2 artifact-bundle workflow with `sentry-cli 3.8.0`; see [release and source-map setup](releases-and-source-maps.md). Other artifact types and CLI versions need their own acceptance. The 256–512 MB memory target has not been measured under a documented workload. Current container acceptance covers Linux amd64.

See the [complete specification](../SPEC.md), [implementation order](../tasks/plan.md) and [task evidence](../tasks/todo.md) before relying on an unlisted capability.
