# Operator gate resolutions

`docs/research/external-runtime-multiplayer/10-decisions-and-open-gates.md`
lists eight open operator gates. That pack is immutable input, so resolutions
are recorded here rather than by editing it, following the same convention
`contracts/externalruntime/v1/README.md` uses for divergences.

Each entry states what was decided, why, and — deliberately — what the decision
does not authorize. A resolved gate removes a blocker; it does not by itself
implement anything.

## Gate 1 — Data license: CC0-like dedication

Resolved 2026-08-26.

Published application data carries a public-domain dedication. ADR-002 already
makes application data public by contract, so the remaining question was only
what a downstream consumer may do with it, and a research corpus with no
third-party users yet has nothing to gain from reuse friction.

Attribution was considered and rejected on a specific ground: the records are
machine-generated and hash-identified, and the corpus has no authorship story
that "attribute to whom" answers cleanly.

This covers application data — sessions, enrollments, captures, normalized
events, evidence sets. It says nothing about the source code license, and
nothing about the CnCNet license boundary in `sources.md`, which is a separate
question about derivation and is addressed by gate 6.

## Gate 2 — Retention: indefinite, with the ceiling documented

Resolved 2026-08-26.

Public history is retained indefinitely, and the durability ceiling is to be
measured and published alongside that claim.

This declares what the implementation already does rather than adding a policy
the code does not honor. There is no `delete` in the non-test external-runtime
sources: the snapshot's growth term is total history. A finite service-retention
window is therefore not a policy toggle but unbuilt work — deletion, snapshot
compaction, and reconciliation of evidence sets that reference removed captures.

The obligation this creates is the measurement. "Indefinite" without a number is
the failure mode `docs/standards/production-readiness.md` warns about in its own
preamble.

That obligation is discharged: see
`docs/assessments/2026-08-26-durability-ceiling.md`. Roughly 7,000 two-player
sessions of accumulated history fit under `MaxStateSnapshotBytes`, and one
mutation costs about 195 ms at half that. Retention is therefore indefinite up
to a measured limit, and the assessment records a failure mode worth fixing
independently of any retention policy: `Save` will write a snapshot `Load`
refuses, so a service can persist itself into a state it cannot restart from.

## Gate 6 — Baseline protocol: Bindery-native, CnCNet as a black box

Resolved 2026-08-26.

This repository implements the Bindery-native wire protocol only. The baseline
provider is an unmodified, separately deployed CnCNet server, spoken to as a
black box. No CnCNet-compatible transport is implemented here.

`sources.md` records that both inspected CnCNet repositories are GPLv3 and that
reuse, derivation, or wire-compatible clean-room implementation is a deliberate
project and legal decision. Declining to implement wire compatibility means that
decision never has to be taken, and `sources.md` already names a separately
deployed baseline as the least committal research starting point.

The cost is accepted and named: ERM-501 compares two providers rather than
demonstrating one unchanged client contract driving both. Where the contracts
diverge, ERM-501's acceptance criterion permits evidencing the incompatibility,
which is the weaker but honest outcome.

## Gate 7 — Deployment boundary: broker and relay in Bindery Core

Resolved 2026-08-26; implemented the same day.

`internal/relay` runs inside `bindery-external-runtime` under
`BINDERY_RELAY_PROVIDER=bindery-native`. See `relay_seam_note` in
`docs/roadmap/post-ra2-hardening.yaml` for what that closes and what it claims.

The in-process boundary is load-bearing rather than incidental. Enrollment mints
a client's transport key, returns it once and retains only a sha256 verifier, so
the key can reach the relay only during the enrollment call itself. Spanning a
process boundary requires an authenticated, encrypted admin channel first.

`cmd/bindery-udp-relay` remains a separately deployable, env-configured relay.
This gate did not remove it, and ERM-503's scale-out and drain work will want
that topology.

## Consequence for PUB-06

PUB-06 ("define retention and data-license policy before accepting unrelated
third-party users") is satisfied by gates 1 and 2 together.

`GET /v1/objects/{content_hash}` is currently not served, and
`contracts/externalruntime/v1/README.md` gives the undefined PUB-06 as the
reason. That reason is now gone. The endpoint remains unserved because nobody
has implemented it — which is a different statement, and the contract README
should not be read as still blocking on policy.

## Gates still open

3 (handle policy), 4 (abuse suspension), 5 (observer realization), and
8 (external-user launch). Gate 5 is the one with a dependency: ERM-401 is
gated on it.
