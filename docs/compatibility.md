# Compatibility and project status

[Documentation](README.md) · [Project home](../README.md)

Compatibility is verified per workflow and client version. The project remains in active development and has not reached full GlitchTip feature parity.

## What works today

| Capability | Verified behavior |
| --- | --- |
| Accounts and people | Password changes with session/token revocation, SMTP password recovery, member roles, teams, organization rename/delete and single-use SMTP invitations. Real browser acceptance includes no-organization account access and long invitation emails. |
| Error tracking | Store/envelope ingestion, issue grouping, duplicate suppression, historical tag/environment/release search, sorting/pages, user/team assignment, editable comments and recorded triage activity with regressions. |
| User feedback | Modern SDK submissions with optional error associations, per-submission attachments and paginated triage, plus legacy reports attached to their original error. |
| JavaScript source maps | Official CLI injection/bundle uploads and native uploads, Debug ID or release/distribution selection, original source context and preserved minified stacks. |
| Debug files | Project-scoped multipart dSYM/DIF file storage and listing with exact bytes, metadata and SHA-1 deduplication. Chunked DIF assembly and native symbolication remain pending. |
| Releases | Shared project versions, observed SDK releases, local commit metadata, completed deploys and compatible CLI lifecycle operations, with a responsive management UI. |
| Performance | Legacy transactions and span v2 segments, latency and error aggregates, time windows, child-operation statistics, stored occurrence detail and a cross-project trace waterfall. |
| Logs | Typed attributes, exact 64-bit integer display, deduplication, pagination and project/text/level/environment/service/time/trace filters. Native OpenTelemetry `/v1/logs` ingestion accepts standard OTLP/HTTP exporters (binary protobuf and JSON, optional gzip) without a Sentry SDK or extra collector. |
| Uptime | HTTP HEAD/GET/POST, TCP, SSL certificate and heartbeat monitors, failure thresholds, pause/resume, history and down/recovery transitions. |
| Alerts | Frequency and uptime rules, environment filters, cooldown, SMTP/consolemail and generic webhooks, durable retries and delivery attempt history. |
| Automation credentials | Expiring, revocable tokens limited to an organization and selected projects, one-time reveal, project metadata reads and optional release writes. |
| Management API | Sentry-compatible API root, current user, organization/project discovery and detail, organization rename/delete with responsive UI, member invitations, active-member detail/role/removal and team assignment, team reads and create/rename/delete, project creation in a team, many-to-many project-team attach/detach, organization/project environment visibility, relation reads, project-key management with DSN metadata and fixed-window Sentry-envelope/OTLP-log limits, strict pagination, session/Bearer scope isolation and instance settings/license metadata. |
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

Remaining work includes complete native crash symbolication, automatic repository integrations, further conditional issue policies, broader management APIs, a live GitLab receipt, social OAuth/OIDC sign-in, administration/OpenAPI and cold archival. Compatible member `teamRoles`, the `manager` role and `set_owner` remain unsupported; pending invitations are visible in member lists but are not yet manageable through the member-item route. Projects can belong to multiple teams and team-email alerts expand across every association; nonzero project throttling remains open. The opt-in MCP endpoint exposes the 17 documented tools through OAuth PKCE or scoped API tokens; its tool results are backed by GoodSentry's PostgreSQL event, span and log projections rather than DuckDB. Debug-file multipart and chunked DIF upload retain bytes and metadata; AMD64/x86/ARM64 minidumps and Breakpad or ELF/Mach-O DWARF symbols are processed, while other native symbol formats remain open.

Native Minidump ingestion currently supports AMD64, x86 and ARM64 dumps with textual Breakpad `.sym`, ELF/DWARF, Mach-O/DWARF or Mach-O dSYM companion files, including bounded ZIP archives containing multiple native objects. Frame-pointer chains are used first, then bounded DWARF CFI can extend stacks without frame pointers using the public ELF/Mach-O unwind sections. Raw dumps are parsed and discarded, and symbolicated/unresolved frames become normal issues. The official `sentry-cli 3.8.0` completes chunk negotiation and upload; uncommon CFI expressions, PDB/SymCache consumption and nested/tar archive discovery remain open.

Slack, Discord, Microsoft Teams, Google Chat, Feishu, ntfy and Zulip formats pass real local HTTP receiver, PostgreSQL and responsive browser checks. Receipts from external provider channels remain unverified. Configure and test your intended destination before relying on it.

Current-image acceptance covers official Go, Python, Node and Browser SDK errors, transactions and typed logs, plus official protobuf/JSON OTLP exporters. SSL checks verify a public CA certificate through the running scheduler. Upgrade, application restart and an independent backup restore preserve the tested rows and telemetry.

## SDK compatibility

| Client | Verified version | Error | Transaction | Typed logs | Notes |
| --- | --- | --- | --- | --- | --- |
| Go | `sentry-go 0.50.0` | Yes | Yes | Yes | Trace ancestry/timing and child operations verified. |
| Python | `sentry-sdk 2.72.0` | Yes | Yes | Yes | Trace correlation and typed attributes verified. |
| Node.js | `@sentry/node 11.7.0` | Yes | Yes | Yes | Trace ancestry/timing and child operations verified. |
| Browser | Browser SDK `11.7.0` | Yes | Yes | Yes | Official CDN bundle, cross-origin delivery and CORS verified in Chromium. |
| CLI | `sentry-cli 3.8.0` | n/a | n/a | n/a | Info, organization listing, project listing, releases and source-map workflows verified. |

Send standard Sentry envelopes to `/api/{project}/envelope/` or legacy `/api/{project}/store/`. OTLP/HTTP logs use `/v1/logs`. Change the existing SDK DSN or Sentry target URL; no GoodSentry-specific client is required for the workflows above.

The official Sentry data source in Grafana `13.0.2` has been exercised against GoodSentry for its organization/project health check and an issue query returning ten rows. GitLab Error Tracking's public client contract is covered by `/api/0/projects/`, project issues, issue detail, latest event and organization repositories; a full GitLab installation-level receipt remains an external integration test.

The verified modern upload protocol is the JavaScript SYSB v2 artifact-bundle workflow with `sentry-cli 3.8.0`; see [release and source-map setup](releases-and-source-maps.md). Other artifact types and CLI versions need their own acceptance. The 256–512 MB memory target has not been measured under a documented workload. Current container acceptance covers Linux amd64.

See the [complete specification](../SPEC.md), [implementation order](../tasks/plan.md) and [task evidence](../tasks/todo.md) before relying on an unlisted capability.
