package externalruntime

import (
	"sort"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

// NormalizeRequest names exactly which normalizer to run. There is no
// "latest": a derivation that cannot name the version that produced it is not
// reproducible, and an unreproducible derivation is an opinion.
type NormalizeRequest struct {
	NormalizerID      string `json:"normalizer_id"`
	NormalizerVersion string `json:"normalizer_version"`
}

// NormalizeCapture derives a new public dataset from a closed raw stream.
//
// It runs on request only, in the caller's goroutine, under the same single
// writer as everything else. A background normalizer would be the second
// writer this control plane's whole durability argument says it does not have.
//
// Replaying the same version is a no-op that returns the existing derivation.
// Running a new version creates a new derived capture and leaves the raw
// stream and every earlier derivation exactly as they were.
func (s *Service) NormalizeCapture(accountToken, captureID string, req NormalizeRequest) (PublicCapture, error) {
	normalizer, err := capture.Lookup(req.NormalizerID, req.NormalizerVersion)
	if err != nil {
		return PublicCapture{}, domainError("NORMALIZER_UNKNOWN", err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return PublicCapture{}, err
	}
	accountID, err := s.authenticateAccountLocked(accountToken)
	if err != nil {
		return PublicCapture{}, err
	}
	source, ok := s.captures[captureID]
	if !ok {
		return PublicCapture{}, domainError("CAPTURE_NOT_FOUND", "capture was not found")
	}
	if source.ProducerClass == ProducerNormalizer {
		return PublicCapture{}, domainError("CAPTURE_IS_DERIVED", "a derivation is not a normalization source")
	}
	session, ok := s.sessions[source.SessionID]
	if !ok {
		return PublicCapture{}, domainError("STATE_INTEGRITY_ERROR", "capture does not resolve to a session")
	}
	// Derivation is a broker-side interpretation published under the session's
	// name, so it is the session creator's call rather than the producer's.
	if session.creatorID != accountID {
		return PublicCapture{}, domainError("TOKEN_INVALID", "only the session creator may derive normalized datasets")
	}
	if existing := s.existingDerivationLocked(captureID, normalizer); existing != nil {
		return existing.public(), nil
	}
	if source.Close == nil {
		return PublicCapture{}, domainError("CAPTURE_NOT_CLOSED", "normalization needs a closed stream")
	}

	events, err := s.rawEventsLocked(source)
	if err != nil {
		return PublicCapture{}, err
	}
	if len(events) == 0 {
		return PublicCapture{}, domainError("CAPTURE_EMPTY", "there is nothing to normalize")
	}
	completeness := source.completeness()
	derived, err := normalizer.Normalize(capture.Input{
		Events:        events,
		MissingRanges: completeness.MissingRanges,
		Closed:        completeness.Closed,
		EndReason:     completeness.EndReason,
	})
	if err != nil {
		return PublicCapture{}, domainError("NORMALIZATION_FAILED", err.Error())
	}

	now := s.now()
	derivedID, err := newUUIDv7(now)
	if err != nil {
		return PublicCapture{}, err
	}
	record := &captureRecord{PublicCapture: PublicCapture{
		CaptureID:            derivedID,
		SessionID:            source.SessionID,
		ExecutionID:          source.ExecutionID,
		ProducerClientID:     source.ProducerClientID,
		ProducerClass:        ProducerNormalizer,
		CaptureMethod:        "normalizer/" + normalizer.ID() + "@" + normalizer.Version(),
		AdapterID:            source.AdapterID,
		AdapterVersion:       source.AdapterVersion,
		Status:               string(CaptureClosed),
		CreatedAt:            now,
		ClosedAt:             &now,
		DerivedFromCaptureID: captureID,
		Normalizer:           &NormalizerRef{ID: normalizer.ID(), Version: normalizer.Version()},
	}}
	record.Close = &CaptureClose{EndReason: "derivation-complete", ClosedAt: now}

	before := s.snapshotLocked()
	if len(derived) > 0 {
		body, err := capture.CanonicalDerivedBatchBytes(derived)
		if err != nil {
			return PublicCapture{}, domainError("NORMALIZATION_FAILED", "derived batch could not be canonically encoded")
		}
		contentHash, err := s.objects.Put(body)
		if err != nil {
			return PublicCapture{}, domainError("STATE_PERSISTENCE_FAILED", "derivation could not be persisted")
		}
		record.Index = append(record.Index, CaptureIndexEntry{
			ContentHash: contentHash, Kind: CaptureEntryDerived,
			FirstSequence: derived[0].Event.Sequence, LastSequence: derived[len(derived)-1].Event.Sequence,
			EventCount: uint64(len(derived)), Bytes: int64(len(body)), ReceivedAt: now,
		})
		record.Close.FinalSequence = derived[len(derived)-1].Event.Sequence
	}
	s.captures[derivedID] = record
	source.DerivationIDs = appendUnique(source.DerivationIDs, derivedID)
	s.refreshSessionCapturesLocked(session)
	if err := s.commitLocked(before); err != nil {
		return PublicCapture{}, err
	}
	return s.captures[derivedID].public(), nil
}

func (s *Service) existingDerivationLocked(sourceID string, normalizer capture.Normalizer) *captureRecord {
	for _, record := range s.captures {
		if record.DerivedFromCaptureID != sourceID || record.Normalizer == nil {
			continue
		}
		if record.Normalizer.ID == normalizer.ID() && record.Normalizer.Version == normalizer.Version() {
			return record
		}
	}
	return nil
}

func (s *Service) rawEventsLocked(record *captureRecord) ([]capture.RawEvent, error) {
	events := make([]capture.RawEvent, 0)
	for _, entry := range record.Index {
		if entry.Kind != CaptureEntryRaw {
			continue
		}
		body, err := s.objects.Get(entry.ContentHash)
		if err != nil {
			return nil, domainError("OBSERVATION_UNREADABLE", "a persisted observation batch could not be read")
		}
		decoded, err := capture.DecodeCanonicalBatch(body)
		if err != nil {
			return nil, domainError("OBSERVATION_UNREADABLE", "a persisted observation batch is not canonical")
		}
		events = append(events, decoded...)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	return events, nil
}
