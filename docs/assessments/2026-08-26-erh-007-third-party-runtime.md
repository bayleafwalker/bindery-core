# ERH-007, second run: a game this repository does not control

Date: 2026-08-26. Reproduce with `make openttd-acceptance`, which fetches a
pinned OpenTTD into a user cache and runs
`go test ./adapters/bindery-openttd-runtime/...`. The findings that do not need
a game installed are pinned in
`go test ./internal/externalruntime -run TestFinding -v`.

This is the run the first ERH-007 assessment asked for without being able to
do. That assessment
([`2026-08-26-erh-007-second-runtime.md`](2026-08-26-erh-007-second-runtime.md))
met the item's acceptance criteria against `adapters/bindery-dedicated-runtime`
and then named its own limit: a second adapter, written by someone who could
read the contracts while writing it, is a lower bound on what a third-party
runtime hits. This run tests the bound.

## What was run

OpenTTD 15.3, unmodified, from the project's own published Linux build. The
game has never heard of Bindery and offers no way to teach it: its integration
surface is a command line and an admin network protocol, both fixed years
before this project existed.

`adapters/bindery-openttd-runtime` implements that protocol from its
specification -- a uint16-framed binary TCP protocol, joined and decoded by the
adapter, with no Bindery type crossing into it -- and drives the control plane
over HTTP with its own re-declared DTOs.

The acceptance run starts a dedicated server that generates a 256x256 map from
a seed, connects two independent admin applications, joins three real game
clients over the network as separate processes, records what the game published
to each observer, kills the broker outright and restarts it on the same state
file, and reconciles the evidence with gate controls. Between sixty and seventy
observations reach each observer per run -- client joins, company creation,
console output, and the game's own command log with its execution frames --
and the count differs run to run, because it is a real game and not a fixture.

Both observers see the same events within a run. The adapter derives each
event's id from the observed content alone, so the two streams are identical
event for event, id included.

## What this establishes that the dedicated runtime could not

Four things, in the order they matter.

The contracts fit a runtime whose integration surface was not designed for
them. Session, placement, execution, enrollment, capture, gate, and evidence
contracts carried the run without modification, and core was not changed at all
for it: the removals ERH-007 already made were sufficient.

The observations are the game's, not the adapter's. What is recorded is what
OpenTTD chose to publish, in the shape it chose, including a command log the
game's own documentation warns is unstable across versions. The adapter records
the length of each command's parameter block rather than its content, because
the content is explicitly not a contract.

A game published on multiple platforms exposes an assumption RA2 could not.
See finding 5.

Two independent observers of one game are a real question rather than a
constructed one, and asking it found finding 6.

## What this still does not establish

It is not a commercial, closed-source title, and it is not a run by anyone
outside this repository. OpenTTD is open source, which is why its protocol
could be implemented from its own header file rather than from observation, and
the whole run is orchestrated by a harness written here.

The admin connection uses the protocol's unsecured login, which OpenTTD
disables by default and this run's generated server config re-enables over
loopback. A production integration would implement the X25519 handshake. That
is work, not a finding.

The observers are admin applications watching the server, not players. In
OpenTTD nothing else can honestly witness the game: clients are told what
happened. That is a property of the game, and it is the same property the
dedicated runtime was built to have -- so the two runs agree about the shape of
server-authoritative games, which is worth something, but neither of them tests
a runtime where clients genuinely witness.

## Findings confirmed against a game nobody here wrote

**Ordered-hash reconciliation still cannot report agreement.** Two admin
applications recorded byte-identical observations, in the same order, with the
same content-derived event ids, and their ordered hashes still differ, because
the canonical encoding binds `producer_client_id`, `capture_id` and
`received_at` into every event. This is the strongest form the first
assessment's finding can take: agreement was made as easy as it can be made,
and the method still reports `inconsistent`.

**Capture streams are minted per client with no opt-out.** Three real game
clients each hold a stream they cannot write to, because OpenTTD clients
witness nothing.

**A producer cannot close a stream as legitimately empty.** Those three streams
close claiming a gap at sequence 0 that does not exist.

**`game_tick` bars nobody.** Most of what OpenTTD publishes has no tick, and
the field goes null; the game's command log carries an execution frame, and it
is filled. A single stream mixes both, and the encoding accepts it. The leak
stays permanent for the reason already recorded: removing it would reissue
every published hash.

## Findings this run added

**5. Every participant must run a byte-identical build of the game.**
Enrollment refuses any client whose `game_hash` differs from the session's.
OpenTTD ships Windows, macOS and Linux builds of one release that play together
over the network; under this rule the second platform cannot be enrolled at
all. Red Alert 2 ships one platform's executable, so nothing in the RA2 slice
could have surfaced it.

The cause is that `game_hash` carries two meanings at once: "which build am I
running", which is provenance, and "are we playing the same thing", which is
compatibility. Only the second belongs in an enrollment check. Deciding what
makes two builds the same game is a contract decision, so it is recorded rather
than patched. Pinned by
`TestFindingEnrollmentRequiresByteIdenticalGameBuilds`, and demonstrated in the
acceptance run against the sha256 the OpenTTD project publishes for the Windows
build of the same release.

**6. An evidence set does not record what interval each observer watched.**
Two admin connections to one server differ by exactly one event when they
connect at different moments: the earlier one observes the later one arriving,
and the later one cannot observe its own arrival. Both are honest. Exact-count
reconciliation reports `inconsistent`, and nothing in the evidence set
distinguishes "these observers saw different things" from "these observers
watched different amounts of the same thing".

The adapter works around it by bounding both recordings between two facts in
the game's own history -- a throwaway admin connection announces the start, the
last client's departure ends it -- and subscribing to nothing time-driven, so
neither recording depends on a clock. That is available only because one
harness owns both observers. Two independently operated observers of the same
execution have no such option, and the evidence set has nowhere to say so.
Pinned by `TestFindingEvidenceSetsRecordNoObservationInterval`.

## Two notes about the game's own contract

Neither is a Bindery finding, and both are the reason a run like this is worth
more than a second adapter.

`docs/admin_network.md` describes `ADMIN_PACKET_SERVER_COMPANY_INFO` as ending
with an `is_ai` boolean. The implementation sends one more byte after it. A
decoder written from the documentation alone, and tested only against packets
it also wrote, would be wrong and would not know.

The same document describes an unsecured JOIN packet without mentioning that
current servers refuse it unless a setting is changed. A published contract and
its implementation drift; running against the implementation is the only way to
find out where.

## Standing

ERH-007 stays `implemented-with-findings`. Its acceptance criteria were met by
the first run and are met again here with a materially harder runtime; what is
open is the findings, which now number six, one of them permanent by decision.
The item closes when they are resolved or formally accepted, not when another
runtime runs.

The ordering rule still holds: this ran ahead of ERH-006, which remains blocked
on a Windows machine, an owned copy of Red Alert 2, and an unresolved spawner
provenance gate. Nothing here closes ERH-006.
