# GoodSentry tasks and evidence

Design authority: ../SPEC.md. Ordering: plan.md. An unchecked item is incomplete, including its verification. User approved the module map and authorized execution on 2026-10-14. Product name: GoodSentry.

## Foundation and first durable SDK event

- [x] Prepare native Go and a working PostgreSQL/container verification environment in WSL.
  - Go 1.27.2 is installed at /tmp/sentrygo-toolchain/go; official archive SHA-256 `ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5`. PostgreSQL 18 accepts real integration traffic; Docker 29.8.1 works through WSL. No shell profiles changed.
- [x] Initialize Git and commit the approved specification package.
  - Commit ca40c22 owns the initial package; subsequent changes are intention-focused local commits. No remote publication.
- [x] Validate runtime configuration and build the executable with health endpoints.
  - Files: go.mod, cmd/goodsentry/main.go, internal/config/config.go and its tests.
  - Verify invalid environment rejection, go test and go build.
- [x] Add PostgreSQL migrations and lifecycle.
  - Files: internal/store/store.go, migrations/*.sql, store_test.go and command integration.
  - Verify clean database migration, repeated startup, database readiness and real persistence.
- [ ] Implement account setup/login/session and organization/project bootstrap.
  - Files: internal/server/server.go, auth.go, workspace.go, auth_test.go and store additions.
  - Verify first-owner race/closure, login/logout/expiry, CSRF and cross-organization access denial against PostgreSQL.
  - Setup/login/logout, CSRF, project DSN and eight concurrent first-owner attempts pass. Complete membership/expiry coverage remains pending.
- [x] Implement SDK envelope/store framing and authentication.
  - Files: internal/protocol/envelope.go, transport.go, protocol_test.go and go.mod dependencies owned by primary.
  - Verify public wire grammar, binary payloads, unknown types, four compression formats, legacy store and invalid request limits.
- [x] Implement pure event normalization and stable fingerprinting.
  - Files: internal/event/event.go, event_test.go.
  - Verify exception/message grouping, custom/default fingerprints, missing IDs and timestamp variants.
- [x] Integrate durable ingestion, issue list/event detail and original responsive UI.
  - Verify real Go/Python/Node/browser SDK errors through authenticated reads, idempotent duplicate events, grouping and restart persistence.
  - Durable errors and UI are implemented. Real Go/Python/Node/browser exceptions pass through authenticated reads. Eight concurrent duplicate events count once. Compose restart preserves an acknowledged event and session.
- [x] Add the non-root Dockerfile and PostgreSQL compose deployment with embedded assets.
  - Verify docker compose up --build from a clean database, readiness, SDK capture and persisted data after restart.
- [x] Compile the user-authorized Tailwind CSS into the embedded UI.
  - Verify a pinned reproducible stylesheet build, browser workflow and responsive widths; retain a single Go runtime image.

## Issue workflow and artifacts

- [ ] Deliver search, tag/environment/release filters, sorting and pagination with UI.
- [ ] Deliver resolved/ignored/regression workflow, assignment, comments and activity history.
- [ ] Deliver merge/unmerge with preserved event partitions and correct future grouping.
- [ ] Deliver releases, commit metadata and deploy records through compatible API.
- [ ] Deliver release/debug-ID JavaScript artifact uploads and verified source mapping.
  - Native upload/list, mapped ingestion and their UI pass. Compatible release lifecycle and CLI endpoints remain required for full acceptance.
- [x] Deliver authenticated native source-map upload/list and mapped capture through API and UI.
  - Owners/admins upload; actual members read metadata. Release/dist and debug-ID selection, original stacks, source location/context, idempotent retries, immutable-content conflict and input/processing budgets are verified.
- [ ] Research and implement modern sentry-cli artifact bundles, then test the actual CLI.
- [ ] Implement native debug-file and minidump ingestion plus verified native symbolication.

For each item above: add a consumer-facing test at the owning boundary, verify the actual UI/API outcome with stored data, inspect the diff and commit a single coherent increment. Do not mark a mapped-stack task complete based only on an upload response.

## Observability and operations

- [x] Accept current SDK span v2 batches atomically, deduplicate retries and aggregate only segments.
  - Preserve child relationships and typed attributes; verify real Node 11.7.0 error, trace and logs after the initially failing trace check.
- [ ] Deliver transaction/span storage, time-window aggregates, average/p50/p95, errors and detail UI.
  - Legacy transactions and standalone spans persist; last-24-hour counts/average/p50/p95/error aggregates and project-filtered UI pass. Selectable time window and complete trace/transaction detail remain pending.
- [ ] Deliver SDK logs with typed attributes, filters, trace correlation and detail UI.
  - SDK ingestion, deduplication, project/text/level/environment/trace filters, 25-row pagination and typed-attribute UI pass. Exact max-int64 decimal display has a real browser RED/GREEN regression. Service/time filters and complete time-window behavior remain pending.
- [ ] Deliver native OTLP /v1/logs and verify an actual exporter.
- [ ] Deliver configurable retention with actual removal/recovery checks.
- [ ] Deliver HTTP HEAD/GET/POST monitors and stored checks with UI.
- [ ] Deliver TCP checks and heartbeat expiry/recovery with UI.
- [ ] Verify scheduler leases and recovery across simultaneous replicas/restarts.
- [ ] Deliver frequency alert rules, cooldown and durable delivery intents.
- [ ] Deliver SMTP/consolemail and webhook retries, plus uptime down/recovery alerts.

## Remaining parity and release verification

- [ ] Complete scoped API tokens, team memberships, invitations and email password recovery.
- [ ] Complete supported Sentry-compatible management API routes, OpenAPI/admin flags and endpoint matrix.
- [ ] Verify GitLab error tracking and Grafana datasource integrations.
- [ ] Deliver optional authenticated MCP with tool-level scope enforcement.
- [ ] Deliver optional social OAuth/OIDC and advanced administration.
- [ ] Deliver optional local/S3 Parquet cold archival with verified historical/span queries.
- [ ] Verify true/false feature flag and GlitchTip alias behavior, including PostgreSQL-only mode.
- [ ] Run pinned real SDK matrix for Go, Python, Node and browser, then publish actual results.
- [ ] Run browser onboarding, DSN setup, issue triage and responsive/accessibility checks.
- [ ] Run full tests, race detection, vet, fuzz/parser checks, PostgreSQL concurrency/recovery and container acceptance.
- [ ] Measure application RSS under the documented repeatable workload before claiming the 256-512 MB target.
- [ ] Publish quickstart, upgrade/backup/restore procedure, SDK and endpoint matrices in README.md.
- [ ] Audit every specification requirement against current artifact/runtime evidence; only then complete the goal.

## Evidence log

- 2026-10-14: Workspace verified empty. WSL2 runs approved /bin/sh commands. Normal sandbox process creation fails before execution. Initial Docker wrapper reports missing WSL integration. Go installation preparation has started in /tmp/sentrygo-toolchain. No application behavior is implemented yet.
- 2026-10-14: Public-documentation research established the GlitchTip baseline and Sentry ingestion contracts. No upstream server implementation source was inspected. Source links and known compatibility gaps are recorded in SPEC.md.
- 2026-10-14: `go test -tags=integration ./...`, `go vet ./...` and `git diff --check` pass against PostgreSQL 18 through commit ad5b209. Full race tests passed before the final source-map parser fixes; rerun after the next integrated increment. Envelope fuzzing completed 31,840 executions without a failure. These checks do not establish unfinished feature parity.
- 2026-10-14: Official sentry-go 0.50.0 passes exception/transaction/typed-log retrieval against native WSL and Compose. Python sentry-sdk 2.72.0 passes the same matrix against Compose. Node 11.7.0 exceptions and typed logs pass; its real streamed-span aggregate initially fails. The new HTTP regression reproduces `unsupportedItems:[span]`; keep the trace check failing until the specified fix lands. SDK packages are isolated external test clients. Node's installation used an explicitly authorized exception to npq's publication-age gate, confined to temporary test tooling.
- 2026-10-14: Tailwind CLI 4.3.3 SHA-256 `dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a` produces a repeatable stylesheet. Browser onboarding/project/DSN/errors/filters/resolve/reopen/comments/login pass at 320/768/1440 px with no document overflow, JS errors or transport failures. Expected anonymous /me 401 is recorded separately. Local ignored evidence: test-results/browser/report.json and responsive screenshots. Compose serves exactly that compiled stylesheet; image/runtime verification currently covers Linux amd64.
- 2026-10-14: Independent pure source-map tests prove original file/line/column/context, debug-ID precedence and preserved original stacks. Upload/storage/API wiring remains incomplete. Official sentry-cli 3.8.0 SHA-256 `13f8cb34ae01a6a272d7d7c22e277a105286615b4020de900ea95a8de47cdbb6` exercised against our recorder establishes request negotiation, SHA-1 chunks, multipart gzip and SYSB-v2 bundle layout. Synthetic server responses are not real Sentry server evidence. The bundle decoder is the next bounded artifact increment.
- 2026-10-14: Span v2 regression now passes at HTTP and real PostgreSQL boundaries. Atomic rejection, eight concurrent retries including reversed batch order, preserved typed attributes/parent relationships and segment-only aggregation pass. Combined legacy/segment fixture: count 4, average 375 ms, p50 375 ms, p95 606.25 ms. Full `go test -tags=integration ./...`, `go vet ./...` and Docker `go test -race -tags=integration ./...` pass against PostgreSQL 18. The unchanged Node 11.7.0 matrix passes through Compose; primary independently retrieved its two related spans and single aggregate.
- 2026-10-14: Browser Sentry SDK 11.7.0 runs in Chromium 156.0.8078.4 using the official CDN tracing/logs bundle with fixed SRI. Exception, transaction and typed log retrieval pass from a separate HTTP origin; actual POST Origin and Access-Control-Allow-Origin are checked. No browser runtime errors or failed ingestion requests. This establishes the current error/transaction/log SDK matrix, not attachments, artifacts, all SDK versions or complete feature parity.
- 2026-10-14: SYSB v2 decoder is implemented and independently verified against the actual bundle generated by the official CLI recorder experiment. Full Go integration/race/vet checks pass with parser cases for unsafe/duplicate/nonregular paths, missing members, malformed metadata and expansion budgets. Bundle/source debug IDs remain distinct. Chunk upload endpoints and complete CLI symbolication are still pending.
- 2026-10-14: Telemetry browser workflow passes: visible 5 transactions / 1 error / average 400 ms / p50 300 ms / p95 880 ms; 25+1 log pagination; project/text/level/environment/trace filters survive reload; trace copy/link and typed disclosures work at 320/768/1440 px. A literal JSON `9223372036854775807` initially displayed incorrectly as `9223372036854776000`; after lossless log display parsing the same test passes with the exact digits and integer label. Evidence is kept separately under ignored test-results/browser-int64-red and browser-int64-green. No JS runtime errors, failed requests or unexpected console errors.
- 2026-10-14: Native source-map HTTP workflow first failed with 405 before routes existed, then passed upload-to-mapped-event checks. Review reproduced invalid indices accepted at upload and panicking on lookup; an independently authored ECMA-426 consumer now rejects those maps and preserves valid unmapped segments. A second HTTP regression fails under an overlay restoring old Debug-ID selection and passes with frame-specific eligibility. Indexed same-URL source context also has a public RED/GREEN regression; exact mapped source records now own content. The former source-map library dependency was removed.
- 2026-10-14: Source-map consumer fuzz campaigns passed 182,860 executions and then 77,653 on the final context implementation. Full PostgreSQL integration, race detection and vet pass. PostgreSQL tests cover restart persistence, eight concurrent identical uploads, conflicting bytes, real member/foreign-organization identities and exact/exceeded count/byte budgets. HTTP tests reject invalid mapping indices, malformed maps, oversized files and NUL metadata with 400/413, and prove maps cannot cross distributions. NUL metadata previously returned storage 503; its boundary regression now passes.
- 2026-10-14: Final browser source-map workflow passes with actual owner/member identities and 27 screenshots at 320/768/1440 px: multipart boundaries/CSRF, retained invalid-upload inputs, 201 new/200 identical, metadata reload, mapped file 3:5/source context and original stack. Evidence: ignored test-results/browser-sourcemaps-final/report.json; zero JS errors, transport failures or unexpected console messages. Intentional /me401 and invalid-map400 are recorded separately. Primary inspected screenshots. Compose upload/mapped capture, artifact checksum and session/event persistence after application restart also pass; Go SDK exception/transaction/log smoke remains green.
- 2026-10-14: OTLP factual research confirms GlitchTip 6.2 release notes announce native /v1/logs while its current logs guide and published OpenAPI do not document that receiver. Product auth/project routing/encoding details remain unverified. OTLP 1.11 defines HTTP JSON/protobuf/gzip, resource/scope/record attributes, nanosecond timestamps, AnyValue types and success/partial/error responses. Candidate external exporter pins: opentelemetry-sdk and opentelemetry-exporter-otlp-proto-http 1.45.1. No OTLP receiver or exporter proof is implemented yet.
