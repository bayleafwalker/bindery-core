# Bindery external-runtime multiplayer research pack

**Status:** research charter and implementation input, not a committed product roadmap  
**Target:** two player clients, an optional separately enrolled observer, and public telemetry  
**Initial adapter:** Red Alert 2 / Yuri's Revenge-class legacy multiplayer

## Thesis

Bindery does not need to run the game process to manage the match around it.
The experiment asks whether Bindery can select, place, bind, scale and observe
latency-sensitive capabilities for an externally executed game whose simulation
remains on unmanaged clients.

The intended system is therefore a hosted **control, transport and observation
plane**, not an authoritative game server and not a replacement for a mature
multiplayer community service.

The smallest useful topology is:

- two player clients running their own game processes;
- one Bindery-managed match session and regional UDP relay;
- telemetry emitted asynchronously by client adapters and the relay;
- optionally, a third client enrolled with class `observer`;
- public session, identity, provenance and capture records;
- bearer credentials kept secret because credentials are not public data.

## Package map

| File | Purpose |
| --- | --- |
| `00-charter.md` | Research question, success condition and permanent boundaries |
| `01-requirements.md` | Prioritized functional and non-functional requirements |
| `02-architecture.md` | Planes, components, trust boundaries and Bindery ownership |
| `03-domain-and-lifecycle.md` | Match, client, identity and capture state models |
| `04-identity-and-public-data.md` | Token identity and the exact meaning of public-by-contract |
| `05-observation-and-telemetry.md` | Observer model, telemetry lanes and provenance rules |
| `06-bindery-alignment.md` | Mapping to the existing `v1alpha1` model and identified gaps |
| `07-api-contract.md` | Minimal HTTP/control contract and error semantics |
| `08-deployment-scaling-and-recovery.md` | Kubernetes placement, relay draining and recovery behavior |
| `09-validation-plan.md` | Research waves, acceptance checks and stop conditions |
| `10-decisions-and-open-gates.md` | Settled ADRs and genuinely unresolved operator choices |
| `sources.md` | Primary-source baseline for the CnCNet comparison |
| `schemas/` | Draft machine-readable identity, session and event envelopes |
| `examples/` | Example session document and capability composition |
| `prompts/` | Bounded implementation and independent-review handoffs |

## Recommended implementation order

1. Implement the broker/relay contract against synthetic UDP clients.
2. Prove a two-player RA2 match through the relay.
3. Capture and publish post-match telemetry with provenance.
4. Add the `observer` client class without changing the player contract.
5. Exercise NFR placement, relay admission scaling and drain behavior.

Do not begin with lobby UI, account recovery, rankings, anti-cheat or live AI
gameplay. Those are either outside the research question or, in the case of
anti-cheat, outside the project permanently.

