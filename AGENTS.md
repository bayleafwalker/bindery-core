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
  `helm/bindery-core/`, `e2e/`, `examples/booklet-bindery-sample/`. Canonical
  specs live in `docs/standards/`. Example resources are in
  `examples/booklet-bindery-sample/k8s/`; there is no `k8s/examples/`.
- **External runtime** — `internal/{externalruntime,relay,harness,capture}`,
  `pkg/{evidencev1,gatev1,relayv1}`, `hack/redaction-corpus/`,
  `cmd/bindery-{external-runtime,udp-relay,redaction-scan}`,
  `contracts/externalruntime/`, `charts/bindery-external-runtime/`,
  `verification/`. An HTTP and UDP control plane for matches simulated in game
  clients Bindery does not own. It defines no CRDs and runs no controllers.

## Orientation

Read these in order before changing platform behavior:

1. `README.md`
2. `docs/standards/index.md`
3. `docs/standards/kubernetes/capabilityresolver.md`
4. `internal/resolver/` and `internal/semver/`

## Invariants

- Capability IDs are immutable. Evolve behavior through SemVer instead — see
  `docs/standards/capabilities/README.md`.
- Do not invent CRD fields. A new field means the CRD schema, the docs, and the
  examples move together, in one change.
- Capability resolution is deterministic: the same inputs produce the same
  bindings in the same order. Preserve both the provider selection and the
  binding sort.
- Prefer small, targeted changes. Do not refactor unrelated packages.

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

- Plain unit tests are `go test ./...`. `make test-integration` runs envtest
  (equivalently `BINDERY_INTEGRATION=1 go test ./... -run Integration`); use it
  only when envtest setup is acceptable. Use `make test-e2e`, `make kind-demo`,
  `make kind-down`, `./k8s/dev/kind-demo.sh`, `./k8s/dev/kind-down.sh`, and
  controller runs (`go run .`, which uses the current kubeconfig context) only
  with an explicitly verified local Kubernetes context. `make test-e2e` creates
  and destroys a Kind cluster and takes minutes.

- Do not apply Helm manifests, mutate a shared cluster, or treat sample game
  assets as a supported production deployment without separate authority.

## Working method

Default to tests first: write or update a failing test, then implement until it
passes. A non-trivial change carries unit tests close to the logic (pure
functions, the resolver, helpers), plus an integration test whenever the
behavior depends on Kubernetes API semantics — the status subresource,
ownership, watches and indexes, or the reconcile loop itself. Prefer envtest for
controller integration tests, since it exercises real apiserver behavior without
a cluster; keep Kind for smoke and real-cluster validation.

Keep tests deterministic. No `time.Sleep`-based assertions — poll with a
timeout. No reliance on map or slice ordering — sort before comparing.

Through a task: plan the change and name the affected files; write the tests and
implement; update `docs/` in the same change, removing entries the change makes
obsolete rather than leaving them to rot; then verify with the targets above and
confirm CI is green with `gh run list`. **Do not use `gh run view`** — it
destabilizes this environment. Commit with conventional messages (`feat:`,
`fix:`) and make sure remote CI passes before calling the task done. If a task
exposes a gap in this guidance, update this file.

## Debugging and logs

Use structured logs (controller-runtime zap) with stable field names, so an
issue stays searchable. When you change reconcile behavior, log enough to
reconstruct the decision: `namespace`, `world`, `binding`, `consumerModule`,
`providerModule`, `capabilityId`, and the counts and choices behind it —
`candidateCount`, `chosenProvider`, `chosenVersion`. Run locally with
`go run . --zap-log-level=debug` for verbose output. Any proposed fix comes with
a minimal repro: a unit test, an integration test, or a `kubectl` inspection
sequence.

## Protobuf and gRPC

`contracts/proto/game/engine/v1/engine.proto` is the source of truth; the
generated Go code next to it is checked in. Regenerate with `make proto`
(needs `protoc` and the Go plugins), following
`docs/standards/rpc/engine-grpc-v1.md`.

## Resolver

Resolution lives in `internal/resolver/default_resolver.go`, with SemVer parsing
and matching in `internal/semver/`. Tests move with the change, first where
practical.

## CRDs

Beyond the `verify-crds` gate described under Validation: schemas are OpenAPI
v3, so keep them valid and keep the examples in step. Prefer standard
`properties` and `required` constructs over schema patterns that break CRD
validation. Changing a subresource (`status`), an ownership boundary (`spec`
versus `status`), or a reconcile side effect calls for an envtest integration
test.

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
- When requirements are ambiguous, take the simplest reading consistent with
  the standards docs. Ask before adding a new concept or field.
