# The durability ceiling, measured

Date: 2026-08-26. Reproduce with
`go test ./internal/externalruntime -run 'Ceiling' -v`.

`docs/decisions/operator-gates.md` declares retention indefinite. That is what
the code does, but "indefinite" without a limit is the claim
`docs/standards/production-readiness.md` warns about in its own preamble. This
document supplies the limit.

## Why there is a ceiling at all

Two properties combine. Nothing is ever deleted — there is no `delete` in the
non-test external-runtime sources — and `commitLocked` rewrites the entire
snapshot on every mutation. So the state file's size is a function of total
history, and the cost of one mutation is a function of every mutation before
it. Neither is a defect on its own: the single-writer, whole-snapshot design is
what makes the rollback in `commitLocked` verifiable. The ceiling is the price.

## What was measured

| Quantity | Measurement |
| --- | --- |
| One session with two players | ~9,563 bytes of snapshot |
| Sessions that fit under the limit | ~7,017 |
| Hard limit | `MaxStateSnapshotBytes`, 64 MiB |
| One mutation at 35 MiB of state | ~195 ms |
| Restart at 35 MiB of state | ~189 ms |

Growth is linear: bytes per session moved less than 2% across the sampled
range, which is what makes the division meaningful.

The 195 ms figure is measured, not extrapolated. `TestCommitCostNearTheCeiling`
restores a 35 MiB state file — half the ceiling — and times one ordinary
identity claim. At half the ceiling a single client request already costs about
a fifth of a second, and the cost is linear in accumulated history, so the
service becomes unpleasant well before it becomes unusable.

## The failure mode is worse than the limit

`Save` will write a snapshot that `Load` cannot read.

`Load` wraps the file in an `io.LimitReader` capped at `MaxStateSnapshotBytes`,
and `Save` has no corresponding check. A service that crosses the limit
therefore persists successfully, keeps serving from memory, and cannot start
again. `TestStateBeyondTheCeilingCannotBeRestored` writes a 66 MiB snapshot,
saves it without error, and gets this back on load:

```
decode state snapshot: unexpected EOF
```

An operator meeting that for the first time reads it as file corruption, not as
a capacity limit. The state is intact; it is merely longer than the reader is
willing to consume.

## What this does and does not say

It says roughly seven thousand two-player sessions of accumulated history, on
one deployment, before restart stops working — and noticeable per-request cost
long before that. It says the current design is adequate for a research
deployment and not for an open one.

It does not measure a fleet, concurrent load, or object-store growth. Heavy
capture bytes live outside the control snapshot in the object store, so they do
not count against this ceiling and have no measured limit of their own.

## What would move it

In rough order of effort, and none of it is in scope here:

1. **Refuse the write.** Have `Save` reject a snapshot larger than
   `MaxStateSnapshotBytes`, converting an unstartable service into a failed
   mutation with an accurate message. This is small and strictly an
   improvement; it lowers no ceiling and removes the confusing failure.
2. **Stop rewriting everything.** An append-only log with periodic compaction
   makes mutation cost independent of history. This is the change that removes
   the per-request cost, and it is a redesign of the durability argument.
3. **Delete something.** A finite retention window, considered and rejected as
   gate 2, needs deletion, compaction, and reconciliation of evidence sets that
   reference removed captures.
