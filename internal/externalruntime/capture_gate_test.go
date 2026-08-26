package externalruntime

import (
	"strings"
	"testing"

	"github.com/bayleafwalker/bindery-core/pkg/gatev1"
)

func reconciliationContext() gatev1.Context {
	return gatev1.Context{Phase: GatePhaseEvidenceReconciliation, ArtifactType: GateArtifactCaptureStream, CapabilitiesKnown: true}
}

func TestCompletenessGateSeparatesAllFiveOutcomes(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "gate")

	// Still open: applicable, and genuinely not complete.
	mustIngest(t, service, fixture.playerA, 0, 2)
	open := evaluateCaptureCompleteness(service.captures[fixture.playerA.capture], service.objects, reconciliationContext())
	if open.Status != string(gatev1.StatusFail) {
		t.Fatalf("open capture status = %s", open.Status)
	}

	// Closed and contiguous.
	if _, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, CaptureCloseRequest{FinalSequence: 2, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}
	complete := evaluateCaptureCompleteness(service.captures[fixture.playerA.capture], service.objects, reconciliationContext())
	if complete.Status != string(gatev1.StatusPass) || !complete.CalibrationValid {
		t.Fatalf("complete capture result = %+v", complete)
	}
	if complete.ImplementationHash == "" || complete.GateID != captureCompletenessGateID {
		t.Fatalf("gate result carries no implementation identity: %+v", complete)
	}

	// Closed with a gap.
	mustIngest(t, service, fixture.playerB, 0, 1)
	mustIngest(t, service, fixture.playerB, 3, 3)
	if _, err := service.CloseCapture(fixture.playerB.lease, fixture.playerB.capture, CaptureCloseRequest{FinalSequence: 3, EndReason: "client-exit"}); err != nil {
		t.Fatal(err)
	}
	gapped := evaluateCaptureCompleteness(service.captures[fixture.playerB.capture], service.objects, reconciliationContext())
	if gapped.Status != string(gatev1.StatusFail) {
		t.Fatalf("gapped capture status = %s", gapped.Status)
	}

	// A known context mismatch is not a failure.
	elsewhere := reconciliationContext()
	elsewhere.ArtifactType = "relay-placement"
	notApplicable := evaluateCaptureCompleteness(service.captures[fixture.playerA.capture], service.objects, elsewhere)
	if notApplicable.Status != string(gatev1.StatusNotApplicable) {
		t.Fatalf("mismatched artifact type status = %s", notApplicable.Status)
	}

	// Neither is missing applicability evidence. This is the specific mistake
	// the 2026-08-25 assessment recorded: gates evaluated where their phase
	// did not apply, and the answer coming back as FAIL.
	unknown := reconciliationContext()
	unknown.Phase = ""
	unresolved := evaluateCaptureCompleteness(service.captures[fixture.playerA.capture], service.objects, unknown)
	if unresolved.Status != string(gatev1.StatusUnresolved) {
		t.Fatalf("missing phase status = %s", unresolved.Status)
	}
}

func TestUnreadableEvidenceIsErrorNotFailure(t *testing.T) {
	// "I cannot read the evidence" is a fact about the broker. "The capture
	// was incomplete" is a fact about the match. Reporting the first as the
	// second is how an infrastructure fault becomes a finding about a game.
	service := NewService()
	fixture := newCaptureFixture(t, service, "unreadable")
	mustIngest(t, service, fixture.playerA, 0, 1)
	if _, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, CaptureCloseRequest{FinalSequence: 1, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}
	record := service.captures[fixture.playerA.capture]
	record.Index[0].ContentHash = "sha256:" + strings.Repeat("0", 64)

	result := evaluateCaptureCompleteness(record, service.objects, reconciliationContext())
	if result.Status != string(gatev1.StatusError) {
		t.Fatalf("unreadable evidence status = %s, reason = %s", result.Status, result.Reason)
	}
}

func TestGateCalibrationRunsItsControlsRatherThanDeclaringThem(t *testing.T) {
	definition := captureCompletenessDefinition()
	if !definition.Consequential || len(definition.Calibration) != 2 {
		t.Fatalf("definition = %+v", definition)
	}
	var positive, negative *gatev1.CalibrationControl
	for index, control := range definition.Calibration {
		switch control.Kind {
		case gatev1.ControlPositive:
			positive = &definition.Calibration[index]
		case gatev1.ControlNegative:
			negative = &definition.Calibration[index]
		}
	}
	if positive == nil || negative == nil {
		t.Fatal("a consequential gate must carry both a known-pass and a known-fail control")
	}
	if positive.Observed != gatev1.StatusPass || negative.Observed != gatev1.StatusFail {
		t.Fatalf("controls did not actually run: positive=%s negative=%s", positive.Observed, negative.Observed)
	}
	if positive.FixtureDigest == negative.FixtureDigest {
		t.Fatal("the two controls are the same fixture")
	}
}

func TestEvaluatorThatFailsItsPositiveControlIsErrorNotStrictness(t *testing.T) {
	// A verifier that rejects everything is broken, not rigorous. This is the
	// exact failure the RA2 slice hit four times in a row.
	definition := captureCompletenessDefinition()
	for index := range definition.Calibration {
		if definition.Calibration[index].Kind == gatev1.ControlPositive {
			definition.Calibration[index].Observed = gatev1.StatusFail
		}
	}
	result := gatev1.Evaluate(definition, reconciliationContext(), func() (gatev1.Status, error) {
		return gatev1.StatusFail, nil
	})
	if result.Status != gatev1.StatusError || result.CalibrationValid {
		t.Fatalf("always-failing verifier result = %+v", result)
	}
}

func TestGateImplementationHashCoversTheDecisionProcedure(t *testing.T) {
	if !strings.HasPrefix(captureGateImplementationHash, "sha256:") || len(captureGateImplementationHash) != 71 {
		t.Fatalf("implementation hash = %q", captureGateImplementationHash)
	}
	if computeCaptureGateHash() != captureGateImplementationHash {
		t.Fatal("the implementation hash is not reproducible within one binary")
	}
	if !strings.Contains(captureGateSource, "captureCompletenessVerdict") {
		t.Fatal("the embedded evaluator source does not contain the evaluator")
	}
}

// TestCompletenessGateBehaviourIsFrozen is the guard the implementation hash
// deliberately is not. The hash is recomputed every build, so it can never
// drift; what needs pinning is the verdict the evaluator reaches, because that
// is what a change would silently alter.
func TestCompletenessGateBehaviourIsFrozen(t *testing.T) {
	for _, expectation := range []struct {
		name   string
		gapped bool
		want   gatev1.Status
	}{
		{"contiguous-closed", false, gatev1.StatusPass},
		{"gapped-closed", true, gatev1.StatusFail},
	} {
		record, objects, digest := fixtureCalibrationCapture(expectation.gapped)
		status, err := captureCompletenessVerdict(record, objects)
		if err != nil {
			t.Fatalf("%s: %v", expectation.name, err)
		}
		if status != expectation.want {
			t.Fatalf("%s: verdict = %s, want %s", expectation.name, status, expectation.want)
		}
		if !strings.HasPrefix(digest, "sha256:") {
			t.Fatalf("%s: fixture digest = %q", expectation.name, digest)
		}
	}
}
