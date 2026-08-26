# Requirements

Requirement identifiers are stable within this research pack. `MUST` denotes
the first credible end-to-end slice; `SHOULD` denotes the intended research
target; `MAY` is an explicit extension point rather than implied scope.

## Functional requirements

### Identity

- **ID-01 MUST:** Create an identity claim from a requested handle and return a high-entropy bearer token once.
- **ID-02 MUST:** Store only a verifier/hash of the bearer token.
- **ID-03 MUST:** Associate sessions, client enrollments and telemetry with a stable public `account_id`.
- **ID-04 MUST:** Expose identity metadata publicly, excluding credentials and transient connection metadata.
- **ID-05 MUST:** Treat loss of the token as loss of the identity; the user creates another identity.
- **ID-06 SHOULD:** Permit token rotation only when the current valid token is presented.
- **ID-07 MUST NOT:** Implement email, password, federation, recovery questions or operator-assisted account recovery.

### Session control

- **SES-01 MUST:** Create a non-discoverable match session with a compatibility manifest and participant limits.
- **SES-02 MUST:** Return a time-bounded join credential to the session creator.
- **SES-03 MUST:** Enroll at least two clients and issue each a session/client lease.
- **SES-04 MUST:** Support client classes `player` and `observer` in the contract.
- **SES-05 MUST:** Publish non-secret session state and participant metadata publicly.
- **SES-06 MUST:** Record lifecycle transitions and the client report/probe that caused them.
- **SES-07 SHOULD:** Reject incompatible game, adapter, mod or map hashes before transport admission.
- **SES-08 SHOULD:** Expire sessions that never reach readiness and end sessions after all leases disappear.
- **SES-09 MUST NOT:** Provide a public browse/list endpoint in the initial target.

### Transport

- **NET-01 MUST:** Allocate one explicitly identified UDP relay to a match.
- **NET-02 MUST:** Bind opaque participant identifiers to observed UDP endpoints only after authenticated enrollment.
- **NET-03 MUST:** Enforce packet-size, packet-rate, session, client and source limits.
- **NET-04 MUST:** Keep game packets opaque to the relay's semantic layer.
- **NET-05 MUST:** Export transport metrics independently of packet forwarding success.
- **NET-06 SHOULD:** Select a relay from participant latency, capacity and region constraints.
- **NET-07 MUST:** Drain active relay instances; never promise transparent live migration.
- **NET-08 MAY:** Add NAT traversal or direct peer negotiation after relay-only operation is proven.

### Observation and telemetry

- **OBS-01 MUST:** Accept telemetry through a sideband path separate from game packet forwarding.
- **OBS-02 MUST:** Preserve source, sequence, capture method, adapter version and receive time for every event batch.
- **OBS-03 MUST:** Publish accepted raw and normalized telemetry as public data.
- **OBS-04 MUST:** Retain contradictory observations rather than silently choosing a winner.
- **OBS-05 MUST:** Label derived facts as derivations and retain links to their source observations.
- **OBS-06 SHOULD:** Separate frequent events/metrics from heavy snapshots or checkpoints.
- **OBS-07 SHOULD:** Use bounded local buffering, batched push and acknowledgement on client adapters.
- **OBS-08 SHOULD:** Enroll an observer as a separate client instance when the game adapter supports it.
- **OBS-09 MAY:** Accept live semantic events from player clients before an observer exists.

### Publication

- **PUB-01 MUST:** Make known session, identity, enrollment and capture records retrievable without authentication.
- **PUB-02 MUST:** Make publicness explicit in creation and enrollment responses.
- **PUB-03 MUST:** Exclude bearer tokens, join credentials, relay credentials and raw source endpoints from all public representations.
- **PUB-04 SHOULD:** Publish immutable raw batches through content-addressed objects.
- **PUB-05 SHOULD:** Provide cursor-based retrieval for normalized event streams.
- **PUB-06 MUST:** Define retention and data-license policy before accepting unrelated third-party users.

## Non-functional requirements

- **NFR-01:** Telemetry failure must not interrupt game traffic.
- **NFR-02:** The relay forwarding hot path must not synchronously depend on the control database or telemetry sink.
- **NFR-03:** Session and capture identifiers must be globally unique and non-semantic.
- **NFR-04:** All public records must carry schema/version information.
- **NFR-05:** Ingest must be idempotent by `(capture_id, producer_id, sequence)` or an equivalent stable key.
- **NFR-06:** Admission decisions must be reproducible from recorded inputs and placement policy versions.
- **NFR-07:** Relay capacity must include active endpoints, packet rate and egress, not CPU alone.
- **NFR-08:** Client disappearance must converge to an explicit state without operator repair.
- **NFR-09:** Secrets must be redacted from structured logs and rejected from telemetry payload fields where feasible.
- **NFR-10:** A captured raw batch must remain interpretable after normalizer changes through pinned schema and adapter versions.

## Excluded requirements

These are not deferred backlog; they are outside the charter:

- authoritative winner determination;
- cheat detection or client integrity scoring;
- gameplay sanctions, reputation or ranking;
- identity recovery;
- public matchmaking/lobby/chat;
- private sessions or private telemetry;
- guarantees that a public record can later become private.

