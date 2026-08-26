# Capability Resolver — Test Plan

This document defines a pragmatic, stability-first test plan for the **CapabilityResolver** system.

Scope: declarative inputs (`ModuleManifest`, `Booklet`, `WorldInstance`) and resolver/controller outputs (`CapabilityBinding`), including schema compatibility and operational behaviors.

## Goals

- Catch correctness regressions early (resolution semantics and deterministic selection).
- Validate Kubernetes integration (CRDs apply, reconciliation converges, idempotency).
- Prove resilience under failure modes (API conflicts, restarts, partial inputs).
- Maintain forward compatibility (CRD + proto schema evolution does not break upgrades).

## Non-goals (for now)

- Validating runtime workload materialization / endpoint publication (owned by RuntimeOrchestrator).
- Performance tuning of a production-scale control plane.
- Game-specific physics/interaction correctness.
- The external runtime, the other subsystem in this repository. It has no CRDs
  and no controllers; its checks are `make verify-external-runtime` (race tests,
  `go vet`, chart lint) and the `external-runtime` CI job. See
  [`../README.md`](../README.md).

## System under test

### Components

- **Resolver library**: internal planning logic that computes desired bindings.
  - Go implementation: `internal/resolver`
  - SemVer implementation: `internal/semver`

- **Controller**: watches resources and applies the resolver plan. It creates,
  updates, and garbage-collects `CapabilityBinding` objects today; it is no
  longer wiring only.
  - Go implementation: `controllers/capabilityresolver_controller.go`
    (`CapabilityResolverReconciler`)

- **Schemas/CRDs**:
  - CRDs: `k8s/crds/*.bindery.platform.yaml`
  - Example CRs (sample game): `examples/booklet-bindery-sample/k8s/`

### Key invariants

- **Determinism**: same inputs → same plan and same binding selection.
- **Idempotency**: repeated reconcile does not cause churn.
- **Safety**: invalid/unsatisfied required deps are surfaced without writing broken objects.
- **Compatibility**: schema changes are additive and preserve existing resources.

## Test environments

What exists today:

- **Unit**: `make test` (`go test ./...`), fast feedback.
- **Integration**: `make test-integration` — envtest (a real apiserver + etcd,
  no Kind), gated on `BINDERY_INTEGRATION=1` and `-run Integration`.
- **E2E**: `make test-e2e` — a Kind cluster, gated on `BINDERY_E2E=1`. Covers
  the sample game through resolution, runtime workloads, and the sharding path
  (`e2e/smoke_test.go`).
- **CI Verification**: GitHub Actions workflow (`.github/workflows/ci.yml`),
  jobs `go-test`, `sample-game-test`, `e2e-smoke`, `external-runtime`.
  - Check status: `gh run list --workflow ci.yml`
  - Watch progress: `gh run watch`

Aspirational, not implemented:

- **Chaos**: Kind + failure injection (delete pods, drop network via tools if available)
- **Load**: `cmd/bindery-load-test` exists but is not run by CI.

## Unit tests

### 1) SemVer parsing and matching

Location: `internal/semver`

Coverage targets:

- Valid constraints: `^1.2.0`, `~1.4`, `>=1.2.0 <2.0.0`, `=1.0.0`, `*`
- Boundary cases:
  - [x] `^0.x` behavior (pre-1.0 compatibility rules)
  - [x] Pre-release handling (`1.2.0-alpha.1`) if/when used in manifests
- Invalid inputs:
  - [x] constraint parse failures (must be rejected and surfaced)
  - [x] version parse failures (providers with invalid versions should not be selected)

Assertions:

- `Satisfies(version, constraint)` correctness.
- `MaxSatisfying` chooses the highest satisfying version.

### 2) Resolver plan generation (pure logic)

Location: `internal/resolver`

Coverage targets:

- Requirement classification:
  - Required vs optional unresolved entries populate the correct diagnostics fields.
- Matching rules:
  - capability ID exact match
  - scope match
  - multiplicity compatibility
  - SemVer constraint satisfaction
- Deterministic provider selection:
  - Highest satisfying version wins
  - Tie-break by provider module name
- Plan stability:
  - Output bindings are sorted deterministically (stable ordering)

Concrete scenarios (unit-level):

1) **No provider found** (Implemented)
- Consumer requires `capabilityId=X`, no module provides `X`.
- Expect: no binding; unresolved required/optional recorded based on `dependencyMode`.

2) **Version incompatible** (Implemented)
- Provider offers `X@1.0.0`, consumer requires `>=2.0.0`.
- Expect: unresolved requirement recorded.

3) **Multiple matching providers**
- Providers offer `X@1.0.0` and `X@1.5.0`, consumer requires `>=1.0.0 <2.0.0`.
- Expect: select `1.5.0`.

4) **Scope mismatch**
- Provider offers `X` at `cluster`, consumer requires `X` at `world`.
- Expect: no binding; unresolved recorded.

5) **Rolling upgrade mixed versions**
- Providers: `X@1.2.0` and `X@1.3.0`, consumer requires `^1.2.0`.
- Expect: select `1.3.0`.

### 3) Controller unit tests (when business logic lands)

Once the controller uses `resolver.Input` built from cluster reads and writes `CapabilityBinding`:

- Fake client tests (`controller-runtime/pkg/client/fake`):
  - Given objects in the fake client, reconcile produces expected create/update operations.
  - Verify owner refs / labels / stable naming (if/when implemented).
- Reconcile edge cases:
  - NotFound WorldInstance should be ignored.
  - Missing Booklet should set status/event (if status logic is implemented).
  - Conflicts should requeue.

## Integration tests (Kind)

### 1) CRD installation and schema validation

Goal: ensure CRDs are valid and apply cleanly.

Steps:

- Create Kind cluster (see existing scripts under `k8s/dev/`).
- Apply CRDs: `kubectl apply -f k8s/crds/`
- Apply sample game: `kubectl apply -f examples/booklet-bindery-sample/k8s/`

Assertions:

- `kubectl apply` succeeds with no schema errors.
- Example resources create successfully.

### 2) Resolver/controller end-to-end (when controller writes bindings)

Prereq: controller runs in-cluster and performs binding reconciliation.

Test cases (concrete):

1) **No provider found**
- Create Booklet that includes a consumer module requiring `time.source`.
- Omit any provider.
- Expect:
  - No `CapabilityBinding` created for that requirement.
  - World status indicates unresolved required deps (once status is implemented).

2) **Version incompatible**
- Provider advertises `time.source@1.0.0`.
- Consumer requires `>=2.0.0`.
- Expect unresolved required.

3) **Multiple matching providers**
- Two providers advertise `time.source@1.2.0` and `time.source@1.3.0`.
- Consumer requires `^1.2.0`.
- Expect binding to `1.3.0`.

4) **Scope mismatch**
- Provider: `messaging.bus` at `cluster`.
- Consumer: requires `messaging.bus` at `world`.
- Expect unresolved.

5) **Rolling upgrade with mixed versions**
- Apply provider module manifest v1.2.0.
- Then apply provider module manifest v1.3.0 (same capabilityId, same scope).
- Expect:
  - Binding converges to v1.3.0.
  - No churn if the chosen provider remains stable.

Assertions to record:

- Number of bindings equals number of satisfiable requirements.
- Bindings reference the right consumer/provider names and versions.
- Re-applying the same inputs produces no net changes (`kubectl diff` is empty or stable).

## Chaos / failure injection

Goal: verify eventual consistency and safe behavior under partial failure.

### Failure modes

1) **Controller restart during reconcile**
- Kill the controller pod mid-update.
- Expect: on restart, controller converges to correct bindings with no duplicates.

2) **API conflicts / resourceVersion races**
- Force concurrent updates to `CapabilityBinding` (e.g., two controllers or manual patch).
- Expect: controller retries and converges.

3) **Transient API outages**
- Simulate apiserver unavailability (in Kind: restart control plane container if feasible).
- Expect: controller logs/requeues, no partial corrupted resources.

4) **Partial inputs**
- Delete a provider `ModuleManifest` while worlds are running.
- Expect: affected bindings removed/updated; world status shows unresolved required.

5) **Event bus unavailable (data plane)**
- For module templates using NATS: run module with invalid `NATS_URL`.
- Expect: module still serves gRPC; publishing becomes a no-op or surfaces errors clearly (implementation-defined).

## Load tests

Goal: validate control-plane scaling characteristics and ensure deterministic behavior at scale.

Workload model:

- N worlds (e.g., 100–1,000)
- M modules per game (e.g., 10–50)
- Each module requires R capabilities (e.g., 2–10)

Scenarios:

1) **Steady state**
- Pre-create all manifests/games/worlds.
- Measure reconcile time distribution and binding count.

2) **Churn**
- Repeatedly update provider versions (simulate rolling upgrades).
- Measure binding churn and reconcile stability.

3) **Burst create**
- Create 500 WorldInstances quickly.
- Validate cluster remains responsive; controller catches up.

Metrics/observables (once implemented):

- Reconcile duration histogram
- Queue depth / rate
- Bindings created/updated per reconcile
- Error rate

## Schema compatibility tests

Goal: prevent breaking schema changes.

### CRD schema (Kubernetes)

- Additive-only changes for `spec` (new optional fields).
- Never change meaning/type of existing fields.
- Avoid making optional fields required.

Validation workflow:

- Apply previous version CRDs + sample resources.
- Upgrade CRDs to new version.
- Ensure existing resources still validate and remain readable.

### Protobuf schema (gRPC)

For shared engine proto contracts:

- Only add new fields with new numbers.
- Never reuse field numbers; reserve removed fields.
- Prefer `oneof` for extensibility.

Validation workflow:

- Compile protos (Go codegen)
- Optional: wire-compat tests (old client ↔ new server) when multiple versions exist.

## Acceptance criteria

- Unit tests cover all concrete scenarios listed above.
- Integration suite can be run on Kind and is repeatable.
- Failure injection tests demonstrate convergence without resource corruption.
- Schema evolution checklist is followed for both CRDs and Protobuf.

## Follow-ups

- ~~Add a dedicated `e2e/` harness that provisions Kind, installs CRDs, deploys
  the controller, and runs assertions.~~ Done: `e2e/smoke_test.go`, run via
  `make test-e2e` and by the `e2e-smoke` CI job.
- Add golden-file tests for resolver plan outputs (stable YAML/JSON snapshots).
- Add property-based tests for semver/provider selection determinism.
