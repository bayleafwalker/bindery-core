# ERH-007: running a second, non-RA2 external runtime

Date: 2026-08-26. Reproduce with
`go test ./adapters/bindery-dedicated-runtime -run ERH007 -v` and
`go test ./internal/externalruntime -run 'TestFinding|TestNonRA2' -v`.

## What was run

`adapters/bindery-dedicated-runtime` is a Linux-native, server-authoritative
runtime. It inverts ADR-001: the server owns the simulation, so clients observe
nothing independently and the server is the only honest producer of
observations. Its world is generated from a seed, so it has no mod and no map.

The acceptance run drives the real control plane as a separate process over
HTTP: a session with ten player seats and two observers, production of the
authoritative stream, a restart drill in which the broker is killed outright
and brought back on the same state file, and evidence reconciliation with
positive and negative gate controls.

Only the adapter differs. Core was changed only by *removing* game-specific
constraints, never by adding a field.

## What this does not establish

It is a second adapter against a second integration mechanism. It is not a
second commercial game, so it cannot surface the constraints of a codebase
nobody here controls, and it was written by someone who could read the
contracts while writing it. Its findings are therefore a lower bound on what a
real third-party runtime would hit, not an upper one.

That bound was tested the same day. See
[`2026-08-26-erh-007-third-party-runtime.md`](2026-08-26-erh-007-third-party-runtime.md),
which runs the same contracts against OpenTTD -- an unmodified third-party
game, over its own admin protocol -- and finds two things this run could not:
enrollment requires every participant to run a byte-identical build, and an
evidence set cannot say what interval an observer watched.

## Abstractions that held

Session, placement, execution, capture, and enrollment contracts carried a
server-authoritative runtime without modification. Idempotency, the durable
restart, broker-derived observation counts, and the completeness gate all
behaved as documented. The restart drill lost nothing: thirty-two observations
and their close survived a killed process.

Gate calibration held under a runtime the gate was not written for: every
evaluation reported `calibration_valid`, with two PASS on the authoritative
streams and ten FAIL on streams that produced nothing.

## Leaks that were removed

Three constraints in core were Red Alert 2's, not Bindery's. All three refused
this runtime outright before 2026-08-26.

1. **Mod and map were mandatory.** A runtime whose world is generated had to
   invent identifiers and sha256 hashes for content that does not exist, purely
   to satisfy a validator. They are now optional, and paired: an id without its
   hash is still refused, because it names content nobody can verify.
2. **Sessions were capped at eight participants.** Eight is RA2's player cap.
   The bound is now `MaximumParticipantsPerSession`, a control-plane resource
   limit that is not one game's number.
3. **At least two players were required.** That encodes ADR-001's assumption
   that clients simulate for each other. A dedicated server with one human is a
   legitimate session.

`TestNonRA2SessionShapesAreAccepted` keeps them removed.

## Leaks that remain, and require change

**1. Ordered-hash reconciliation cannot ever report agreement.** This is the
significant finding, and it is not about second runtimes at all.

`capture.OrderedHash` composes per-event digests of the canonical encoding, and
that encoding binds `producer_client_id`, `capture_id` and `received_at` into
every event. Two producers that observed exactly the same thing therefore hash
differently by construction, and reconcile as `inconsistent`. `OrderedHash`'s
own doc comment states the opposite intent: *"two producers that batched the
same events differently must still agree."* The implementation contradicts its
documented purpose.

RA2's two-client cross-check has the same defect. A second runtime is simply
what made anyone look. The fix is a contract decision rather than a patch: it
requires deciding what makes two observations *the same observation* across
producers, which at minimum means excluding producer identity, capture
identity and receive time from the compared digest, and deciding whether two
producers must agree on `event_id` for a fact they both witnessed.
`TestFindingOrderedHashAlwaysDivergesAcrossProducers` pins it.

**2. A single-authority execution can produce no evidence at all.**
Reconciliation requires at least two independent observations. That is correct
for cross-checking, but "evidence set" conflates *the record of what was
observed* with *the cross-check between observers*, and a server-authoritative
runtime has exactly one authority. Its observations are persisted,
broker-derived and hash-identified, and there is no way to publish them.

The runtime works around this the way such systems really do: a hot standby
replays the same deterministic world and enrolls as a second observer, which
is the divergence a server-authoritative runtime actually needs to detect. That
is a legitimate design, not a fix for the gap.

**3. Capture streams are minted per client, with no per-client opt-out.**
`mintCaptureLocked` opens a stream for every enrollment, gated only by a
session-wide `semantic_events` boolean. Ten player clients that produce nothing
by design each hold a stream. This is ADR-001 leaking again: if clients own the
simulation, every client is a producer.

**4. A producer cannot close a stream as legitimately empty.**
`CaptureCloseRequest.FinalSequence` is unsigned, so the smallest claim a
producer can make is that sequence 0 exists. A client that honestly observed
nothing must close with a gap it does not have, and is indistinguishable from
one that lost its first event. In this run that made a usable negative gate
control by accident, which is not a defence of it.
`TestFindingAnEmptyStreamCannotCloseCleanly` pins it.

## The leak accepted permanently

`game_tick` is in the canonical encoding, so every published event hash covers
a field only a tick-based game can fill. A runtime without ticks sends null, so
it bars no one; but the field cannot be removed, because the canonical encoding
is frozen and dropping it would reissue every hash this repository has
published. Recorded as permanent rather than fixed, which is what an
abstraction test is for. `TestFindingGameTickIsFrozenIntoEveryPublishedHash`
fails if the encoding moves.

## Standing

ERH-007's ordering rule asks that ERH-001..006 close first. This ran ahead of
ERH-006, which remains blocked on a Windows machine, an owned copy of the game,
and an unresolved spawner provenance gate. Nothing here closes ERH-006, and the
run does not claim to: a second adapter is what ERH-007's acceptance asks for,
and the leaks above are what it was for.
