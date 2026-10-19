FROM scratch AS tailwind-amd64
ADD --checksum=sha256:dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-x64 /tailwindcss

FROM scratch AS tailwind-arm64
ADD --checksum=sha256:55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195 https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-arm64 /tailwindcss

FROM tailwind-${BUILDARCH} AS tailwind

FROM --platform=$BUILDPLATFORM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY --from=tailwind /tailwindcss /usr/local/bin/tailwindcss
RUN chmod 0755 /usr/local/bin/tailwindcss
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN printf '%s\n' \
      'd69de07c7463bb1f41548e2688f88ee105cdadf9779fce5b3c47b3bfaa6be472  third_party/daisyui/daisyui.mjs' \
      'e48a35d8326d650181ca600560e8520cf6b44cf9242bbd9d45825607e641f251  third_party/daisyui/daisyui-theme.mjs' \
    | sha256sum -c - \
    && tailwindcss -i internal/server/web/styles.css -o internal/server/web/app.css --minify \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /goodsentry ./cmd/goodsentry \
    && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /goodsentry /goodsentry
COPY --from=build --chown=65532:65532 /data /data
COPY third_party/daisyui/LICENSE /licenses/daisyui-LICENSE
ENV PORT=8000 UPLOAD_DIR=/data
EXPOSE 8000
USER 65532:65532
ENTRYPOINT ["/goodsentry"]
