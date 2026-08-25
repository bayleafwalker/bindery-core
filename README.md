# Bindery Core

This repository contains the generic Bindery contracts and the external-runtime
research reference service. The research pack under
[`docs/research/external-runtime-multiplayer`](docs/research/external-runtime-multiplayer)
is immutable input. Runtime code imports only the promoted contracts under
[`contracts/externalruntime/v1`](contracts/externalruntime/v1).

The RA2 external-runtime path has now been demonstrated end to end once. The
reference control plane persists identity, session, placement, execution,
enrollment, idempotency, and reconciled evidence records through a crash-safe
single-writer state store. The current file-backed mode is intentionally not a
multi-replica database.

The evidence and gate boundary is described in
[`docs/architecture/evidence-and-gates.md`](docs/architecture/evidence-and-gates.md).
The dated RA2 result and its limits are recorded in
[`docs/assessments/2026-08-25-ra2-vertical-slice.md`](docs/assessments/2026-08-25-ra2-vertical-slice.md).

## Local verification

```sh
go test ./...
go vet ./...
BINDERY_RELAY_ENDPOINT=127.0.0.1:50001 \
BINDERY_BUILD_REVISION="$(git rev-parse HEAD)" \
BINDERY_STATE_PATH=/tmp/bindery-control-state.json \
go run ./cmd/bindery-external-runtime
```

The `/tmp` path above is disposable local development state. Deployments mount
a persistent volume and run exactly one replica. Multiple replicas require a
shared relational store; changing the count alone is rejected by the chart.

`GET /v1/sessions` is intentionally not implemented. Known session IDs are
public; discovery is not. The same known-ID rule applies to placements,
executions, enrollments, and evidence sets.
