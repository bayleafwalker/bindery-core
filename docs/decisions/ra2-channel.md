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

## What the channel uses from the control plane

The channel needs no new endpoint beyond the three changes recorded below.
Everything else it uses already existed:

- **Back-to-back matches** are ordinary sessions: one session, one placement
  and one set of enrollments per match, with fresh idempotency keys each time.
- **The spectator client** enrolls as `observer` under the existing
  `participant_policy.maximum_observers` limit (`OBSERVER_CAPACITY_EXCEEDED`
  above it). ADR-006 already made the observer a client class.
- **Replays and decision traces** fit the existing capture-object lane
  (`POST /v1/captures/{capture_id}/objects`), which is content-addressed and
  takes any concrete media type. Decision traces use
  `application/vnd.bindery.decision-trace.v1+ndjson` (below). The adapter does
  not upload either yet.

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

A player enrollment may now *declare* its controller (below), so a public
record says which seats were agent-driven. That is a label on a player seat,
not a new class. The work still stops short of the deferred items in the
research pack:

- no new client class;
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

## Core changes made for the channel

The three candidate core changes this note first listed were approved and
implemented on 2026-09-27. Each is documented in
[`contracts/externalruntime/v1/README.md`](../../contracts/externalruntime/v1/README.md),
with the shapes in `openapi.yaml` and the JSON schemas beside it.

1. **Declared controller on player enrollment.** The enrollment request takes
   an optional `controller`
   (`{"kind": "human" | "builtin_ai" | "agent", "controller_id", "controller_version"}`),
   and the public enrollment echoes it. An agent must name an id and a version;
   the other kinds carry neither (`CONTROLLER_INVALID`). An observer may not
   declare one (`CONTROLLER_NOT_ALLOWED`). An absent controller is undeclared,
   not human. The declaration is persisted with the enrollment, and state files
   written before it existed still load. Code:
   `internal/externalruntime/types.go` and `validateController` in
   `internal/externalruntime/service.go`.
2. **A media type for decision traces.**
   `application/vnd.bindery.decision-trace.v1+ndjson`, exported as
   `externalruntime.DecisionTraceMediaType` in
   `internal/externalruntime/capture_object.go`. Core does not parse or
   validate a trace. No RA2-specific media type is defined (ADR-010).
3. **`GET /v1/objects/{content_hash}` is served**, so replays and traces
   uploaded through captures can be fetched back. It is a public known-ID read
   that returns the stored bytes under their stored media type, with the
   content hash as `ETag` and an immutable `Cache-Control`. Code: `GetObject`
   in `internal/externalruntime/capture_object.go` and `getObject` in
   `internal/externalruntime/http.go`.

None of these blocks the channel's first build step, which broadcasts a
two-player match. None of them resolves gate 5 or ERM-401.
