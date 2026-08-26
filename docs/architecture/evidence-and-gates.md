# Evidence, reconciliation, and calibrated gates

Scope: the **external runtime** (`internal/externalruntime`, `internal/relay`,
`pkg/evidencev1`, `pkg/gatev1`, `cmd/bindery-external-runtime`,
`charts/bindery-external-runtime`). It defines no CRDs and runs no controllers,
and none of the operator's capability model applies to it. See
[`../README.md`](../README.md) for how the two subsystems in this repository
relate.

Implementation status of the two package boundaries described below:

- `pkg/evidencev1` **is wired**: `internal/externalruntime/service.go`,
  `state_store.go`, and `types.go` import it.
- `pkg/gatev1` **is wired**, as of the capture plane. Its caller is
  `internal/externalruntime/capture_gate.go`, which defines the consequential
  gate `bindery.capture.completeness` and evaluates it over every capture
  before that capture is allowed to contribute an observation summary to an
  evidence set. Its five outcomes, its applicability rules, and its
  positive/negative calibration controls are exercised against the running
  service in `capture_gate_test.go`, not only inside the package's own tests.

## Who counted

Every observation summary records a `source`:

| source | meaning |
| --- | --- |
| `broker-derived` | The broker read the persisted observations, counted them, and hashed them itself. |
| `client-reported` | The producer supplied the numbers. |

Where an execution has captured streams, `client-reported` summaries are
refused outright with `OBSERVATION_ADJUDICATION_FORBIDDEN`. This is the point
of the capture plane rather than a side effect of it: an evidence set assembled
from figures the adapters reported about themselves records agreement between
two self-assessments, which is a weaker claim than it looks, and was mistaken
for a stronger one once already — see
[`../assessments/2026-08-25-ra2-vertical-slice.md`](../assessments/2026-08-25-ra2-vertical-slice.md).

The `client-reported` path survives only for executions with no capture plane
at all, where there is nothing for the broker to derive from.

## Observations are durable, and separate from the snapshot

Raw observations are content-addressed objects in the state directory; the
control snapshot holds only each capture's metadata and its batch index. The
snapshot is rewritten in full on every mutation, so a snapshot write stays
proportional to the number of batches rather than the number of events. The
reference run recorded 6,651 events per participant; that does not belong in a
file that is rewritten on every request.

The write order is: object first, then the in-memory index, then the snapshot
commit. The invariant that falls out of it is stated once and holds everywhere:
*the index never references bytes that are not fully durable; the disk may hold
bytes nothing references.* Every crash point leaves unreferenced
content-addressed files, which is the only residue that cannot corrupt
anything. Orphaned objects are counted at startup and never swept, because
deleting content-addressed evidence on a heuristic is how evidence gets lost.

## Derivation is additive

Raw observations are never edited or reinterpreted in place. A normalizer
produces a *derived capture*: its own record, carrying `producer_class:
normalizer`, a link back to the stream it came from, and a
`derivation{normalizer_id, normalizer_version, source_event_ids}` on every
event. Replaying the same version returns the existing derivation; a new
version creates another dataset beside the old one.

Derived captures are excluded from evidence comparison. Reconciling a stream
against a function of itself would manufacture agreement, which is the opposite
of what evidence is for.

## Durable graph

The external-runtime evidence graph is:

```text
Identity <- Session <- Execution <- Observation <- EvidenceSet
                    ^
                    |
                 Placement
```

All arrows are durable identifiers. Public records may embed a convenience
copy, but the identifier remains the referential boundary.

- `Identity` says who controls a token-backed claim.
- `Session` says what was admitted and under which compatibility policy.
- `Placement` says which transport allocation and allocator implementation were
  selected.
- `Execution` names the external run independently of admission state.
- `Observation` says what one observer reported or measured.
- `EvidenceSet` retains compared observations and the reconciliation performed
  over them.

An observation is never silently promoted to truth. Reconciliation reports
agreement or disagreement at the level promised by its method.

## Reconciliation methods

| Method | Meaning | Core status |
| --- | --- | --- |
| `exact-count` | All independent streams report the same event count | Implemented |
| `ordered-hash` | All independent streams report the same ordered-stream digest | Implemented |
| `semantic-equivalence` | Domain normalizer considers streams equivalent | Reserved |
| `quorum` | A declared observer quorum agrees | Reserved |
| `domain-specific` | Versioned adapter/domain policy | Reserved |

Reserved methods fail explicitly as unsupported. They are not aliases for count
equality with more ambitious names.

An evidence set requires at least two distinct observers. Two streams emitted
by one observer are useful redundancy but not independent evidence.

## Gate evaluation order

Gate evaluation has three separate questions:

1. Does the gate apply to this phase, artifact type, and capability context?
2. Is this exact gate implementation calibrated against known-pass and
   known-fail fixtures?
3. What did the calibrated evaluator observe for the subject?

The order matters. Evaluating first and inventing applicability afterward is
how unrelated checks become impressive-looking blockers.

Consequential gates carry:

```text
gate_id
gate_version
implementation_hash
applies_when
positive_control
negative_control
```

The result status is one of `PASS`, `FAIL`, `NOT_APPLICABLE`, `UNRESOLVED`, or
`ERROR`. `FAIL` is reserved for an applicable, calibrated evaluator rejecting
the subject. Broken fixtures, missing evaluator context, and internal failures
have their own states.

## Persistence boundary

The reference store is an atomic JSON snapshot containing private verifier
material and public records. It is suitable only for one writer:

- state file mode is `0600`;
- symlink targets are rejected;
- writes use a same-directory temporary file, file sync, atomic rename, and
  directory sync;
- a failed durable write rolls the in-memory mutation back;
- startup rejects an unsupported schema or dangling identity/session/
  placement/execution/evidence reference.

The Helm deployment enforces one replica and `Recreate` over one persistent
volume. PostgreSQL is the next store when multiple brokers become a real
requirement. Increasing `replicaCount` before that is not scaling; it is a
random-history generator.

## Recovery checks

Before accepting this slice:

1. Create two identities, one session, one placement, one execution, two
   enrollments, and one evidence set.
2. Stop the broker after all mutation responses have completed.
3. Start it against the same state file.
4. Resolve every public record by its stable ID.
5. Authenticate with the pre-restart account and client lease tokens.
6. Verify an injected persistence failure returns an error and leaves no
   in-memory mutation.
7. Verify a positive-control failure yields gate `ERROR`, and a context mismatch
   yields `NOT_APPLICABLE`.

Rollback is the previous binary plus the pre-upgrade state snapshot. A new
binary must not rewrite an older snapshot in place until a versioned migration
and downgrade path exist.
