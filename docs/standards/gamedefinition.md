# `Booklet` Standard (v1alpha1)

This document defines the YAML manifest schema that defines a game composed of modules.

## 1) High-level shape

A `Booklet` is a declarative document that:

- Defines the game identity and version.
- Lists the modules that constitute the game.
- Defines co-location strategies for performance optimization.
- Sets default parameters for the game instance.

## 2) Reference schema (human-readable)

```yaml
apiVersion: bindery.platform/v1alpha1
kind: Booklet
metadata:
  name: string                  # DNS-like name
  labels:                       # arbitrary tags
    string: string

spec:
  gameId: string                # Stable game identifier
  version: string               # Semantic version
  description: string

  modules:
    - name: string              # Name of a ModuleManifest
      required: boolean         # Default: true
      desiredScope: enum(cluster|region|world|world-shard|session)
      parameters:               # Module-specific configuration
        string: string

  colocation:                   # Optional co-location groups
    - name: string              # Group name
      strategy: enum(Node|Pod)  # Co-location strategy
      modules:                  # List of module names in this group
        - string

  defaults:
    region: string
    shardCount: integer
    tickRateHz: number
```

## 3) Co-location Strategies

The `colocation` field allows optimizing inter-module latency by grouping modules.

- **Node**: Schedules modules on the same Kubernetes node using Pod Affinity. This reduces network latency to localhost or loopback speeds but keeps modules in separate Pods.
- **Pod**: Merges modules into a single Pod (sidecar pattern). This allows communication via Unix Domain Sockets (UDS) or localhost, providing the lowest possible latency.

When `strategy: Pod` is used, the platform injects:
- A shared volume at `/var/run/bindery`.
- Environment variables `BINDERY_UDS_DIR` and `BINDERY_MODULE_NAME`.
- Environment variables for dependencies: `BINDERY_UDS_<CAPABILITY_ID>`.

## 4) Examples

```yaml
apiVersion: bindery.platform/v1alpha1
kind: Booklet
metadata:
  name: my-game
spec:
  gameId: my-game
  version: 1.0.0
  modules:
    - name: physics-engine
    - name: interaction-engine
  colocation:
    - name: physics-interaction
      strategy: Pod
      modules:
        - physics-engine
        - interaction-engine
```
