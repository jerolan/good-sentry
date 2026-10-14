# GoodSentry tasks and evidence

Design authority: ../SPEC.md. Ordering: plan.md. An unchecked item is incomplete, including its verification. User approved the module map and authorized execution on 2026-10-14. Product name: GoodSentry.

## Foundation and first durable SDK event

- [ ] Prepare native Go and a working PostgreSQL/container verification environment in WSL.
  - Verify official compiler checksum/version and actual database connectivity. Record tooling paths; do not alter system shell profiles.
- [ ] Initialize Git and commit the approved specification package.
  - Verify exact file contents, local links, git diff --check and clean working tree.
- [ ] Validate runtime configuration and build the executable with health endpoints.
  - Files: go.mod, cmd/goodsentry/main.go, internal/config/config.go and its tests.
  - Verify invalid environment rejection, go test and go build.
- [ ] Add PostgreSQL migrations and lifecycle.
  - Files: internal/store/store.go, schema.sql, store_test.go and command integration.
  - Verify clean database migration, repeated startup, database readiness and real persistence.
- [ ] Implement account setup/login/session and organization/project bootstrap.
  - Files: internal/server/server.go, auth.go, workspace.go, auth_test.go and store additions.
  - Verify first-owner race/closure, login/logout/expiry, CSRF and cross-organization access denial against PostgreSQL.
- [ ] Implement SDK envelope/store framing and authentication.
  - Files: internal/protocol/envelope.go, transport.go, protocol_test.go and go.mod dependencies owned by primary.
  - Verify public wire grammar, binary payloads, unknown types, four compression formats, legacy store and invalid request limits.
- [ ] Implement pure event normalization and stable fingerprinting.
  - Files: internal/event/event.go, event_test.go.
  - Verify exception/message grouping, custom/default fingerprints, missing IDs and timestamp variants.
- [ ] Integrate durable ingestion, issue list/event detail and original responsive UI.
  - Verify real Go/Python/Node/browser SDK errors through authenticated reads, idempotent duplicate events, grouping and restart persistence.
- [ ] Add the non-root Dockerfile and PostgreSQL compose deployment with embedded assets.
  - Verify docker compose up --build from a clean database, readiness, SDK capture and persisted data after restart.

## Issue workflow and artifacts

- [ ] Deliver search, tag/environment/release filters, sorting and pagination with UI.
- [ ] Deliver resolved/ignored/regression workflow, assignment, comments and activity history.
- [ ] Deliver merge/unmerge with preserved event partitions and correct future grouping.
- [ ] Deliver releases, commit metadata and deploy records through compatible API.
- [ ] Deliver release/debug-ID JavaScript artifact uploads and verified source mapping.
- [ ] Research and implement modern sentry-cli artifact bundles, then test the actual CLI.
- [ ] Implement native debug-file and minidump ingestion plus verified native symbolication.

For each item above: add a consumer-facing test at the owning boundary, verify the actual UI/API outcome with stored data, inspect the diff and commit a single coherent increment. Do not mark a mapped-stack task complete based only on an upload response.

## Observability and operations

- [ ] Deliver transaction/span storage, time-window aggregates, average/p50/p95, errors and detail UI.
- [ ] Deliver SDK logs with typed attributes, filters, trace correlation and detail UI.
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
