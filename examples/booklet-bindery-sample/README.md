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
  - Note: this runs with `--metrics-bind-address=0` to keep `localhost:8080` free for the web demo.

4) Build/load the demo module images, then apply the sample resources:
- `./examples/booklet-bindery-sample/dev/kind-demo.sh`

5) Port-forward the web client and open it in a browser:
- `bash ./examples/booklet-bindery-sample/dev/port-forward-web.sh`
- Visit `http://localhost:8080`
  - If `8080` is already in use, pick another local port: `bash ./examples/booklet-bindery-sample/dev/port-forward-web.sh bindery-demo core-web-client 18080`

## Iteration workflow (recommended)

This sample runs modules in Kubernetes, so any code/UI change requires rebuilding + reloading the images and reconciling the `ModuleManifest`s.

- One-shot “bring everything up + port-forward”:
  - `bash examples/booklet-bindery-sample/dev/demo-up.sh bindery 18080`
  - Open `http://localhost:18080`
- Rebuild + redeploy modules after code/UI changes:
  - `bash examples/booklet-bindery-sample/dev/demo-redeploy.sh bindery`
  - If the port-forward dropped, rerun `bash examples/booklet-bindery-sample/dev/port-forward-web.sh bindery-demo core-web-client 18080`
- Reset world state (without redeploying):
  - Click **Reset world** in the UI, or run `bash examples/booklet-bindery-sample/dev/demo-reset.sh 18080`

## Notes for splitting into a standalone repo

This folder is structured so it can be extracted into its own repository later.
The only coupling is that the demo modules import the engine RPC contract from `bindery-core` (by Go module).

When extracting, remove the local `replace` directive in `go.mod` and pin `github.com/bayleafwalker/bindery-core` to a real version/tag.
