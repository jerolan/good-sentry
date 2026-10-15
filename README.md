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

`down` preserves the named volumes. Adding `-v` deletes them. Migrations apply automatically at startup under a database lock and verify their checksums.

## What works today

| Capability | Verified behavior |
| --- | --- |
| Error tracking | Store/envelope ingestion, issue grouping, duplicate suppression, context, search, filters, resolve/reopen and comments. |
| JavaScript source maps | Authenticated native uploads, release/distribution or Debug ID selection, original file/line/column/context and preserved minified stacks. |
| Performance | Legacy transactions and span v2 segments, counts, errors, average, p50/p95, time windows and stored occurrence detail with context and paginated spans. |
| Logs | Typed attributes, exact 64-bit integer display, deduplication, pagination and project/text/level/environment/service/time/trace filters. |
| Uptime | HTTP HEAD/GET/POST, TCP and heartbeat monitors, failure thresholds, pause/resume, history and down/recovery transitions. |
| Alerts | Frequency and uptime rules, environment filters, cooldown, SMTP/consolemail and generic webhooks, durable retries and delivery attempt history. |
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

The Go and Node checks also open the captured transaction occurrence and verify root/child timing, parent identifiers and original SDK context.

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

### Current gaps

Remaining work includes aggregate child-operation statistics and a full trace waterfall, release and deploy management, `sentry-cli` upload endpoints, native symbolication, native OTLP, retention, assignment and merge/unmerge, invitations and password recovery, SSL certificate monitors, provider-specific alert formats, management API parity, integrations, MCP, OAuth/OIDC and cold archival.

The modern CLI bundle decoder is tested; its upload endpoints remain unconnected. The 256–512 MB memory target has not been measured under a documented workload. Current container acceptance covers Linux amd64.

See the [complete specification](SPEC.md), [implementation order](tasks/plan.md) and [task evidence](tasks/todo.md) before relying on an unlisted capability.

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
| `GOODSENTRY_ENABLE_LOGS` | `true` | Enable SDK log ingestion and its UI. |
| `GOODSENTRY_ENABLE_UPTIME` | `true` | Enable monitor routes, UI and scheduler. |
| `MONITOR_WORKERS` | `2` | Concurrent checks per process, from 1 to 16. |
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
