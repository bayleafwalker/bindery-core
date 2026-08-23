# Decisions and open gates

## Settled decisions

### ADR-001 — Clients own simulation

Bindery hosts the match envelope and transport. It does not become an
authoritative RA2 server.

### ADR-002 — Application data is public by contract

Identity, session, participant, capture and telemetry records are public.
Credentials and transient connection details are security mechanics and are
excluded from public representations.

### ADR-003 — Identity is a bearer-token claim without recovery

Possession of the account token proves the claim. Lost token means lost
identity. A new identity is created; historical data is not reassigned.

### ADR-004 — Anti-cheat is permanently excluded

No cheat detection, integrity attestation, competitive adjudication, gameplay
bans or trusted rankings are planned. Availability controls and protocol
validation remain required.

### ADR-005 — No public lobby in the first target

Sessions are created by API/CLI and join credentials are shared out of band.
Known session records are public, but there is no discovery/listing surface.

### ADR-006 — Observer is a client class

Observation is expressed through a separately enrolled `observer` client when
the adapter supports it. The first two-player slice may capture through players,
but it may not hardcode “all clients are players.”

### ADR-007 — Telemetry is asynchronous and provenance-preserving

The relay does not decode semantic traffic in its forwarding hot path. Raw
source events remain immutable; normalization and consensus are derivations.

### ADR-008 — Active relays drain rather than migrate

Scaling adds/removes admission capacity. Existing matches remain pinned to
their relay until completion or explicit failure.

### ADR-009 — AI gameplay is a future client realization

Native AI gameplay, if explored, enrolls as another client/controller class and
uses explicit observation/action capabilities. It does not justify implementing
agent logic in the broker, relay or initial adapter.

### ADR-010 — Keep game-specific code outside core

RA2 launch files, hooks, offsets, packet compatibility and statistics parsing
belong in an adapter package/repository. Bindery Core owns generic capability,
enrollment, lease, telemetry and lifecycle contracts.

## Recommended but reversible choices

- unique immutable `handle`, non-unique mutable `display_name`;
- one `WorldInstance` per match during the experiment;
- one explicit public UDP endpoint per relay instance initially;
- control data in a relational store, raw batches/objects in object storage;
- two players as the mandatory first topology, observer added in the next wave;
- existing CnCNet tunnel as a baseline provider and native relay as the research provider.

## Open operator gates

These choices materially affect public deployment but do not block a local
research implementation:

1. **Data license:** CC0-like dedication, attribution license, or another explicit policy.
2. **Retention:** indefinite public history versus a declared finite service-retention window.
3. **Handle policy:** case folding, Unicode normalization and prohibited/reserved names.
4. **Abuse suspension:** what resource abuse permits token/session rejection and how it is recorded.
5. **Observer realization:** native game observer slot, injected headless client or post-process capture only.
6. **Baseline protocol:** CnCNet-compatible transport versus a Bindery-native wire protocol plus custom shim.
7. **Deployment boundary:** keep broker/relay in Bindery Core or use a sibling research repository.
8. **External-user launch:** legal/privacy review and contribution/publication terms.

## Decisions explicitly deferred until evidence exists

- a dedicated `ExternalClient` or `MatchSession` CRD;
- live semantic capture at game-tick granularity;
- NAT traversal/direct peer path;
- more than one observer;
- AI observation/action schemas;
- general multi-game adapter SDK.

## Next decision checkpoint

After Wave 2, review whether `WorldInstance` accurately carries match lifecycle
and whether runtime enrollment can remain broker-owned. Do not add a new CRD
because the noun sounds architectural; add it only if there is durable desired
state for a controller to reconcile.

