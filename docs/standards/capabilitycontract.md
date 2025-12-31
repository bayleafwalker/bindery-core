# `CapabilityContract` Standard (v1alpha1)

This document defines a machine-readable capability contract document: `CapabilityContract`.

Why this exists:
- `ModuleManifest` describes *who provides/requires what*.
- `CapabilityContract` describes *what a capability means* and *how its interfaces evolve*.

This enables:
- consistent semantics across modules
- central validation and review of capability evolution
- declarative deprecations and compatibility notes

Machine validation schema: `../schemas/capabilitycontract.schema.json`.

---

## 1) High-level shape

A `CapabilityContract` describes a **single capability ID** at a specific contract version.

- `apiVersion` and `kind` identify the schema.
- `metadata` identifies the document.
- `spec` defines semantics, scope/multiplicity rules, features, NFR expectations, interfaces, and deprecations.

---

## 2) Reference schema (human-readable)

```yaml
apiVersion: bindery.platform/v1alpha1
kind: CapabilityContract
metadata:
  name: string                   # registry-friendly name; suggested: <capabilityId>@<version>
  labels:
    string: string

spec:
  capabilityId: string           # e.g., physics.engine
  version: string                # semver of the capability contract
  owner: string                  # team/email
  status: enum(active|deprecated|retired)

  purpose: string                # short description

  semantics:
    invariants:
      - string
    authoritativeSource: string  # free-form, but kept short
    failureBehavior:
      - string
    ordering:
      guarantees:
        - string

  scope:
    allowed:
      - enum(cluster|region|world|world-shard|session)
    recommended: enum(cluster|region|world|world-shard|session)

  multiplicity:
    recommended: enum(1|many)

  features:
    defined:
      - name: string
        description: string
        stability: enum(experimental|stable|deprecated)

  nfrDefaults:
    tickRateHz:
      value: number
      constraint: enum(hard|soft)
    latency:
      p95Ms:
        value: number
        constraint: enum(hard|soft)
    determinism:
      value: enum(required|best-effort|none)
      constraint: enum(hard|soft)

  interfaces:
    grpc:
      - package: string
        service: string
        protoRef: string
        compatibility:
          additiveUntil: string   # optional semver
    events:
      - stream: string
        schema:
          id: string
          version: string
          format: enum(protobuf|json|avro)
          schemaRef: string
        orderingKey: string

  deprecations:
    - appliesTo:
        contractVersionRange: string     # semver range; e.g., "<1.0.0"
      deprecatedOn: string               # ISO date
      retiresOn: string                  # ISO date
      replacement:
        capabilityId: string
        versionRange: string
      notes:
        - string
```

---

## 3) Usage rules

- A `CapabilityContract` must be the canonical definition for semantics of `capabilityId@version`.
- `ModuleManifest.spec.provides[].version` should reference a capability contract version that exists.
- `ModuleManifest.spec.requires[].versionConstraint` should be satisfiable by published capability contract versions.
- Deprecations should be declared on capability contracts (not scattered across module manifests).

---

## 4) Examples

Examples are maintained as YAML files alongside capability docs:

- `capabilities/physics.engine.contract.yaml`
- `capabilities/interaction.engine.contract.yaml`
