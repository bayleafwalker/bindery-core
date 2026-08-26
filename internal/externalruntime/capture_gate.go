package externalruntime

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
	"github.com/bayleafwalker/bindery-core/pkg/gatev1"
)

// The completeness gate is the first consequential gate in this repository to
// run through pkg/gatev1 rather than as a bare error return.
//
// It exists because of a specific failure: in the 2026-08-25 RA2 slice an
// adapter read a diagnostic string as a verdict and reported successful runs
// as failures and failed runs as successes, four times running. Repetition
// made the answer stable, not correct. The rules that came out of that are the
// ones gatev1 enforces here -- applicability is decided before the evaluator
// runs, a consequential gate carries a version and an implementation hash, and
// it must demonstrate a known-pass and a known-fail control before its verdict
// counts for anything.

//go:embed capture_gate.go
var captureGateSource string

const (
	captureCompletenessGateID = "bindery.capture.completeness"
	captureGateVersion        = "1"
	capturePolicyVersion      = "capture-completeness/v1"

	// GatePhaseEvidenceReconciliation and GateArtifactCaptureStream are the
	// applicability axes. A gate asked about something else is
	// NOT_APPLICABLE; a gate asked without them is UNRESOLVED. Neither is a
	// failure, and that distinction is the whole point.
	GatePhaseEvidenceReconciliation = "evidence-reconciliation"
	GateArtifactCaptureStream       = "capture-stream"
)

// PublicGateResult is the published outcome of one gate evaluation, matching
// contracts/externalruntime/v1/gate-result.schema.json.
type PublicGateResult struct {
	SchemaVersion      string `json:"schema_version"`
	GateID             string `json:"gate_id"`
	GateVersion        string `json:"gate_version"`
	ImplementationHash string `json:"implementation_hash"`
	CaptureID          string `json:"capture_id"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	CalibrationValid   bool   `json:"calibration_valid"`
}

// captureGateImplementationHash identifies the decision procedure: the
// evaluator's own source together with the policy constants it applies.
//
// It is computed at init rather than pinned as a hand-maintained literal. A
// literal would have to be updated by hand on every edit, which in practice
// means it gets updated without thought and stops meaning anything. Computing
// it means the hash is always true about the binary that emitted it; a frozen
// *behaviour* vector over the calibration corpus is what catches an
// unintended change, and that lives in capture_gate_test.go.
var captureGateImplementationHash = computeCaptureGateHash()

func computeCaptureGateHash() string {
	policy, err := json.Marshal(struct {
		GateID        string `json:"gate_id"`
		Version       string `json:"version"`
		PolicyVersion string `json:"policy_version"`
		Phase         string `json:"phase"`
		ArtifactType  string `json:"artifact_type"`
	}{captureCompletenessGateID, captureGateVersion, capturePolicyVersion, GatePhaseEvidenceReconciliation, GateArtifactCaptureStream})
	if err != nil {
		panic("capture gate policy is not encodable: " + err.Error())
	}
	digest := sha256.Sum256(append([]byte(captureGateSource), policy...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// errObservationUnreadable separates "the evidence cannot be read" from "the
// capture was incomplete". The first is ERROR and means the broker has a
// problem; the second is FAIL and means the capture does. Collapsing them is
// how an infrastructure fault gets recorded as a finding about a match.
var errObservationUnreadable = errors.New("capture observations could not be read")

// evaluateCaptureCompleteness runs the gate over one capture.
func evaluateCaptureCompleteness(record *captureRecord, objects ObjectStore, context gatev1.Context) PublicGateResult {
	definition := captureCompletenessDefinition()
	result := gatev1.Evaluate(definition, context, func() (gatev1.Status, error) {
		return captureCompletenessVerdict(record, objects)
	})
	return PublicGateResult{
		SchemaVersion:      SchemaVersion,
		GateID:             result.GateID,
		GateVersion:        result.GateVersion,
		ImplementationHash: result.ImplementationHash,
		CaptureID:          record.CaptureID,
		Status:             string(result.Status),
		Reason:             result.Reason,
		CalibrationValid:   result.CalibrationValid,
	}
}

// captureCompletenessVerdict is the evaluator proper. It may only return PASS
// or FAIL; every other outcome is the framework's to assign.
func captureCompletenessVerdict(record *captureRecord, objects ObjectStore) (gatev1.Status, error) {
	// Read every persisted batch and check it against the hash it is filed
	// under. This is the exhaustive verification startup deliberately skips:
	// here it has a consequence, so here is where it belongs.
	for _, entry := range record.Index {
		if entry.Kind != CaptureEntryRaw {
			continue
		}
		body, err := objects.Get(entry.ContentHash)
		if err != nil {
			return "", fmt.Errorf("%w: %s: %v", errObservationUnreadable, entry.ContentHash, err)
		}
		if capture.DigestOf(body) != entry.ContentHash {
			return "", fmt.Errorf("%w: %s does not match its recorded hash", errObservationUnreadable, entry.ContentHash)
		}
	}
	switch CaptureStatus(record.Status) {
	case CaptureClosed:
	case CaptureAbandoned:
		// The producer went away without accounting for its stream. That is a
		// complete absence of a completeness claim, not an unreadable one.
		return gatev1.StatusFail, nil
	default:
		return gatev1.StatusFail, nil
	}
	completeness := record.completeness()
	if len(completeness.MissingRanges) > 0 || completeness.LocalDrops > 0 {
		return gatev1.StatusFail, nil
	}
	return gatev1.StatusPass, nil
}

func captureCompletenessDefinition() gatev1.Definition {
	positive, negative := captureCalibrationControls()
	return gatev1.Definition{
		GateID:             captureCompletenessGateID,
		Version:            captureGateVersion,
		ImplementationHash: captureGateImplementationHash,
		Consequential:      true,
		AppliesWhen: gatev1.AppliesWhen{
			Phases:        []string{GatePhaseEvidenceReconciliation},
			ArtifactTypes: []string{GateArtifactCaptureStream},
		},
		Calibration: []gatev1.CalibrationControl{positive, negative},
	}
}

// captureCalibrationControls builds a known-pass and a known-fail capture and
// runs the evaluator over both.
//
// Observed is filled in by actually running the evaluator, never declared. A
// declared Observed would make gatev1's Observed-equals-Expected check a
// formality that passes by construction, which is exactly the kind of
// reassuring nothing this gate exists to avoid.
func captureCalibrationControls() (gatev1.CalibrationControl, gatev1.CalibrationControl) {
	completeRecord, completeObjects, completeDigest := fixtureCalibrationCapture(false)
	gappedRecord, gappedObjects, gappedDigest := fixtureCalibrationCapture(true)

	completeStatus, completeErr := captureCompletenessVerdict(completeRecord, completeObjects)
	if completeErr != nil {
		completeStatus = gatev1.StatusError
	}
	gappedStatus, gappedErr := captureCompletenessVerdict(gappedRecord, gappedObjects)
	if gappedErr != nil {
		gappedStatus = gatev1.StatusError
	}
	return gatev1.CalibrationControl{
		Kind: gatev1.ControlPositive, FixtureID: "capture-complete", FixtureDigest: completeDigest,
		Expected: gatev1.StatusPass, Observed: completeStatus,
	}, gatev1.CalibrationControl{
		Kind: gatev1.ControlNegative, FixtureID: "capture-gapped", FixtureDigest: gappedDigest,
		Expected: gatev1.StatusFail, Observed: gappedStatus,
	}
}

// fixtureCalibrationCapture builds a self-contained capture in a scratch
// object store: contiguous 0..3 closed at 3, or the same stream with sequence
// 2 never delivered and one drop reported.
func fixtureCalibrationCapture(gapped bool) (*captureRecord, ObjectStore, string) {
	objects := capture.NewMemoryObjectStore()
	stamp := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	record := &captureRecord{PublicCapture: PublicCapture{
		CaptureID: "calibration-capture", SessionID: "calibration-session", ExecutionID: "calibration-execution",
		ProducerClientID: "calibration-producer", ProducerClass: string(ClientPlayer),
		CaptureMethod: "calibration", AdapterID: "bindery.calibration", AdapterVersion: "1",
		Status: string(CaptureClosed), CreatedAt: stamp,
	}}
	ranges := [][2]uint64{{0, 1}, {2, 3}}
	if gapped {
		ranges = [][2]uint64{{0, 1}, {3, 3}}
	}
	digest := sha256.New()
	for _, span := range ranges {
		events := calibrationEvents(record, span[0], span[1], stamp)
		body, err := capture.CanonicalBatchBytes(events)
		if err != nil {
			panic("calibration fixture is not encodable: " + err.Error())
		}
		contentHash, err := objects.Put(body)
		if err != nil {
			panic("calibration fixture could not be stored: " + err.Error())
		}
		digest.Write(body)
		record.Index = append(record.Index, CaptureIndexEntry{
			ContentHash: contentHash, Kind: CaptureEntryRaw,
			FirstSequence: span[0], LastSequence: span[1],
			EventCount: span[1] - span[0] + 1, Bytes: int64(len(body)), ReceivedAt: stamp,
		})
	}
	closedAt := stamp
	record.ClosedAt = &closedAt
	record.Close = &CaptureClose{FinalSequence: 3, EndReason: "calibration"}
	if gapped {
		record.Close.ObservedGaps = [][2]uint64{{2, 2}}
		record.Close.LocalDrops = 1
	}
	closeBytes, err := json.Marshal(record.Close)
	if err != nil {
		panic("calibration close is not encodable: " + err.Error())
	}
	digest.Write(closeBytes)
	return record, objects, "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func calibrationEvents(record *captureRecord, first, last uint64, stamp time.Time) []capture.RawEvent {
	events := make([]capture.RawEvent, 0, last-first+1)
	for sequence := first; sequence <= last; sequence++ {
		events = append(events, capture.RawEvent{
			EventID:          fmt.Sprintf("calibration-%016d", sequence),
			SessionID:        record.SessionID,
			ExecutionID:      record.ExecutionID,
			CaptureID:        record.CaptureID,
			ProducerClientID: record.ProducerClientID,
			ProducerClass:    record.ProducerClass,
			CaptureMethod:    record.CaptureMethod,
			AdapterID:        record.AdapterID,
			AdapterVersion:   record.AdapterVersion,
			Sequence:         sequence,
			ReceivedAt:       stamp,
			EventType:        "game.match.lifecycle",
			Payload:          json.RawMessage(`{"control":"calibration"}`),
		})
	}
	return events
}
