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

## Debugging & logs

### Controller logs (local)

The controller manager runs locally via controller-runtime; logs go to stdout:

- Run against your current kubecontext: `go run .`
- More verbose logs: `go run . --zap-log-level=debug`
- Production-style JSON logs: `go run . --zap-encoder=json --zap-devel=false`

Useful flags (see `go run . -h`): `--zap-log-level`, `--zap-encoder`, `--zap-time-encoding`.

### Inspecting resources (Kind or any cluster)

- Worlds: `kubectl get worldinstances -A`
- Bindings: `kubectl get capabilitybindings -A`
- Binding details (incl. runtime endpoint): `kubectl get capabilitybinding -n anvil-demo <name> -o yaml`
- Runtime workloads created by RuntimeOrchestrator: `kubectl get deploy,svc -n anvil-demo`

### Integration test debugging (envtest)

Integration tests are gated to avoid requiring envtest binaries during normal unit runs:

- Run integration tests: `make test-integration`
- Verbose single-package run: `ANVIL_INTEGRATION=1 go test -v ./controllers -run Integration`
