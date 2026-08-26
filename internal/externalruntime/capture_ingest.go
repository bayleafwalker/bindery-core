package externalruntime

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

// eventTypePattern is the published contract's event-type shape. Enforcing it
// at ingest keeps malformed provenance out of the immutable record, where it
// could not be corrected later without reissuing content hashes.
var eventTypePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)

// TelemetryEventInput is what a producer sends. It deliberately omits
// session, execution, capture, producer, adapter and capture-method: those are
// already fixed by the capture the lease resolves to, so a client cannot
// restate them, and therefore cannot contradict them. It also omits
// received_at, which is the broker's to assign -- a receive time a producer
// could choose would not be a receive time.
type TelemetryEventInput struct {
	EventID        string          `json:"event_id"`
	Sequence       uint64          `json:"sequence"`
	GameTick       *uint64         `json:"game_tick,omitempty"`
	ProducerTime   *time.Time      `json:"producer_time,omitempty"`
	EventType      string          `json:"event_type"`
	PayloadVersion string          `json:"payload_version,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

type IngestBatchRequest struct {
	FirstSequence uint64 `json:"first_sequence"`
	LastSequence  uint64 `json:"last_sequence"`
	// ProducerDigest is the client's own hash over the events it is sending.
	// It cannot be a hash of the stored bytes, because those include a receive
	// time the client does not get to choose.
	ProducerDigest string                `json:"producer_digest,omitempty"`
	Events         []TelemetryEventInput `json:"events"`
}

type CaptureReceipt struct {
	SchemaVersion string `json:"schema_version"`
	CaptureID     string `json:"capture_id"`
	ExecutionID   string `json:"execution_id"`
	// AcknowledgedThrough is the highest contiguous sequence held from zero,
	// and is -1 when sequence zero is still missing. It is not a count.
	AcknowledgedThrough int64       `json:"acknowledged_through"`
	MissingRanges       [][2]uint64 `json:"missing_ranges"`
	RawObjectHash       string      `json:"raw_object_hash"`
	Duplicate           bool        `json:"duplicate,omitempty"`
}

// IngestCaptureBatch accepts one immutable batch of raw observations.
//
// Ingest is at-least-once from the client's side and idempotent on ours, keyed
// by the batch's own content rather than by a header the client chooses: an
// identical retry is answered from state already held and writes nothing at
// all, which is what keeps a retry storm from costing O(state) per attempt.
func (s *Service) IngestCaptureBatch(clientLease, captureID, idempotencyKey string, req IngestBatchRequest) (CaptureReceipt, error) {
	if idempotencyKey == "" {
		return CaptureReceipt{}, domainError("IDEMPOTENCY_KEY_REQUIRED", "an idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.convergeAndPersistLocked(s.now()); err != nil {
		return CaptureReceipt{}, err
	}
	record, ok := s.captures[captureID]
	if !ok {
		return CaptureReceipt{}, domainError("CAPTURE_NOT_FOUND", "capture was not found")
	}
	enrollment, ok := s.enrollments[record.ProducerClientID]
	if !ok {
		return CaptureReceipt{}, domainError("STATE_INTEGRITY_ERROR", "capture producer is not resolvable")
	}
	if !verifyCredential(clientLease, enrollment.leaseVerifier) {
		return CaptureReceipt{}, domainError("TOKEN_INVALID", "client lease token is invalid")
	}
	now := s.now()
	if enrollment.expiresAt.Before(now) {
		return CaptureReceipt{}, domainError("LEASE_EXPIRED", "client lease has expired")
	}
	status := CaptureStatus(record.Status)
	if status != CaptureOpen && status != CaptureDegraded {
		return CaptureReceipt{}, domainError("CAPTURE_NOT_OPEN", "capture stream is "+record.Status+" and no longer accepts batches")
	}

	events, err := s.buildRawEventsLocked(record, req, now)
	if err != nil {
		return CaptureReceipt{}, err
	}
	batch := capture.Batch{
		CaptureID: captureID, ExecutionID: record.ExecutionID, ProducerClientID: record.ProducerClientID,
		FirstSequence: req.FirstSequence, LastSequence: req.LastSequence, Events: events,
	}
	if err := capture.ValidateBatch(batch); err != nil {
		return CaptureReceipt{}, domainError("BATCH_INVALID", "batch shape is invalid: "+err.Error())
	}
	body, err := capture.CanonicalBatchBytes(events)
	if err != nil {
		return CaptureReceipt{}, domainError("BATCH_INVALID", "batch could not be canonically encoded")
	}
	if int64(len(body)) > MaxCaptureBatchBytes {
		return CaptureReceipt{}, domainError("PAYLOAD_TOO_LARGE", "batch exceeds the negotiated size limit")
	}
	contentHash := capture.DigestOf(body)
	producerDigest, err := capture.ProducerDigest(events)
	if err != nil {
		return CaptureReceipt{}, domainError("BATCH_INVALID", "batch could not be canonically encoded")
	}
	if req.ProducerDigest != "" && req.ProducerDigest != producerDigest {
		return CaptureReceipt{}, domainError("SEQUENCE_CONFLICT", "declared producer digest does not match the batch body")
	}

	replay, err := record.replayFor(req.FirstSequence, req.LastSequence, producerDigest)
	if err != nil {
		return CaptureReceipt{}, err
	}
	if replay {
		return s.receiptFor(record, record.storedHashFor(req.FirstSequence, req.LastSequence), true), nil
	}

	// Write the bytes before anything references them. A crash here leaves an
	// object nothing points at, which is inert; the reverse order would leave
	// an index entry naming bytes that do not exist.
	before := s.snapshotLocked()
	if _, err := s.objects.Put(body); err != nil {
		return CaptureReceipt{}, domainError("STATE_PERSISTENCE_FAILED", "capture batch could not be persisted")
	}
	record.Index = append(record.Index, CaptureIndexEntry{
		ContentHash: contentHash, ProducerDigest: producerDigest, Kind: CaptureEntryRaw,
		FirstSequence: req.FirstSequence, LastSequence: req.LastSequence,
		EventCount: uint64(len(events)), Bytes: int64(len(body)), ReceivedAt: now,
	})
	record.recordClockQuality(events)
	if err := s.commitLocked(before); err != nil {
		return CaptureReceipt{}, err
	}
	return s.receiptFor(s.captures[captureID], contentHash, false), nil
}

func (s *Service) buildRawEventsLocked(record *captureRecord, req IngestBatchRequest, now time.Time) ([]capture.RawEvent, error) {
	if len(req.Events) == 0 {
		return nil, domainError("BATCH_INVALID", "batch contains no events")
	}
	if len(req.Events) > MaxCaptureBatchEvents {
		return nil, domainError("PAYLOAD_TOO_LARGE", "batch exceeds the negotiated event count")
	}
	events := make([]capture.RawEvent, 0, len(req.Events))
	for _, input := range req.Events {
		if len(input.EventID) < 16 || len(input.EventID) > 64 {
			return nil, domainError("BATCH_INVALID", "event_id must be between 16 and 64 characters")
		}
		if !eventTypePattern.MatchString(input.EventType) {
			return nil, domainError("BATCH_INVALID", "event_type "+input.EventType+" is not a dotted lowercase type")
		}
		if len(input.Payload) == 0 || !json.Valid(input.Payload) {
			return nil, domainError("BATCH_INVALID", "event payload must be a JSON value")
		}
		event := capture.RawEvent{
			EventID:          input.EventID,
			SessionID:        record.SessionID,
			ExecutionID:      record.ExecutionID,
			CaptureID:        record.CaptureID,
			ProducerClientID: record.ProducerClientID,
			ProducerClass:    record.ProducerClass,
			CaptureMethod:    record.CaptureMethod,
			AdapterID:        record.AdapterID,
			AdapterVersion:   record.AdapterVersion,
			Sequence:         input.Sequence,
			GameTick:         input.GameTick,
			ReceivedAt:       now,
			EventType:        input.EventType,
			PayloadVersion:   input.PayloadVersion,
			Payload:          append(json.RawMessage(nil), input.Payload...),
		}
		if input.ProducerTime != nil {
			producerTime := input.ProducerTime.UTC()
			event.ProducerTime = &producerTime
		}
		events = append(events, event)
	}
	return events, nil
}

// replayFor decides whether this batch has already been accepted.
//
// An exact range match with an identical body is a retry. An exact range match
// with a different body is corruption and says so. A partial overlap is
// refused rather than merged: the broker would have to read and compare every
// persisted event to tell a benign re-batching from a rewrite, and a producer
// that changes its batch boundaries mid-stream is not a case worth guessing at.
func (r *captureRecord) replayFor(first, last uint64, producerDigest string) (bool, error) {
	for _, entry := range r.Index {
		if entry.Kind != CaptureEntryRaw {
			continue
		}
		if entry.FirstSequence == first && entry.LastSequence == last {
			if entry.ProducerDigest == producerDigest {
				return true, nil
			}
			return false, domainError("SEQUENCE_CONFLICT", "this sequence range was already recorded with different bytes")
		}
		if first <= entry.LastSequence && entry.FirstSequence <= last {
			return false, domainError("SEQUENCE_CONFLICT", "batch overlaps an already recorded sequence range")
		}
	}
	return false, nil
}

// storedHashFor returns the content hash actually persisted for a range, so a
// replay receipt names the object the broker holds rather than one the retry
// would have produced.
func (r *captureRecord) storedHashFor(first, last uint64) string {
	for _, entry := range r.Index {
		if entry.Kind == CaptureEntryRaw && entry.FirstSequence == first && entry.LastSequence == last {
			return entry.ContentHash
		}
	}
	return ""
}

func (r *captureRecord) recordClockQuality(events []capture.RawEvent) {
	for _, event := range events {
		if event.ProducerTime != nil {
			r.TimedEvents++
		}
	}
	total := uint64(0)
	for _, entry := range r.Index {
		if entry.Kind == CaptureEntryRaw {
			total += entry.EventCount
		}
	}
	switch {
	case r.TimedEvents == 0:
		r.ClockQuality = ClockReceiveOnly
	case r.TimedEvents == total:
		r.ClockQuality = ClockProducerTimed
	default:
		r.ClockQuality = ClockPartiallyTimed
	}
}

func (s *Service) receiptFor(record *captureRecord, contentHash string, duplicate bool) CaptureReceipt {
	sequences := record.observedSequences()
	missing := capture.MissingRanges(sequences)
	if missing == nil {
		missing = [][2]uint64{}
	}
	return CaptureReceipt{
		SchemaVersion:       SchemaVersion,
		CaptureID:           record.CaptureID,
		ExecutionID:         record.ExecutionID,
		AcknowledgedThrough: capture.AcknowledgedThrough(sequences),
		MissingRanges:       missing,
		RawObjectHash:       contentHash,
		Duplicate:           duplicate,
	}
}
