# RA2 channel: what it needs from Bindery Core

Recorded 2026-09-27.

The RA2 adapter repository now contains an on-demand broadcast channel. It
plays the private RA2 match back to back, captures one rendered client with
OBS to a local room stream (Twitch is optional), and can put an agent
controller in a player seat. The design and its status are in
[`bindery-ra2-adapter/docs/architecture/ra2-channel.md`](https://github.com/bayleafwalker/bindery-ra2-adapter/blob/main/docs/architecture/ra2-channel.md).

This note records what that work needs from Bindery Core, and what it does not
authorize. Like [`operator-gates.md`](operator-gates.md), it is recorded here
rather than in the immutable research pack.

## No control-plane contract changes

The channel changes no endpoint, DTO, schema or CRD in this repository.
Everything it uses already exists:

- **Back-to-back matches** are ordinary sessions: one session, one placement
  and one set of enrollments per match, with fresh idempotency keys each time.
- **The spectator client** enrolls as `observer` under the existing
  `participant_policy.maximum_observers` limit (`OBSERVER_CAPACITY_EXCEEDED`
  above it). ADR-006 already made the observer a client class.
- **Replays and decision traces** fit the existing capture-object lane
  (`POST /v1/captures/{capture_id}/objects`), which is content-addressed and
  takes any concrete media type. `application/x-ndjson` is a reasonable choice
  for a decision trace. The adapter does not upload them yet.

## Broadcast stays outside core

OBS, MediaMTX, the Twitch relay and the channel's match records live in the
adapter repository and its lab deployment. This follows ADR-010: the scenes,
captured window and audio inputs are specific to RA2 and to one lab.

It also keeps two secrets out of this repository: the OBS websocket password
and the Twitch stream key. Neither belongs anywhere near a public DTO, and the
redaction rule (no field ending in `url`, `endpoint`, `password` and so on)
would reject the shapes that carry them anyway. The video stream is not
application data under ADR-002. It is a rendering of one client, not evidence
about the execution.

## Agent seats are a realization of ADR-009, not a new client class

ADR-009 says native AI gameplay "enrolls as another client/controller class
and uses explicit observation/action capabilities". The channel's first step is
narrower. A controller runs inside a **player** client's seat, so the
enrollment class stays `player`. The adapter filters the spectator-grade
stream down to what that house may know, and fails closed where the bridge
gives no ownership or visibility.

This deliberately stops short of the deferred items in the research pack:

- no new client class and no controller field on enrollment;
- no AI observation or action schema in `contracts/`;
- no agent logic in the broker or relay.

An observation/action schema should be promoted here only once a real
controller has played through a real per-house command path. The adapter's
filter conventions (`house`, `visible_to`, `winner` payload fields) are
candidate evidence for that schema, not a contract.

## Gate 5 and ERM-401 remain open

Gate 5 (observer realization) is **not** resolved by this work. The adapter
now implements one candidate, the native game spectator slot: a third cloned
client with `IsSpectator=Yes`, its own tunnel port and an `observer`
enrollment. No lab run has exercised it. ERM-401's acceptance criterion
(measure and document the observer's game-protocol cost and visibility) still
needs that run.

The adapter follows ERM-402's intended semantics already. A failed or
undeparted observer marks the run `observer_degraded` and does not change
the players' lifecycle completeness. That matches the broker's existing rule
that a degraded observer is a worse witness, not a failed player.

## Candidate core changes, not made

These would each be a new concept or field, so per `AGENTS.md` they need
asking about first:

1. A declared controller on player enrollment (human, built-in AI, or an agent
   ID and version), so public records say which seats were agent-driven.
2. A media-type convention for decision traces among capture objects.
3. Serving `GET /v1/objects/{content_hash}`, so replays uploaded through
   captures can be fetched back.

None of these blocks the channel's first build step, which broadcasts a
two-player match.
