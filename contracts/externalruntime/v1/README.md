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
  consequential gates require known-pass and known-fail calibration evidence.
