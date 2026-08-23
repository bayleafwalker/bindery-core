# Bindery Core

This repository contains the generic Bindery contracts and the external-runtime
research reference service. The research pack under
[`docs/research/external-runtime-multiplayer`](docs/research/external-runtime-multiplayer)
is immutable input. Runtime code imports only the promoted contracts under
[`contracts/externalruntime/v1`](contracts/externalruntime/v1).

The first implementation slice is deliberately in-memory and synthetic. It
proves the public/authenticated DTO split, token-backed identity, session and
enrollment lifecycle, idempotent mutations, and public redaction before
PostgreSQL, object storage, Windows clients, or live UDP are introduced.

## Local verification

```sh
go test ./...
go vet ./...
go run ./cmd/bindery-external-runtime
```

`GET /v1/sessions` is intentionally not implemented. Known session IDs are
public; discovery is not.

