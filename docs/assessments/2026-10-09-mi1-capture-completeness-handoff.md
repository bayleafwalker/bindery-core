# MI-1 Track B: capture-completeness contract review (handoff)

Status: **UNVALIDATED DRAFT. Not an MI-1 completion claim. No game, RA2, ERH-006 or hardware evidence.**
Every fixture named here is a unit fixture. No fixture in this report is a run result.

## Source and provenance

- Source revision reviewed: `fa71b2344cbbf844c83498c627fb6fcedf419c2f` in public `bayleafwalker/bindery-core`.
- Produced by a hosted Claude session; the trusted coordinator recovered the successful report-write bytes and removed session identity, connector inventory and authentication metadata before publication.
- No direct hosted Vuoro run was established. Original observations remain on the trusted host. This public artifact is a redacted handoff, not the original hosted commit object.

## Scope

Read-only review of `internal/externalruntime/capture_gate.go`, `capture_gate_test.go`, `pkg/gatev1/gate.go`, `pkg/gatev1/gate_test.go`, plus the callers and docs named below. No source, policy, canonical-encoding or capture-data change.

## The five outcomes

Only `gatev1.Evaluate` (`pkg/gatev1/gate.go:74-118`) assigns statuses. The evaluator closure may return only PASS or FAIL. Any other status becomes ERROR (`gate.go:110-114`).

| Status | Assigned when | Source |
|---|---|---|
| NOT_APPLICABLE | Context is known but its phase or artifact type is not in the gate's `AppliesWhen`. A required capability that is known and absent also gives this. The evaluator is **not called**. | `gate.go:134-136`, `142-144`, `150-153`; test `TestWrongContextIsNotFailure` |
| UNRESOLVED | Applicability evidence is missing: empty phase, empty artifact type, or `CapabilitiesKnown=false` when capabilities are required. The evaluator is not called. | `gate.go:131-133`, `139-141`, `147-149`; `TestUnknownApplicabilityIsUnresolved`; the capture-gate case in `capture_gate_test.go:59-64` |
| ERROR | Invalid definition (empty id/version, bad implementation hash), failed calibration, nil evaluator, evaluator returned an error, or evaluator returned a status other than PASS/FAIL. | `gate.go:80-84`, `93-97`, `99-103`, `104-108`, `110-114` |
| FAIL | Applicable, calibration valid, evaluator returned `StatusFail` with no error. | `gate.go:115` |
| PASS | Same preconditions, evaluator returned `StatusPass`. | `gate.go:115` |

Order in `Evaluate`: definition validity, then applicability, then calibration, then the evaluator. Calibration is therefore checked only for applicable contexts. A NOT_APPLICABLE or UNRESOLVED result carries `CalibrationValid=false` even if the controls would have been fine (`gate.go:86-91`, `98`).

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

Derived behaviour not covered by any test, from reading the source only: the read loop runs **before** the status switch. An *abandoned* or *open* capture with unreadable evidence is therefore ERROR, not FAIL. A capture with unreadable evidence cannot be FAIL on any other ground.

## How calibration controls execute

- `captureCompletenessDefinition()` (`capture_gate.go:141-154`) calls `captureCalibrationControls()` on **every** construction, and `evaluateCaptureCompleteness` constructs it on every call (`:91`). Calibration is re-run per evaluation, not cached.
- `captureCalibrationControls()` (`:163-182`) builds a contiguous fixture (0..3, closed at 3) and a gapped fixture (ranges 0-1 and 3-3, `ObservedGaps {2,2}`, `LocalDrops=1`). It runs the **real** `captureCompletenessVerdict` over each and stores the result in `Observed`. A verdict error becomes `StatusError` there (`:167-174`). `Observed` is never a literal.
- `validateCalibration` (`gate.go:159-190`) requires, for a consequential gate:
  - every control has a non-empty id and a `sha256:` digest;
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
- The only input identifier in the result is `capture_id`.
- The result carries **no** content hashes, `observed_hash`, `ordered_hash`, raw object hashes, close fields or event count. It does not commit to the specific bytes it judged.
- The stored gate results are keyed by evidence set id (`service.go:72`, `service.go:409`).

## Source/runbook discrepancies I can demonstrate

1. **Implementation hash does not cover the full decision procedure.** `TestGateImplementationHashCoversTheDecisionProcedure` is named as if it did. The embedded source is only `capture_gate.go`. The verdict also depends on `record.completeness()`, `capture.MissingRanges`, `capture.MissingThrough`, `capture.DigestOf` (`capture.go:182-210` and the `internal/capture` package) and on `gatev1.Evaluate`. A behaviour change in any of those leaves `implementation_hash` unchanged. Conversely, any comment-only edit to `capture_gate.go` changes it. Only the frozen-verdict test is a backstop, and it covers two fixtures.
2. **Doc overstates test coverage.** `docs/architecture/evidence-and-gates.md:14-20` says the five outcomes, applicability rules and calibration controls "are exercised against the running service in `capture_gate_test.go`". The calibration test calls `captureCompletenessDefinition()` directly and not the service. Only `TestCompletenessGateSeparatesAllFiveOutcomes` goes through a `Service` record, and then only via `evaluateCaptureCompleteness`. It does not go through `deriveObservationsLocked`.
3. **Doc field names differ from the type.** `evidence-and-gates.md` (around line 166-173) lists consequential-gate fields `applies_when / positive_control / negative_control`. `gatev1.Definition` and `gate-definition.schema.json` use a single `calibration` array with `kind` `positive|negative`.
4. **Schema vs. calibration inputs (unverified).** `gate-result.schema.json` declares `capture_id` with `format: uuid`. Calibration uses `"calibration-capture"`, but that value never appears in a published result. I did not establish the format of production capture ids, so this is only a question to check, not a finding.
5. **Comment/behaviour mismatch.** The comment at `capture_gate.go:107-108` says the evaluator "may only return PASS or FAIL". It also returns `("", err)` for unreadable evidence. Behaviour is correct under `Evaluate`, but the comment is imprecise.
6. **FAIL reason is generic.** For FAIL and PASS alike, `Result.Reason` is `"gate evaluated in an applicable, calibrated context"` (`gate.go:116`). A published FAIL does not say whether the cause was open, abandoned, a gap or drops. `gate-result.schema.json` allows a `reason` up to 512 chars. Callers see only `status` (`capture_evidence.go:50-55`), where non-PASS captures are skipped.

## Tests attempted and outcomes

Command, run at `fa71b23` (before this report was added):

```
go test -count=1 -v -run 'Gate|Completeness|Unreadable|Calibration|WrongContext|Unknown|AlwaysFailing' ./internal/externalruntime/ ./pkg/gatev1/
```

Outcome: both packages `ok`. All matched tests passed, including the six in `capture_gate_test.go` and the four in `pkg/gatev1/gate_test.go`. Other matched tests also passed (e.g. `TestAnIncompleteStreamIsExcludedByTheGateAndNamedInTheResult`, `TestERM005RedactionAndIdempotencyGates`, `TestCompletenessManifestMakesGapsAndDropsExplicit`). Those were not reviewed in depth.

Not run: `make verify`, `make verify-external-runtime`, `-race`, `go vet`, `helm lint`, `make redaction`, envtest, Kind, or any cluster action. These passing tests say nothing about RA2 or ERH-006, which remain **pending**.

## Explicit unknowns

- Direct hosted evidence capture is not established.
- Production capture id format versus the schema's `uuid`.
- Whether abandoned/open + unreadable evidence is intended to be ERROR (derived from reading, untested).
- Whether the hash scope (finding 1) is intended; the source comment frames it as the evaluator source plus policy constants.
- Full-suite status (`make verify*`) and remote CI status.

## Successor task (deliberately unfinished)

The successor receives only this file and repository source. Do not trust this report:

1. Independently validate every statement above against source at `fa71b23`, citing lines.
2. Write targeted Go tests (in a scratch copy, or as a separate change) for the untested claims:
   - abandoned/open capture with unreadable evidence returns ERROR;
   - an edit to `completeness()` or `internal/capture` leaves `implementation_hash` unchanged;
   - the published FAIL reason is generic.
3. Check discrepancies 2-6 and the unknown on capture id format.
4. Correct mistakes **in this same artifact**.
5. Add a completion verdict (e.g. `VALIDATED`, `VALIDATED-WITH-CORRECTIONS`, `REJECTED`) with reasoning, and resolve or restate each unknown.

The successor must not claim MI-1 completion, RA2 reproduction or ERH-006 closure.

## Completion verdict

**NOT PROVIDED.** Reserved for the successor.
