# Booklet Repo: `bindery-sample`

This folder is a **sample game/booklet repository** for Bindery Core.

It is intentionally small, but exercises:
- a `Booklet` + `WorldInstance`
- sharded `ModuleManifest`s (`world-shard`)
- dependency injection via `CapabilityBinding` (web → physics)

## Contents

- `k8s/`: Kubernetes manifests for the sample game (capability definitions, module manifests, booklet, world instance).
- `cmd/`: Demo module implementations (physics sim + web client).
- `dev/`: Local helper scripts (build/load images, apply manifests, kind demo).

## Quickstart (monorepo)

From the **repo root**:

1) Create a kind cluster:
- `./k8s/dev/kind-up.sh`

2) Install CRDs:
- `kubectl apply -f k8s/crds/`

3) Run the Bindery controllers (in another terminal):
- `make run-controller`

4) Build/load the demo module images, then apply the sample resources:
- `./examples/booklet-bindery-sample/dev/kind-demo.sh`

5) Port-forward the web client and open it in a browser:
- `bash ./examples/booklet-bindery-sample/dev/port-forward-web.sh`
- Visit `http://localhost:8080`

## Notes for splitting into a standalone repo

This folder is structured so it can be extracted into its own repository later.
The only coupling is that the demo modules import the engine RPC contract from `bindery-core` (by Go module).

When extracting, remove the local `replace` directive in `go.mod` and pin `github.com/bayleafwalker/bindery-core` to a real version/tag.
