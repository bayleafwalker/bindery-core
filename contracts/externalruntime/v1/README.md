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

