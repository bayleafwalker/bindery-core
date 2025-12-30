# Capability: interaction.engine

- **Capability ID:** `interaction.engine`
- **Contract version (documented):** `0.9.0`
- **Owner:** platform-gameplay@studio.example

## 1) Purpose

Provides authoritative processing of gameplay interactions: interpreting player/world actions into state transitions and events.

## 2) Semantics

- Accepts actions/commands and applies interaction rules.
- May validate actions against physics constraints by consulting `physics.engine`.
- Emits interaction events for downstream consumers (UI feeds, narrative triggers, telemetry).

Failure behavior:
- If unavailable, action submission must fail fast or be queued according to session/world policies.

## 3) Scope & multiplicity

- Allowed scopes: `world-shard` (recommended for v0.1)
- Recommended scope: `world-shard`
- Multiplicity: `1` provider per `world-shard`

## 4) Feature flags

- `server-authoritative`: provider enforces authoritative validation and ordering.

## 5) Non-functional requirements (NFR)

Typical expectations:
- Tick rate: aligned to physics/world tick (e.g., 60 Hz hard requirement)
- Latency: p95 target is typically looser than physics but must not violate end-to-end action budget

## 6) Interfaces

### 6.1 gRPC

- Proto ref: `registry://protos/game.interaction.v1`
- Service: `game.interaction.v1.InteractionEngine`
- Key method (illustrative):
  - `SubmitAction(SubmitActionRequest) -> SubmitActionResponse`

### 6.2 Events

- Stream: `interaction.events.v1` (publish)
- Schema ref: `registry://schemas/game.interaction.event/1.0.0`
- Ordering/partition key: `worldShardId`

## 7) Compatibility notes

- Additive changes may ship in MINOR/PATCH.
- Breaking changes require MAJOR (1.0.0 will be the first stability milestone).

## 8) Changelog

- `0.9.0`: Baseline contract as referenced by the worked examples.
