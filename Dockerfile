FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goodsentry ./cmd/goodsentry

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/goodsentry /goodsentry
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/goodsentry"]
CMD ["serve"]
