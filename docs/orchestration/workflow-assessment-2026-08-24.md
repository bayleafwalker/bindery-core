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

The first failure demonstrates useful preflight semantics. The second exposes a
workflow defect: the worker can finish a tiny mechanical mutation while the
driver's cumulative cached-token accounting makes the attempt non-clean. The
candidate was reviewed and merged because the user approved merging and the
semantic gates passed, but the canonical receipt keeps
`qualification_eligible=false`.

## Evidence ledger

| Area | Verified result | Evidence |
|---|---|---|
| Shared authority | `bindery-core` is served by Vuoro with repo UUID `2f6f7f1c-0f0c-4dcb-90e7-0f1a4b7f89f2`; reservation 18 was live, refreshed, and released | Sprintctl events 2499–2501; profile `workstation-vuoro-shared` |
| Exact packet preparation | Revised packet fit from commit `002261317c47cb3a40f3bdc964eba45c98da5104`; packet hash `sha256:18a5980ef629116a96769154bf564ed5e084bca45dff2c75498223d9965a495d` | `/tmp/erm-204-fixture-placement-r2.packet.json`; prepare receipt |
| Containment | `agentworker` changed only `internal/externalruntime/fixtures.go`; coordinator tree stayed untouched; no containment override | `erm-204-fixture-placement-r2.receipt.json` |
| Real local inference | OpenCode 1.18.21 emitted valid JSON events with stable session `ses_fcb6dd814ffeMG4BDIDNPCM4tc`; no terminal error | `erm-204-fixture-placement-r2.review-evidence.json` |
| Independent review | Candidate exactly matched the coordinator reference patch; protected paths and diff scope passed | `erm-204-fixture-placement-r2.review-evidence.json`; Sprintctl note 2500 |
| Code gates | Race, vet, redaction, verification artifacts, scope, and protected-path gates passed both in the worker/post-gate path and after coordinator merge | receipt; coordinator commit `85e1a23` |
| Budget | Reported cost `$0.00`, cumulative tokens `207039`, soft ceiling `16000`, hard ceiling `32000`; driver disposition `budget_exceeded` | receipt; Auditctl `ad:01M0T9BXSR9CCRAA0PZ1WC4P8E` |
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
- Accounting: explain why a seven-line task accumulated 207,039 tokens and
  decide whether the worker stop rule, context payload, or budget accounting
  needs correction before a qualification exercise.
- Durability: confirm the Sprintctl candidate event, Auditctl event IDs,
  receipt, review evidence, and pushed commit all resolve without relying on
  conversational claims.

## Remaining product/workflow blockers

- ERM-204 remains `pending`. Full acceptance still requires a live control-plane
  session and two separate RA2 clients completing a match through one relay
  allocation; synthetic loopback evidence is not that match.
- Kctl reusable-conclusion publication remains blocked until the served identity
  receives the real `knowledge.candidate.intake` authority.
- The route is intentionally globally unqualified. The r2 result is a reviewed
  candidate and a workflow observation, not qualification evidence.
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
4. Treat `budget_exceeded` as a workflow finding, not as a code-quality pass.
   The candidate can be merged under explicit coordinator judgment, while the
   attempt remains unsuitable for qualification or unattended reuse.

