# Identity and public-data contract

## Identity is a token-backed claim

The system intentionally avoids the machinery of a recoverable account.

Creation flow:

1. A caller requests a handle and optional public metadata.
2. The broker creates a random `account_id` and high-entropy bearer token.
3. The token is returned once.
4. The broker stores only a verifier/hash suitable for constant-time checking.
5. The public identity record becomes immediately readable.

If the token is lost, the identity is lost. The user creates another identity.
The old public record remains associated with its historical sessions and
captures.

Recommended naming semantics:

- `account_id` is globally unique and stable;
- `handle` is unique and immutable in the first target;
- `display_name` is optional, mutable and non-unique;
- a lost token does not release or recycle the old handle.

This avoids an operator recovery process and avoids silently transferring
historical telemetry to a later claimant.

## Public means public by contract

The service provides no privacy boundary for application-domain data. A caller
must assume that accepted records can be copied, mirrored and retained by
others.

Public application data includes:

- identity identifiers, handles and declared metadata;
- match manifests and lifecycle history;
- enrolled identities, client classes and adapter versions;
- placement region/provider and non-secret policy decisions;
- transport aggregates that do not reveal raw endpoints;
- raw accepted captures, normalized observations and derivations;
- provenance, disagreement and completeness metadata.

There is no endpoint for creating a private match or private capture in the
initial charter. “No public lobby” means sessions are not indexed for discovery;
it does not make a known session identifier confidential.

## Credentials and operational state are not public data

The public-data rule must not be implemented as “serialize the database row.”
The following are security mechanics and are never part of the public dataset:

- identity bearer tokens and token verifiers;
- session join credentials;
- client and relay lease tokens;
- raw source IP addresses and UDP ports;
- internal service addresses, tracing baggage and rate-limit keys;
- unredacted request headers or structured logs.

Raw endpoints should remain memory-resident where possible. If retained for
operations, they need an explicit short TTL and must not flow into capture
objects.

## Token semantics

- Use at least 256 bits of cryptographically secure random material.
- Send tokens only in authorization headers or protocol metadata, never URLs.
- Compare verifiers in constant time.
- Scope join and transport tokens to one session/client and give them short TTLs.
- Permit account-token rotation only when the current token is valid.
- A compromised token is equivalent to transfer of the identity. There is no
  identity-recovery promise.
- Operator suspension may deny resource use, but does not rewrite or hide the
  existing public history.

## Public provenance rather than trusted truth

Because clients own their processes, telemetry is a set of attributable claims.
The publication model must answer:

- who or what produced the observation;
- which adapter and capture method produced it;
- which raw object or sequence range supports it;
- when the producer says it occurred and when Bindery received it;
- whether another source agreed, disagreed or did not observe it;
- which normalizer version produced a derived representation.

The absence of anti-cheat is therefore not an absence of epistemology. Bindery
records what it knows and how it knows it; it simply declines to become a
nostalgic esports court.

## Before unrelated public users

The following are launch gates, not requirements for a local research run:

- choose a data license and contribution/publication terms;
- settle retention and deletion behavior;
- define acceptable-use and service-abuse suspension rules;
- review legal/privacy obligations for the intended jurisdictions;
- publish the exact public-field schemas and credential exclusions.

Declaring data public is a product contract. It is not, by itself, a legal or
operational policy.

