# Copilot instructions (bindery)

## What this repo is
Two subsystems sharing one Go module and one CI workflow, with no shared runtime
code path (merged from unrelated histories on 2026-08-26, `b162f22`). See
`docs/README.md`.

1. **Kubernetes operator** — capability-driven game platform.
   - Canonical specs live in `docs/standards/`.
   - CRDs live in `k8s/crds/`, mirrored in `helm/bindery-core/crds/`. Example
     resources are in `examples/booklet-bindery-sample/k8s/` (there is no
     `k8s/examples/`).
   - The controller manager entrypoint is `main.go` (controller-runtime).
2. **External runtime** — an HTTP + UDP control plane for matches simulated in
   game clients Bindery does not own. `internal/externalruntime`,
   `internal/relay`, `internal/harness`, `internal/capture`, `pkg/*`,
   `cmd/bindery-{external-runtime,udp-relay,redaction-scan}`,
   `contracts/externalruntime/v1`, `charts/bindery-external-runtime`. It defines
   no CRDs and runs no controllers. Its check is
   `make verify-external-runtime`.

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
- Integration tests (envtest): `make test-integration` (or `BINDERY_INTEGRATION=1 go test ./... -run Integration`)
- Local CRD/example validation on Kind:
  - up + apply: `./k8s/dev/kind-demo.sh`
  - down: `./k8s/dev/kind-down.sh`
- Run controller manager locally (uses current kubeconfig context): `go run .`

## Debugging & logs
- Prefer structured logs (controller-runtime zap) with stable fields so issues are searchable.
- When changing reconcile behavior, include enough context in logs to understand the decision:
  - `namespace`, `world`, `binding`, `consumerModule`, `providerModule`, `capabilityId`
  - counts/choices: `candidateCount`, `chosenProvider`, `chosenVersion`
- For local debugging, run with verbose logs: `go run . --zap-log-level=debug`
- When proposing a fix, include a minimal repro command sequence (unit test, integration test, or `kubectl` inspection).

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

## Full Development Lifecycle
When executing a task, follow this lifecycle to ensure quality and consistency:

1. **Plan**: Analyze the request, identify affected files, and outline the changes.
2. **Test & Implement**:
   - Create or update tests first (TDD).
   - Implement the changes to pass the tests.
3. **Document**:
   - Update relevant documentation in `docs/` to reflect new features or behavior.
   - Refactor existing docs and remove obsolete entries to keep information clean.
4. **Verify**:
   - Run `make verify` to execute `fmt`, `tidy`, `tidy-sample-game`, `test`,
     `test-sample-game`, and `verify-crds` locally.
   - For external-runtime changes run `make verify-external-runtime`
     (race tests, `go vet`, chart lint) as well.
   - This ensures code style and dependencies are correct before pushing, preventing CI failures.
   - Run integration tests: `make test-integration`
   - Ensure CI pipelines pass (check via `gh run list`).
   - **Do not use `gh run view`** as it may cause stability issues in the environment.
5. **Refine Guidelines**: If the task reveals a gap in these instructions, update `.github/copilot-instructions.md`.
6. **Commit & Push**:
   - Commit with clear, conventional messages (e.g., `feat: ...`, `fix: ...`).
   - Push to the feature branch.
   - Ensure CI passes on the remote before considering the task done.

## Protobuf / gRPC
- Source of truth: `contracts/proto/game/engine/v1/engine.proto`
- Generated Go code is checked in under `contracts/proto/game/engine/v1/`.
- Regenerate with `make proto` (requires `protoc` plus the Go plugins).
- If you change the proto, regenerate stubs as documented in `docs/standards/rpc/engine-grpc-v1.md`.

## When editing the resolver
- Resolver logic: `internal/resolver/default_resolver.go`.
- SemVer parsing/matching helpers: `internal/semver/`.
- Add/adjust unit tests alongside changes (tests first where practical).
- Preserve deterministic provider selection + deterministic binding sort.

## When editing CRDs
- Edit both `k8s/crds/` and `helm/bindery-core/crds/`; `make verify-crds` fails
  if they differ, or if a scheme-registered kind has no manifest.
- Do **not** run `make manifests` to satisfy that gate. Most manifests are
  hand-written and carry more validation than the Go markers generate;
  regenerating drops it. See `docs/standards/kubernetes/crds.md`.
- Validate schemas (OpenAPI v3) and keep examples in sync.
- Don’t use schema patterns that break CRD validation (prefer standard `properties`/`required` constructs).
- Add an envtest integration test when changing subresources (`status`), ownership boundaries (`spec` vs `status`), or reconcile side effects.

## If requirements are ambiguous
- Default to the simplest interpretation that matches existing standards docs.
- Ask 1–3 clarifying questions before adding new concepts/fields.
