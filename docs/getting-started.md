# Getting started

[Documentation](README.md) · [Project home](../README.md)

Build and run GoodSentry with one application container and PostgreSQL.

## Quickstart

You need Docker with Compose and OpenSSL. On Windows, enable Docker Desktop's integration with your WSL2 distribution.

```sh
git clone https://github.com/jerolan/good-sentry.git
cd good-sentry
test -f .env || printf 'SECRET_KEY=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up --build -d
```

Open **http://localhost:8000**. Create the first account and organization, create a project and copy its DSN into your SDK configuration. There is no shared initial password.

The first start builds the application image from this repository. No registry image is required. The runtime image contains the Go binary and embedded frontend, runs as `65532:65532` and needs one PostgreSQL container. PostgreSQL also coordinates the scheduler and delivery retries.

Keep `.env` between restarts so sessions retain the same secret. Set your public `GOODSENTRY_DOMAIN` and PostgreSQL credentials before exposing the installation. For the supplied Compose database URL, choose a URL-safe password such as random hexadecimal text. For HTTPS, terminate TLS at your reverse proxy and use an `https://` public domain. The [configuration reference](configuration.md) describes the available `.env` settings.

```sh
docker compose logs -f goodsentry
docker compose restart goodsentry
docker compose down
```

`down` preserves the named volumes. Adding `-v` deletes them. Migrations apply automatically at startup under a database lock and verify their checksums. Stop older backend replicas before starting a version with native issue grouping. Current-version replicas coordinate grouping through PostgreSQL locks.
