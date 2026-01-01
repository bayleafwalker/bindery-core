# Booklets & Game Repositories

Bindery Core ships the **platform** (CRDs + controllers). Games are published as **Booklets** that reference the modules they need.

This repository includes one **sample game** (`bindery-sample`) under `examples/booklet-bindery-sample/` so the end-to-end flow can be exercised without mixing game assets into the platform folders.

## Recommended split

### `bindery-core` (this repo)
- CRDs (`k8s/crds/`) and controller manager (`controllers/`)
- Helm chart to deploy the platform (`helm/bindery-core/`)
- (No game content required)

### A game repo (future / separate)
A game repo owns the *composition* and module implementations, for example:
- `Booklet` manifests (one or more)
- `ModuleManifest` manifests for each module the game uses
- Module source code + Dockerfiles + build/publish pipeline

The platform only needs the YAML applied into the cluster and the referenced images available in a registry.

## Deployment model

1) Deploy Bindery Core to a cluster (Helm):
- `helm install bindery-core ./helm/bindery-core -n bindery-system --create-namespace`

2) Deploy a game (from its repo):
- `kubectl apply -n <game-namespace> -f booklet.yaml`
- `kubectl apply -n <game-namespace> -f modulemanifests/`

3) Create a world:
- `kubectl apply -n <game-namespace> -f worldinstance.yaml`

## Local development: sample game

`make kind-demo` creates a Kind cluster, installs CRDs, builds the demo module images locally, loads them into Kind, and applies the sample game manifests from `examples/booklet-bindery-sample/k8s/`.

The demo modules are intentionally small:
- `bindery/demo-physics:0.1.0`: demo physics module (tick + queued commands)
- `bindery/demo-interaction:0.1.0`: consumes `physics.engine` via injected env vars

## Practical guidance for game repos

- Pin module container images by tag/digest; keep `ModuleManifest.spec.module.version` and `ModuleManifest.spec.runtime.image` aligned.
- Prefer `ModuleManifest.spec.runtime` for deployment settings (image/port/env/hooks); legacy runtime annotations are still accepted by the RuntimeOrchestrator.
- Keep Booklets small and compositional; treat them like “game server charts” and manage them with GitOps if possible.
