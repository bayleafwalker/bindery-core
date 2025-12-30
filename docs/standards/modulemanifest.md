# `ModuleManifest` Standard (v0.1)

This document defines the YAML manifest schema that modules publish to declare:

- module identity
- capabilities **provided**
- capabilities **required**
- interface definitions (gRPC + event schemas)
- scaling/sharding metadata

For the capability model (IDs, scopes, resolution), see `capability-model.md`.

For machine validation, see `../schemas/modulemanifest.schema.json`.

---

## 1) High-level shape

A `ModuleManifest` is a declarative contract document.

- `apiVersion` and `kind` identify the schema version.
- `metadata` provides a human/registry-friendly name and tags.
- `spec.module` identifies the module artifact.
- `spec.provides[]` and `spec.requires[]` declare capability contracts.
- `spec.scaling` declares intended scaling/sharding semantics.

---

## 2) Reference schema (human-readable)

```yaml
apiVersion: game.platform/v0.1
kind: ModuleManifest
metadata:
  name: string                  # DNS-like name within your registry
  labels:                        # arbitrary tags
    string: string
  annotations:                   # arbitrary annotations
    string: string

spec:
  module:
    id: string                  # globally unique module identifier (e.g., "core.physics")
    version: string             # module artifact semver (e.g., "2.1.0")
    description: string
    owners:
      - string
    repo: string                # optional URL
    license: string             # optional

  provides:
    - capabilityId: string      # e.g., "physics.engine"
      version: string           # capability contract semver served by this module
      scope: enum(cluster|region|world|world-shard|session)
      multiplicity: enum(1|many)

      features:
        supported:
          - string              # feature flags supported by provider

      nfr:
        latency:
          p95Ms: { value: number, constraint: enum(hard|soft) }
        tickRateHz:
          value: number
          constraint: enum(hard|soft)
        determinism:
          value: enum(required|best-effort|none)
          constraint: enum(hard|soft)

      interfaces:
        grpc:
          - package: string
            service: string
            protoRef: string     # reference to proto definition (URI or registry key)
            methods:
              - name: string
                request: string
                response: string
        events:
          - name: string         # logical stream/topic name
            direction: enum(publish|subscribe|both)
            schema:
              id: string         # schema identifier (namespaced)
              version: string    # schema semver
              format: enum(protobuf|json|avro)
              schemaRef: string  # URI or registry key
            orderingKey: string  # optional partition/ordering key semantic

  requires:
    - capabilityId: string
      versionConstraint: string  # semver range expression, e.g. "^1.4.0"
      scope: enum(cluster|region|world|world-shard|session)
      multiplicity: enum(1|many)
      dependencyMode: enum(required|optional)

      features:
        required:
          - string
        preferred:
          - string

      nfr:
        latency:
          p95Ms: { value: number, constraint: enum(hard|soft) }
        tickRateHz:
          value: number
          constraint: enum(hard|soft)

  scaling:
    defaultScope: enum(cluster|region|world|world-shard|session)
    statefulness: enum(stateless|stateful)
    sharding:
      strategy: enum(none|world|world-shard|session|custom)
      key: string               # e.g., "worldShardId"
    autoscaling:
      minReplicas: number
      maxReplicas: number
      metricHints:
        - type: enum(cpu|memory|rps|tick-lag|custom)
          target: string
```

---

## 3) Interface references

`protoRef` and `schemaRef` are intentionally abstract: they should reference artifacts in a registry (or a VCS URL) rather than embedding full definitions in the manifest.

Recommended schemes:
- `registry://protos/<package>`
- `registry://schemas/<schemaId>/<version>`
- `https://...` (pinned commits/tags)

**Rule:** Interface artifacts must be content-addressable or pinned (tag/commit/digest) to keep deployments reproducible.

---

## 4) Examples

Worked examples are maintained as separate files:

- `examples/physics-engine.modulemanifest.yaml`
- `examples/interaction-engine.modulemanifest.yaml`
