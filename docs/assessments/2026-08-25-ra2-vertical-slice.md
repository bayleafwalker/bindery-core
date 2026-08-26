# RA2 external-runtime vertical slice — 2026-08-25

## Conclusion

Bindery's external-runtime thesis is empirically demonstrated once. It is no
longer only an architecture whose pieces plausibly compose.

The demonstrated path was:

```text
control plane -> allocator -> placement -> isolated clients -> pinned transport
              -> live game -> instrumentation -> cross-client evidence
```

The material result was not merely that Yuri's Revenge ran. Two independently
instrumented clients produced matching 6,651-event accounts of the execution.
That is stronger evidence than a zero exit code, one adapter verdict, or a
server observing two connections.

This remains one external runtime and one run shape. The accurate claim is
therefore **demonstrated once**, not generalized, production-ready, or proven
across arbitrary games.

## What the run established

### External-runtime orchestration works

The game process remained outside Bindery. Bindery still established identity,
allocated topology, launched role-specific participants, pinned transport,
collected observations, and evaluated the result.

### Reproducible does not mean homogeneous

Byte-identical appliance clones acquired distinct runtime identities and
roles. `IsSpectator`, AI houses, and launch policy were session inputs rather
than image mutations. The appliance remained reproducible while the execution
topology varied.

### Independent observation is a correctness primitive

Neither client was treated as authoritative. Their observations remained
attributable and were reconciled. Equal event counts establish consistency at
the count level; they do not establish semantic equivalence. Bindery Core now
models that distinction as an `EvidenceSet` with an explicit reconciliation
method and outcome.

The 6,651/6,651 result is retained as the regression fixture for
`exact-count`, the first and deliberately weakest policy.

## What failed usefully

### A deterministic gate was repeatedly unrelated to truth

The adapter interpreted a diagnostic string as a verdict and reported both
successful runs as failures and failed runs as successes across four
iterations. Repetition made the answer stable, not correct.

Design rule:

> Consequential gates require calibration evidence, not merely implementation
> evidence.

A consequential gate must carry its version and implementation hash, plus at
least one known-pass and one known-fail control. A verifier that fails its
positive control is `ERROR`; it does not become reassuringly strict.

### Some gates were evaluated in the wrong context

Oracle tracing and Kctl checks were applied where their phase, artifact type,
or capabilities did not make them applicable. Gate outcomes are therefore
five-state:

- `PASS`
- `FAIL`
- `NOT_APPLICABLE`
- `UNRESOLVED`
- `ERROR`

Missing applicability evidence is `UNRESOLVED`. A known context mismatch is
`NOT_APPLICABLE`. Neither silently becomes `FAIL`.

### Control-plane state could outlive neither restart nor replica boundaries

The service held identity, session, placement, and enrollment records in
process maps while the chart requested two replicas. That allowed two brokers
to expose mutually unrelated histories behind one Service. Persistence was not
the only missing property; there was no coherent writer.

The reference service now uses a crash-safe, file-backed snapshot as a bounded
single-instance implementation. A mutation is acknowledged only after the
snapshot is written, synced, renamed, and the directory entry synced. Failed
writes restore the previous in-memory snapshot. The chart enforces one replica,
`Recreate`, and a persistent volume.

This is not a substitute for PostgreSQL. It is the smallest implementation that
makes the ontology survive restart without pretending to support concurrent
writers. Moving back to multiple replicas requires a shared relational store
and transactional mutation semantics.

### The allocator lacked durable implementation identity

The allocator was behaviorally indispensable but had previously existed
outside Git. Every new placement now records:

```text
implementation
repository
revision
config_digest
```

The reference allocator refuses to start without a full Git revision. A
historical question such as “which allocator produced this placement?” is now
answerable from the placement record rather than the operator's current
checkout.

## Historical evidence limitation

The successful run happened before these durable core types existed. Its
reported chain, 39-test adapter result, and matching 6,651-event observations
are valid historical findings, but they must not be backfilled with invented
`execution_id`, `placement_id`, or `evidence_set_id` values.

The next run through the hardened control plane should create those records
natively. That run is the recovery path from narrative provenance to
referentially complete provenance.

## Next boundary

Do not add richer RA2 behavior until the durability, reconciliation,
applicability, and calibration changes pass repository CI and one restart drill.
After that, run an external runtime that is not RA2. The purpose is abstraction
pressure, not another game-specific trophy:

- the runtime must use a materially different integration mechanism;
- only the adapter may change;
- `Session -> Placement -> Execution -> Observation -> EvidenceSet` must remain
  intact;
- every behaviorally significant component must resolve to an implementation
  identity;
- the run must include both a process restart and a known-fail calibration
  control.

RA2 has supplied depth. The next runtime must test whether Bindery has an
abstraction rather than a particularly well-documented CnCNet harness.
