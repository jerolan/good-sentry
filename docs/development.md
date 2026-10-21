# Development

[Documentation](README.md) · [Project home](../README.md)

Build, test and preview the project locally.

## Development

Use Go 1.27.2. The server uses `go:embed`; Node is not part of its runtime. Tailwind CSS 4.3.3 and daisyUI 5.7.50 compile `internal/server/web/styles.css` into `app.css`. The dashboard uses original styling inspired by the [Nexus dashboard examples](https://nexus.daisyui.com/). Docker verifies the official CLI and vendored plugin checksums. The generated CSS is committed so local Go builds do not require frontend tools. The daisyUI dependency and its MIT notice live in [third_party/daisyui](../third_party/daisyui); the runtime image includes that notice.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/goodsentry ./cmd/goodsentry
```

For integration tests, set `DATABASE_URL` to a disposable PostgreSQL instance. Tests create isolated schemas. Race detection requires a C compiler.

```sh
go test -race -tags=integration ./...
```

Pinned external SDK clients and browser workflows live in [tests/compat](../tests/compat). They are test tooling, separate from the server module and image. The CI workflow runs formatting, vet, the integration/race suite and the actual Docker build.

The [container workflow](../.github/workflows/image.yml) publishes amd64 and arm64 images to GHCR after matching `main` CI succeeds. It also verifies release tags and manual runs before publication. Images include provenance and an SBOM; `main`, `latest` and `sha-<full-commit>` tags identify the published builds. To build and run a local image instead:

```sh
GOODSENTRY_IMAGE=goodsentry:local docker compose up --build -d
```

The application-memory baseline starts the public image with a disposable PostgreSQL database and runs the official Go 0.50.0, Python 2.72.0, Node 11.7.0 and Chromium Browser 11.7.0 error/transaction/typed-log tests concurrently. The kernel reports the application process's VmRSS and VmHWM; PostgreSQL memory is excluded. On the `sha-8160b3` image, the maximum observed application RSS was **24.4 MiB**. This is a transport-load baseline, not a retention-capacity or hostile-traffic benchmark.

The product landing lives in [site](../site). [GitHub Pages Actions](../.github/workflows/pages.yml) publishes that directory from `main` or a manual dispatch. Preview it locally with:

```sh
python3 -m http.server 8001 --directory site
```

## Independent implementation

GoodSentry is developed from public protocol specifications, official documentation and black-box tests with official SDKs and the CLI. Design decisions, source references and verification limits are recorded in [SPEC.md](../SPEC.md).
