# Versioning & Deprecation Policy (v0.1)

This document defines how **capability contracts** and **interfaces** evolve over time.

The goals are:
- predictable upgrades
- safe coexistence of old/new modules
- a declarative way to express compatibility, deprecations, and feature negotiation

---

## 1) What is versioned

### 1.1 Capability contract versions
Each capability (e.g., `physics.engine`) has its own **contract version** (SemVer). This version reflects the semantics + interface expectations for that capability.

### 1.2 Module versions
Module versions are separate and may change for implementation reasons without changing capability contract versions.

### 1.3 Interface artifacts
Interface artifacts (protobuf schemas, event schemas) are versioned and referenced immutably.

---

## 2) SemVer rules (capabilities)

A capability contract version is `MAJOR.MINOR.PATCH`.

- PATCH: clarifications, bug fixes, strictly backward compatible additions
- MINOR: backward compatible additions (new optional fields, new RPC methods)
- MAJOR: breaking changes (removed fields, renamed concepts, incompatible semantics)

**Baseline rule:** Consumers should not be expected to work across MAJOR versions without an explicit compatibility layer.

---

## 3) Deprecation lifecycle

Deprecation is a policy applied to:
- capability contract versions
- specific interface versions
- individual features

Recommended stages:

- `active`: current supported contract
- `deprecated`: still supported, but scheduled for retirement
- `retired`: must not be used for new deployments; may be blocked by policy

### 3.1 Deprecation declaration
Deprecations should be declared declaratively in the capability contract document (see `capabilitycontract.md`).

A deprecation should include:
- what is deprecated (contract version range, interface version, feature)
- replacement guidance
- target retirement date (or milestone)

### 3.2 Enforcement policies
Enforcement is environment-specific and should be policy-driven:
- dev/test environments may allow deprecated contracts
- production may block new deployments using deprecated or retired contracts

---

## 4) Feature flags vs versions

Use **SemVer** for compatibility boundaries.

Use **feature flags** for optional behaviors that do not fundamentally change semantics.

Guidance:
- New features should default to off unless negotiated.
- A feature must never silently change default behavior across MINOR versions without a declared default policy.

---

## 5) Compatibility bridges

When a breaking change is needed, prefer one of:
- dual-serving modules (serve both old and new contract versions)
- explicit adapter modules (consume old, provide new; or vice versa)

Adapters should be expressed as capabilities too, so resolution remains declarative.

---

## 6) Interface evolution baselines

### 6.1 gRPC
- Additive changes OK in MINOR/PATCH (new methods, new optional fields)
- Breaking changes require MAJOR

### 6.2 Events
- Published event schemas are immutable
- Additive fields OK if consumers ignore unknown fields
- Breaking changes require MAJOR and a new schema ID or major version bump

---

## 7) Suggested retirement windows

This is a placeholder policy; tune per team/scale.

- Deprecate a MAJOR version only after:
  - replacement MAJOR is available
  - adapters exist (or migration completed)
- Typical windows:
  - dev/test: 0–30 days
  - production: 60–180 days

Use explicit exceptions rather than implicit drift.
