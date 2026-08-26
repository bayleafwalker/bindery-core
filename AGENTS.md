# Bindery Core Agent Guidance

> Shared environment guidance lives in `/projects/dev/AGENTS.md`.

Bindery Core is an experimental, pre-alpha Go and Kubernetes-native game
platform. Its `v1alpha1` CRDs, controller behavior, Helm resources, and gRPC
contracts can change; do not describe the project as production-ready.

## Two subsystems, one module

This repository contains two subsystems that were merged from unrelated git
histories on 2026-08-26 (`b162f22`). They share one Go module and one CI
workflow, and no runtime code path. Know which one you are changing — see
`docs/README.md` for the full split.

- **Kubernetes operator** — `api/v1alpha1/`, `controllers/`, `main.go`,
  `internal/{resolver,semver,graph}`, `modules/`, `k8s/crds/`,
  `helm/bindery-core/`, `e2e/`, `examples/booklet-bindery-sample/`.
- **External runtime** — `internal/{externalruntime,relay,harness,capture}`,
  `pkg/{evidencev1,gatev1,relayv1}`, `hack/redaction-corpus/`,
  `cmd/bindery-{external-runtime,udp-relay,redaction-scan}`,
  `contracts/externalruntime/`, `charts/bindery-external-runtime/`,
  `verification/`. It defines no CRDs and runs no controllers.

## Validation

Both targets run over the whole module, so either one will compile the other.

- Operator changes:
  ```bash
  make verify
  ```
  `verify` runs `fmt`, `tidy`, `tidy-sample-game`, `test`, `test-sample-game`,
  and `verify-crds`, then fails if any `go.mod`/`go.sum` moved. It rewrites
  files — review the resulting diff.

- External-runtime changes:
  ```bash
  make verify-external-runtime
  ```
  That is `test-race`, `vet`, and `helm lint charts/bindery-external-runtime` —
  the checks the external-runtime line ran in its own CI, kept as one target. It
  does **not** run `verify-crds` or the sample-game tests, so run `make verify`
  as well if you touched anything outside the external-runtime packages.

- `make verify-crds` (also run standalone, and on every CI run) asserts that
  every kind registered into the scheme has a manifest in `k8s/crds/`, and that
  `k8s/crds/` and `helm/bindery-core/crds/` are byte-identical. `main.go`
  registers a controller for every kind unconditionally, so a missing manifest
  makes the manager exit on a cache-sync timeout. Never satisfy this gate by
  running `make manifests` — see `docs/standards/kubernetes/crds.md`, which
  explains why the generator is not the source of truth.

- Use `make test-integration` (envtest) only when envtest setup is acceptable.
  Use `make test-e2e`, `make kind-demo`, `make kind-down`, and controller runs
  only with an explicitly verified local Kubernetes context. `make test-e2e`
  creates and destroys a Kind cluster and takes minutes.

- Do not apply Helm manifests, mutate a shared cluster, or treat sample game
  assets as a supported production deployment without separate authority.

## Conventions

- Keep API definitions, controllers, CRD manifests (both copies), Helm
  resources, and contract documentation aligned when changing a capability or
  lifecycle boundary.
- Read the relevant standards and contract documents before changing semantic
  versioning, capability resolution, storage binding, or RPC behavior.
- `docs/research/external-runtime-multiplayer/` is an immutable research pack.
  Link to it; do not rewrite it.
- The external runtime has been demonstrated end to end **once**, in a lab. Do
  not generalize that result, and do not backfill durable identifiers onto it —
  see `docs/assessments/2026-08-25-ra2-vertical-slice.md`.
- The capture plane is served and durable, and `pkg/gatev1` has a real caller
  in `internal/externalruntime/capture_gate.go`. Roadmap item ERH-006 is still
  **pending** regardless: what exists is the ability to repeat the RA2 run
  through durable identifiers, not a repetition of it. Do not describe ERH-006
  as closed, and do not describe test fixtures as run results.
- Raw observations are immutable and content-addressed. Never edit a persisted
  batch, never change `internal/capture/canon.go`'s encoding without accepting
  that every published hash moves, and never satisfy a completeness question by
  relaxing the gate — `canon_test.go` and `capture_gate_test.go` freeze both.
- No public DTO field may end in
  `authorization|bearer|token|credential|secret|password|url|ip|port|endpoint`.
  `internal/externalruntime/redaction.go` is the release-blocking oracle and
  `make redaction` runs it over the real DTO shapes.
