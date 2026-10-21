# Grafana stats_v2 acceptance (2026-10-21)

Verified that the official Grafana Sentry data source can render a stats panel
from GoodSentry's compatible `stats_v2` endpoint, both from a local build and
from the published image.

## Environment

- Grafana `13.0.2` (commit `3fcdbc5a`), container `goodsentry-grafana-20261020`, listening on `127.0.0.1:18194`.
- Data source plugin `grafana-sentry-datasource 2.2.8` (Grafana Labs, updated 2026-10-07), UID `cg1v1pywuj5dsf`, organization slug `stats-grafana`.
- GoodSentry built from commit `7175fcc` as `/tmp/goodsentry-stats` (sha256 `8f1d89bdc11a175c`...), `GOODSENTRY_AUTH=required`, PostgreSQL 18 container `goodsentry-verification-20261020` with schema `goodsentry_stats_grafana_20261021_2130`.
- Fixture: owner `stats@example.test`, organization `stats-grafana`, project `1` (`Stats Project`), three stored error events five minutes past three consecutive hours.
- The data source URL is set through `jsonData.url`, not the top-level `url` field; the plugin ignores the latter. It currently points at the published-image container on `172.21.0.1:18216`.

## Local-build receipts

| File | Contents |
| --- | --- |
| `grafana-health.json` | Grafana version and commit. |
| `grafana-plugin.json` | Installed Sentry data source plugin identity and version. |
| `grafana-datasource.json` | Data source configuration (secrets removed). |
| `grafana-stats-v2-error-1h.json` | Raw `/api/ds/query` reply for hourly error counts. |
| `grafana-stats-v2-variants.json` | Transaction `1m`, all-project `-1`, daily and `groupBy` variants. |
| `goodsentry-stats-v2-direct.json` | Direct Bearer response from GoodSentry for the same window. |
| `goodsentry-stats-v2-rejections.json` | Outcome, reason, non-category `groupBy` and hidden-project responses. |
| `local-binary.json` | Local build identity. |

## Published-image receipts

| File | Contents |
| --- | --- |
| `public-source.json` | GitHub tree at `7175fcc` against `git ls-tree`: 1224 blobs, exact. |
| `verify_source.py` | Script that produced the comparison. |
| `public-image.json` | Index digest, per-architecture digests, labels, user, environment and size. |
| `public-binary.json` | Extracted `/goodsentry` per architecture against local `-trimpath -buildvcs=false` builds: exact for amd64 and arm64. |
| `public-container.json` | Container identity, port mapping, environment and fixture schema. |
| `public-image-stats-v2-direct.json` | Direct Bearer response from the published image. |
| `public-image-grafana-stats-v2.json` | Grafana panel reply served by the published image. |
| `public-image-grafana-stats-v2-variants.json` | The same five query variants against the published image. |

Published index `sha256:464e8e4fed7680c7bd584932ef36e1cc15ea43d6ea1f75062b27cd8cd718a09e` (amd64 `sha256:17a1476276e8fee0f02de5fb0dfcd2b27e5abaee18b5e8a3bbf41c84f710d6a7`, arm64 `sha256:45e155f6074fc8095a59f0d3b4eb28818b1ce25eaf403f8368f2f30bb8a37523`), labeled with revision `7175fcc474ded2b72f5ab83f80c428df5a5ba268` and running as `65532:65532`.

## Observed behavior

- The Grafana panel returned status `200` with four hourly buckets carrying `1, 1, 1, 0` errors, matching the three stored events and the `sum(times_seen)` total of `3`. The published image produced the same frame from the same query.
- The transaction `1m` variant returned `181` buckets with zero counts, which is correct for a project holding only error events; the frame shape and interval alignment are what the panel consumes.
- The all-project `-1` selector, the `1d` interval and the `groupBy=category` variant each returned `200` frames on both builds.
- `outcome`, `reason` and non-category `groupBy` requests returned `400`; a project outside the token's grant returned `404`.
- Local binary verification needs `-buildvcs=false` to match the image, because the image build has no `.git` directory. Without it the sizes still match but the bytes differ.

## Reproduce

Start GoodSentry against the fixture schema, seed it with `/tmp/seed-stats.mjs`, point a Grafana Sentry data source at it through `jsonData.url`, then run the `statsV2` query with `statsFields: ["sum(times_seen)"]`, `statsCategory: ["error"]`, `statsInterval: "1h"` and `projectIds: ["1"]`.
