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
| `GOODSENTRY_AUTH` | `disabled` | `disabled` uses one implicit local owner without sessions. `required` enables setup, login and per-user accounts. Use `required` before exposing the service. |
| `GOODSENTRY_DEFAULT_EMAIL` | `owner@localhost` | Implicit owner email in open mode. |
| `GOODSENTRY_DEFAULT_NAME` | `GoodSentry Owner` | Implicit owner display name. |
| `GOODSENTRY_DEFAULT_ORGANIZATION` | `Default Organization` | Implicit owner workspace. |
| `GOODSENTRY_DEFAULT_PASSWORD` | Empty | Optional 12–72 byte password assigned only when the implicit owner is first created. |
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
| `GOODSENTRY_ENABLE_MCP` | `false` | Enable `/mcp`, OAuth discovery and consent for MCP clients. |
| `ENABLE_OPENAPI` | `false` | Serve the generated OpenAPI document at `/api/openapi.json` and its endpoint matrix at `/api/docs/`. |
| `ENABLE_ADMIN` | `false` | Enable the read-only Administration view and `/api/v1/admin/overview` for organization owners and admins. |
| `GOODSENTRY_ENABLE_ORGANIZATION_CREATION` | `false` | Allow owners/admins to create additional organizations through the compatible API. `ENABLE_ORGANIZATION_CREATION` is accepted as a GlitchTip-compatible alias. |
| `GOODSENTRY_OIDC_ISSUER` | Empty | Optional OpenID Connect issuer URL. HTTPS is required except on loopback. |
| `GOODSENTRY_OIDC_CLIENT_ID` | Empty | OIDC client identifier; required with an issuer. |
| `GOODSENTRY_OIDC_CLIENT_SECRET` | Empty | OIDC client secret; required with an issuer and never sent to browsers. |
| `GOODSENTRY_OIDC_NAME` | `Single sign-on` | Button label for the configured identity provider. |
| `GOODSENTRY_OIDC_SCOPES` | `openid profile email` | Space-separated scopes; must include `openid`. |
| `GOODSENTRY_OIDC_AUTO_CREATE` | `false` | Create an OIDC-only account when a verified email has no existing account. |
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

The process also accepts `MAX_BODY_BYTES`, defaulting to 32 MiB for compressed and expanded request bodies. `GLITCHTIP_DOMAIN` and supported `GLITCHTIP_ENABLE_*` aliases ease migration; `GOODSENTRY_*` names take precedence. `SERVER_ROLE` is `all_in_one`. Leave `VALKEY_URL` empty. Enabling the unavailable DuckDB or Valkey options fails startup rather than reporting them as available. The mail transport is shared by alerts and account password recovery; use `consolemail://` only for local development.

### API documentation and administration

Set `ENABLE_OPENAPI=true` to publish the API reference. `/api/openapi.json` is generated at process startup from the routes that build actually registered, and `/api/docs/` presents the same data as a browsable method/path matrix. Routes for optional features appear only when those features are enabled. The document uses the configured `GOODSENTRY_DOMAIN` as its server URL and describes each route family's authentication boundary without exposing credentials.

Set `ENABLE_ADMIN=true` to add **Administration** to the sidebar. Owners and admins see non-secret operational totals for the organizations they manage, enabled capabilities and effective retention. The server rechecks the current role on every request, so a demoted account loses access immediately. This is GoodSentry's own read-only operations view, not Django admin.

### Authentication mode

`GOODSENTRY_AUTH=disabled` is the frictionless default. On startup GoodSentry creates or repairs the configured implicit owner, owner membership, default organization and default team using an advisory-lock transaction. Every session-authenticated request resolves to that owner without a cookie; restarting does not create duplicates or replace the existing password. `/api/v1/setup` reports `required:false`, while `/api/v1/me` returns the implicit owner and a stable CSRF token. All mutations still require same-origin and that CSRF token.

This mode is a single-user administrative mode for local use or a trusted private network. Set `GOODSENTRY_AUTH=required` before public exposure to restore the existing first-owner setup, login, sessions, invitations, OIDC and per-user flows.

### OpenID Connect

Register this callback with your identity provider:

```text
<GOODSENTRY_DOMAIN>/auth/oidc/callback
```

Configure the issuer, client ID, client secret and a display name. GoodSentry
discovers the provider metadata, requires the authorization-code flow with
S256 PKCE, verifies the signed ID token, issuer, audience, nonce and expiry,
and accepts only a verified email claim. An identity is linked once to the
matching normalized account email; later logins use the provider subject.
With auto-creation disabled, an unknown email must first receive an
organization invitation. OIDC-only accounts have no local password until the
owner completes account recovery. The provider secret and raw identity tokens
are never sent to the browser.

## Scoped API tokens

Owners and admins can open **API tokens** to create a credential, select projects and set its lifetime. The secret appears once; copy it before dismissing or leaving the view. Tokens expire after 90 days by default, with a selectable lifetime of 1–366 days. Revocation takes effect on subsequent requests. An all-project credential also includes projects created later; a restricted credential keeps only its selected projects.

Choose only the scopes your automation needs:

| Scope | Supported operations |
| --- | --- |
| `org:read` | Organization, people, team and authorized-project discovery. |
| `org:write` | Member invitations, team changes, organization rename/delete and additional organization creation when enabled. |
| `project:read` | Project metadata/events, event attachments and legacy user reports. |
| `project:write` | SDK key management, fixed-window quotas and environment visibility; legacy feedback submission and attachment reads. |
| `event:read` | Issues and issue events. |
| `event:write` | Issue reads, individual/bulk changes and merging. |
| `log:read` | Retained Sentry and OTLP logs through MCP; only available when MCP and logs are enabled. |
| `event:admin` | Issue deletion. |
| `project:releases` | Release management. |

Scopes preserve the current membership and project boundaries. Organization changes also require the current owner/admin role; deletion requires an owner. Ordinary compatible project responses exclude SDK keys and DSNs.

Set `SENTRY_AUTH_TOKEN` to the issued secret, then use the verified sentry-cli 3.8.0 command:

```sh
SENTRY_URL=http://localhost:8000/ SENTRY_ORG=my-workspace sentry-cli projects list
```

Each compatible route checks its implemented scope and current project grants. Existing tokens retain their scopes. Broader management API coverage remains pending.

## MCP clients

Enable the opt-in endpoint with `GOODSENTRY_ENABLE_MCP=true`, then point an OAuth-capable MCP client at `https://your-goodsentry.example/mcp`. Clients discover protected-resource and authorization-server metadata automatically, register as public PKCE clients and ask you to choose an organization and project grant. The consent screen exposes the requested scopes; access tokens expire after one hour and refresh tokens rotate after each use.

Clients without OAuth can use a scoped API token as `Authorization: Bearer <token>`. A token needs `org:read`, `project:read`, `event:read`/`event:write` or `log:read` for the corresponding tools. Revoking an MCP token from **API tokens** invalidates its linked refresh grant immediately.
