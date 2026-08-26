# The Bindery Core operator (controller manager).
#
# The external runtime has its own image; see Dockerfile.external-runtime. The
# two share a repository and a Go module but are separate artifacts, and
# conflating them would let a bindery-core install deploy the wrong service.
FROM golang:1.23-bookworm AS build
ARG SOURCE_COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/bindery-core .

FROM gcr.io/distroless/static-debian12:nonroot
ARG SOURCE_COMMIT=unknown
LABEL org.opencontainers.image.title="Bindery Core operator" \
      org.opencontainers.image.source="https://github.com/bayleafwalker/bindery-core" \
      org.opencontainers.image.revision="$SOURCE_COMMIT" \
      org.opencontainers.image.licenses="NOASSERTION"
COPY --from=build /out/bindery-core /app/bindery-core
USER nonroot:nonroot
ENTRYPOINT ["/app/bindery-core"]
