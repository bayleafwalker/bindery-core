# External runtime contract v1

These files are the implementation boundary promoted from the research pack.
The source pack remains unchanged under `docs/research`. Public DTOs contain
application data only. Authenticated DTOs are separate and may contain a
credential exactly once at creation or enrollment time.

Invariants:

- bearer values are 256-bit base64url strings and only SHA-256 verifiers are
  retained;
- public reads are known-ID reads; there is no session collection endpoint;
- all create/report operations accept an idempotency key;
- raw source endpoints, authorization headers, upload credentials, join tokens,
  lease tokens, and transport credentials are never public fields or logs;
- identifiers are UUIDv7 values and wire timestamps are RFC 3339 UTC;
- client classes in v1 are `player` and `observer`.
- identity, session, placement and execution records survive a reference-service
  restart and resolve by stable identifier;
- every placement names the allocator implementation repository, exact revision
  and configuration digest that produced it;
- observations refer to an execution; evidence sets retain every compared
  stream and record a reconciliation method and outcome;
- exact event-count equality is reconciliation policy #1. It establishes stream
  consistency at that level only, not semantic truth;
- gate results use `PASS`, `FAIL`, `NOT_APPLICABLE`, `UNRESOLVED`, or `ERROR`;
  consequential gates require known-pass and known-fail calibration evidence;
- raw observations are immutable. Normalization is additive and versioned:
  replaying a normalizer version returns the existing derivation, and a new
  version creates another dataset beside it rather than replacing one;
- a derived capture is published as a capture in its own right, carries
  `producer_class: normalizer` and a `derivation` on every event, and is never
  eligible as an independent observation in an evidence set;
- where an execution has captured streams, observation summaries are computed
  by the broker from persisted events. Client-supplied summaries are refused
  for those executions, and every summary records which of the two it is.

## Declared seat controllers

`POST /v1/sessions/{session_id}/enrollments` accepts an optional `controller`
object, echoed as `controller` on the public enrollment:

```json
{"kind": "agent", "controller_id": "bindery.ra2-agent", "controller_version": "0.1.0"}
```

- `kind` is `human`, `builtin_ai` or `agent`. Any other value is refused with
  `CONTROLLER_INVALID` (400).
- `controller_id` and `controller_version` are required for `agent` and must be
  absent or empty for the other kinds (`CONTROLLER_INVALID`). Each is 1-128
  characters of `[A-Za-z0-9._:@/+-]`, starting with a letter or digit:
  `^[A-Za-z0-9][A-Za-z0-9._:@/+-]{0,127}$`.
- Only a `player` may declare one. An `observer` that sends `controller` is
  refused with `CONTROLLER_NOT_ALLOWED` (400).
- An absent `controller` means undeclared. It is not defaulted to `human`, and
  the public enrollment omits the field.
- The declaration is the client's claim, validated for shape and not verified.
- It is part of the enrollment request, so re-enrolling the same client
  instance with a different controller is `IDEMPOTENCY_CONFLICT` (409), like any
  other change to the body.

This records which seats were agent-driven. It is not a new client class and
not an observation/action schema; see `docs/decisions/ra2-channel.md`.

## Capture objects

`POST /v1/captures/{capture_id}/objects` stores opaque bytes under their sha256
and a producer-declared media type matching
`^[a-z0-9]+/[a-z0-9][a-z0-9.+-]{0,126}$`. `GET /v1/objects/{content_hash}`
returns them:

- the body is the stored bytes and `Content-Type` is the media type they were
  stored under;
- `ETag` is the quoted content hash (`"sha256:<hex>"`), `If-None-Match` yields
  `304`, and `Cache-Control` is `public, max-age=31536000, immutable`;
- the response carries `X-Content-Type-Options: nosniff` and
  `Content-Security-Policy: sandbox`, because the media type is the producer's
  declaration and must not cause active content to run on this origin;
- a malformed hash is `OBJECT_HASH_INVALID` (400), a hash no capture object has
  is `OBJECT_NOT_FOUND` (404), and bytes that are missing or no longer match
  their hash are `OBJECT_UNREADABLE` (500), all in the error schema;
- one capture cannot record a hash under two media types, but two captures
  can. The media type served is then the one recorded first: earliest
  `received_at`, then lowest `capture_id`.

### Decision traces

`application/vnd.bindery.decision-trace.v1+ndjson` is the media type for an
agent controller's decision trace uploaded as a capture object (Go:
`externalruntime.DecisionTraceMediaType`). The body is newline-delimited JSON.
This is a naming convention only: core does not parse, validate or interpret a
trace, and stores and serves it like any other object. It is deliberately
game-neutral; a runtime-specific format belongs in that runtime's adapter
(ADR-010), and core defines none.

## Closing an empty capture

`final_sequence` on `POST /v1/captures/{capture_id}:close` is nullable. `null`
says the producer observed nothing, and the stream closes with no expected
range and no missing ranges, so it can pass the completeness gate. Zero is a
different claim: that sequence 0 exists. An empty close from a producer that
ingested observations, or that reports `observed_gaps`, is refused with
`CLOSE_CONTRADICTS_OBSERVATIONS`, and a stream closed as empty accepts no
later observations. Code: `CloseCapture` in
`internal/externalruntime/capture_close.go`.

## Divergences from the research pack

`docs/research/external-runtime-multiplayer/` is immutable input, so these are
recorded here rather than by editing it.

- **Heavy objects are uploaded directly, not reserved.** The pack describes
  `POST /v1/captures/{id}/objects` as creating an upload reservation. Any
  response carrying an upload location is an operational endpoint in a public
  DTO, which the secret-redaction invariant excludes; content addressing also
  removes the reason for a second private identifier. The bytes are therefore
  sent in the request body.
- **`GET /v1/objects/{content_hash}` is served unconditionally.** The pack
  conditions it on publication policy. PUB-06 (retention and data licence) was
  resolved on 2026-08-26 as a CC0-like dedication with indefinite retention,
  recorded in `docs/decisions/operator-gates.md`, so the policy the pack waits on
  exists and permits it. The endpoint is a public known-ID read with no
  authentication. It serves capture objects only; raw and derived batches share
  the content-addressed store but are read through the event endpoints.
- **Batches are uncompressed.** The pack says "one compressed batch". Server-side
  decompression behind a lease is a new attack surface for a bandwidth saving
  nobody has measured; there is a hard byte cap instead.
- **Batch idempotency is keyed on content, not on the header.** `Idempotency-Key`
  is required for contract conformance, but the replay key is the sequence range
  plus the producer digest. A per-header replay table would grow without bound
  across a stream.
