FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
ADD --checksum=sha256:dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-x64 /usr/local/bin/tailwindcss
RUN chmod 0755 /usr/local/bin/tailwindcss
COPY . .
RUN tailwindcss -i internal/server/web/styles.css -o internal/server/web/app.css --minify \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /goodsentry ./cmd/goodsentry \
    && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /goodsentry /goodsentry
COPY --from=build --chown=65532:65532 /data /data
ENV PORT=8000 UPLOAD_DIR=/data
EXPOSE 8000
USER 65532:65532
ENTRYPOINT ["/goodsentry"]
