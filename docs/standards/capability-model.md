# Capability Model Standard (v0.1)

This document defines the **capability contract model** used to compose modules.

A module declares:
- **provides**: capabilities it implements
- **requires**: capabilities it depends on

For the `ModuleManifest` schema, see `modulemanifest.md`.

---

## 1) Core Concepts

### 1.1 Module
A **module** is a deployable unit (typically one or more Kubernetes workloads) that implements one or more **capabilities** and may depend on other capabilities.

A module has:
- a stable **module identity**
- a **module version** (semantic version of the module artifact)
- a set of **provided capability contracts**
- a set of **required capability contracts**
- declared **interfaces** (gRPC + events) that realize those capabilities
- declared **scaling/sharding metadata** for operational placement

### 1.2 Capability
A **capability** is a named contract representing *what* is provided/required (not *how* it’s implemented). Capabilities are resolved across modules and bound to concrete interfaces at deploy/runtime.

A capability has:
- a **capability ID** (namespaced string)
- a **capability semantic version** (contract version)
- an intended **scope**
- **multiplicity** constraints (singleton vs many per scope)
- optional **feature flags** and **non-functional requirements** (NFRs)

### 1.3 Interface (Realization)
Capabilities are realized by interfaces:
- **gRPC APIs**: request/response, command/query style
- **Event schemas**: publish/subscribe, streaming, state-change notifications

A single capability may be realized by:
- one gRPC service, or several services
- one or more event streams/topics
- both

---

## 2) Capability IDs

### 2.1 Format
Capability IDs are dot-namespaced, lowercase, stable identifiers:

- **Syntax**: `segment("." segment)+`
- **segment**: `[a-z][a-z0-9-]*`

Examples:
- `physics.engine`
- `time.source`
- `interaction.engine`
- `narrative.runtime`
- `world.state`

### 2.2 Namespacing guidance
Use the first segment(s) to establish domain ownership and avoid collisions:
- Domain-first: `physics.engine`, `time.source`
- Multi-org convention (optional): `studio.physics.engine`, `vendorx.netcode.relay`

**Rule:** Capability IDs are immutable; changes require new versions, not renaming.

---

## 3) Capability Versioning (Semantic Versioning)

Each capability uses **SemVer**: `MAJOR.MINOR.PATCH`.

Interpretation is **contract-first**:
- **PATCH**: clarifications, bug fixes, non-breaking schema additions
- **MINOR**: backward-compatible additions (new optional fields, new RPC methods)
- **MAJOR**: breaking changes (removed/renamed fields, incompatible semantics)

### 3.1 Compatibility policy
A requirement declares a **version constraint** over the capability contract (not the module version).

Examples:
- `^1.4.0` → compatible with `>=1.4.0 <2.0.0`
- `~1.4.0` → compatible with `>=1.4.0 <1.5.0`
- `>=1.2.0 <1.6.0`

**Rule:** By default, **MAJOR differences are incompatible**.

---

## 4) Scopes

Scopes define *where* a capability instance is valid and what it serves.

Supported scopes:
- `cluster`: shared platform-wide service
- `region`: shared within a geographic/latency region
- `world`: shared for an entire world
- `world-shard`: shared for one world shard/partition
- `session`: per match/instance/session

**Rule:** A module must declare the scope of each provided capability and the scope expected for each requirement.

If a consumer scope differs from a provider scope, the system must use an explicit bridge/adapter capability rather than silently binding across scope boundaries.

---

## 5) Multiplicity

Multiplicity constrains the number of providers bound per scope unit:

- `1`: exactly one provider must be bound per scope unit
- `many`: multiple providers may be bound per scope unit

**Rule:** If a requirement declares multiplicity `1`, the resolver must bind exactly one provider per target scope unit.

---

## 6) Dependency Modes

Each required capability is either:
- `required`: module is invalid/unstartable without a bound provider
- `optional`: module can start without it, but must degrade/disable features

**Rule:** Optional dependencies must be paired with explicit feature gating.

---

## 7) Compatibility & Version Resolution

### 7.1 Resolution inputs
Given a set of module manifests selected for an environment (cluster/region/world/etc.), the resolver builds a dependency graph.

For each **required** capability, select one or more providers that match:
- same `capabilityId`
- provider version satisfies the requirement’s version constraint
- scope is compatible
- multiplicity constraints can be satisfied
- required features can be negotiated
- hard NFR constraints can be met

### 7.2 Deterministic selection
When multiple providers satisfy a single required capability, selection must be deterministic:

1. Filter providers by version, scope, required features, hard NFR constraints.
2. Prefer providers with:
   - highest compatible capability version
   - lowest estimated latency (if latency targets exist)
   - locality/topology preference (same region/shard)
3. Break ties using stable ordering:
   - provider module ID lexicographic
   - provider module version descending

### 7.3 Failure behavior
- If any `required` dependency cannot be resolved → **composition fails**.
- If an `optional` dependency cannot be resolved → module still deploys, but must:
  - expose degraded-mode status/telemetry
  - disable the features tied to that capability

---

## 8) Interface Evolution (Rules)

### 8.1 gRPC evolution (recommended baseline)
- Additive changes are allowed in MINOR/PATCH:
  - new RPC methods
  - new fields (safe defaults)
- Breaking changes require MAJOR:
  - renaming/removing fields
  - changing semantics incompatibly
  - changing method signatures

### 8.2 Event evolution (recommended baseline)
- Events are versioned and immutable once published.
- Additive fields allowed in MINOR/PATCH if consumers ignore unknown fields.
- Removing/renaming fields, changing meaning, or changing partitioning keys requires MAJOR.

---

## 9) Feature Flags & Optional Capabilities

### 9.1 Features within a capability
A capability may expose **features** (negotiable options):
- `required`: provider must support
- `preferred`: use if supported, otherwise ignore

**Rule:** Features do not replace SemVer. Use them for orthogonal optional behavior, not breaking contract changes.

### 9.2 Optional dependencies
Optional dependencies must declare:
- which features are disabled without the optional capability
- what fallback behavior exists (if any)

---

## 10) Non-Functional Requirements (NFRs)

Capabilities can carry NFRs for placement and simulation correctness.

Recommended NFR categories:
- latency budgets (p95/p99)
- tick rate and jitter tolerance
- throughput (ops/sec, event rate)
- determinism requirements
- state size/snapshot frequency
- availability expectations
- locality/affinity expectations

Hard vs soft constraints:
- `hard`: binding must satisfy
- `soft`: used for ranking; violations produce warnings

**Rule:** NFRs must be machine-readable and comparable.
