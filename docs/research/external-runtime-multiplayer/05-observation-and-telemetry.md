# Observation and telemetry

## Observation model

The observation plane accepts that no single client is inherently authoritative.
Every captured fact is stored with provenance; confidence and consensus are
derived later.

Preferred source order is contextual rather than absolute:

1. Relay for transport facts it directly measures.
2. Observer client for broad semantic visibility.
3. Player client for local semantic and process facts.
4. Post-match dump for final aggregates exposed by the game.

A higher item in this list does not overwrite a lower one. The sources measure
different things and may disagree.

## Separate client class

The control contract supports `observer` from the start. Implementation proceeds
in two stages:

### Stage A: capture without a separate observer

- two player clients enroll;
- each adapter emits process lifecycle and whatever semantic capture is available;
- the relay emits network metrics;
- post-match dumps are uploaded after process exit.

This proves the transport and telemetry paths without making observer support a
prerequisite for the first RA2 match.

### Stage B: separately enrolled observer

- a third client enrolls with class `observer`;
- the placement/admission layer accounts for its relay and game-protocol slot;
- its failure marks the capture degraded but does not terminate gameplay;
- its events use a distinct `capture_id` and producer identity;
- player-produced streams remain available for comparison.

If RA2 requires the observer to act as an ordinary lockstep peer, the adapter
must say so in its capability/features. Bindery does not label a normal peer
“passive” and hope the packets appreciate the distinction.

## Three telemetry lanes

### 1. Operational metrics

Low-cardinality metrics for service operation:

- active sessions/endpoints;
- packets and bytes by relay/session direction;
- packet drops and rate-limit events;
- relay queue pressure, event-loop lag, CPU and egress;
- broker/ingest latency and error counts;
- adapter heartbeats and buffered-byte gauges.

Avoid account or session identifiers as unbounded Prometheus labels. Link
detailed diagnostics through logs/events instead.

### 2. Semantic event batches

Moderate-volume ordered events such as:

- match lifecycle;
- player actions and commands;
- construction, destruction, captures and kills;
- resource/cash changes;
- alliances, outcomes and disconnects.

Client behavior:

- append locally to a bounded durable log or RAM buffer according to capture policy;
- assign monotonically increasing sequence numbers per capture;
- batch by size/time;
- push asynchronously;
- retain until acknowledged or until the declared overflow policy applies;
- never block the game thread on remote acknowledgement.

The term used for the local buffer matters less than its actual durability and
overflow behavior. A queue does not become more reliable merely by wearing a
WAL name badge.

### 3. Heavy capture/checkpoint objects

Large or infrequent artifacts travel separately:

- `stats.dmp` or equivalent post-match dumps;
- replay files;
- state snapshots/checkpoints;
- diagnostic crash bundles permitted by capture policy.

Upload these as content-addressed objects with a small manifest in the event
stream. Do not insert multi-megabyte snapshots into the hot semantic event path.

## Event envelope

Every accepted semantic event carries:

- `schema` and `schema_version`;
- `event_id`, `session_id`, `capture_id` and `producer_client_id`;
- capture method and adapter version;
- producer sequence;
- producer time when available and mandatory receive time;
- semantic type and versioned payload;
- optional raw-object/source references;
- public provenance and derivation metadata.

See `schemas/telemetry-event.schema.json`.

## Ordering and delivery

- Ordering is guaranteed only within one `capture_id` by sequence.
- Delivery is at least once from adapters; ingest deduplicates idempotently.
- Gaps are retained as explicit capture-gap records or manifest ranges.
- Cross-producer ordering is derived from game tick/time and receive time; it is
  not fabricated into a total order.
- Late events remain appendable after session end and carry their receive time.

## Normalization

Raw source-specific events remain immutable. Normalizers produce a generic
public schema such as:

- `game.match.lifecycle`;
- `game.player.resource-changed`;
- `game.entity.constructed`;
- `game.entity.destroyed`;
- `game.player.action-observed`;
- `game.capture.gap`.

Normalization is versioned and replayable. Updating a normalizer creates a new
derived dataset; it does not mutate the original capture.

## Completeness

Every closed capture publishes a completeness manifest:

- expected and observed sequence ranges;
- missing ranges;
- start/end reason;
- adapter overflow/drop counters;
- clock quality;
- source coverage;
- raw object hashes;
- normalizer versions applied.

This is more useful than an unexplained `complete=true`, a boolean with the
evidentiary depth of a cheerful shrug.

