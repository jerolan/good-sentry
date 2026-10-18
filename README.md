<p align="center">
  <img src="site/assets/favicon.svg" width="64" height="64" alt="GoodSentry">
</p>

<h1 align="center">GoodSentry</h1>
<p align="center"><strong>Sentry SDKs. One Go service.</strong></p>
<p align="center">Self-hosted error tracking, logs and uptime.<br>One application container, backed by PostgreSQL.</p>

<p align="center">
  <a href="https://github.com/jerolan/good-sentry/actions/workflows/ci.yml"><img src="https://github.com/jerolan/good-sentry/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&amp;logoColor=white" alt="Go 1.27">
  <img src="https://img.shields.io/badge/PostgreSQL-18-4169E1?logo=postgresql&amp;logoColor=white" alt="PostgreSQL 18">
  <img src="https://img.shields.io/badge/status-in%20development-e9b949" alt="In development">
</p>

<p align="center">
  <a href="https://jerolan.github.io/good-sentry/">Website</a> ·
  <a href="#quickstart">Get started</a> ·
  <a href="#sdk-compatibility">SDK compatibility</a> ·
  <a href="tasks/todo.md">Roadmap and evidence</a>
</p>

<p align="center">
  <img src="site/assets/app-preview.png" width="1200" alt="GoodSentry dashboard with fictional demo errors">
</p>
<p align="center"><sub>Actual GoodSentry interface. Fictional demo data.</sub></p>

GoodSentry is an independent Go implementation of Sentry-compatible error tracking. Point your existing SDK at a GoodSentry project DSN, capture an event and inspect it in the embedded Tailwind interface. The same Go process handles ingestion, the UI, uptime checks and alert delivery. PostgreSQL stores the data and coordinates background work.

> GoodSentry is in active development. It has **not reached the requested full GlitchTip feature parity**. The verified workflows and remaining gaps are listed below.

## Quickstart

You need Docker with Compose and OpenSSL. On Windows, enable Docker Desktop's integration with your WSL2 distribution.

```sh
git clone https://github.com/jerolan/good-sentry.git
cd good-sentry
test -f .env || printf 'SECRET_KEY=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up --build -d
```

Open **http://localhost:8000**. Create the first account and organization, create a project and copy its DSN into your SDK configuration. There is no shared initial password.

The first start builds the application image from this repository. No registry image is required. The runtime image contains the Go binary and embedded frontend, runs as `65532:65532` and needs one PostgreSQL container. PostgreSQL also coordinates the scheduler and delivery retries.

Keep `.env` between restarts so sessions retain the same secret. Set your public `GOODSENTRY_DOMAIN` and PostgreSQL credentials before exposing the installation. For HTTPS, terminate TLS at your reverse proxy and use an `https://` public domain.

```sh
docker compose logs -f goodsentry
docker compose restart goodsentry
docker compose down
```

`down` preserves the named volumes. Adding `-v` deletes them. Migrations apply automatically at startup under a database lock and verify their checksums. Stop older backend replicas before starting a version with native issue grouping. Current-version replicas coordinate grouping through PostgreSQL locks.

## What works today

| Capability | Verified behavior |
| --- | --- |
| Error tracking | Store/envelope ingestion, issue grouping, duplicate suppression, historical tag/environment/release search, sorting/pages, user/team assignment, editable comments and recorded triage activity with regressions. |
| JavaScript source maps | Official CLI injection/bundle uploads and native uploads, Debug ID or release/distribution selection, original source context and preserved minified stacks. |
| Releases | Shared project versions, observed SDK releases, local commit metadata, completed deploys and compatible CLI lifecycle operations, with a responsive management UI. |
| Performance | Legacy transactions and span v2 segments, latency and error aggregates, time windows, child-operation statistics, stored occurrence detail and a cross-project trace waterfall. |
| Logs | Typed attributes, exact 64-bit integer display, deduplication, pagination and project/text/level/environment/service/time/trace filters. Native OpenTelemetry `/v1/logs` ingestion accepts standard OTLP/HTTP exporters (binary protobuf and JSON, optional gzip) without a Sentry SDK or extra collector. |
| Uptime | HTTP HEAD/GET/POST, TCP and heartbeat monitors, failure thresholds, pause/resume, history and down/recovery transitions. |
| Alerts | Frequency and uptime rules, environment filters, cooldown, SMTP/consolemail and generic webhooks, durable retries and delivery attempt history. |
| Automation credentials | Expiring, revocable tokens limited to an organization and selected projects, one-time reveal, project metadata reads and optional release writes. |
| Deployment | One non-root application image, PostgreSQL-only coordination, embedded Tailwind CSS and persistence across application restarts. |

The browser workflows run at 320, 768 and 1440 pixels. PostgreSQL integration and race tests cover concurrent ingestion, duplicate retries, tenant boundaries, fenced worker leases and restart recovery. Alert acceptance uses actual local HTTP/SMTP receivers, including retries across two application replicas.

### SDK compatibility

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

These checks establish the listed workflows for those versions. Attachments, additional envelope types and other SDK versions need their own verification. Unknown envelope items are retained and reported through `unsupportedItems`; retention alone does not implement their functionality.

### OpenTelemetry logs

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

### Issue triage

Assign an issue to a workspace member or team, or clear its assignment. Search people and teams from issue detail. Comments and activity have independent pages of 25 entries. Authors and workspace owners/admins can edit or delete comments; editing preserves their original creation time.

Simple Resolve records the current receipt time. A new unique error reopens the issue and records a regression, even when that error occurred earlier. Retrying an already accepted event ID preserves its count, resolution, activity and release metadata.

In the Resolution panel, choose an existing project release or use the issue's observed release. A newer release reopens the issue. GoodSentry uses semantic version order for recognized project histories and compatible package names, ignoring build metadata; other identities use recorded release creation dates. The selected reference and comparison stay fixed when metadata changes or is deleted.

**Resolve in next release** waits for a different open project release created or finalized after resolution. It needs a valid observed baseline. The first selected next release, or a newer release, reopens the issue when an error arrives. The application worker can select it from metadata without another event. Saved pending and selected states survive restart. Errors without a valid release reopen either policy. Applying the same policy preserves its progress; clear to simple resolution or reopen before starting a fresh next policy.

Errors that remain resolved are still captured and counted, but do not create new issue notifications. Activity shows the selected boundary and the reason for a regression. The compatible individual and bulk issue APIs share these native rules. Commit-triggered resolution remains pending.

Ignore pauses new issue notifications while collecting every error. In issue detail, mute indefinitely, for a number of minutes, until a number of additional unique errors, or until either condition is met. The last additional error reopens the issue before normal alert eligibility runs. Retries never consume the count, and applying the same active condition preserves its deadline and progress. New captures and the maintenance worker evaluate elapsed time using the database receipt clock; SDK timestamps and timezone changes do not move the deadline. Activity records why a mute ended.

The maintenance worker runs inside the application, including when uptime and log ingestion are disabled. It catches up after restart. Lists may show an expired mute until its next maintenance pass. Captures still contribute to project-wide alert windows, so a threshold crossed during mute is not replayed afterward. Deliveries already queued or in progress retain their status. Native conditional mute does not yet implement the compatible API's affected-user or sliding-window conditions.

### Merge and separate issues

Select 2 to 100 issues from one project and confirm **Merge selected**. Selections stay across pages with the same filters. The lowest issue ID retains its assignment, status and active mute or release policy. Absorbed roots clear their active policies and record the previous state in activity. Merging history does not consume mute progress, reopen a resolution or queue notifications.

In **Original groups**, search or page through the original fingerprints and separate selected groups, including the current root. Each selected group becomes standalone; unselected groups stay together. Active signatures follow their separated origin. **Historical only** origins retain their recorded events; separating them does not reclaim a signature already assigned to a newer issue. Counts and dates describe the resulting stored events. Comments and activity stay with their original group, including after separation.

Old issue links open the current group. If another user merges an issue while you edit it, the application preserves your drafts and provides an **Open current group** link. These controls implement native GoodSentry grouping. Compatible API0 bulk merging uses the same rules.

### Reset future grouping

In **Future grouping**, select active signatures across pages and confirm **Reset selected signatures**. Existing events, assignment, mute and resolution policies stay on their recorded issue. The next new matching SDK event starts another issue. Retrying an already stored event preserves its original receipt and does not reactivate a reset signature.

Compatible clients can use `GET /api/0/organizations/{org}/issues/{issue}/hashes/` with `event:read` or `event:write`. Each signature has a stable public UUID and its own latest retained event sample, or null when no sample remains. Responses include cursor Links; full samples are enabled by default with at most10 per page. Use `full=false` for summaries with up to100 per page.

Send `DELETE` to the same URL with repeated `id` query values and an empty body to reset selected signatures. This requires `event:write`, or a session with CSRF. A successful response is202 with no body after the reset commits. Foreign or inactive signatures are ignored. Writes to a merged alias return409 with `mergedInto`; reads open its current group. The signature UUID remains stable when its next capture creates a new generation.

### Delete issues

Use **Delete issue** in detail or select up to 1,000 issues across list pages and confirm **Delete selected**. Deletion removes each whole current group, its recorded errors, attachments, comments, assignment and active triage policies. Future matching captures can create a new issue. Already queued notifications and shared release metadata remain available. Merge remains limited to 2–100 issues from one project.

Compatible clients use `DELETE /api/0/organizations/{org}/issues/{issue}/` with an empty body and the optional `event:admin` scope. Individual deletion returns204 after commit; a merged alias returns409 with `mergedInto`. Organization bulk deletion uses the same collection URL with repeated `id` values and returns200 with an empty JSON object. If any supplied ID is missing or outside the credential's access, the whole bulk request makes no changes. The UI reports completion without inventing a deleted count and offers a read retry if refreshing the list fails.

`event:admin` grants deletion only and stays unchecked by default. It does not grant issue reads or other writes. Deletion is not permanent discard: retrying an actually deleted event ID can create a new receipt; retries of retained events remain idempotent. The official CLI 3.8.0 has no issue deletion command.

### Issue search

The Issues view combines project, status, environment and release filters with text and tag queries:

```text
region:eu timeout
region:[eu,us] !browser:Safari
release:"web@1" level:error
tags[release]:"build *"
```

Terms intersect; list values are alternatives. Bare `*` is a wildcard, while quoted values match literally. `tags[key]` searches a custom tag whose name collides with a reserved field. Select **All statuses** to include resolved and ignored issues.

Environment, release, tags and text must match the same stored event, including historical events. The displayed count, dates and sorting describe the issue's whole history. Choose Last seen, First seen or Event count; each page contains 25 issues. Filters persist through reload, paging and detail/back navigation. Clicking an event tag opens an exact search, including literal stars and punctuation.

Invalid queries keep the filter form editable. Broader query syntax remains separate work.

### Issue analytics and compatible reads

Issue detail shows identified users from retained events and a real 24-hour event chart, including an accessible table of hourly counts in UTC. Anonymous events add to event counts without creating an identified user. Historical counts remain visible even when recent activity is empty.

An explicit `event:read` or `event:write` token can read organization/project issue lists, organization/global issue detail and organization `issues-stats` under `/api/0/`. Reads honor its project grants. Omitted `query` selects unresolved issues; `query=` includes every status. Environment and occurrence filters return matching metrics alongside historical totals. Organization `statsPeriod` selects a relative time window; on the deprecated project list it selects the chart (`24h`, `14d` or empty to disable). `groupStatsPeriod` controls list charts separately.

Affected-user counts use the first nonblank valid ID, email, username or IP in each retained event. Numeric IDs share an identity with their equivalent decimal string; attributes stay separate and case-sensitive. This is GoodSentry's explicit counting rule. Unsupported query options return 400. Compatible issue deletion, sliding-window conditions and feedback remain pending.

### Compatible individual issue changes

Create a token with the optional `event:write` permission and select its projects. It can update `/api/0/issues/{issue}/` or `/api/0/organizations/{org}/issues/{issue}/` with `PUT`. Use the trailing slash and replace `123` with an accessible issue ID:

```sh
curl --request PUT http://localhost:8000/api/0/issues/123/ \
  --header "Authorization: Bearer $SENTRY_AUTH_TOKEN" \
  --header 'Content-Type: application/json' \
  --data '{"status":"resolved","statusDetails":{"inRelease":"latest"}}'
```

The response contains the saved compatible issue detail and real analytics. `latest` requires an existing release associated with that project. Use an exact release version instead, or `"status":"resolvedInNextRelease"` to apply the native next-release policy. Plain `resolved`, `unresolved` and `ignored` follow the native rules; `muted` is an alias of `ignored`.

`assignedTo` accepts a member's ID, `user:ID`, `team:ID` or primary email. Null or an empty string unassigns. Omitted assignment and policy fields follow the native omission rules. For an ignored issue, `ignoreDuration` and `ignoreCount` select elapsed minutes and additional unique errors; zero disables that condition. These fields may also appear inside `statusDetails`. Identical policies preserve their existing progress.

An explicit `statusDetails:null` or `{}` clears the current condition while keeping the effective status. An empty `inRelease` or `inNextRelease:false` clears only a resolved condition and is rejected for other statuses. Invalid targets or incompatible policies roll back the whole request. A merged alias returns409 with `mergedInto`; open the current group before retrying. An empty request object reads the authorized issue without expiring or changing its policies.

Writer tokens inherit issue and issue-event reads and can reset future grouping. Project event routes still need `project:read`, release routes need `project:releases`, and native mutation routes require a session. Compatible issue deletion, username handles and user/window/commit conditions remain pending; unsupported fields return400.

### Compatible bulk changes

Use the same optional `event:write` token with `PUT /api/0/projects/{org}/{project}/issues/` or `PUT /api/0/organizations/{org}/issues/`. Repeat `id` for explicit selection. Changes share one transaction and honor every project grant. If any explicit ID is absent or outside that scope, the whole request returns an empty acknowledgement without changing issues.

```sh
curl --request PUT 'http://localhost:8000/api/0/projects/my-workspace/my-project/issues/?id=123&id=456' \
  --header "Authorization: Bearer $SENTRY_AUTH_TOKEN" \
  --header 'Content-Type: application/json' \
  --data '{"merge":true,"status":"unresolved"}'
```

Merging requires explicit IDs from one project and retains the lowest current root. `merge:1` is also accepted; false, zero and null do not separate groups. Assignment and policy-only changes require IDs too. A status change can select by current `status`, project, environment, search and occurrence time. Without IDs or a query, the project route includes all statuses; the organization route selects unresolved issues. Explicit `query=` includes every status. Operations select at most1000 roots by default; an explicit `limit` accepts1–100. Release selectors require one project. The response acknowledges supplied changes and the actual merge parent/children; issue GETs show each saved state.

The real official sentry-cli3.8.0 has passed resolve, mute, unresolve, next-release and selection by IDs, status or `--all`, followed by authenticated state checks. Bulk `snoozeDuration:null` is accepted, and a nonnull value aliases ignoreDuration in minutes. SDK capture, retries and future routing remain intact. Compatible issue deletion remains a required follow-up.

### Compatible event reads

Issue event routes accept an explicit `event:read` or `event:write` token restricted to the selected projects. Project event routes use `project:read`. The token form keeps `org:read` and `project:read` as defaults; both event scopes start unchecked.

Read errors from `/api/0/projects/{org}/{project}/events/`, `/api/0/issues/{issue}/events/` or `/api/0/organizations/{org}/issues/{issue}/events/`. Append the SDK event ID for full detail; issue routes also accept `latest`, `oldest` and `recommended` (latest). Project detail can retrieve a stored transaction by ID. The organization issue route additionally exposes `{event}/json/` as a legacy JSON projection.

Lists support environment, occurrence dates, `statsPeriod`, bounded search, `limit`/`per_page` and cursor Links. Dates without an offset use UTC. Omitted time filters retain all history; `full=true` limits each page to10 events. Reads retain the stored SDK payload, current group ownership and original receipt time.

### Release automation

In **API tokens**, create a token with `org:read`, `project:read` and the optional `project:releases` permission. Choose the projects it may access. A shared release can be changed only when the token covers every associated project.

The official `sentry-cli 3.8.0` has passed this workflow against the application image, including local Git comparison with the preceding release:

```sh
export SENTRY_URL=http://localhost:8000
export SENTRY_ORG=your-organization
export SENTRY_PROJECT=your-project
export SENTRY_AUTH_TOKEN=your-token
version="$(git rev-parse HEAD)"

sentry-cli releases new "$version"
sentry-cli releases set-commits "$version" --local
sentry-cli releases finalize "$version"
sentry-cli deploys new --release "$version" --env production
sentry-cli deploys list --release "$version"
```

Create, finalize, list, info, archive, restore and delete also pass with the issued credential. Revocation disables subsequent requests. Release deletion removes its metadata and release-specific source maps while preserving captured events and stacks. Repository integration with `--auto` remains pending.

### JavaScript source maps

With the same CLI configuration and `project:releases` permission, inject your built files and upload their artifact bundle:

```sh
sentry-cli sourcemaps inject ./dist
sentry-cli sourcemaps upload --wait ./dist
```

Deploy those injected files so the SDK can attach their Debug IDs to captured exceptions. The official CLI `3.8.0` and Node SDK `11.7.0` have passed this complete workflow, including the original file, line and source context, preserved minified stack, duplicate uploads and project isolation. Uploading the same build under additional release/dist selectors preserves its usable identity.

Optional `--release` and `--dist` values must match your SDK configuration. An upload with `--release` does not create release metadata; a separate release command or fresh SDK capture does. The release/dist workflow without injection also passes when the artifact URL prefix matches the emitted filename. Incorrect version/distribution selectors remain unmapped.

Chunks persist in PostgreSQL so interrupted uploads can resume after a restart. Temporary staging has a configurable quota and expiry, with automatic cleanup in the Go process. Native debug-file and minidump uploads remain pending.

### Current gaps

Remaining work includes automatic repository integration, native debug-file/minidump uploads and symbolication, retention for other data classes, further conditional policies, SDK feedback, invitations and password recovery, SSL certificate monitors, provider-specific alert formats, management API parity, integrations, MCP, OAuth/OIDC and cold archival.

The verified modern upload protocol is the JavaScript SYSB v2 artifact-bundle workflow with the pinned CLI above. Other artifact types and CLI versions need their own acceptance. The 256–512 MB memory target has not been measured under a documented workload. Current container acceptance covers Linux amd64.

See the [complete specification](SPEC.md), [implementation order](tasks/plan.md) and [task evidence](tasks/todo.md) before relying on an unlisted capability.

### Log retention

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
not recover deleted logs. Expiration does not archive logs or implement
retention for errors, uptime, completed files or releases.

### Transaction and span retention

Legacy transaction events and standalone spans expire by server receipt time.
`GOODSENTRY_TRANSACTION_RETENTION_DAYS` overrides
`GLITCHTIP_TRANSACTION_RETENTION_DAYS`, then inherits the master lifetime above.
The default is 90 days; valid values are whole days from 1 to 3650.

Each PostgreSQL transaction removes at most 250 legacy transactions and 250
standalone spans. A legacy transaction's attachments, opaque envelope items and
span-operation projections leave with its receipt. Errors, logs and releases
keep their independent lifetimes, including when they share a trace. Standalone
opaque items without a selected parent remain for future file retention.

Cleanup runs at startup and after the configured retention interval, including
when logs or uptime are disabled. Locked receipts wait for a later pass.
Transaction metrics and trace nodes reflect the surviving data; a trace can
still show related errors or logs after its last span expires. Retrying a
physically expired SDK identity can create a new receipt. Raising the lifetime
does not restore deleted data; recovery requires an operator backup.

## Configuration

Compose supplies the local database connection. Environment values override the defaults below.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SECRET_KEY` | Required | At least 32 characters. Generate it once and keep it across restarts. |
| `DATABASE_URL` | Supplied by Compose | PostgreSQL connection URL. |
| `GOODSENTRY_DOMAIN` | `http://localhost:8000` | Public origin and optional path prefix used for DSNs and resource links. |
| `POSTGRES_PASSWORD` | `goodsentry-local` | Local Compose database password; replace it for your deployment. |
| `PORT` | `8000` | Published Compose port. Update the public domain when changing it. |
| `EMAIL_URL` | `consolemail://` | Console output or `smtp://`, `smtp+starttls://`, `smtps://`. |
| `DEFAULT_FROM_EMAIL` | `goodsentry@localhost` | Sender address for alert emails. |
| `ARTIFACT_CHUNK_TTL_HOURS` | `24` | Temporary upload chunk availability, from 1 to 168 hours. |
| `ARTIFACT_STAGING_MAX_BYTES` | `268435456` | Per-organization temporary chunk quota; at least `MAX_BODY_BYTES`, from 1 MiB to 1 TiB. |
| `ARTIFACT_MAX_MAP_RECORDS` | `1000` | Maximum maps × selected projects in one atomic assembly, from 1 to 100000. |
| `GOODSENTRY_ENABLE_LOGS` | `true` | Enable SDK log ingestion and its UI. |
| `GOODSENTRY_OTLP_MAX_RECORDS` | `1000` | Maximum records per OTLP log batch, from 1 to 100000. |
| `GOODSENTRY_OTLP_MAX_NODES` | `100000` | Maximum decoded nodes per OTLP log batch, from 100 to 1000000. |
| `GOODSENTRY_ENABLE_UPTIME` | `true` | Enable monitor routes, UI and scheduler. |
| `GOODSENTRY_RETENTION_DAYS` | `90` | Master receipt lifetime; whole days from 1 to 3650. |
| `GOODSENTRY_LOG_RETENTION_DAYS` | Master lifetime | Sentry and OTLP log lifetime. |
| `GOODSENTRY_TRANSACTION_RETENTION_DAYS` | Master lifetime | Legacy transaction and standalone span lifetime. |
| `GOODSENTRY_RETENTION_INTERVAL_SECONDS` | `60` | Idle cleanup interval, from 1 to 3600 seconds. |
| `MONITOR_WORKERS` | `2` | Concurrent checks per process, from 1 to 16. |
| `ISSUE_MAINTENANCE_INTERVAL_SECONDS` | `1` | Polling for expired mutes and pending next-release resolution, from 1 to 3600 seconds. Longer intervals delay metadata-only binding and status display; fresh captures evaluate immediately. |
| `ALERT_WORKERS` | `2` | Concurrent deliveries per process, from 1 to 16. |
| `ALERT_MAX_ATTEMPTS` | `5` | Maximum delivery attempts, from 1 to 20. |
| `ALERT_DELIVERY_TIMEOUT_SECONDS` | `10` | Delivery timeout, from 1 to 60 seconds. |
| `GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS` | `false` | Allow monitors to reach your private network. |
| `GOODSENTRY_WEBHOOK_ALLOW_PRIVATE_IPS` | `false` | Allow webhooks to reach your private network. |

The process also accepts `MAX_BODY_BYTES`, defaulting to 32 MiB for compressed and expanded request bodies. `GLITCHTIP_DOMAIN` and supported `GLITCHTIP_ENABLE_*` aliases ease migration; `GOODSENTRY_*` names take precedence. `SERVER_ROLE` is `all_in_one`. Leave `VALKEY_URL` empty. Enabling currently unavailable MCP, DuckDB, admin, OpenAPI or Valkey options fails startup rather than reporting them as available.

<details>
<summary><strong>Time windows and service filters</strong></summary>

Transactions and Logs offer past-hour, 24-hour, seven-day and 30-day presets. Applying one writes absolute UTC bounds into the URL. Pagination, other filters and reload reuse those bounds. Transactions default to the last 24 hours; Logs default to all stored history. Service matching is exact, including case and spaces.

The native query APIs accept paired RFC3339 `start` and `end` instants with time zones and a maximum interval of 366 days. They normalize to UTC microseconds, include the start and exclude the end. Logs also accepts `service`. Invalid or incomplete ranges return 400.

Open a transaction name to inspect its latest stored occurrence. Legacy events show their nested spans; standalone segments show spans in the same project and trace, with their recorded parent IDs. Spans use 50-row pages and retain unavailable timing explicitly. Detail and related-log navigation preserve the originating filters; those time bounds do not hide the occurrence's spans.

Choose **Operations** for a transaction group's child-operation counts, average, p95 and accumulated time. The breakdown combines matching legacy transactions and standalone segments within the selected project and interval. Standalone children use their recorded segment association; roots are excluded. Counts include unavailable timing, while duration metrics use valid durations only. Accumulated time includes overlapping spans.

Choose **View trace** from a transaction, occurrence, log or error to inspect its stored spans across projects. The waterfall uses recorded parent IDs and UTC timestamps, with 50-node pages, links to parents on other pages, selected raw JSON and related errors and logs. Missing, ambiguous and cyclic relationships remain visible. Incomplete timing stays unavailable; zero-duration spans appear as points. A trace includes all stored time in its selected project scope, and Back restores the originating filters and page.

On upgrade, GoodSentry indexes historical legacy spans inside the application, one event per transaction. This resumes after restart and preserves original event JSON and operation statistics. Pending history shows an indexing message and retry action. Related trace logs include historical uppercase IDs; ordinary Logs filters retain exact matching.

</details>

<details>
<summary><strong>Scoped API tokens</strong></summary>

Owners and admins can open **API tokens** to create a credential, select projects and set its lifetime. The secret appears once; copy it before dismissing or leaving the view. Tokens expire after 90 days by default, with a selectable lifetime of 1–366 days. Revocation takes effect on subsequent requests. An all-project credential also includes projects created later; a restricted credential keeps only its selected projects.

Choose `org:read` to list authorized projects, `project:read` for project metadata/events, `event:read` for issues and issue events, `event:write` for those reads, individual/bulk issue changes and merging, `event:admin` for issue deletion only, or `project:releases` for release management. Compatible project responses exclude SDK keys and DSNs. Set `SENTRY_AUTH_TOKEN` to the issued secret, then use the verified sentry-cli 3.8.0 command:

```sh
SENTRY_URL=http://localhost:8000/ SENTRY_ORG=my-workspace sentry-cli projects list
```

Each compatible route checks its implemented scope and current project grants. Existing tokens retain their scopes. Broader management API coverage remains pending.

</details>

<details>
<summary><strong>Monitor and alert behavior</strong></summary>

Monitor intervals range from 1 to 86400 seconds and timeouts from 1 to 60. Consecutive failures confirm a down state; one successful result recovers it. GET/POST expected-body matching uses a literal substring within the first MiB. POST sends an empty request body. Heartbeats accept POST before each deadline; pausing retains history and stops checks. Owners/admins manage monitors; members read their state and history.

Network checks and webhooks reject private, loopback, link-local and multicast targets by default. Explicitly enable their separate private-IP settings for internal services. HTTPS verifies certificates. Webhooks do not follow redirects or use environment proxies.

Frequency alerts count received errors and message events, excluding duplicate retries, transactions and logs. Uptime alerts use the monitor's associated project and optional environment. Editing or deleting a rule cancels pending work while retaining completed history.

`consolemail://` writes MIME messages to the console. SMTP credentials require verified TLS. Generic webhooks receive compact JSON and a resource link. Transient failures retry after 15, 30, 60 seconds and so on, capped at 900 seconds. A crash after remote acceptance can cause a repeat with the same `X-GoodSentry-Delivery-ID`, JSON ID and email Message-ID.

</details>

## Development

Use Go 1.27.2. The server uses `go:embed`; Node is not part of its runtime. Tailwind CSS 4.3.3 and daisyUI 5.7.50 compile `internal/server/web/styles.css` into `app.css`. The dashboard uses original styling inspired by the [Nexus dashboard examples](https://nexus.daisyui.com/). Docker verifies the official CLI and vendored plugin checksums. The generated CSS is committed so local Go builds do not require frontend tools. The daisyUI dependency and its MIT notice live in [third_party/daisyui](third_party/daisyui); the runtime image includes that notice.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/goodsentry ./cmd/goodsentry
```

For integration tests, set `DATABASE_URL` to a disposable PostgreSQL instance. Tests create isolated schemas. Race detection requires a C compiler.

```sh
go test -race -tags=integration ./...
```

Pinned external SDK clients and browser workflows live in [tests/compat](tests/compat). They are test tooling, separate from the server module and image. The CI workflow runs formatting, vet, the integration/race suite and the actual Docker build.

The product landing lives in [site](site). [GitHub Pages Actions](.github/workflows/pages.yml) publishes that directory from `main` or a manual dispatch. Preview it locally with:

```sh
python3 -m http.server 8001 --directory site
```

## Independent implementation

GoodSentry is developed from public protocol specifications, official documentation and black-box tests with official SDKs and the CLI. Design decisions, source references and verification limits are recorded in [SPEC.md](SPEC.md).
