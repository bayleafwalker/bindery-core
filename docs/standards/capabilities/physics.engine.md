# Capability: physics.engine

- **Capability ID:** `physics.engine`
- **Contract version (documented):** `1.2.0`
- **Owner:** platform-sim@studio.example

## 1) Purpose

Provides an authoritative physics simulation step for a bounded simulation domain.

## 2) Semantics

- Produces deterministic physics outcomes **within a single world shard**, assuming the same initial state and the same ordered inputs.
- Owns collision resolution, constraint solving, and physics state evolution.
- Exposes authoritative state updates (or deltas) via events.

Failure behavior:
- If unavailable, dependent modules that require it must not progress authoritative simulation.

## 3) Scope & multiplicity

- Allowed scopes: `world-shard` (recommended for v0.1)
- Recommended scope: `world-shard`
- Multiplicity: `1` provider per `world-shard`

## 4) Feature flags

- `deterministic-step`: provider guarantees deterministic stepping given ordered inputs.
- `continuous-collision`: provider supports continuous collision detection.

## 5) Non-functional requirements (NFR)

Typical expectations for `world-shard` authoritative physics:
- Tick rate: 60 Hz target (hard for shard-wide determinism)
- Latency: p95 ≤ 10–12ms from step input to step output (example targets)
- Determinism: `required` for authoritative simulation

## 6) Interfaces

### 6.1 gRPC

- Proto ref: `registry://protos/game.physics.v1`
- Service: `game.physics.v1.PhysicsEngine`
- Key methods (illustrative):
  - `Step(StepRequest) -> StepResponse`
  - `ApplyImpulse(ApplyImpulseRequest) -> ApplyImpulseResponse`

### 6.2 Events

- Stream: `physics.state.v1` (publish)
- Schema ref: `registry://schemas/game.physics.state/1.0.0`
- Ordering/partition key: `worldShardId`

## 7) Compatibility notes

- Additive message fields and new RPC methods are allowed in MINOR/PATCH.
- Breaking changes require MAJOR (2.0.0).

## 8) Changelog

- `1.2.0`: Baseline contract as referenced by the worked examples.
