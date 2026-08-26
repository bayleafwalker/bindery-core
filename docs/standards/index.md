# Standards Index

This folder defines the **declarative module/capability standard** for the
Kubernetes operator.

> **Scope.** Nothing in `docs/standards/` applies to the external runtime, the
> other subsystem in this repository. The external runtime defines no CRDs, runs
> no controllers, and has no capability model; its contract lives in
> [`../../contracts/externalruntime/v1/`](../../contracts/externalruntime/v1/)
> and its design notes in
> [`../architecture/evidence-and-gates.md`](../architecture/evidence-and-gates.md).
> See [`../README.md`](../README.md) for the split.

## Read first

- Capability model (IDs, versions, scopes, resolution, features, NFRs): `capability-model.md`
- Module manifest contract: `modulemanifest.md`
- Game definition contract: `gamedefinition.md`
- Capability contract document: `capabilitycontract.md`
- Versioning & deprecation policy: `versioning-and-deprecation.md`

## Runtime and lifecycle

- Realm / world / shard hierarchy: `realm-architecture.md`
- Module lifecycle, root bindings, readiness init containers: `runtime-coordination.md`
- Dynamic sharding and the `ShardAutoscaler`: `shard-autoscaling.md`
- Scalability limits and operational guidance: `production-readiness.md`

  (`production-readiness.md` is a guide to Kubernetes-imposed limits. It is not
  a claim that the project is production-ready; it is not.)

## Capability contracts (per-capability)

Capability contracts live under `capabilities/`:

- `capabilities/README.md` — how capability specs are written
- `capabilities/_template.md` — starter template for new capabilities
- `capabilities/physics.engine.md`
- `capabilities/interaction.engine.md`
 - `capabilities/physics.engine.contract.yaml`
 - `capabilities/interaction.engine.contract.yaml`

## Worked examples

Concrete `ModuleManifest` examples live under `examples/`:

- `examples/physics-engine.modulemanifest.yaml`
- `examples/interaction-engine.modulemanifest.yaml`

## Machine validation

- JSON Schema for `ModuleManifest`: `../schemas/modulemanifest.schema.json`
- JSON Schema for `CapabilityContract`: `../schemas/capabilitycontract.schema.json`

## Kubernetes

- CRD definitions and notes: `kubernetes/crds.md`
- Controller design: `kubernetes/capabilityresolver.md`

The nine shipped CRDs are listed in `kubernetes/crds.md`. Read its "Generation
and drift" section before editing any manifest: most of them are hand-written
and `make manifests` is deliberately **not** the source of truth.

## RPC

- Engine gRPC Protobuf contract (v1): `rpc/engine-grpc-v1.md`

## Testing

- CapabilityResolver test plan: `../testing/capability-resolver-test-plan.md`

## Debugging

- Runtime coordination troubleshooting: `../debugging/runtime-coordination.md`
