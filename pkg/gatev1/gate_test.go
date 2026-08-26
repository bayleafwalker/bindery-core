package gatev1

import (
	"errors"
	"testing"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestWrongContextIsNotFailure(t *testing.T) {
	called := false
	result := Evaluate(calibratedDefinition(), Context{
		Phase:             "run",
		ArtifactType:      "task-packet",
		Capabilities:      []string{"oracle-read-tracing"},
		CapabilitiesKnown: true,
	}, func() (Status, error) {
		called = true
		return StatusFail, nil
	})
	if result.Status != StatusNotApplicable || called {
		t.Fatalf("result = %+v, evaluator called = %t", result, called)
	}
}

func TestUnknownApplicabilityIsUnresolved(t *testing.T) {
	result := Evaluate(calibratedDefinition(), Context{
		Phase:        "freeze",
		ArtifactType: "task-packet",
	}, func() (Status, error) { return StatusPass, nil })
	if result.Status != StatusUnresolved {
		t.Fatalf("status = %s, want UNRESOLVED", result.Status)
	}
}

func TestAlwaysFailingValidatorIsCalibrationError(t *testing.T) {
	definition := calibratedDefinition()
	definition.Calibration[0].Observed = StatusFail
	called := false
	result := Evaluate(definition, applicableContext(), func() (Status, error) {
		called = true
		return StatusFail, nil
	})
	if result.Status != StatusError || called {
		t.Fatalf("result = %+v, evaluator called = %t", result, called)
	}
}

func TestCalibratedApplicableGateCanPassOrFail(t *testing.T) {
	for _, expected := range []Status{StatusPass, StatusFail} {
		result := Evaluate(calibratedDefinition(), applicableContext(), func() (Status, error) { return expected, nil })
		if result.Status != expected || !result.CalibrationValid {
			t.Fatalf("result = %+v, want %s", result, expected)
		}
	}
	result := Evaluate(calibratedDefinition(), applicableContext(), func() (Status, error) { return StatusError, errors.New("fixture unreadable") })
	if result.Status != StatusError {
		t.Fatalf("evaluator error became %s", result.Status)
	}
}

func calibratedDefinition() Definition {
	return Definition{
		GateID:             "oracle-read-tracing",
		Version:            "1.0.0",
		ImplementationHash: testDigest,
		Consequential:      true,
		AppliesWhen: AppliesWhen{
			Phases:        []string{"freeze"},
			ArtifactTypes: []string{"task-packet"},
			Capabilities:  []string{"oracle-read-tracing"},
		},
		Calibration: []CalibrationControl{
			{Kind: ControlPositive, FixtureID: "known-pass", FixtureDigest: testDigest, Expected: StatusPass, Observed: StatusPass},
			{Kind: ControlNegative, FixtureID: "known-fail", FixtureDigest: testDigest, Expected: StatusFail, Observed: StatusFail},
		},
	}
}

func applicableContext() Context {
	return Context{
		Phase:             "freeze",
		ArtifactType:      "task-packet",
		Capabilities:      []string{"oracle-read-tracing"},
		CapabilitiesKnown: true,
	}
}
