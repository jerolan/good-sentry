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

Open **http://localhost:8000**. Create the first account and organization, create a project and copy its DSN into your SDK configuration. There is no shared initial password.

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

For an explicit build from the checked-out source, use `GOODSENTRY_IMAGE=goodsentry:local docker compose up --build -d`. See the [development guide](development.md) for local checks.
