# Capability Contracts

This folder contains **per-capability contract documents**.

There are two complementary representations:

- Human-readable docs (`*.md`) for narrative semantics and guidance.
- Machine-readable contracts (`*.contract.yaml`) validated against `docs/schemas/capabilitycontract.schema.json`.

A capability contract is the canonical place to define:
- capability semantics (what it means, invariants)
- supported scopes and expected multiplicity
- feature flags and their meaning
- NFR expectations (tick rate, latency budget, determinism)
- interface artifacts (proto/event schemas) and their evolution rules

Modules then reference these contracts via `spec.provides[].capabilityId` + `version` and `spec.requires[].capabilityId` + `versionConstraint`.

## How to add a new capability

1. Copy `./_template.md` to `./<capabilityId>.md`.
2. Fill out semantics + interface references.
3. If the capability is widely used, keep a changelog section per contract version.

**Rule:** A capability ID is immutable. Evolve via SemVer.
