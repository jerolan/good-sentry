# Getting started

[Documentation](README.md) · [Project home](../README.md)

Run GoodSentry with one application container and PostgreSQL.

## Quickstart

You need Git, Docker with Compose and OpenSSL. On Windows, enable Docker Desktop's integration with your WSL2 distribution.

```sh
git clone https://github.com/jerolan/good-sentry.git
cd good-sentry
test -f .env || printf 'SECRET_KEY=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up -d
```

Open **http://localhost:8000**. With the default `GOODSENTRY_AUTH=disabled`, Compose creates an implicit owner and workspace and opens the dashboard without a login. Create a project and copy its DSN into your SDK configuration. Restarting preserves the same owner, workspace and projects.

The implicit account defaults to `owner@localhost`, `GoodSentry Owner` and `Default Organization`; override `GOODSENTRY_DEFAULT_EMAIL`, `GOODSENTRY_DEFAULT_NAME` and `GOODSENTRY_DEFAULT_ORGANIZATION` when needed. Open mode still requires same-origin and CSRF validation for mutations and is intended only for local use or a trusted network. Set `GOODSENTRY_AUTH=required` before exposing the service. For OAuth or MCP authorization through a password login, `GOODSENTRY_DEFAULT_PASSWORD` can give the implicit owner a password of 12 to 72 bytes at first creation.

Compose pulls `ghcr.io/jerolan/good-sentry:main` for amd64 or arm64 without a registry login. The runtime image contains the Go binary and embedded frontend, runs as `65532:65532` and needs one PostgreSQL container. PostgreSQL also coordinates the scheduler and delivery retries. Set `GOODSENTRY_IMAGE` in `.env` to select a `sha-<full-commit>` revision tag or pin an immutable image digest.

Keep `.env` between restarts so sessions retain the same secret. Set your public `GOODSENTRY_DOMAIN` and PostgreSQL credentials before exposing the installation. For the supplied Compose database URL, choose a URL-safe password such as random hexadecimal text. For HTTPS, terminate TLS at your reverse proxy and use an `https://` public domain. The [configuration reference](configuration.md) describes the available `.env` settings.

```sh
docker compose logs -f goodsentry
docker compose restart goodsentry
docker compose down
```

`down` preserves the named volumes. Adding `-v` deletes them. Migrations apply automatically at startup under a database lock and verify their checksums. Stop older backend replicas before starting a version with native issue grouping. Current-version replicas coordinate grouping through PostgreSQL locks.

To update the published images:

```sh
docker compose pull
docker compose up -d
```

Migrations run automatically after a database lock. Back up before upgrading and keep the previous image available while validating the new application:

```sh
mkdir -p backups
docker compose stop goodsentry
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
docker compose exec -T postgres pg_dump -U goodsentry -Fc goodsentry > "backups/goodsentry-$STAMP.dump"
APP_CONTAINER=$(docker compose ps -a -q goodsentry)
docker run --rm --volumes-from "$APP_CONTAINER":ro -v "$PWD/backups:/backups" busybox:1.36 \
  tar -C /data -czf "/backups/uploads-$STAMP.tar.gz" .
docker compose pull
docker compose up -d
curl -fsS http://localhost:8000/readyz
```

The PostgreSQL dump and uploads archive must come from the same stopped-app backup. The uploads volume contains local Parquet objects under `/data/archive` as well as attachments and source maps. Keep both backup files outside Docker. With the S3 backend, preserve the bucket objects separately and restore the same bucket, region, endpoint and prefix before starting GoodSentry.

To restore into a new empty Compose database and uploads volume, start PostgreSQL, create (but do not start) the application container to allocate its named volume, restore the files, then restore the database:

```sh
docker compose up -d postgres
docker compose create goodsentry
APP_CONTAINER=$(docker compose ps -a -q goodsentry)
docker run --rm --volumes-from "$APP_CONTAINER" -v "$PWD/backups:/backups:ro" busybox:1.36 \
  tar -C /data -xzf /backups/uploads-TIMESTAMP.tar.gz
docker compose exec -T postgres dropdb -U goodsentry --if-exists goodsentry
docker compose exec -T postgres createdb -U goodsentry goodsentry
docker compose exec -T postgres pg_restore -U goodsentry -d goodsentry --no-owner < backups/goodsentry-TIMESTAMP.dump
docker compose start goodsentry
```

Replace both `TIMESTAMP` values with the matching backup pair. This restores PostgreSQL metadata and the corresponding local Parquet objects; keep a second copy outside the host for disaster recovery.

For an explicit build from the checked-out source, use `GOODSENTRY_IMAGE=goodsentry:local docker compose up --build -d`. See the [development guide](development.md) for local checks.
