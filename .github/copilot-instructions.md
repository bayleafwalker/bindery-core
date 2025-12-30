# Copilot instructions (anvil)

## What this repo is
- Kubernetes-native, capability-driven game platform.
- Canonical specs live in `docs/standards/`.
- CRDs live in `k8s/crds/` (examples in `k8s/examples/`).
- The controller manager entrypoint is `main.go` (controller-runtime).

## Read this first (in order)
1. `README.md`
2. `docs/standards/index.md`
3. `docs/standards/kubernetes/capabilityresolver.md`
4. `internal/resolver/` and `internal/semver/`

## Hard rules / invariants
- Capability IDs are immutable; evolve behavior via SemVer (see `docs/standards/capabilities/README.md`).
- Don’t invent new CRD fields unless you update the CRD schema + docs + examples together.
- Keep resolution deterministic and stable (same inputs → same bindings order).
- Prefer small, targeted changes; don’t refactor unrelated packages.

## Common workflows
- Unit tests: `go test ./...`
- Integration tests (envtest): `make test-integration` (or `ANVIL_INTEGRATION=1 go test ./... -run Integration`)
- Local CRD/example validation on Kind:
  - up + apply: `./k8s/dev/kind-demo.sh`
  - down: `./k8s/dev/kind-down.sh`
- Run controller manager locally (uses current kubeconfig context): `go run .`

## Test-driven development (TDD)
- Default to tests-first for new behavior: write or update a failing test, then implement until green.
- Any non-trivial change should include:
  - Unit tests close to the logic (pure functions, resolver, helpers).
  - An integration test when behavior depends on Kubernetes API semantics (status subresource, ownership, watches/indexes, reconciliation loops).
- Keep tests deterministic:
  - No time.Sleep-based assertions; use polling with timeouts.
  - No reliance on non-deterministic ordering; sort before compare.
- Prefer envtest for controller integration tests:
  - Covers real apiserver behavior without requiring Kind.
  - Use Kind only for smoke/real-cluster validation.

## Protobuf / gRPC
- Source of truth: `proto/game/engine/v1/engine.proto`
- Generated Go code is checked in under `proto/game/engine/v1/`.
- If you change the proto, regenerate stubs as documented in `docs/standards/rpc/engine-grpc-v1.md`.

## When editing the resolver
- Resolver logic: `internal/resolver/default_resolver.go`.
- SemVer parsing/matching helpers: `internal/semver/`.
- Add/adjust unit tests alongside changes (tests first where practical).
- Preserve deterministic provider selection + deterministic binding sort.

## When editing CRDs
- Validate schemas (OpenAPI v3) and keep examples in sync.
- Don’t use schema patterns that break CRD validation (prefer standard `properties`/`required` constructs).
- Add an envtest integration test when changing subresources (`status`), ownership boundaries (`spec` vs `status`), or reconcile side effects.

## If requirements are ambiguous
- Default to the simplest interpretation that matches existing standards docs.
- Ask 1–3 clarifying questions before adding new concepts/fields.
