# /contracts

This folder is the home for **interface contracts** shared across the platform.

What belongs here:
- Protobuf/gRPC API IDLs (control plane + module interfaces)
- Event schema definitions (when formalized)
- Capability contract docs / machine-readable contracts

Current canonical sources in this repo:
- Protobuf IDL: `proto/` (e.g. `proto/game/engine/v1/engine.proto`)
- Capability contracts: `docs/standards/capabilities/`

Notes:
- Generated code should remain checked in only if the repo standard requires it.
- When changing a proto, regenerate stubs as documented in `docs/standards/rpc/engine-grpc-v1.md`.
