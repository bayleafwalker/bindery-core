# Validation plan

## Strategy

Validate one uncertainty at a time. The relay is easy to demonstrate with
synthetic packets; the client adapter is easy to blame for everything. Keeping
those stages separate prevents a week of reverse engineering from being
misdiagnosed as a Kubernetes networking problem.

## Wave 0 — contract proof

Build:

- identity, session and telemetry schemas;
- public/authenticated representation split;
- in-memory broker and fake relay allocator;
- synthetic client harness.

Acceptance:

- identity token is returned once and only its verifier is stored;
- lost-token flow creates a new identity without altering the old one;
- session can enroll two players and one observer class in the model;
- public JSON never contains credential/endpoints fixtures;
- duplicate create/batch requests are idempotent;
- no public session-list endpoint exists.

Stop condition: if the contract needs RA2-specific fields outside the opaque
compatibility/adapter metadata, move those fields back into the adapter schema.

## Wave 1 — synthetic two-client relay

Build:

- one region, one relay allocator and at least two relay instances;
- explicit relay endpoint allocation;
- authenticated UDP endpoint registration and opaque forwarding;
- relay heartbeat/expiry, packet limits and network metrics.

Acceptance:

- two clients exchange bidirectional opaque datagrams through one allocation;
- a third unenrolled endpoint cannot use the allocation;
- malformed, oversized and rate-exceeding packets are dropped and counted;
- telemetry sink failure does not measurably block forwarding;
- draining prevents new allocation but preserves the active synthetic match;
- forced relay loss produces explicit failure and partial capture closure.

## Wave 2 — RA2 two-player vertical slice

Build:

- Windows adapter with identity/session enrollment;
- compatibility hashing and launch-configuration rendering;
- RA2/YR launch and process lifecycle reports;
- transport adaptation compatible with the selected relay protocol.

Acceptance:

- two separate machines join via an out-of-band join credential;
- incompatible map/mod/game fixtures fail before launch;
- a full match starts, exchanges game traffic and exits;
- session lifecycle converges without manual database edits;
- no lobby, chat, account recovery or ranking implementation is required.

Stop condition: if stock-client compatibility requires a large injected runtime,
split it into a dedicated adapter repository before adding further core changes.

## Wave 3 — telemetry and public capture

Build:

- relay metrics capture;
- player adapter batch queue and acknowledgement;
- post-match dump/object upload;
- raw object manifests, normalized events and public queries;
- completeness/gap reporting.

Acceptance:

- ingest outage produces bounded local buffering while the match continues;
- retry produces one raw object/event range, not duplicates;
- post-match statistics remain linked to source client and adapter version;
- contradictory player observations coexist;
- a failed normalizer can be rerun from raw capture;
- all known public session/capture endpoints require no authentication;
- all mutation/ingest endpoints still require scoped credentials.

## Wave 4 — observer client

Build:

- `observer` admission and compatibility feature negotiation;
- RA2 observer realization or a documented alternative capture mechanism;
- observer-specific capture stream and degraded-capture semantics.

Acceptance:

- two players behave identically with or without the observer;
- observer failure does not terminate the match;
- observer events remain attributable and do not overwrite player streams;
- relay/admission accounting includes the observer's real protocol cost;
- capture completeness records observer coverage and loss.

Rollback: disable observer admission in the Booklet/session policy; retain the
two-player and player-source telemetry path.

## Wave 5 — Bindery research experiments

Experiments:

- substitute baseline CnCNet tunnel and native relay providers;
- select between two regions using participant probes and capacity;
- scale accepting relay capacity under synthetic packet/session load;
- drain and upgrade relays without moving active matches;
- compare local player capture, observer capture and relay metrics;
- test client-low-latency buffer plus server/object storage tier behavior.

Evidence required:

- replayable manifests and pinned contract/provider versions;
- placement inputs and deterministic decision traces;
- latency, packet loss and forwarding saturation results;
- capture gap/completeness reports;
- failure-injection timeline and recovery outcome;
- conclusion identifying which Bindery abstractions held, leaked or need change.

## Definition of research completion

The track is complete when it can support an evidence-backed conclusion about
external runtime binding, not when it resembles a consumer multiplayer service.
A public lobby would add users and UI; it would not answer the research question.

