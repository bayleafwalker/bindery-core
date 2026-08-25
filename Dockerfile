FROM golang:1.23-bookworm AS build
ARG SOURCE_COMMIT=unknown
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.buildRevision=${SOURCE_COMMIT}" -o /out/bindery-external-runtime ./cmd/bindery-external-runtime \
 && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/bindery-udp-relay ./cmd/bindery-udp-relay

FROM gcr.io/distroless/static-debian12:nonroot
ARG SOURCE_COMMIT=unknown
LABEL org.opencontainers.image.title="Bindery external-runtime" \
      org.opencontainers.image.source="https://github.com/bayleafwalker/bindery-core" \
      org.opencontainers.image.revision="$SOURCE_COMMIT" \
      org.opencontainers.image.licenses="NOASSERTION"
COPY --from=build /out/bindery-external-runtime /app/bindery-external-runtime
COPY --from=build /out/bindery-udp-relay /app/bindery-udp-relay
USER nonroot:nonroot
ENTRYPOINT ["/app/bindery-external-runtime"]
