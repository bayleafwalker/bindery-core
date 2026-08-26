# Minimal API contract

This is an HTTP-shaped draft for implementation and review. Protobuf/gRPC or a
different edge protocol may realize the same semantics. Public representations
and authenticated mutation representations must be separate types.

## Conventions

- API prefix: `/v1` for the experimental external-runtime service contract.
- Identifiers: opaque UUIDv7/ULID-equivalent values; never handles or IPs.
- Authentication: `Authorization: Bearer <token>`.
- Idempotency: `Idempotency-Key` on all create/report operations.
- Time: RFC 3339 UTC on the wire.
- Errors: stable machine code, human message, request correlation id.
- Public reads require no authentication.
- Tokens appear only in create/join responses and are never returned again.

## Identity

### `POST /v1/identities`

Creates a token-backed identity.

Request:

```json
{
  "handle": "tesla-coil-17",
  "display_name": "Tesla Coil",
  "metadata": {
    "client_family": "ra2"
  }
}
```

Response `201`:

```json
{
  "public_identity": {
    "account_id": "01J...",
    "handle": "tesla-coil-17",
    "display_name": "Tesla Coil",
    "claimed_at": "2026-08-23T10:00:00Z",
    "public_data_notice_version": "1.0"
  },
  "account_token": "returned-once",
  "recovery": "none"
}
```

### `GET /v1/identities/{account_id}`

Returns the public identity and links to public enrollments/captures. It never
returns token status details useful for credential probing.

### `POST /v1/identities/{account_id}:rotate-token`

Requires the current account token and returns the replacement once. The old
token becomes invalid atomically. This endpoint may be omitted from the first
slice without violating the no-recovery model.

## Sessions

### `POST /v1/sessions`

Requires an account token.

Request:

```json
{
  "compatibility": {
    "game_family": "ra2-yr",
    "game_version": "1.001",
    "adapter_id": "bindery.ra2-adapter",
    "adapter_version": "0.1.0",
    "mod_id": "vanilla-yr",
    "mod_hash": "sha256:...",
    "map_id": "official:map-name",
    "map_hash": "sha256:..."
  },
  "participant_policy": {
    "required_players": 2,
    "maximum_players": 2,
    "maximum_observers": 1
  },
  "placement": {
    "allowed_regions": ["eu-north"],
    "latency_p95_ms": 100
  },
  "capture": {
    "semantic_events": true,
    "post_match_dump": true,
    "observer_preferred": true
  }
}
```

Response `201` returns:

- public session object;
- creator enrollment offer;
- join credential and expiry;
- explicit public-data notice/version.

### `GET /v1/sessions/{session_id}`

Public read containing compatibility, lifecycle, public participants, redacted
transport placement, capture manifests and transition provenance.

### No `GET /v1/sessions`

The initial target deliberately lacks a browse/list/search endpoint. Known IDs
are public; discovery is absent.

## Enrollment

### `POST /v1/sessions/{session_id}/enrollments`

Requires both a valid account token and session join credential.

Request:

```json
{
  "client_instance_id": "01J...",
  "client_class": "player",
  "adapter": {
    "id": "bindery.ra2-adapter",
    "version": "0.1.0"
  },
  "compatibility": {
    "game_hash": "sha256:...",
    "mod_hash": "sha256:...",
    "map_hash": "sha256:..."
  },
  "region_probes": [
    {"region": "eu-north", "rtt_ms": 34}
  ]
}
```

Response `201` contains:

- public enrollment record;
- client lease token and expiry;
- relay endpoint and opaque participant identifier when allocated;
- transport credential scoped to this client/session;
- capture stream offers and ingest endpoint.

`client_class` is `player` or `observer` in v1. Unsupported classes return a
stable `CLIENT_CLASS_UNSUPPORTED` error rather than being coerced into player.

### `POST /v1/enrollments/{client_id}/reports`

Requires the client lease token. Reports readiness, game start, clean exit,
failure or capture degradation. Reports are idempotent and become public after
credential fields are stripped.

### `POST /v1/enrollments/{client_id}:heartbeat`

Renews the client lease. A successful heartbeat is operational state; public
history need not contain every heartbeat.

## Telemetry ingest

### `POST /v1/captures/{capture_id}/batches`

Requires the client/capture lease token.

Headers identify the sequence range and content hash. The body contains one
compressed batch using the negotiated event schema. Successful response
acknowledges the highest contiguous persisted sequence and reports known gaps.

Response:

```json
{
  "capture_id": "01J...",
  "acknowledged_through": 8191,
  "missing_ranges": [[4096, 4351]],
  "raw_object_hash": "sha256:..."
}
```

### `POST /v1/captures/{capture_id}/objects`

Creates an upload reservation for a heavy capture artifact. The resulting public
manifest exposes media type, byte length, content hash, capture method and
producer; it does not expose upload credentials.

### `POST /v1/captures/{capture_id}:close`

Closes the producer stream with final sequence, observed gaps, local drop counts
and end reason. Ingest may also close abandoned captures after lease expiry.

## Public observation reads

- `GET /v1/sessions/{session_id}/captures`
- `GET /v1/captures/{capture_id}`
- `GET /v1/captures/{capture_id}/events?cursor=...`
- `GET /v1/sessions/{session_id}/events?cursor=...`
- `GET /v1/objects/{content_hash}` when publication policy permits direct object retrieval

## Error codes

| Code | Meaning | Retry |
| --- | --- | --- |
| `HANDLE_TAKEN` | Immutable unique handle already claimed | No, choose another |
| `TOKEN_INVALID` | Bearer proof failed | No |
| `JOIN_CREDENTIAL_INVALID` | Join secret invalid/expired | Obtain a new invitation out of band |
| `SESSION_NOT_ADMITTING` | Session phase rejects new clients | No |
| `CLIENT_CLASS_UNSUPPORTED` | Adapter/broker does not support requested class | No |
| `COMPATIBILITY_MISMATCH` | Game/mod/map/adapter contract mismatch | Correct local installation |
| `RELAY_CAPACITY_UNAVAILABLE` | No provider satisfies placement/capacity | Retry with backoff or relax policy |
| `LEASE_EXPIRED` | Client/capture lease has ended | Re-enroll if session permits |
| `SEQUENCE_CONFLICT` | Same idempotency key/range has different bytes | Stop and surface capture corruption |
| `PAYLOAD_TOO_LARGE` | Batch/object exceeds negotiated limit | Split batch or use object lane |

## Secret-redaction invariant

Contract tests must serialize every public response and search for:

- supplied bearer/join/transport token values;
- raw source IP/port fixtures;
- authorization headers;
- internal upload credentials.

Any match is a release-blocking failure.

