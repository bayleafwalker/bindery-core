# Standards Index

This folder defines the **declarative module/capability standard** for the platform.

## Read first

- Capability model (IDs, versions, scopes, resolution, features, NFRs): `capability-model.md`
- Module manifest contract: `modulemanifest.md`
- Game definition contract: `gamedefinition.md`
- Capability contract document: `capabilitycontract.md`
- Versioning & deprecation policy: `versioning-and-deprecation.md`

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

## RPC

- Engine gRPC Protobuf contract (v1): `rpc/engine-grpc-v1.md`

## Testing

- CapabilityResolver test plan: `../testing/capability-resolver-test-plan.md`
