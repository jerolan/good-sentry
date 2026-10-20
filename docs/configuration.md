# Configuration and API tokens

[Documentation](README.md) · [Project home](../README.md)

The tables below describe application process settings. Compose supplies the local database connection and loads optional settings from `.env`. Keep the same `SECRET_KEY` across restarts.



## Configuration

Set these values in `.env` when using Compose, then recreate the application container with `docker compose up -d`. A simple container restart does not reload changed environment values. Compose uses `PORT` for the host port and keeps the application's internal port at 8000.

For the supplied local database connection, use a URL-safe `POSTGRES_PASSWORD` such as a random hexadecimal value. Custom PostgreSQL connection URLs must percent-encode reserved characters in credentials.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SECRET_KEY` | Required | At least 32 characters. Generate it once and keep it across restarts. |
| `DATABASE_URL` | Supplied by Compose | PostgreSQL connection URL. |
| `GOODSENTRY_IMAGE` | `ghcr.io/jerolan/good-sentry:main` | Compose application image; accepts a revision tag or immutable image digest. |
| `GOODSENTRY_DOMAIN` | `http://localhost:8000` | Public origin and optional path prefix used for DSNs and resource links. |
| `POSTGRES_PASSWORD` | `goodsentry-local` | Local Compose database password; replace it for your deployment. |
| `PORT` | `8000` | Published Compose port. Update the public domain when changing it. |
| `EMAIL_URL` | `consolemail://` | Console output or `smtp://`, `smtp+starttls://`, `smtps://`. |
| `DEFAULT_FROM_EMAIL` | `goodsentry@localhost` | Sender address for alert and account emails. |
| `ARTIFACT_CHUNK_TTL_HOURS` | `24` | Temporary upload chunk availability, from 1 to 168 hours. |
| `ARTIFACT_STAGING_MAX_BYTES` | `268435456` | Per-organization temporary chunk quota; at least `MAX_BODY_BYTES`, from 1 MiB to 1 TiB. |
| `ARTIFACT_MAX_MAP_RECORDS` | `1000` | Maximum maps × selected projects in one atomic assembly, from 1 to 100000. |
| `GOODSENTRY_ENABLE_LOGS` | `true` | Enable SDK log ingestion and its UI. |
| `GOODSENTRY_OTLP_MAX_RECORDS` | `1000` | Maximum records per OTLP log batch, from 1 to 100000. |
| `GOODSENTRY_OTLP_MAX_NODES` | `100000` | Maximum decoded nodes per OTLP log batch, from 100 to 1000000. |
| `GOODSENTRY_ENABLE_UPTIME` | `true` | Enable monitor routes, UI and scheduler. |
| `GOODSENTRY_RETENTION_DAYS` | `90` | Master data lifetime; whole days from 1 to 3650. |
| `GOODSENTRY_EVENT_RETENTION_DAYS` | Master lifetime | Error and message event lifetime; issue history remains available. |
| `GOODSENTRY_LOG_RETENTION_DAYS` | Master lifetime | Sentry and OTLP log lifetime. |
| `GOODSENTRY_TRANSACTION_RETENTION_DAYS` | Master lifetime | Legacy transaction and standalone span lifetime. |
| `GOODSENTRY_UPTIME_RETENTION_DAYS` | Master lifetime | Completed monitor result lifetime; cached state and alert history remain available. |
| `GOODSENTRY_FILE_RETENTION_DAYS` | Master lifetime | Source map, attachment and opaque envelope item lifetime; retained event history remains available. |
| `GOODSENTRY_RELEASE_RETENTION_DAYS` | `365` | Organization/version metadata lifetime, independent of file and master lifetimes. |
| `GOODSENTRY_RETENTION_INTERVAL_SECONDS` | `60` | Idle cleanup interval, from 1 to 3600 seconds. |
| `MONITOR_WORKERS` | `2` | Concurrent checks per process, from 1 to 16. |
| `ISSUE_MAINTENANCE_INTERVAL_SECONDS` | `1` | Polling for expired mutes and pending next-release resolution, from 1 to 3600 seconds. Longer intervals delay metadata-only binding and status display; fresh captures evaluate immediately. |
| `ALERT_WORKERS` | `2` | Concurrent deliveries per process, from 1 to 16. |
| `ALERT_MAX_ATTEMPTS` | `5` | Maximum delivery attempts, from 1 to 20. |
| `ALERT_DELIVERY_TIMEOUT_SECONDS` | `10` | Delivery timeout, from 1 to 60 seconds. |
| `GOODSENTRY_UPTIME_ALLOW_PRIVATE_IPS` | `false` | Allow monitors to reach your private network. |
| `GOODSENTRY_WEBHOOK_ALLOW_PRIVATE_IPS` | `false` | Allow webhooks to reach your private network. |

The process also accepts `MAX_BODY_BYTES`, defaulting to 32 MiB for compressed and expanded request bodies. `GLITCHTIP_DOMAIN` and supported `GLITCHTIP_ENABLE_*` aliases ease migration; `GOODSENTRY_*` names take precedence. `SERVER_ROLE` is `all_in_one`. Leave `VALKEY_URL` empty. Enabling currently unavailable MCP, DuckDB, admin, OpenAPI or Valkey options fails startup rather than reporting them as available. The mail transport is shared by alerts and account password recovery; use `consolemail://` only for local development.

## Scoped API tokens

Owners and admins can open **API tokens** to create a credential, select projects and set its lifetime. The secret appears once; copy it before dismissing or leaving the view. Tokens expire after 90 days by default, with a selectable lifetime of 1–366 days. Revocation takes effect on subsequent requests. An all-project credential also includes projects created later; a restricted credential keeps only its selected projects.

Choose only the scopes your automation needs:

| Scope | Supported operations |
| --- | --- |
| `org:read` | Organization, people, team and authorized-project discovery. |
| `org:write` | Member invitations, team changes and organization rename/delete. |
| `project:read` | Project metadata/events, event attachments and legacy user reports. |
| `project:write` | SDK key management and fixed-window quotas; legacy feedback submission and attachment reads. |
| `event:read` | Issues and issue events. |
| `event:write` | Issue reads, individual/bulk changes and merging. |
| `event:admin` | Issue deletion. |
| `project:releases` | Release management. |

Scopes preserve the current membership and project boundaries. Organization changes also require the current owner/admin role; deletion requires an owner. Ordinary compatible project responses exclude SDK keys and DSNs.

Set `SENTRY_AUTH_TOKEN` to the issued secret, then use the verified sentry-cli 3.8.0 command:

```sh
SENTRY_URL=http://localhost:8000/ SENTRY_ORG=my-workspace sentry-cli projects list
```

Each compatible route checks its implemented scope and current project grants. Existing tokens retain their scopes. Broader management API coverage remains pending.
