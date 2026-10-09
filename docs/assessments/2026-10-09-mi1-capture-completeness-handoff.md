# MI-1 Track B: capture-completeness contract review (handoff)

Status: **VALIDATED-WITH-CORRECTIONS (native successor review). Not an MI-1 completion claim. No game, RA2, ERH-006 or hardware evidence.**
Every fixture named here is a unit fixture. No fixture in this report is a run result.

## Source and provenance

- Source revision reviewed: `fa71b2344cbbf844c83498c627fb6fcedf419c2f` in public `bayleafwalker/bindery-core`.
- Produced by a hosted Claude session; the trusted coordinator recovered the successful report-write bytes and removed session identity, connector inventory and authentication metadata before publication.
- No direct hosted Vuoro run was established. Original observations remain on the trusted host. This public artifact is a redacted handoff, not the original hosted commit object.

## Scope

Read-only review of `internal/externalruntime/capture_gate.go`, `capture_gate_test.go`, `pkg/gatev1/gate.go`, `pkg/gatev1/gate_test.go`, plus the callers and docs named below. No source, policy, canonical-encoding or capture-data change.

## The five outcomes

`gatev1.Evaluate` (`pkg/gatev1/gate.go:74-118`) assigns the final result status. The evaluator returns PASS/FAIL on success, or an error; errors and other returned statuses become ERROR (`gate.go:104-114`). Applicability helpers and calibration controls also produce statuses internally.

| Status | Assigned when | Source |
|---|---|---|
| NOT_APPLICABLE | Context is known but its phase or artifact type is not in the gate's `AppliesWhen`. A required capability that is known and absent also gives this. The evaluator is **not called**. | `gate.go:134-136`, `142-144`, `150-153`; test `TestWrongContextIsNotFailure` |
| UNRESOLVED | A required applicability axis lacks evidence: empty phase, empty artifact type, or `CapabilitiesKnown=false` when capabilities are required. The evaluator is not called. | `gate.go:131-133`, `139-141`, `147-149`; `TestUnknownApplicabilityIsUnresolved`; the capture-gate case in `capture_gate_test.go:59-64` |
| ERROR | Invalid definition (empty id/version, bad implementation hash), failed calibration, nil evaluator, evaluator returned an error, or evaluator returned a status other than PASS/FAIL. | `gate.go:80-84`, `93-97`, `99-103`, `104-108`, `110-114` |
| FAIL | Applicable, calibration valid, evaluator returned `StatusFail` with no error. | `gate.go:115` |
| PASS | Same preconditions, evaluator returned `StatusPass`. | `gate.go:115` |

Order in `Evaluate`: definition validity, then applicability, then calibration, then the evaluator. Calibration validation is therefore checked only for applicable contexts. Capture calibration controls themselves execute earlier, during definition construction (`capture_gate.go:91`, `141-142`), even for NOT_APPLICABLE/UNRESOLVED requests. A NOT_APPLICABLE or UNRESOLVED result carries `CalibrationValid=false` even if the controls would have been fine (`gate.go:86-91`, `98`).

For `bindery.capture.completeness`, applicability is phase `evidence-reconciliation` and artifact type `capture-stream` (`capture_gate.go:40-41`, `148-151`). It requires no capabilities. The production caller passes `CapabilitiesKnown: true` (`capture_evidence.go:32-36`).

## Why unreadable evidence is ERROR and a gap is FAIL

`captureCompletenessVerdict` (`capture_gate.go:109-139`) first loops over every raw index entry:

- `objects.Get` fails → returns `("", errObservationUnreadable…)` (`:117-120`).
- The body's `capture.DigestOf` differs from the filed `ContentHash` → same sentinel error (`:121-123`).

`Evaluate` turns any evaluator error into ERROR (`gate.go:104-108`). The declared reason (`capture_gate.go:83-87`) is that inability to read is a fact about the broker, while incompleteness is a fact about the capture.

FAIL is returned only after all evidence has been read:

- status other than `CaptureClosed`, which covers both abandoned and still open (`:125-133`);
- `completeness().MissingRanges` non-empty or `LocalDrops > 0` (`:134-137`).

`completeness()` is in `capture.go:182-210`. `MissingRanges` is recomputed against `Close.FinalSequence` when one is set.

Tests:
- `TestUnreadableEvidenceIsErrorNotFailure` (`capture_gate_test.go:67-84`) corrupts `Index[0].ContentHash` to a zero digest and asserts ERROR.
- `TestCompletenessGateSeparatesAllFiveOutcomes` covers an open capture as FAIL, a closed contiguous capture as PASS, a closed gapped capture as FAIL, and the NOT_APPLICABLE and UNRESOLVED cases.

Behaviour not covered by the original tests, now confirmed by native scratch tests: the read loop runs **before** the status switch. An *abandoned* or *open* capture with unreadable evidence is therefore ERROR, not FAIL. In an applicable, calibrated evaluation, an indexed raw object read failure or hash mismatch takes precedence over any FAIL ground. This gate checks readability and digest equality, not JSON decoding: canonical decoding occurs later in `summarizeCaptureLocked` (`capture_evidence.go:61-81`).

## How calibration controls execute

- `captureCompletenessDefinition()` (`capture_gate.go:141-154`) calls `captureCalibrationControls()` on **every** construction, and `evaluateCaptureCompleteness` constructs it on every call (`:91`). Calibration is re-run per evaluation, not cached.
- `captureCalibrationControls()` (`:163-182`) builds a contiguous fixture (0..3, closed at 3) and a gapped fixture (ranges 0-1 and 3-3, `ObservedGaps {2,2}`, `LocalDrops=1`). It runs the **real** `captureCompletenessVerdict` over each and stores the result in `Observed`. A verdict error becomes `StatusError` there (`:167-174`). `Observed` is never a literal.
- `validateCalibration` (`gate.go:159-190`) requires, for a consequential gate:
  - every control has a non-empty id and a digest matching `^sha256:[0-9a-f]{64}$`;
  - `Observed == Expected` for every control;
  - the positive control expects PASS and the negative control expects FAIL;
  - both kinds are present.
- A broken evaluator that rejects everything fails the positive control, so the gate returns ERROR and the evaluator is never run on the real subject (`TestAlwaysFailingValidatorIsCalibrationError`, `TestEvaluatorThatFailsItsPositiveControlIsErrorNotStrictness`).
- `TestGateCalibrationRunsItsControlsRatherThanDeclaringThem` asserts positive `Observed=PASS`, negative `Observed=FAIL`, and different fixture digests.

Limits (source reading):
- The negative control exercises only the missing-range / local-drop FAIL path, with a status of closed.
- Neither control exercises the unreadable-evidence ERROR path, the abandoned/open FAIL branches, or an empty (`final_sequence=null`) close.
- `TestCompletenessGateBehaviourIsFrozen` checks only the two fixture verdicts and that the digest has a `sha256:` prefix. It does **not** pin the digest values.

## Fields that bind gate evidence to implementation and input

Output type: `PublicGateResult` (`capture_gate.go:46-55`), declared as matching `gate-result.schema.json`.

Implementation binding:
- `gate_id`, `gate_version` (`"1"`), `implementation_hash`.
- `implementation_hash` (`:66-81`) is `sha256(captureGateSource || json(policy))`. `captureGateSource` is the **embedded text of `capture_gate.go` only** (`:28-29`). The policy JSON holds gate id, version, `capture-completeness/v1`, phase and artifact type.
- The hash is computed at init, not pinned.

Calibration binding (inside the in-memory `gatev1.Definition`, not in `PublicGateResult`):
- per control: `fixture_id`, `fixture_digest`, `expected`, `observed`.
- `calibration_valid` is the only calibration field published.
- `fixture_digest` is sha256 of the concatenated canonical batch bytes plus the JSON of `Close` (`:200-231`).

Input binding:
- The dedicated subject identifier in the result is `capture_id`. ERROR `reason` strings can also contain the raw object hash (`capture_gate.go:119`, `122`).
- The result has **no dedicated fields for** content hashes, `observed_hash`, `ordered_hash`, raw object hashes, close fields or event count. It does not commit to the specific bytes it judged.
- The stored gate results are keyed by evidence set id (`service.go:72`, `service.go:409`).

## Source/runbook discrepancies I can demonstrate

1. **Implementation hash does not cover the full decision procedure.** `TestGateImplementationHashCoversTheDecisionProcedure` is named as if it did. The embedded source is only `capture_gate.go`. The verdict also depends on `record.completeness()`, `capture.MissingRanges`, `capture.MissingThrough`, `capture.DigestOf` (`capture.go:182-210` and the `internal/capture` package) and on `gatev1.Evaluate`. A behaviour change in any of those leaves `implementation_hash` unchanged. Conversely, any comment-only edit to `capture_gate.go` changes it. The frozen-verdict test covers two fixtures; other unit tests and runtime calibration provide additional checks, but do not expand the hash scope.
2. **Doc overstates test coverage.** `docs/architecture/evidence-and-gates.md:14-20` says the five outcomes, applicability rules and calibration controls "are exercised against the running service in `capture_gate_test.go`". The calibration test calls `captureCompletenessDefinition()` directly and not the service. `TestCompletenessGateSeparatesAllFiveOutcomes` and `TestUnreadableEvidenceIsErrorNotFailure` both use `Service` records via `evaluateCaptureCompleteness`; neither goes through `deriveObservationsLocked`.
3. **Doc field names differ from the type.** `evidence-and-gates.md` (around line 166-173) lists consequential-gate fields `gate_version / applies_when / positive_control / negative_control`. Definition version is actually `version` (`gate_version` belongs to the result). `gatev1.Definition` and `gate-definition.schema.json` use a single `calibration` array with `kind` `positive|negative`.
4. **Capture ID question resolved; no demonstrated production mismatch.** `gate-result.schema.json` declares `capture_id` with `format: uuid`. `mintCaptureLocked` calls `newUUIDv7` (`capture.go:270`); `id.go:13-33` formats a hyphenated UUID with version 7 and RFC variant bits. Derived IDs use the same generator (`capture_normalize.go:82-87`). Native service-fixture IDs match UUIDv7 syntax. `"calibration-capture"` is an internal control identifier; controls call the verdict directly, not the public-result wrapper (`capture_gate.go:167-180`). This establishes source behavior, not the IDs of any unobserved deployment.
5. **Comment/behaviour mismatch.** The comment at `capture_gate.go:107-108` says the evaluator "may only return PASS or FAIL". It also returns `("", err)` for unreadable evidence. Behaviour is correct under `Evaluate`, but the comment is imprecise.
6. **FAIL reason is generic.** For FAIL and PASS alike, `Result.Reason` is `"gate evaluated in an applicable, calibrated context"` (`gate.go:116`). A published FAIL does not say whether the cause was open, abandoned, a gap or drops. `gate-result.schema.json` allows a `reason` up to 512 chars. Admission inspects only `status`, but retains the entire result, including reason (`capture_evidence.go:50-55`); evidence-set callers receive `GateResults` (`service.go:417`). Non-PASS captures are skipped. If too few observations remain, reconciliation can return an error instead of publishing results (`erh006_test.go:121-127`).

## Tests attempted and outcomes

Command, run at `fa71b23` (before this report was added):

```
go test -count=1 -v -run 'Gate|Completeness|Unreadable|Calibration|WrongContext|Unknown|AlwaysFailing' ./internal/externalruntime/ ./pkg/gatev1/
```

Predecessor-reported outcome: both packages `ok`; independently reproduced below. All matched tests passed, including **five of the six** in `capture_gate_test.go` and all four in `pkg/gatev1/gate_test.go`. The regex omits `TestEvaluatorThatFailsItsPositiveControlIsErrorNotStrictness`; the native successor ran that separately and it passed. Other matched tests also passed (e.g. `TestAnIncompleteStreamIsExcludedByTheGateAndNamedInTheResult`, `TestERM005RedactionAndIdempotencyGates`, `TestCompletenessManifestMakesGapsAndDropsExplicit`). Those were not reviewed in depth.

Not run: `make verify`, `make verify-external-runtime`, `-race`, `go vet`, `helm lint`, `make redaction`, envtest, Kind, or any cluster action. These passing tests establish no RA2 reproduction or ERH-006 closure; ERH-006 remains **pending**.

## Native successor validation and corrections

I am a different native harness continuing from the durable public handoff alone,
without predecessor conversation or private observations. No model/build/owner
identity or additional authorization is inferred. The Git diff from source
`fa71b2344cbbf844c83498c627fb6fcedf419c2f` to starting artifact
`dc4b4d895fe32ef00c9d1f4c31964c3b337f47b4` contains only this assessment;
the runtime source reviewed here is unchanged from the named source revision.
Hosted provenance and coordinator redaction are assertions inherited from the
handoff, not independently verified execution history.

Short predecessor-claim/correction record:

- “Only the five-outcomes test uses a Service record”: the unreadable-evidence
  test does too (`capture_gate_test.go:70-82`); neither tests public derivation.

- “Calibration checked only for applicable contexts”: true for validation inside
  `Evaluate`, false for execution of capture controls, which runs eagerly before
  `Evaluate`. The framework checks supplied observations; it does not itself
  execute controls or guarantee the supplied evaluator produced them.
- “Only Evaluate assigns statuses” / “only PASS or FAIL”: final results belong to
  `Evaluate`; helpers return statuses too, and the evaluator may return an error.
  Applicability axes are conditional and checked in phase/artifact/capability
  order (`gate.go:129-156`); a known phase mismatch wins over missing later axes.
- “Six capture-gate tests matched”: five matched; the omitted sixth passed in a
  separate run. `TestCompletenessGateSeparatesAllFiveOutcomes` itself exercises
  four statuses; ERROR is exercised by the separate unreadable test.
- “Only frozen verdict is a backstop” / “callers see only status” / “no hashes”:
  other tests and runtime controls also check behavior; admission uses status but
  retains public results; ERROR reason can include a content hash. There is still
  no explicit commitment to all subject bytes/close metadata in a gate result.
- Documentation mismatch also includes definition `version` versus result
  `gate_version`. The “running service” claim overstates test coverage: tests use
  in-process service fixtures, not a deployed service. `erh006_test.go:98-141`
  does exercise `CreateEvidenceSet` and derivation, but is not evidence that all
  five outcomes or calibration execute through that public path.

All remaining implementation statements above were checked against the cited
source, tests, type and JSON schema. Digest arithmetic also checked against
`internal/capture/canon.go:275-281`, `capture.go:103-139`, and
`filestore.go:268`. The original negative control combines a gap and a drop, so
it does not isolate either condition; the scratch drop-only check does.

### Actual native commands and results

Go was initially absent from PATH (exit 127, no tests executed). The installed
local toolchain was then added with:

```bash
export PATH=/nix/store/iqq9yz2yrcrzsn52h3vkkz72i92h1zds-go-1.26.8/bin:$PATH
export GOCACHE=/tmp/mi1-bindery-go-build
export GOMODCACHE=/tmp/mi1-bindery-go-mod
go test -count=1 -v -run 'Gate|Completeness|Unreadable|Calibration|WrongContext|Unknown|AlwaysFailing' ./internal/externalruntime/ ./pkg/gatev1/
go test -count=1 -v -run '^TestEvaluatorThatFailsItsPositiveControlIsErrorNotStrictness$' ./internal/externalruntime/
```

Both commands exited 0. First command: externalruntime `ok` (0.084s), gatev1
`ok` (0.002s); second command: externalruntime `ok` (0.002s). These are unit
fixtures, including the matched ERH-named tests, never RA2/ERH-006 run evidence.

Scratch source was exported directly from the reviewed commit, not copied from
private state:

```bash
mkdir -p /tmp/mi1-native-audit
git archive fa71b2344cbbf844c83498c627fb6fcedf419c2f | tar -x -C /tmp/mi1-native-audit
# In that scratch directory, with the same PATH and cache exports:
go test -count=1 -v -run '^TestMI1' ./internal/externalruntime
go test -count=1 -v -run '^TestMI1HashScopeProbe$' ./internal/externalruntime
```

The first scratch command passed all three tests (0.003s):
`TestMI1UnreadablePrecedence` checks missing reads and mismatched bytes for open,
abandoned and closed gapped/drop captures; `TestMI1ReasonsAndIDs` checks generic
FAIL reasons for open/abandoned/gapped captures, a drop-only FAIL, UUIDv7 syntax
of a service-minted capture, and an empty closed PASS; `TestMI1HashScopeProbe`
records the hash and contiguous-fixture verdict.

Between scratch commands only, `captureRecord.completeness()` was altered to
set `manifest.LocalDrops = 1` before return. The second command exited 0 (0.002s)
and logged a contiguous verdict changing from PASS to FAIL while the hash stayed:

```text
sha256:42a721a8fe10c434545f3e805e49563e58245137a615c81e9f6a433ac59a3099
```

This falsifies full-decision-procedure hash coverage. The probe passing means
it successfully reported the changed behavior, not that the mutant is valid.
Source inspection establishes the same exclusion for `internal/capture` and
`pkg/gatev1`; no mutation of canonical encoding was needed. Scratch tests and
mutation exist only under `/tmp/mi1-native-audit`; no worktree test was added.

### Resolved and remaining unknowns

- Production ID generation: resolved from source as UUIDv7; actual deployment
  state and any loaded historical IDs remain unobserved.
- Open/abandoned plus unreadable evidence: observed ERROR in scratch tests.
  The read-before-status order and error comment support this behavior; any
  further product intent remains unestablished, and no policy is changed.
- Implementation hash scope: resolved as this one embedded file plus policy
  JSON, not the transitive procedure. Whether that limited scope is intended
  or sufficient remains a policy/design question, not authorization to fix it.
- Direct hosted evidence capture, original hosted execution/test history and
  provenance cannot be established from source or this redacted artifact.
- Full-suite and remote CI status remain unchecked. No `make verify*`, race,
  vet, Helm, redaction, envtest, Kind, API, cluster or hardware run was performed.
  RA2 history is documented in `2026-08-25-ra2-vertical-slice.md:54-56`;
  ERH-006 is explicitly pending in `docs/roadmap/post-ra2-hardening.yaml:52-68`.

## Completion verdict

**VALIDATED-WITH-CORRECTIONS.** The contract review is supported by unchanged
source at `fa71b2344cbbf844c83498c627fb6fcedf419c2f`, actual targeted test passes,
and scratch falsifiers. Corrections above narrow calibration timing, status,
test-coverage, type-name and result-field claims and resolve the capture-ID and
unreadable-precedence questions. Hash scope is demonstrably limited; generic
FAIL reasons and documentation discrepancies remain findings, not runtime fixes.
Only this assessment is changed. No encoding, runtime code, policy or immutable
capture data was changed; no commit, push or PR was made. This completes the
native handoff validation only, **not MI-1 completion, RA2 reproduction, ERH-006
closure or a hardware result**. Publication remains with the trusted coordinator.
