package externalruntime

import (
	"sort"

	"github.com/bayleafwalker/bindery-core/internal/capture"
	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
	"github.com/bayleafwalker/bindery-core/pkg/gatev1"
)

// EvidenceSetResult is the evidence set together with the gate results that
// decided which captures were allowed to contribute to it. The evidence set's
// own fields stay at the top level, so a reader that only wants the set is
// unaffected by the addition.
type EvidenceSetResult struct {
	evidencev1.EvidenceSet
	GateResults []PublicGateResult `json:"gate_results,omitempty"`
}

// deriveObservationsLocked computes an observation summary per capture from
// the persisted raw events, rather than accepting the producer's own count.
//
// This is the difference between an evidence set that records two independent
// accounts and one that records two clients' opinions of themselves. The
// 2026-08-25 slice had the latter and could not tell the difference; roadmap
// item ERH-006 asks for the former in as many words.
//
// A capture whose completeness gate does not PASS contributes nothing and is
// named in the result with its gate outcome. The gate therefore changes what
// evidence exists without adjudicating the match itself.
func (s *Service) deriveObservationsLocked(executionID string, captureIDs []string) ([]evidencev1.ObservationSummary, []PublicGateResult, error) {
	context := gatev1.Context{
		Phase:             GatePhaseEvidenceReconciliation,
		ArtifactType:      GateArtifactCaptureStream,
		CapabilitiesKnown: true,
	}
	summaries := make([]evidencev1.ObservationSummary, 0, len(captureIDs))
	results := make([]PublicGateResult, 0, len(captureIDs))
	for _, captureID := range captureIDs {
		record, ok := s.captures[captureID]
		if !ok {
			return nil, nil, domainError("CAPTURE_NOT_FOUND", "capture "+captureID+" was not found")
		}
		if record.ExecutionID != executionID {
			return nil, nil, domainError("CAPTURE_NOT_ON_EXECUTION", "capture "+captureID+" does not belong to this execution")
		}
		if record.ProducerClass == ProducerNormalizer {
			return nil, nil, domainError("CAPTURE_IS_DERIVED", "capture "+captureID+" is a derivation, not an independent observation")
		}
		result := evaluateCaptureCompleteness(record, s.objects, context)
		results = append(results, result)
		if result.Status != string(gatev1.StatusPass) {
			continue
		}
		summary, err := s.summarizeCaptureLocked(record)
		if err != nil {
			return nil, nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, results, nil
}

func (s *Service) summarizeCaptureLocked(record *captureRecord) (evidencev1.ObservationSummary, error) {
	events := make([]capture.RawEvent, 0)
	for _, entry := range record.Index {
		if entry.Kind != CaptureEntryRaw {
			continue
		}
		body, err := s.objects.Get(entry.ContentHash)
		if err != nil {
			return evidencev1.ObservationSummary{}, domainError("OBSERVATION_UNREADABLE", "a persisted observation batch could not be read")
		}
		decoded, err := capture.DecodeCanonicalBatch(body)
		if err != nil {
			return evidencev1.ObservationSummary{}, domainError("OBSERVATION_UNREADABLE", "a persisted observation batch is not canonical")
		}
		events = append(events, decoded...)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	orderedHash, err := capture.OrderedHash(events)
	if err != nil {
		return evidencev1.ObservationSummary{}, domainError("OBSERVATION_UNREADABLE", "observations could not be canonically hashed")
	}
	return evidencev1.ObservationSummary{
		ObserverID:  record.ProducerClientID,
		ExecutionID: record.ExecutionID,
		StreamID:    record.CaptureID,
		EventCount:  uint64(len(events)),
		OrderedHash: orderedHash,
		Source:      evidencev1.SourceBrokerDerived,
	}, nil
}

// executionCaptureIDsLocked lists every capture attached to an execution, in a
// stable order.
func (s *Service) executionCaptureIDsLocked(executionID string) []string {
	ids := make([]string, 0)
	for id, record := range s.captures {
		if record.ExecutionID == executionID && record.ProducerClass != ProducerNormalizer {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
