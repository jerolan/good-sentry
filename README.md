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
  <a href="docs/README.md">Documentation</a> ·
  <a href="tasks/todo.md">Roadmap</a>
</p>

<p align="center">
  <img src="site/assets/app-preview.png" width="1200" alt="GoodSentry dashboard with fictional demo errors">
</p>
<p align="center"><sub>Actual GoodSentry interface. Fictional demo data.</sub></p>

GoodSentry is an independent Go implementation of Sentry-compatible error tracking. Connect your existing Sentry SDK with a GoodSentry project DSN. The same Go process serves the Tailwind CSS + daisyUI interface, receives telemetry, checks uptime and delivers alerts. PostgreSQL stores the data and coordinates background work.

## Quickstart

You need Git, Docker with Compose, and OpenSSL. On Windows, use a WSL2 distribution integrated with Docker Desktop.

```sh
git clone https://github.com/jerolan/good-sentry.git
cd good-sentry
test -f .env || printf 'SECRET_KEY=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up -d
```

Open **http://localhost:8000**, create the first owner account and workspace, then create a project. Copy its DSN into your SDK configuration. There is no shared initial password.

Compose pulls the public application image for amd64 or arm64. Keep `.env` across restarts; configure your public domain and database credentials before exposing the installation. See [installation details](docs/getting-started.md) and the [configuration reference](docs/configuration.md).

## Connect your SDK

For an existing Sentry integration, start by changing its DSN. Keep your application's existing sampling settings. A minimal Node.js example:

```js
import * as Sentry from "@sentry/node";

Sentry.init({ dsn: process.env.SENTRY_DSN });
Sentry.captureException(new Error("Hello from GoodSentry"));
```

Official Go `0.50.0`, Python `2.72.0`, Node.js `11.7.0` and Browser JavaScript `11.7.0` clients have verified error, transaction and typed-log workflows. See [SDK compatibility](docs/compatibility.md#sdk-compatibility) for the tested scope, settings and limits.

## Features

| Capability | Available workflows |
| --- | --- |
| Errors and feedback | Grouped issues, stack traces, tags, search, assignment, comments, triage and SDK feedback. |
| Releases | Sentry CLI lifecycle operations, commit/deploy metadata and JavaScript source maps. |
| Native diagnostics | Official CLI chunked DIF upload, debug-file storage, AMD64/x86/ARM64 minidump ingestion and Breakpad plus ELF/Mach-O DWARF symbolication; more native formats are in progress. |
| Performance and logs | Transactions, spans, trace waterfalls, typed logs and OTLP/HTTP log ingestion. |
| Uptime and alerts | HTTP, TCP, SSL certificate and heartbeat monitors; email and webhooks with durable retries. |
| Accounts and teams | Password changes and email recovery, member roles, teams and single-use email invitations. |
| Integration API | Sentry-compatible discovery for users, organizations, projects, members and teams; organization rename/delete, member invitations, team management, project-key DSNs with fixed-window rate limits, plus scoped automation credentials. |
| MCP | Opt-in Model Context Protocol server with 17 organization, issue, log, alert, monitor and performance tools; OAuth PKCE or scoped API tokens. |
| Deployment | One non-root application image, PostgreSQL, automatic migrations and configurable retention. |

## Documentation

- [Getting started](docs/getting-started.md) — installation and first project.
- [Configuration](docs/configuration.md) — environment variables and scoped API tokens.
- [Product guides](docs/README.md) — issues, feedback, releases, observability and retention.
- [Compatibility and status](docs/compatibility.md) — verified workflows and remaining gaps.

## Project status

GoodSentry is in active development. **Full Sentry compatibility and GlitchTip feature parity are not complete.** Remaining work includes native crash symbolication, broader management APIs, GitLab/Grafana integrations, social OAuth/OIDC sign-in and cold archival. The [compatibility guide](docs/compatibility.md) describes tested workflows and limits; the [roadmap](tasks/todo.md) records outstanding work.

## Development

Use Go **1.27.2**. Frontend assets are embedded, so local Go builds do not require Node.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/goodsentry ./cmd/goodsentry
```

See the [development guide](docs/development.md) for PostgreSQL integration/race tests, pinned frontend tooling and GitHub Pages publishing.

## Independent implementation

GoodSentry is developed from public protocol specifications, official documentation and black-box tests with official SDKs and the CLI. Protocol references, design decisions and verification limits are recorded in [SPEC.md](SPEC.md). Its original dashboard styling is inspired by the [Nexus daisyUI examples](https://nexus.daisyui.com/).
