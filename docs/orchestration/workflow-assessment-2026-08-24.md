# Practical workflow assessment preparation — 2026-08-24

Status: ready for the final workflow assessment run. This document records what
the shared Vuoro/Sprintctl/AgentOps/OpenCode path demonstrated in practice; it
does not claim that ERM-204 production acceptance is complete.

## Assessment conclusion so far

The workflow is operationally useful for bounded W0 work. It enforced exact
commits, live shared reservations, protected paths, a disposable `agentworker`
worktree, registered gates, independent coordinator review, and durable
Sprintctl/Auditctl evidence. It also caught two real failures before they could
silently become accepted work:

1. The first fixture packet was rejected before inference because its protected
   redaction oracle contradicted the public `relay_endpoint` placement field.
2. The revised worker run produced the right seven-line candidate but exceeded
   the 32,000-token hard ceiling, so the driver stopped with
   `budget_exceeded`.

The first failure demonstrates useful preflight semantics. The second exposed a
workflow accounting problem: the worker finished a tiny mechanical mutation
while the driver's cumulative cached-token accounting made the attempt
non-clean. Per operator direction, the bindery route is now telemetry-first:
the manifest records a temporary 1,000,000 soft / 2,000,000 hard observation
bound, but ordinary contained local runs are not stopped at the old 32,000
threshold. Timeout, containment, scope, and deterministic gates remain
enforced. Qualification remains false.

The follow-up reusable-placement pass then completed under that policy: 163,470
cumulative tokens, reported cost `$0.00`, no budget stop, exact reference match,
and all post-dispatch gates green. The policy is now widened further to
intentionally excessive telemetry-only bounds (`1B/2B` tokens and `$1M` schema
cap) so no ordinary local run is rejected for lack of usage metrics.

The wide fixture-wave pass then exercised three frozen implementation seams in
one contained attempt: capture, harness, and relay placement fixtures. The
worker reported 280,190 cumulative tokens and `$0.00`, matched the coordinator
reference patch exactly, and produced no terminal error. Its registered race
invocation was denied by the worker overlay, so Luna reran the cold race gate
independently before merging `f2c8432`. This is a useful operational result:
the excessive policy allowed the local attempt to complete, while the
coordinator still retained an explicit post-gate obligation when the worker
environment could not execute one registered command.

## Evidence ledger

| Area | Verified result | Evidence |
|---|---|---|
| Shared authority | `bindery-core` is served by Vuoro with repo UUID `2f6f7f1c-0f0c-4dcb-90e7-0f1a4b7f89f2`; reservation 18 was live, refreshed, and released | Sprintctl events 2499–2501; profile `workstation-vuoro-shared` |
| Exact packet preparation | Revised packet fit from commit `002261317c47cb3a40f3bdc964eba45c98da5104`; packet hash `sha256:18a5980ef629116a96769154bf564ed5e084bca45dff2c75498223d9965a495d` | `/tmp/erm-204-fixture-placement-r2.packet.json`; prepare receipt |
| Containment | `agentworker` changed only `internal/externalruntime/fixtures.go`; coordinator tree stayed untouched; no containment override | `erm-204-fixture-placement-r2.receipt.json` |
| Real local inference | OpenCode 1.18.21 emitted valid JSON events with stable session `ses_fcb6dd814ffeMG4BDIDNPCM4tc`; no terminal error | `erm-204-fixture-placement-r2.review-evidence.json` |
| Independent review | Candidate exactly matched the coordinator reference patch; protected paths and diff scope passed | `erm-204-fixture-placement-r2.review-evidence.json`; Sprintctl note 2500 |
| Follow-up mechanical pass | Reusable `fixtureRelayPlacement` builder was dispatched, independently reviewed, and merged without a budget stop | `erm-204-fixture-builder-r2.receipt.json`; Sprintctl note 2506 |
| Wide mechanical pass | One contained r3 attempt covered capture, harness, and relay fixture seams; exact reference match; reservation 23 released after merge | `erm-wide-fixture-wave-r3.receipt.json`; `erm-wide-fixture-wave-r3.review.json`; Auditctl `ad:01M0TE724J8VGVS51P0MQWDCMG`, `ad:01M0TE726E94KVRFH7TXF9MHQ7` |
| Code gates | Race, vet, redaction, verification artifacts, scope, and protected-path gates passed both in the worker/post-gate path and after coordinator merge | receipt; coordinator commit `85e1a23` |
| Budget/accounting | Initial fixture pass reported `$0.00` and `207039` tokens against `16k/32k`, producing `budget_exceeded`; follow-up reported `$0.00` and `163470` tokens with no budget stop; wide pass reported `$0.00` and `280190` tokens with no budget stop; current policy uses intentionally excessive `1B/2B` telemetry bounds and `$1M` schema cap | r2 receipts; wide receipt; Auditctl `ad:01M0T9BXSR9CCRAA0PZ1WC4P8E`, `ad:01M0TC18KF8BF9KQ1J90JN56F6`, `ad:01M0TE724J8VGVS51P0MQWDCMG` |
| Qualification | Route remains globally unqualified. The raw run-stage eligibility field was operationally true, but the canonical receipt is false and this attempt is excluded from qualification evidence | receipt; review evidence |
| Redaction contradiction | Coordinator fixed the legitimate public placement exception and added a regression test | commit `d927542`; Auditctl `ad:01M0T8J6QQ8Z9R8PSD6B0KCW33` records the rejected predecessor |
| Adapter slice | Authenticated relay codec/client and two-client loopback tests passed; Windows CI passed | `erm-204-r1.integration-evidence.json`; [GitHub Actions run](https://github.com/bayleafwalker/bindery-ra2-adapter/actions/runs/32747960289) |
| Knowledge publication | Kctl served intake was attempted but rejected by the real `knowledge.candidate.intake` authority; no local fallback was fabricated | Auditctl `ad:01M0T78S6A8PJSKSYRVWGT7MYV` |

## What the final assessment run should measure

The final run should evaluate the workflow, not repeat implementation for its
own sake. Use the same shared workstation profile and record pass/fail/partial
for each item:

- Authority: `sprintctl item show --id 2293 --json`, reservation list, project
  binding, and zero active reservations after handoff.
- Dispatch integrity: packet hash, exact starting commit, route model,
  worker identity, protected paths, and the canonical qualification fields.
- Harness observability: valid `step_start`, `text`, `step_finish`, stable
  session ID, and absence of terminal error in the actual OpenCode JSON stream.
- Containment: worker write denial against the coordinator checkout and the
  protected-path report from the disposable worktree.
- Oracle quality: rerun the packet validator with `strace` if available. The
  current recorded packet used `--allow-untraced-oracle`, so
  `oracle_reads_within_paths` is a limitation (`skipped:untraced`), not a
  pass.
- Review quality: compare candidate diff to the frozen reference independently
  and run cold coordinator gates from the merged commit.
- Accounting: collect frequent-use telemetry under the intentionally excessive
  telemetry-only policy, then decide whether any durable spend control is
  justified before qualification.
- Durability: confirm the Sprintctl candidate event, Auditctl event IDs,
  receipt, review evidence, and pushed commit all resolve without relying on
  conversational claims.

## Remaining product/workflow blockers

- ERM-204 remains `pending`. Full acceptance still requires a live control-plane
  session and two separate RA2 clients completing a match through one relay
  allocation; synthetic loopback evidence is not that match.
- Kctl reusable-conclusion publication remains blocked until the served identity
  receives the real `knowledge.candidate.intake` authority.
- The route is intentionally globally unqualified. Both r2 results are reviewed
  candidates and workflow observations, not qualification evidence.
- Oracle read tracing remains partial until `strace` is available or the
  assessment explicitly accepts the recorded `skipped:untraced` limitation.
- No human acceptance gate was imposed in this workstream, per the approved
  workflow decision that human acceptance is perpendicular to these merges.

## Reusable conclusions

1. Keep coordinator-authored interfaces, tests, and reference overlays outside
   worker writable paths. The preflight rejection found a contradiction before
   inference, and the revised packet became fit only after the coordinator
   repaired the contract/oracle boundary.
2. Treat worker output as a candidate even when registered gates pass. The first
   placement worker weakened validation semantics; independent review caught it.
3. Keep containment and qualification as separate facts. The r2 worker was
   contained and on the requested model, but the route is still unqualified and
   the hard budget was exceeded.
4. Treat spend as telemetry while local-provider behavior is being measured.
   Keep the raw token/cost observations and qualification exclusion, but do not
   use an unmeasured token threshold to reject ordinary local work.
5. Preserve a coordinator post-gate path when the worker cannot run a
   registered validation command. The wide pass remained reviewable and
   mergeable because its exact reference, scope, and cold gates were captured
   independently rather than treating an overlay denial as an implicit pass.
