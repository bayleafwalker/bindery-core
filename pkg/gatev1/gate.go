// Package gatev1 defines calibrated, context-aware verification gates.
package gatev1

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Status string

const (
	StatusPass          Status = "PASS"
	StatusFail          Status = "FAIL"
	StatusNotApplicable Status = "NOT_APPLICABLE"
	StatusUnresolved    Status = "UNRESOLVED"
	StatusError         Status = "ERROR"
)

type AppliesWhen struct {
	Phases        []string `json:"phases,omitempty"`
	ArtifactTypes []string `json:"artifact_types,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

type Context struct {
	Phase             string   `json:"phase,omitempty"`
	ArtifactType      string   `json:"artifact_type,omitempty"`
	Capabilities      []string `json:"capabilities,omitempty"`
	CapabilitiesKnown bool     `json:"capabilities_known"`
}

type ControlKind string

const (
	ControlPositive ControlKind = "positive"
	ControlNegative ControlKind = "negative"
)

type CalibrationControl struct {
	Kind          ControlKind `json:"kind"`
	FixtureID     string      `json:"fixture_id"`
	FixtureDigest string      `json:"fixture_digest"`
	Expected      Status      `json:"expected"`
	Observed      Status      `json:"observed"`
}

type Definition struct {
	GateID             string               `json:"gate_id"`
	Version            string               `json:"version"`
	ImplementationHash string               `json:"implementation_hash"`
	Consequential      bool                 `json:"consequential"`
	AppliesWhen        AppliesWhen          `json:"applies_when"`
	Calibration        []CalibrationControl `json:"calibration,omitempty"`
}

type Result struct {
	GateID             string `json:"gate_id"`
	GateVersion        string `json:"gate_version"`
	ImplementationHash string `json:"implementation_hash"`
	Status             Status `json:"status"`
	Reason             string `json:"reason"`
	CalibrationValid   bool   `json:"calibration_valid"`
}

type Evaluator func() (Status, error)

// Evaluate separates applicability and verifier health from the fact being
// checked. A broken or uncalibrated verifier is ERROR; it is never evidence
// that the subject failed.
func Evaluate(definition Definition, context Context, evaluator Evaluator) Result {
	result := Result{
		GateID:             definition.GateID,
		GateVersion:        definition.Version,
		ImplementationHash: definition.ImplementationHash,
	}
	if err := validateDefinition(definition); err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	status, reason := applicability(definition.AppliesWhen, context)
	if status != StatusPass {
		result.Status = status
		result.Reason = reason
		return result
	}

	if err := validateCalibration(definition); err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	result.CalibrationValid = true
	if evaluator == nil {
		result.Status = StatusError
		result.Reason = "gate evaluator is not configured"
		return result
	}
	observed, err := evaluator()
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	if observed != StatusPass && observed != StatusFail {
		result.Status = StatusError
		result.Reason = fmt.Sprintf("evaluator returned invalid terminal status %q", observed)
		return result
	}
	result.Status = observed
	result.Reason = "gate evaluated in an applicable, calibrated context"
	return result
}

func validateDefinition(definition Definition) error {
	if definition.GateID == "" || definition.Version == "" {
		return errors.New("gate id and version are required")
	}
	if !digestPattern.MatchString(definition.ImplementationHash) {
		return errors.New("gate implementation hash must be a sha256 digest")
	}
	return nil
}
func applicability(rule AppliesWhen, context Context) (Status, string) {
	if len(rule.Phases) > 0 {
		if context.Phase == "" {
			return StatusUnresolved, "phase is required to establish gate applicability"
		}
		if !slices.Contains(rule.Phases, context.Phase) {
			return StatusNotApplicable, "gate does not apply to this phase"
		}
	}
	if len(rule.ArtifactTypes) > 0 {
		if context.ArtifactType == "" {
			return StatusUnresolved, "artifact type is required to establish gate applicability"
		}
		if !slices.Contains(rule.ArtifactTypes, context.ArtifactType) {
			return StatusNotApplicable, "gate does not apply to this artifact type"
		}
	}
	if len(rule.Capabilities) > 0 {
		if !context.CapabilitiesKnown {
			return StatusUnresolved, "capabilities are required to establish gate applicability"
		}
		for _, required := range rule.Capabilities {
			if !slices.Contains(context.Capabilities, required) {
				return StatusNotApplicable, fmt.Sprintf("gate requires capability %q", required)
			}
		}
	}
	return StatusPass, "gate applies"
}

func validateCalibration(definition Definition) error {
	if !definition.Consequential {
		return nil
	}
	positive, negative := false, false
	for _, control := range definition.Calibration {
		if control.FixtureID == "" || !digestPattern.MatchString(control.FixtureDigest) {
			return errors.New("every calibration control requires an id and sha256 fixture digest")
		}
		if control.Observed != control.Expected {
			return fmt.Errorf("%s control %q observed %s, expected %s", control.Kind, control.FixtureID, control.Observed, control.Expected)
		}
		switch control.Kind {
		case ControlPositive:
			if control.Expected != StatusPass {
				return errors.New("positive control must expect PASS")
			}
			positive = true
		case ControlNegative:
			if control.Expected != StatusFail {
				return errors.New("negative control must expect FAIL")
			}
			negative = true
		default:
			return fmt.Errorf("unknown calibration control kind %q", control.Kind)
		}
	}
	if !positive || !negative {
		return errors.New("consequential gate requires passing positive and negative calibration controls")
	}
	return nil
}
