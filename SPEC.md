# GoodSentry

Status: design authorized by the user on 2026-10-14. Implementation and verification remain pending.

## Objective and acceptance boundary

Build an independently authored Go error tracking and observability server. Existing applications use their Sentry SDK by changing the DSN to GoodSentry. Deliver one application image containing the HTTP server, UI, background jobs, scheduler and migrations. PostgreSQL is the only required external service. Valkey is optional. Development and verification run in WSL2.

The user approved the capability map and authorized elaborating and executing its specifications, plans and tasks. The requested product name is GoodSentry. The existing workspace directory remains named SentryGo.

GlitchTip's publicly documented functionality is the minimum target, not merely an ingestion endpoint. Shipping an intermediate increment does not satisfy the complete goal. A capability is complete only when its observable result has been verified. Accepting and dropping an unsupported item is not functional compatibility.

## Clean-room method

Use public protocol specifications, product documentation, release notes, published API contracts and black-box SDK/CLI traffic. Do not read, copy, translate or adapt Sentry, Relay or GlitchTip server implementation source, tests, assets or database schema. Test SDKs as external clients. Keep provenance links here. Third-party Go libraries may implement general protocols such as PostgreSQL, compression and source maps.

## Capability map and ownership

| Stable id | Responsibility | Depends on |
|---|---|---|
| runtime | Environment, PostgreSQL, migrations, HTTP lifecycle, jobs, embedded UI | none |
| workspace | Accounts, sessions, organizations, teams, memberships, projects, DSNs and API tokens | runtime |
| ingestion | Sentry store/envelope wire formats, authentication, compression, CORS, limits | workspace |
| artifacts | Releases, deploys, source maps, debug IDs, native symbols | workspace |
| issues | Error grouping, event context, filtering, triage, assignment, comments, merge/unmerge | ingestion, artifacts |
| telemetry | Transactions, spans, logs, aggregates and correlation | ingestion |
| uptime | HTTP/TCP checks and incoming heartbeats | workspace |
| alerts | Frequency rules, uptime transitions, email and webhook delivery | issues, uptime |
| integrations | Supported management API, OpenAPI, MCP and cold archival | issues, telemetry, uptime, alerts |

Construct the UI alongside each capability. Provider contracts and all design decisions live in this specification. Implementation order and execution evidence live in tasks/plan.md and tasks/todo.md.

## Architecture

Use one Go module, a small executable in cmd/goodsentry, net/http routing and explicit dependency construction. Embed static HTML, CSS and JavaScript using go:embed. The UI has no external CDN or runtime build service. Use pgx for PostgreSQL, migrations embedded with the binary, and transactions for consistency. Keep the database schema relational for memberships, keys and grouping; retain original structured event data in JSONB.

Ingest events durably before returning success. A request may normalize synchronously and commit the event and its alert work atomically. Postgres owns jobs and retry state. Acknowledged events must survive restarts. A unique project/event-id constraint prevents SDK retries from incrementing counts twice. Concurrent grouping uses a unique project/fingerprint constraint and database atomic operations. Claim background work with row locking and recover expired leases. Never require Redis for correctness.

HTTP, workers and scheduler stop on termination signals and close database connections. Startup applies forward-only, versioned migrations under a PostgreSQL advisory lock. Readiness checks the database. Deployment rollback requires a compatible binary or database backup restoration; destructive rollback migrations are not automatic.

Serve an original, responsive application UI with a compact sidebar, neutral surfaces, a teal accent, readable tables and accessible forms. Show loading, empty, error and permission states. Preserve filters in URLs. Escape telemetry as text, including stack traces, tags and log bodies. Never render event content as trusted HTML.

## Deployment and configuration

Default HTTP port is 8000. docker compose up --build starts GoodSentry and PostgreSQL with health checks, persistent database storage and an uploads volume. The application image runs as a non-root user and includes CA roots. No separate web/worker/cron containers are needed. An externally managed PostgreSQL URL also works.

Canonical variables are DATABASE_URL, SECRET_KEY, GOODSENTRY_DOMAIN, PORT, EMAIL_URL, DEFAULT_FROM_EMAIL, VALKEY_URL and GOODSENTRY_ENABLE_* flags. Accept GLITCHTIP_DOMAIN and the supplied GLITCHTIP_ENABLE_LOGS/UPTIME/MCP/DUCKDB names as migration aliases, with explicit GoodSentry variables taking precedence. SERVER_ROLE=all_in_one is supported. Reject unsupported role values rather than silently losing background tasks. ENABLE_OPENAPI and ENABLE_ADMIN control GoodSentry's own API documentation and administration surface, not Django functionality.

An empty VALKEY_URL selects PostgreSQL-only operation. Optional features must report their actual availability. Never accept a true feature flag while leaving its behavior unimplemented. EMAIL_URL supports consolemail:// and SMTP, with credentials excluded from logs. Persist uploads under a configurable directory. Reject placeholder production secrets. Document local development defaults separately.

Memory target: measure application RSS under an explicit repeatable workload and publish results. The user's 256-512 MB example is an operational target, not a measured claim. Do not claim this target until a representative load test demonstrates it.

## Runtime and workspace contracts

Provide /healthz for process health and /readyz for database readiness. Setup creates the first account and organization without a shipped shared password. Accounts can log in/out and recover passwords when email is configured. Use a memory-hard or established adaptive password hash. Sessions are opaque random secrets, stored hashed, expire, and use HttpOnly/SameSite cookies with Secure under HTTPS. Browser mutations require same-origin/CSRF protection. API Bearer tokens are hashed, revocable and scoped to authorized organizations/projects. SDK ingestion keys identify a project and do not grant management access.

Organizations contain memberships and teams. Projects belong to an organization and may be assigned teams. The same identity can access multiple organizations only through membership. Enforce scope at every read and mutation. Owners/admins manage memberships, projects, credentials and alerts; members can perform the approved issue workflow. Do not expose a bootstrap endpoint after the first owner has been created. Record account, organization and project identifiers as opaque strings at the HTTP boundary.

## Ingestion contract

Support POST /api/{project}/store/ and /api/{project}/envelope/, including deployments under a URL path prefix. Parse Sentry protocol v7 authentication from X-Sentry-Auth, query parameters and the envelope dsn header. Multiple supplied identities must agree and match the route's project. An absent or invalid SDK key returns 403. Never use management tokens as SDK credentials.

Envelope headers and item headers are one-line UTF-8 JSON. Item length counts bytes and permits arbitrary binary data and internal newlines. Missing length consumes one line. An optional terminal newline, zero-length attachments, unknown attributes and unknown item types must not corrupt subsequent items. Reject malformed/truncated framing and negative/overflowing lengths. Preserve unsupported item metadata and account for unsupported types without pretending to provide their feature.

Accept identity, gzip, zlib-wrapped deflate, Brotli and Zstandard HTTP bodies. Support legacy store zlib+base64 encoding without Content-Encoding. Apply compressed and decompressed byte limits and bounded item counts. Return 413 on limits, 400 on malformed data, 415 on unsupported encodings and 503 on unavailable durable storage. Successful event submission returns 200 JSON with its event id. Return rate limits using 429, Retry-After and X-Sentry-Rate-Limits. Browser SDK requests support the documented simple content types and OPTIONS/CORS, with rate-limit headers exposed.

Persist errors/messages with timestamps, exception chains, stack frames, breadcrumbs, tags, users, request, contexts, SDK, environment, release and arbitrary extra fields. Store binary attachments associated with event IDs using generated paths; never trust client filenames as paths. Protect attachment reads with project authorization.

## Issues and artifacts

Default grouping is an independently designed stable hash of exception type and normalized stack location, with message fallback. Respect SDK custom fingerprint arrays and the {{ default }} marker. Grouping is project-scoped and deterministic. Repeated events update first/last seen and occurrence counts once. A new event for a resolved issue creates a visible regression according to recorded resolution state. Support unresolved/resolved/ignored, assignment, comments, search by text/status/environment/release/tags and sorting with pagination.

Merge groups without losing events or their original grouping keys; subsequent events follow the merged association. Unmerge restores the original partition. Retain activity history. Release operations support create/finalize/list/delete, commit metadata and deploys. Traditional source maps and debug-ID artifacts must turn a known minified JavaScript frame into its original file, line, column and source context. Upload acceptance alone is insufficient. Native minidumps and debug symbol formats require explicit upload and symbolication proof. Modern sentry-cli artifact bundles need their own verified compatibility contract.

## Telemetry, uptime and alerts

Transactions persist trace/span relationships, operation, endpoint, duration, status, release and environment. Provide counts, error counts, average, p50 and p95 for a selected time window, plus transaction detail and span breakdown. Logs support Sentry log envelopes and native OTLP /v1/logs, preserving typed attributes, severity and trace links. Filter by time, level, service, environment, text and trace. Retention is configurable by data class.

Uptime monitors support HEAD, GET, POST with expected status, TCP and heartbeat intervals. Store checks, response times and up/down transitions. Scheduler coordination prevents duplicate checks across application replicas; missed heartbeats become down and a subsequent heartbeat recovers. Configured monitor targets are the approved network boundary; bound timeouts, response reads and redirect behavior.

Alert rules use error counts over a rolling window and notify on threshold crossing with cooldown. Uptime transitions trigger down/recovery messages. Persist deliveries, retry failures with bounded backoff and expose delivery status. Email and webhook payloads contain a link to the actual issue/monitor. Do not claim exactly-once external delivery; persisted intents provide at-least-once delivery with stable delivery identifiers.

## Integrations and compatibility limits

Management API routes use /api/0 where compatible with published Sentry contracts. Supported routes return stable structured JSON, pagination and meaningful status codes. The independent UI may use /api/v1 for application-specific resources. Publish supported endpoint and SDK matrices. Verify representative GitLab/Grafana workflows before claiming them.

Optional MCP uses Streamable HTTP with authenticated scoped access to issues, logs, transactions, alerts and monitors. Optional cold archival uses Parquet and embedded DuckDB or another explicitly approved implementation with equivalent supported queries and local/S3 storage. Social OAuth/OIDC and advanced administration remain tracked parity requirements. Session tracking is explicitly unsupported by GlitchTip; supporting or adding it in GoodSentry must be described separately from the baseline. Session Replay and profiling are Sentry expansion work, not substitutes for unfinished GlitchTip parity.

## Commands and source layout

Commands to make executable as increments land:

```sh
go run ./cmd/goodsentry
go build -trimpath -o bin/goodsentry ./cmd/goodsentry
go test ./...
go test -race ./...
go vet ./...
DATABASE_URL=postgres://goodsentry:goodsentry@localhost:5432/goodsentry go test ./... -tags=integration
docker compose up --build -d
docker compose down
```

cmd/goodsentry owns composition and lifecycle. internal/config owns environment parsing. internal/protocol owns wire decoding. internal/event owns event normalization. internal/store owns PostgreSQL and migrations. internal/server owns HTTP/auth/application endpoints and embedded UI. internal/jobs owns scheduled checks and deliveries. Tests live beside their production owners; external SDK and browser exercises live under tests/compat.

Use gofmt, ordinary Go errors with context, parameterized SQL and explicit context propagation. Example style:

```go
func (s *Store) Project(ctx context.Context, id string) (Project, error) {
    var p Project
    err := s.pool.QueryRow(ctx, projectSQL, id).Scan(&p.ID, &p.Name)
    if err != nil {
        return Project{}, fmt.Errorf("read project: %w", err)
    }
    return p, nil
}
```

## Verification and delivery

Protect protocol parsing with table cases and fuzzing for binary framing, decompression bombs and malformed lengths. Test default/custom grouping and timestamp semantics at their owning boundary. Use actual PostgreSQL for migrations, duplicate retries, concurrency, isolation, restart persistence and job recovery. Do not mock SQL to establish persistence claims.

Exercise pinned official Go, Python, Node and browser Sentry SDKs against the running server. Verify the event through the authenticated API/UI after capture. Include transactions, logs and attachments where each SDK supports them. Exercise source-map CLI uploads, mapped frames, alert delivery, uptime down/recovery and retention. Use a real browser for onboarding, project setup, DSN, issue triage and responsive UI. Container verification builds the image, starts the documented compose stack, sends SDK traffic, restarts and checks persistence. Record exact checks and outcomes in tasks/todo.md.

Each new test must identify the consumer contract, a credible regression, the gap in existing coverage and whether it would introduce a test-only production seam. Prefer observable outcomes and real boundary exercises. Run focused checks during implementation, then full tests, race detection, vet, relevant integration and browser checks. Failed checks remain visible; do not weaken them to finish. Commit each coherent verified increment atomically with a why-focused commit message.

Always preserve clean-room provenance, user scope, existing controls and data. Ask before publishing, deploying to an external environment, destructive migration or changing an approved design materially. Never commit secrets, remove failing proof to obtain green checks, report untested compatibility, or call partial GlitchTip parity complete.

## Current evidence and unresolved proofs

At initialization the workspace is empty and has no Git repository. Native Go is being prepared in a temporary WSL directory. Docker Desktop exists but WSL integration was not available in the initial probe. No product behavior has been implemented or verified. The local sandbox launcher fails before process creation; approved individual WSL commands run successfully.

Modern artifact-bundle negotiation and native symbolication need focused contract research and runtime proof. The June 2026 GlitchTip release adds /v1/logs and optional email despite older guides saying otherwise. Advanced span storage and optional DuckDB behavior must be verified separately. These are open work, not scope reductions.

## Public provenance

- [GlitchTip documentation](https://glitchtip.com/documentation/), [installation](https://glitchtip.com/documentation/install/), [SDK integration](https://glitchtip.com/sdkdocs/all-sdks/).
- [Error tracking](https://glitchtip.com/documentation/error-tracking/), [performance](https://glitchtip.com/documentation/performance/), [uptime](https://glitchtip.com/documentation/uptime-monitoring/), [logs](https://glitchtip.com/documentation/logs/), [CLI contracts](https://glitchtip.com/documentation/cli/).
- [GlitchTip 6.2 release](https://glitchtip.com/blog/2026-06-22-glitchtip-6-2-released/), [6.1 release](https://glitchtip.com/blog/2026-03-23-glitchtip-6-1-released/), [merge behavior](https://glitchtip.com/blog/2025-05-19-glitchtip-5-0-released/).
- [Sentry authentication specification](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/foundations/transport/authentication.mdx), [envelope grammar](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/foundations/envelopes/index.mdx), [item types](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/foundations/envelopes/envelope-items.mdx).
- [Transport](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/foundations/transport/index.mdx), [compression](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/foundations/transport/compression.mdx), [rate limiting](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/foundations/transport/rate-limiting.mdx), [logs](https://raw.githubusercontent.com/getsentry/sentry-docs/master/develop-docs/sdk/telemetry/logs.mdx).
- [Traditional release file upload API](https://docs.sentry.io/api/releases/upload-a-new-organization-release-file/). Legacy store/session/check-in documentation is in the archived getsentry/develop documentation repository and must not override current contracts.
