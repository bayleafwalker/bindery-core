# anvil

Baseline docs live under `docs/`.

## Standards

- Standards index: `docs/standards/index.md`
- Capability model: `docs/standards/capability-model.md`
- `ModuleManifest` standard: `docs/standards/modulemanifest.md`

## Kubernetes CRDs

- CRD YAMLs: `k8s/crds/`
- CRD documentation: `docs/standards/kubernetes/crds.md`
- CapabilityResolver controller design: `docs/standards/kubernetes/capabilityresolver.md`

## RPC Contracts

- Engine gRPC Protobuf API (v1): `proto/game/engine/v1/engine.proto`
- Contract documentation + regeneration notes: `docs/standards/rpc/engine-grpc-v1.md`

## Example Modules

- Physics engine module template: `examples/modules/physics-engine-template/`

## Testing

- CapabilityResolver test plan: `docs/testing/capability-resolver-test-plan.md`

### Developer workflow (TDD)

Most changes should be developed tests-first:

- Unit tests (fast): `go test ./...`
- Integration tests (envtest, real apiserver semantics): `make test-integration` (or `ANVIL_INTEGRATION=1 go test ./... -run Integration` with `KUBEBUILDER_ASSETS` configured)

Use Kind for end-to-end smoke validation (controllers + real cluster):
- up + apply: `./k8s/dev/kind-demo.sh`
- down: `./k8s/dev/kind-down.sh`

## Agent Entrypoints

- Agent prompt entrypoints (including strict mode): `docs/agent/entrypoints.md`

## Repo layout

Scaffold folders (new):
- `contracts/` — interface contracts (proto, capability contracts)
- `services/` — platform services (entrypoints typically live in `cmd/`)
- `controllers/` — Kubernetes controllers/reconcilers
- `deploy/` — deployment artifacts (manifests/overlays/scripts)
- `docs/` — canonical documentation
- `samples/` — end-to-end sample configs (also see `k8s/examples/`)

Canonical existing locations:
- Protobuf IDL + generated stubs: `proto/`
- CRDs + examples + Kind scripts: `k8s/`
- Go entrypoints: `cmd/`
- Resolver logic: `internal/resolver/` + `internal/semver/`

## Local demo (Kind)

If you have Docker and want a quick local cluster for validating the CRDs/examples:

- Bring up cluster + apply CRDs/examples: `./k8s/dev/kind-demo.sh`
- Tear down: `./k8s/dev/kind-down.sh`
