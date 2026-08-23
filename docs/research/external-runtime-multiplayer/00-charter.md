# Research charter

## Decision

Extend Bindery Core with an **external-runtime multiplayer** research track.
Bindery will manage the match envelope while player-owned clients continue to
execute the game and its deterministic multiplayer simulation.

This is core research because it tests whether capability binding survives a
provider/consumer boundary that Kubernetes cannot schedule, restart or inspect
directly. RA2 is the first stress case, not a domain object that belongs in the
core API.

## Primary research question

Can Bindery reliably compose a match from:

- externally executed player and observer clients;
- a latency-selected, capacity-constrained UDP relay;
- session-scoped enrollment and transport leases;
- asynchronous semantic and transport telemetry;
- public, provenance-preserving capture storage;
- lifecycle and failure state that remains intelligible when clients disappear?

## Target outcome

A successful research slice demonstrates all of the following:

1. Two independently operated clients claim identities and join a non-discoverable match.
2. Bindery allocates a relay and issues session-scoped transport credentials.
3. Both clients launch the same compatible game configuration and complete a match.
4. The relay exports transport metrics without decoding semantic game traffic.
5. At least one client adapter exports semantic or post-match telemetry through a separate lane.
6. Public APIs expose the session, participants, capture provenance and telemetry.
7. No bearer token, join credential or raw transient endpoint is exposed through the public dataset.
8. A third client can eventually join as `observer` without changing the player enrollment contract.
9. Relay scale-out affects new admissions; existing matches drain rather than migrate.

## Permanent boundaries

The project does not build or roadmap:

- authoritative game simulation;
- anti-cheat, cheat scoring, integrity attestation or gameplay bans;
- competitive adjudication or trusted rankings;
- recovery of lost identity tokens;
- a full public lobby, chat community or matchmaking network;
- transparent migration of an active UDP match between relays.

Protocol validation, schema validation, rate limiting and denial-of-service
controls remain required. Preventing a relay from becoming an unusually
nostalgic packet amplifier is operations, not anti-cheat.

## Initial topology

The implementation must support an arbitrary participant list in its contract,
but the acceptance topology is deliberately bounded:

- required: two `player` clients;
- optional in the first executable slice: one `observer` client;
- excluded initially: dedicated spectators beyond the capture observer, bots,
  AI controllers and public session discovery.

The observer is a first-class client role. For RA2, its concrete adapter may
initially need to join using the game's existing observer mechanics. Bindery
must not pretend that a control-plane role can manufacture passive protocol
support that the game does not have.

## Core-value test

This track earns its place in Bindery only if it produces general findings
about at least three of these boundaries:

- dynamic external client enrollment versus static capability resolution;
- NFR placement using participant latency and relay capacity;
- stateful UDP admission, draining and autoscaling;
- public observation with untrusted, multi-source provenance;
- client-local versus cluster-local storage tiers;
- capability substitution between a baseline tunnel and a native relay.

If it becomes mostly a Windows launcher project, keep the adapter in a separate
repository and retain only the generic contracts and findings in Bindery Core.

