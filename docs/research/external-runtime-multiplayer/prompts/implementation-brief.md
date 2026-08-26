# Implementation handoff prompt

Use the text below to brief an implementation agent. Replace bracketed values
and narrow the wave before dispatch.

---

Implement **Wave [0/1]** from the Bindery external-runtime multiplayer research
pack against the current `bindery-core` repository.

Read, in order:

1. repository `AGENTS.md` and relevant standards;
2. pack `00-charter.md`, `01-requirements.md` and `06-bindery-alignment.md`;
3. pack `07-api-contract.md` and `09-validation-plan.md`;
4. schemas and examples used by the selected wave.

Constraints:

- Preserve the research boundary: externally executed clients own simulation.
- Do not implement a lobby, recoverable account, ranking or anti-cheat surface.
- Public application records and authenticated mutation types must be separate.
- Never persist or expose raw bearer tokens; token verifiers are non-public.
- Telemetry must not be synchronous in the UDP forwarding path.
- Do not add a new CRD unless the task proves durable desired state that the
  existing objects and broker records cannot express.
- Keep RA2-specific launch/protocol/capture code out of Bindery Core.
- Do not apply to a shared Kubernetes cluster without explicit authority.

Deliver:

- a concise implementation plan tied to requirement IDs;
- code/contracts for only the selected wave;
- unit/integration tests, including public-response secret fixtures;
- `make verify` results and any narrower validation commands;
- changed-file ledger and any generated CRD/contract alignment work;
- unresolved evidence gaps and a clean rollback path.

Acceptance is defined by the selected wave in `09-validation-plan.md`. Stop at
that boundary even if adjacent product work looks convenient.

---

