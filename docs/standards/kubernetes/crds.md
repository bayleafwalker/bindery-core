# Kubernetes CRDs (`bindery.platform/v1alpha1`)

This repo ships Kubernetes Custom Resource Definitions (CRDs) under `k8s/crds/` (and mirrored under `helm/bindery-core/crds/`). Bindery’s controllers treat these resources as the platform control plane.

## Install

```bash
kubectl apply -f k8s/crds/
```

## CRDs (overview)

- `ModuleManifest` (namespaced): a module’s identity and its `provides[]` / `requires[]` contracts, plus scaling/scheduling hints.
  - File: `k8s/crds/modulemanifests.bindery.platform.yaml`
- `Booklet` (namespaced): a “game composition” (set of modules + optional co-location groups).
  - File: `k8s/crds/booklets.bindery.platform.yaml`
- `WorldInstance` (namespaced): instantiates a `Booklet` into a running world; sets `region` and `shardCount`, optionally links to a `Realm`.
  - File: `k8s/crds/worldinstances.bindery.platform.yaml`
- `WorldShard` (namespaced): explicit shard objects for a `WorldInstance` (created/removed based on `WorldInstance.spec.shardCount`).
  - File: `k8s/crds/worldshards.bindery.platform.yaml`
- `CapabilityBinding` (namespaced): resolved dependency edge from consumer → provider (source of truth for runtime wiring).
  - File: `k8s/crds/capabilitybindings.bindery.platform.yaml`
- `Realm` (namespaced): realm-scoped “global modules” shared by multiple worlds.
  - File: `k8s/crds/realms.bindery.platform.yaml`
- `WorldStorageClaim` (namespaced): requests world/world-shard scoped storage; reconciled into a PVC (server tiers) or an external URI (client tiers).
  - File: `k8s/crds/worldstorageclaims.bindery.platform.yaml`
- `ShardAutoscaler` (namespaced): adjusts `WorldInstance.spec.shardCount` based on resource utilization.
  - File: `k8s/crds/shardautoscalers.bindery.platform.yaml`
- `CapabilityDefinition` (cluster-scoped): capability discovery/policy metadata (versions/scopes/features defaults).
  - File: `k8s/crds/capabilitydefinitions.bindery.platform.yaml`

## Example resources

Example resources live under `k8s/examples/`.

Suggested apply order:

```bash
kubectl apply -f k8s/examples/00-namespace.yaml
kubectl apply -f k8s/examples/01-capabilitydefinition-physics-engine.yaml
kubectl apply -f k8s/examples/02-capabilitydefinition-interaction-engine.yaml
kubectl apply -f k8s/examples/game-dev/
```

## Labels used by controllers

These labels are used for selection/debugging:

- `bindery.platform/managed-by`
- `bindery.platform/world`
- `bindery.platform/game`
- `bindery.platform/module`
- `bindery.platform/shard`

