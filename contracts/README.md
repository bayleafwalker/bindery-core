# /contracts

This folder is the home for **interface contracts** shared across the platform.

What belongs here:
- Protobuf/gRPC API IDLs (control plane + module interfaces)
- Event schema definitions (when formalized)
- Capability contract docs / machine-readable contracts

Current canonical sources in this repo:
- Protobuf IDL (operator, engine modules): `contracts/proto/`
  (e.g. `contracts/proto/game/engine/v1/engine.proto`)
- Capability contracts (operator): `docs/standards/capabilities/`
- External-runtime HTTP + relay contract: `contracts/externalruntime/v1/`
  (OpenAPI, JSON Schemas, relay wire format, and its invariants in
  `contracts/externalruntime/v1/README.md`)

These two contract families belong to different subsystems and evolve
independently — see `docs/README.md`.

Notes:
- Generated code should remain checked in only if the repo standard requires it.
- When changing a proto, regenerate stubs as documented in `docs/standards/rpc/engine-grpc-v1.md`.
