package externalruntime

// CaptureCloseRequest is the producer's final account of its own stream. Every
// field in it is a claim: the broker records it next to what it independently
// observed rather than in place of it, so a producer that under-reports its
// drops is contradicted by the manifest instead of believed by it.
type CaptureCloseRequest struct {
	FinalSequence uint64      `json:"final_sequence"`
	ObservedGaps  [][2]uint64 `json:"observed_gaps,omitempty"`
	LocalDrops    uint64      `json:"local_drops"`
	EndReason     string      `json:"end_reason"`
}

// CloseCapture ends a producer stream and publishes its completeness manifest.
//
// A stream that closes with gaps closes as `closed`, not `degraded`: the gaps
// are a fact about the observations and belong in the manifest, where the
// completeness gate can weigh them. `degraded` is reserved for the producer
// telling us its capture was impaired, which is a different claim.
func (s *Service) CloseCapture(clientLease, captureID string, req CaptureCloseRequest) (PublicCapture, error) {
	if req.EndReason == "" || len(req.EndReason) > 256 {
		return PublicCapture{}, domainError("END_REASON_REQUIRED", "an end reason between 1 and 256 characters is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return PublicCapture{}, err
	}
	record, ok := s.captures[captureID]
	if !ok {
		return PublicCapture{}, domainError("CAPTURE_NOT_FOUND", "capture was not found")
	}
	enrollment, ok := s.enrollments[record.ProducerClientID]
	if !ok {
		return PublicCapture{}, domainError("STATE_INTEGRITY_ERROR", "capture producer is not resolvable")
	}
	if !verifyCredential(clientLease, enrollment.leaseVerifier) {
		return PublicCapture{}, domainError("TOKEN_INVALID", "client lease token is invalid")
	}
	if record.Close != nil {
		if !sameClose(record.Close, req) {
			return PublicCapture{}, domainError("IDEMPOTENCY_CONFLICT", "capture was already closed with a different account")
		}
		return record.public(), nil
	}
	if CaptureStatus(record.Status) == CaptureAbandoned {
		return PublicCapture{}, domainError("CAPTURE_NOT_OPEN", "capture was abandoned after its producer lease ended")
	}

	before := s.snapshotLocked()
	now := s.now()
	record.Close = &CaptureClose{
		FinalSequence: req.FinalSequence,
		ObservedGaps:  append([][2]uint64(nil), req.ObservedGaps...),
		LocalDrops:    req.LocalDrops,
		EndReason:     req.EndReason,
		ClosedAt:      now,
	}
	record.Status = string(CaptureClosed)
	closedAt := now
	record.ClosedAt = &closedAt
	if err := s.commitLocked(before); err != nil {
		return PublicCapture{}, err
	}
	return s.captures[captureID].public(), nil
}

// markCaptureDegradedLocked records a producer's own report that its capture
// is impaired. It never ends the stream and never touches the match: a
// degraded observer is a worse witness, not a failed player.
func (s *Service) markCaptureDegradedLocked(clientID string) {
	for _, record := range s.captures {
		if record.ProducerClientID != clientID {
			continue
		}
		if CaptureStatus(record.Status) == CaptureOpen {
			record.Status = string(CaptureDegraded)
		}
	}
}

func sameClose(existing *CaptureClose, req CaptureCloseRequest) bool {
	if existing.FinalSequence != req.FinalSequence || existing.LocalDrops != req.LocalDrops || existing.EndReason != req.EndReason {
		return false
	}
	if len(existing.ObservedGaps) != len(req.ObservedGaps) {
		return false
	}
	for index, gap := range existing.ObservedGaps {
		if gap != req.ObservedGaps[index] {
			return false
		}
	}
	return true
}
