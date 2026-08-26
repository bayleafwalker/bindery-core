# /docs

This folder contains the documentation for the platform.

**This repository holds two subsystems.** They were developed on separate,
unrelated git histories and merged into `main` on 2026-08-26 (commit `b162f22`).
They share one Go module and one CI workflow (`.github/workflows/ci.yml`), and
nothing else: no runtime code path crosses between them. Most of the documents
below describe exactly one of the two, so check which one you are reading.

| | Kubernetes operator | External runtime |
| --- | --- | --- |
| What it is | Capability-driven game platform reconciled from CRDs | HTTP + UDP control plane for matches simulated in game clients Bindery does not own |
| Code | `api/v1alpha1/`, `controllers/`, `main.go`, `internal/{resolver,semver,graph}`, `modules/` | `internal/{externalruntime,relay,harness,capture}`, `pkg/{evidencev1,gatev1,relayv1}`, `cmd/bindery-{external-runtime,udp-relay,redaction-scan}`, `hack/redaction-corpus/` |
| CRDs | `k8s/crds/`, mirrored in `helm/bindery-core/crds/` | none — it defines no CRDs and runs no controllers |
| Chart | `helm/bindery-core/` | `charts/bindery-external-runtime/` |
| Contracts | `contracts/proto/game/engine/v1/` | `contracts/externalruntime/v1/` |
| Local checks | `make verify` | `make verify-external-runtime` |

Both are **pre-alpha and experimental**. `v1alpha1` CRDs and the
`externalruntime/v1` contract can change.

## Kubernetes operator

Start here:
- Standards index: [`standards/index.md`](standards/index.md)
- Platform architecture: [`platform-architecture.md`](platform-architecture.md)

Notable areas:
- Standards/specs: [`standards/`](standards/)
- Kubernetes CRD + controller design: [`standards/kubernetes/`](standards/kubernetes/)
- RPC contracts: [`standards/rpc/`](standards/rpc/)
- Workflows/how-to: [`workflows/`](workflows/)
- Testing guidance: [`testing/`](testing/)
- Debugging guides: [`debugging/`](debugging/)
- JSON Schemas for `ModuleManifest` / `CapabilityContract`: [`schemas/`](schemas/)

## External runtime

Start here:
- Evidence, reconciliation, and gates: [`architecture/evidence-and-gates.md`](architecture/evidence-and-gates.md)
- The one demonstrated end-to-end result and its limits:
  [`assessments/2026-08-25-ra2-vertical-slice.md`](assessments/2026-08-25-ra2-vertical-slice.md)
- Remaining tracked work: [`roadmap/post-ra2-hardening.yaml`](roadmap/post-ra2-hardening.yaml)
- Promoted wire contract: [`../contracts/externalruntime/v1/README.md`](../contracts/externalruntime/v1/README.md)
- Adapter provenance notes: [`compatibility-findings.md`](compatibility-findings.md)

The RA2 vertical slice was demonstrated **once**, in a lab. The assessment
deliberately refuses to backfill durable identifiers onto that pre-hardening
run; repeating it through the hardened control plane is roadmap item ERH-006 and
is **pending**. The control plane can now host such a run — the capture plane is
served and durable, and observation summaries are derived by the broker rather
than reported by adapters — but the run itself has not happened, and test
fixtures are not run results.

## Reference material (not maintained as current documentation)

- [`research/external-runtime-multiplayer/`](research/external-runtime-multiplayer/)
  — the immutable research pack the external runtime was designed from. It is
  input, not a committed roadmap, and it is not edited to track the code.
- [`orchestration/`](orchestration/) — per-task receipts, review records, and
  reference patches from the external-runtime build-out, plus
  [`orchestration/workflow-assessment-2026-08-24.md`](orchestration/workflow-assessment-2026-08-24.md).
  Historical records of how work was executed; they are dated and are not kept
  in sync with the code.

## Agent guidance

- Repository rules for agents: [`../AGENTS.md`](../AGENTS.md)
- Copy/paste prompt entrypoints: [`agent/entrypoints.md`](agent/entrypoints.md)
