# Compatible retained-log API acceptance (2026-10-21)

Verified the GlitchTip-shaped log reads end to end on a local build and on the
published image: list with filters and keyset paging, detail, resource names and
hourly statistics.

## Environment

- GoodSentry from commit `e91fb9a`, published index `sha256:0f0009dc5dde5c9674f27f22cc1d05d553d97f0b4ef7caccff9404f94676cd37`, running as `65532:65532` in `goodsentry-logs-public-20261021` on `127.0.0.1:18220`.
- PostgreSQL 18 container `goodsentry-verification-20261020` with the fresh schema `goodsentry_logs_public_20261021`; startup applied migration054.
- `GOODSENTRY_AUTH=required` with `GOODSENTRY_ENABLE_LOGS=true`, owner `logs@example.test`, organization `logs-org`, one Go project.
- Two logs arrived through a real Sentry envelope: a `warn` record with `service.name`, `environment` and `host.name` attributes plus trace/span IDs, and an `error` record from a second host. A one-day `event:read` token authenticated the reads.

## Receipts

| File | Contents |
| --- | --- |
| `public-api.json` | Published-image responses for the list page, filtered list, detail, missing detail, resources, host resources, statistics summary and the rejected requests. |
| `public-image.json` | Index digest, per-architecture digests, labels and runtime configuration. |
| `public-binary.json` | Extracted `/goodsentry` against local `-trimpath -buildvcs=false` builds: exact for amd64 and arm64. |
| `public-source.json` | GitHub tree at `e91fb9a` against `git ls-tree`: 1272 blobs, exact. |
| `verify_source.py` | Script that produced the comparison. |
| `public-container.json` | Container identity, port mapping, environment and schema. |

## Observed behavior

- The list returned the newest log first with `id`, UTC `timestamp`, `level`, `body`, `service`, `environment`, `host`, `traceID`, `spanID`, `severityNumber`, `data` and `projectId`. A `limit=1` page carried a `Link: rel="next"` cursor.
- The stored OpenTelemetry `warn` level was published as `warning`, the `level=warning` filter matched it, and `severityNumber` was 13 for it and 17 for the error log.
- `data` contained the unwrapped attributes (`service.name`, `environment`, `host.name`) as plain values.
- `/logs/1/` returned the row, `/logs/999999/` returned 404, a query on the detail route returned 400, an unpublished level returned 400 and an invalid Bearer credential returned 401.
- `/logs/resources/` listed the two services, the environment and both hosts ordered by last use; `resource_type=host` returned only hosts.
- `/logs/stats/` returned floored hourly intervals covering the default seven-day window and one series per level with zero-filled counts.

## Local gates

The focused store and HTTP suites cover filter containment, half-open windows, keyset paging without duplicates, resource aggregation, statistics alignment, the disabled-feature 404 and OTLP `host.name` projection. The full integration race, integration-tag vet, `go mod verify`, formatting and the build pass. One unrelated pre-existing project-key rate test failed once at a one-second window boundary and passed three focused reruns; CI passed the same suite.

## Reproduce

Start the image with log ingestion enabled, create the owner and project through `/api/v1/setup`, send a `log` envelope item carrying `service.name`, `environment` and `host.name`, mint an `event:read` token and request the four `/api/0/organizations/{org}/logs/` routes with it.
