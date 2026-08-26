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

## Divergences from the research pack

`docs/research/external-runtime-multiplayer/` is immutable input, so these are
recorded here rather than by editing it.

- **Heavy objects are uploaded directly, not reserved.** The pack describes
  `POST /v1/captures/{id}/objects` as creating an upload reservation. Any
  response carrying an upload location is an operational endpoint in a public
  DTO, which the secret-redaction invariant excludes; content addressing also
  removes the reason for a second private identifier. The bytes are therefore
  sent in the request body.
- **`GET /v1/objects/{content_hash}` is not served.** The pack conditions it on
  publication policy, and PUB-06 (retention and data licence) is undefined.
  Serving arbitrary uploaded bytes publicly is a policy decision, not an
  implementation one.
- **Batches are uncompressed.** The pack says "one compressed batch". Server-side
  decompression behind a lease is a new attack surface for a bandwidth saving
  nobody has measured; there is a hard byte cap instead.
- **Batch idempotency is keyed on content, not on the header.** `Idempotency-Key`
  is required for contract conformance, but the replay key is the sequence range
  plus the producer digest. A per-header replay table would grow without bound
  across a stream.
