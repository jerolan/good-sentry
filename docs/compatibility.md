# Compatibility and project status

[Documentation](README.md) · [Project home](../README.md)

Compatibility is verified per workflow and client version. The project remains in active development and has not reached full GlitchTip feature parity.

## What works today

| Capability | Verified behavior |
| --- | --- |
| Accounts and people | Default cookie-free open mode with an implicit owner/workspace, or `GOODSENTRY_AUTH=required` for password changes with session/token revocation, SMTP password recovery, optional TOTP two-factor sign-in with single-use recovery codes, GlitchTip-compatible account API tokens, staged recovery codes and invitation acceptance, per-organization OpenID Connect sign-in applications with a shareable login page, current-account deletion from the API or Account page with typed confirmation and sole-owner recovery, member roles, teams, organization rename/delete and single-use SMTP invitations. Real browser acceptance includes open dashboard startup, required setup/login, two-factor enrollment and challenge sign-in at 390 and 1440, no-organization account access and long invitation emails. |
| Error tracking | Store/envelope ingestion, issue grouping, duplicate suppression, historical tag/environment/release search, sorting/pages, user/team assignment, editable comments and recorded triage activity with regressions. |
| User feedback | Modern SDK submissions with optional error associations, per-submission attachments and paginated triage, legacy reports attached to their original error, and a public DSN-bound embed error page. |
| JavaScript source maps | Official CLI injection/bundle uploads and native uploads, Debug ID or release/distribution selection, original source context and preserved minified stacks. |
| Debug files | Project-scoped multipart dSYM/DIF file storage and listing with exact bytes, metadata and SHA-1 deduplication. Chunked DIF assembly and native symbolication remain pending. |
| Releases | Shared project versions, observed SDK releases, organization repository records, local commit metadata, completed deploys, compatible CLI lifecycle operations and release-file list/detail/delete over stored source maps, with a responsive management UI. |
| Performance | Legacy transactions and span v2 segments, latency and error aggregates, time windows, child-operation statistics, stored occurrence detail, a cross-project trace waterfall, and compatible transaction-group/detail/trend/span plus organization span-group/N+1 reads with project and time filters. |
| Logs | Typed attributes, exact 64-bit integer display, deduplication, pagination and project/text/level/environment/service/time/trace filters, plus compatible `GET /api/0/organizations/{org}/logs/`, `/logs/{id}/`, `/logs/resources/` and `/logs/stats/` reads. Native OpenTelemetry `/v1/logs` ingestion accepts standard OTLP/HTTP exporters (binary protobuf and JSON, optional gzip) without a Sentry SDK or extra collector. Retained log identities are numeric, and `host` is populated only from records that carried `host.name`. |
| Uptime | HTTP HEAD/GET/POST, TCP, SSL certificate and heartbeat monitors, failure thresholds, pause/resume, history, down/recovery transitions and the compatible public heartbeat-check endpoint. |
| Alerts | Frequency and uptime rules, environment filters, cooldown, SMTP/consolemail and generic webhooks, durable retries and delivery attempt history. |
| Automation credentials | Expiring, revocable organization tokens limited to selected projects, plus GlitchTip-compatible account tokens with scoped Bearer reads, one-time reveal, project metadata reads and optional release writes. |
| Management API | Sentry-compatible API root, current user and verified secondary emails, current-account deletion with sole-owner protection, `sentry-wizard` hash/project/credential handoff, organization/project discovery and detail, opt-in organization creation plus rename/delete with responsive UI, member invitations, active-member detail/role/removal and team assignment, team reads and create/rename/delete, project creation in a team, many-to-many project-team attach/detach, organization/project environment visibility, relation reads, project-key management with DSN metadata and fixed-window Sentry-envelope/OTLP-log limits, strict pagination, session/Bearer scope isolation and instance settings/license metadata. |
| Deployment | One non-root application image, PostgreSQL-only coordination, empty-volume Compose startup directly into the dashboard, required-auth setup/login mode, embedded Tailwind CSS and persistence across application restarts. |

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

Remaining work includes complete native crash symbolication, automatic repository integrations, further conditional issue policies, broader management APIs, a live GitLab receipt, vendor and external identity-provider receipts beyond the tested OpenID Connect flow, administration/OpenAPI and cold archival. Compatible member `teamRoles` and the `manager` role remain unsupported; pending invitations can be inspected and canceled through the member-item route, and the GlitchTip `set_owner` transfer is supported. Verified secondary emails are supported through the API and Account UI and can authenticate. TOTP two-factor authentication is optional per account through the API and Account UI; WebAuthn second factors remain open. Projects can belong to multiple teams and team-email alerts expand across every association; nonzero project throttling remains open. The opt-in MCP endpoint exposes the 17 documented tools through OAuth PKCE or scoped API tokens; its tool results are backed by GoodSentry's PostgreSQL event, span and log projections rather than DuckDB. Debug-file multipart and chunked DIF upload retain bytes and metadata; AMD64/x86/ARM64 minidumps and Breakpad or ELF/Mach-O DWARF symbols are processed, while other native symbol formats remain open.

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

Grafana `13.0.2` with the official Sentry data source plugin `2.2.8` has been exercised against GoodSentry for its organization/project health check, an issue query returning ten rows and a stats panel over `/stats_v2/` that rendered hourly error counts from stored events. GitLab Error Tracking's public client contract is covered by `/api/0/projects/`, project issues, issue detail, latest event and organization repositories; a full GitLab installation-level receipt remains an external integration test.

The verified modern upload protocol is the JavaScript SYSB v2 artifact-bundle workflow with `sentry-cli 3.8.0`; see [release and source-map setup](releases-and-source-maps.md). Other artifact types and CLI versions need their own acceptance. The 256–512 MB memory target has not been measured under a documented workload. Current container acceptance covers Linux amd64.

See the [complete specification](../SPEC.md), [implementation order](../tasks/plan.md) and [task evidence](../tasks/todo.md) before relying on an unlisted capability.
