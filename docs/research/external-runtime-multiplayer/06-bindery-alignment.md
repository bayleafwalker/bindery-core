# Alignment with Bindery Core `v1alpha1`

## Current model findings

The current repository already contains several relevant primitives:

- capability scopes include `region`, `realm`, `world`, `world-shard` and `session`;
- `WorldInstance` selects a `Booklet`, region, Realm and shard count;
- `Realm` supplies shared modules to multiple worlds;
- `ModuleManifest.spec.runtime.image` may be empty for a non-server-orchestrated module;
- `WorldStorageClaim` recognizes a `client-low-latency` external tier;
- `CapabilityBinding` records resolved consumer/provider edges;
- the runtime controller intentionally does not create workloads for client-side modules.

The missing concept is not “external things exist.” It is **dynamic instances of
external client classes enrolling at runtime**. Static `ModuleManifest` and
`CapabilityBinding` resolution identify module types and provider choices; they
do not issue per-process leases or publish a reachable endpoint for an unmanaged
client.

Two smaller alignment gaps also matter:

- the checked-in `CapabilityDefinition` CRD currently omits `realm` from its
  allowed scopes even though the Go common types, `ModuleManifest` and
  `CapabilityBinding` support it;
- `ModuleRuntimeSpec` and the current RuntimeOrchestrator materialize one TCP
  ClusterIP/gRPC-style port, not a publicly advertised UDP endpoint.

The first is ordinary API/CRD drift to correct when capability definitions are
used. The second is a real research boundary: deploy/register the first relay
through a dedicated manifest or sibling chart, then decide whether generic
`ports[]` protocol/exposure semantics have earned a place in the core runtime API.

## Recommended mapping

| Research concept | Existing Bindery object | Decision |
| --- | --- | --- |
| Multiplayer network/ruleset | `Realm` | Use a Realm to host shared broker, relay-selection and publication modules |
| One match | `WorldInstance` | Use tentatively; one world instance is one externally executed match envelope |
| Game/adapter composition | `Booklet` | Define the broker, relay, telemetry and external adapter class requirements |
| Managed broker/ingest/relay | `ModuleManifest` with runtime | Normal server-owned modules |
| RA2 player/observer adapter class | `ModuleManifest` without runtime | Declares class capabilities but is not scheduled by Bindery |
| Static provider choice | `CapabilityBinding` | Continue using for broker, relay, ingest and publication provider selection |
| Runtime client process | No current object | Represent first through broker-owned enrollment/lease records, not a CRD |
| Match-local raw capture | `WorldStorageClaim` where suitable | Server storage for durable objects; client tier documents local buffering |
| Relay pool capacity | Region/Realm capability provider | Do not model relay instances as `WorldShard`s |

## Why not introduce a client CRD immediately

Kubernetes is useful for durable declarative intent. A game process on a user's
Windows machine is short-lived, heartbeat-driven and not reconciled by the
cluster. Mirroring every connection into a CRD would add API-server churn and
pretend that Bindery can converge the external process back into existence.

Start with a versioned runtime contract and broker-owned records. Introduce a
CRD only if the research demonstrates a durable operator intent that must be
reconciled independently of the broker, such as an externally managed capture
fleet.

## Proposed capability families

These names follow the current dot-namespaced model and remain experimental:

| Capability | Scope | Role |
| --- | --- | --- |
| `multiplayer.session-broker` | realm | Identity, session and enrollment control |
| `multiplayer.transport-relay` | region | UDP forwarding and transport metrics |
| `multiplayer.relay-placement` | realm | Select relay provider/instance from NFR and capacity inputs |
| `multiplayer.external-player` | session | Contract expected from an enrolled player adapter class |
| `multiplayer.external-observer` | session | Optional observer adapter class |
| `telemetry.event-ingest` | realm | Idempotent semantic batch ingest |
| `telemetry.object-ingest` | realm | Heavy capture/checkpoint object ingest |
| `telemetry.publication` | realm | Public query and dataset publication |

The existing capability ID syntax requires at least one dot-separated segment;
hyphenated terminal names are compatible with it.

## Booklet direction

The multiplayer Booklet should contain:

- server-owned session broker;
- relay-placement provider;
- telemetry event/object ingest;
- public publication provider;
- external RA2 player adapter class;
- optional external RA2 observer adapter class.

The actual enrolled player instances are not deployed from the Booklet. The
Booklet defines the contracts that a client must satisfy before the broker
issues a lease.

## World and shard caveat

The current `WorldShard` controller materializes at least one shard even when
`WorldInstance.spec.shardCount` is below one. For this experiment:

- set `shardCount: 1` for compatibility with existing behavior;
- treat that shard as a current platform implementation artifact;
- do not identify it with the UDP relay, participant set or capture partition;
- do not apply `ShardAutoscaler` to matches.

Relay autoscaling is fleet admission scaling based on active leases, packet rate
and egress. It is not world-shard scaling and should not be forced through the
current CPU/memory-only `ShardAutoscaler`.

## Smallest core extension

The first implementation should add contracts and an experimental service, not
broaden every CRD:

1. Add capability contract documents for session broker, transport relay and telemetry ingest.
2. Add an external-client enrollment protocol under `contracts/`.
3. Add a broker reference service under `cmd/`; deploy the initial UDP relay
   through a dedicated research manifest/chart until the runtime exposure model is proven.
4. Add a Booklet example demonstrating runtime-less player/observer adapter manifests.
5. Record resolved relay provider and public match state without storing bearer credentials in CRDs.

Only after the vertical slice should the project decide whether `WorldInstance`
needs explicit `executionMode: external`, participant status, or a separate
session resource. A pre-alpha API can change, but it should still change for an
observed reason.
