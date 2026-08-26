package externalruntime

import (
	"sort"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

// CaptureStatus is the lifecycle of one producer's observation stream. It is
// deliberately not a completeness verdict: a stream that closed with gaps is
// `closed`, and the gaps live in the completeness manifest where something can
// reason about them. Overloading `degraded` to mean "incomplete" would hide
// the difference between "the producer told us it was struggling" and "the
// broker can see events missing".
type CaptureStatus string

const (
	CaptureOpen      CaptureStatus = "open"
	CaptureClosed    CaptureStatus = "closed"
	CaptureDegraded  CaptureStatus = "degraded"
	CaptureAbandoned CaptureStatus = "abandoned"
)

// ObjectStore is the durability boundary for capture bytes. Event bodies and
// heavy artifacts are content-addressed and stored outside the control
// snapshot, which is rewritten in full on every mutation and therefore cannot
// carry per-event volume.
type ObjectStore interface {
	Put(data []byte) (string, error)
	Get(contentHash string) ([]byte, error)
	Has(contentHash string) bool
	Size(contentHash string) (int64, error)
}

// Capture index entry kinds.
const (
	CaptureEntryRaw     = "raw"
	CaptureEntryObject  = "object"
	CaptureEntryDerived = "derived"
)

// ProducerNormalizer marks a capture the broker produced from another capture.
// A derived stream is never an independent account of the execution, so it is
// excluded from evidence comparison: reconciling a stream against a function
// of itself would manufacture agreement.
const ProducerNormalizer = "normalizer"

const (
	// defaultCaptureMethod is used when an adapter enrolls without naming how
	// it captures. It is deliberately vague rather than a plausible-looking
	// guess: an unlabelled provenance is better than a wrong one.
	defaultCaptureMethod = "adapter-reported"
	// MaxCaptureBatchBytes and MaxCaptureBatchEvents are advertised in the
	// stream offer so a client can size its buffer instead of discovering the
	// limit by being rejected.
	MaxCaptureBatchBytes  = 4 << 20
	MaxCaptureBatchEvents = 4096

	// Clock quality is derived, not claimed: it records whether producers gave
	// us their own timestamps or whether receive time is all we have.
	ClockProducerTimed  = "producer-timed"
	ClockPartiallyTimed = "partially-producer-timed"
	ClockReceiveOnly    = "receive-time-only"
)

// CaptureIndexEntry names one immutable object belonging to a capture. The
// index lives in the control snapshot; the bytes it names do not.
//
// This makes a snapshot write O(batches per capture) rather than O(events),
// which is the point. It is not free: a producer that sends one event per
// batch drives O(batches^2) total index bytes over the life of a stream. The
// stream offer therefore advertises a batch size, and adapters are expected to
// batch. The alternative -- an append-only index log -- buys a better bound at
// the cost of a second crash-recovery mechanism, and is not worth it until a
// real producer is observed misbehaving.
type CaptureIndexEntry struct {
	ContentHash string `json:"content_hash"`
	// ProducerDigest covers only what the client sent, so a retry -- which
	// arrives with a new broker receive time and therefore different stored
	// bytes -- is still recognisable as the same batch.
	ProducerDigest string    `json:"producer_digest,omitempty"`
	Kind           string    `json:"kind"`
	FirstSequence  uint64    `json:"first_sequence"`
	LastSequence   uint64    `json:"last_sequence"`
	EventCount     uint64    `json:"event_count"`
	MediaType      string    `json:"media_type,omitempty"`
	Bytes          int64     `json:"bytes"`
	ReceivedAt     time.Time `json:"received_at"`
}

// CaptureClose is the producer's own account of how its stream ended. It is
// retained as a claim, not as truth: the broker's observed ranges are computed
// independently and the two are compared in the completeness manifest.
type CaptureClose struct {
	FinalSequence uint64      `json:"final_sequence"`
	ObservedGaps  [][2]uint64 `json:"observed_gaps,omitempty"`
	LocalDrops    uint64      `json:"local_drops"`
	EndReason     string      `json:"end_reason"`
	ClosedAt      time.Time   `json:"closed_at"`
}

type captureRecord struct {
	PublicCapture
	Index []CaptureIndexEntry
	Close *CaptureClose
	// ClockQuality is accumulated across ingests rather than published on the
	// capture itself: it is a property of the observations, so it belongs in
	// the completeness manifest with the rest of them.
	ClockQuality string
	// DerivationIDs names the derived captures produced from this stream.
	DerivationIDs []string
	// TimedEvents counts events that carried a producer timestamp, which is
	// what ClockQuality is derived from.
	TimedEvents uint64
}

func (r *captureRecord) clone() *captureRecord {
	copied := &captureRecord{PublicCapture: clonePublicCapture(r.PublicCapture), ClockQuality: r.ClockQuality, TimedEvents: r.TimedEvents}
	copied.DerivationIDs = append([]string(nil), r.DerivationIDs...)
	copied.Index = append([]CaptureIndexEntry(nil), r.Index...)
	if r.Close != nil {
		closeCopy := *r.Close
		closeCopy.ObservedGaps = append([][2]uint64(nil), r.Close.ObservedGaps...)
		copied.Close = &closeCopy
	}
	return copied
}

func clonePublicCapture(value PublicCapture) PublicCapture {
	value.Objects = append([]string(nil), value.Objects...)
	if value.Normalizer != nil {
		normalizer := *value.Normalizer
		value.Normalizer = &normalizer
	}
	if value.ClosedAt != nil {
		closedAt := *value.ClosedAt
		value.ClosedAt = &closedAt
	}
	if value.Completeness != nil {
		completeness := *value.Completeness
		completeness.ObservedRanges = append([][2]uint64(nil), value.Completeness.ObservedRanges...)
		completeness.MissingRanges = append([][2]uint64(nil), value.Completeness.MissingRanges...)
		completeness.RawObjectHashes = append([]string(nil), value.Completeness.RawObjectHashes...)
		completeness.DerivationIDs = append([]string(nil), value.Completeness.DerivationIDs...)
		if value.Completeness.ExpectedThrough != nil {
			expected := *value.Completeness.ExpectedThrough
			completeness.ExpectedThrough = &expected
		}
		value.Completeness = &completeness
	}
	return value
}

// observedSequencesLocked reconstructs the sequence numbers the broker holds
// for a capture from its index alone -- no object reads. Ranges are contiguous
// per entry because ingest rejects a non-contiguous batch.
func (r *captureRecord) observedSequences() []uint64 {
	sequences := make([]uint64, 0)
	seen := make(map[uint64]struct{})
	for _, entry := range r.Index {
		if entry.Kind != CaptureEntryRaw {
			continue
		}
		for sequence := entry.FirstSequence; sequence <= entry.LastSequence; sequence++ {
			if _, ok := seen[sequence]; ok {
				continue
			}
			seen[sequence] = struct{}{}
			sequences = append(sequences, sequence)
		}
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	return sequences
}

// completeness builds the public manifest. The gap arithmetic is
// internal/capture's, reused rather than reimplemented, so the manifest and
// the ingest receipt can never disagree about what is missing.
func (r *captureRecord) completeness() CaptureCompleteness {
	sequences := r.observedSequences()
	manifest := CaptureCompleteness{
		ObservedRanges:  capture.Ranges(sequences),
		MissingRanges:   capture.MissingRanges(sequences),
		EventCount:      uint64(len(sequences)),
		RawObjectHashes: r.rawObjectHashes(),
		DerivationIDs:   append([]string(nil), r.DerivationIDs...),
		ClockQuality:    r.ClockQuality,
		SourceCoverage:  string(r.ProducerClass),
	}
	if r.Close != nil {
		expected := r.Close.FinalSequence
		manifest.ExpectedThrough = &expected
		manifest.MissingRanges = capture.MissingThrough(sequences, expected)
		manifest.LocalDrops = r.Close.LocalDrops
		manifest.Closed = true
		manifest.EndReason = r.Close.EndReason
	}
	if manifest.ObservedRanges == nil {
		manifest.ObservedRanges = [][2]uint64{}
	}
	if manifest.MissingRanges == nil {
		manifest.MissingRanges = [][2]uint64{}
	}
	return manifest
}

func (r *captureRecord) rawObjectHashes() []string {
	hashes := make([]string, 0, len(r.Index))
	for _, entry := range r.Index {
		if entry.Kind == CaptureEntryRaw {
			hashes = append(hashes, entry.ContentHash)
		}
	}
	return hashes
}

// public renders the capture for a known-ID read, completeness included.
func (r *captureRecord) public() PublicCapture {
	rendered := clonePublicCapture(r.PublicCapture)
	completeness := r.completeness()
	rendered.Completeness = &completeness
	return rendered
}

// GetCapture is a known-ID public read. As with sessions, there is no
// collection endpoint: knowing an id is the whole access-control model.
func (s *Service) GetCapture(captureID string) (PublicCapture, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.captures[captureID]
	if !ok {
		return PublicCapture{}, domainError("CAPTURE_NOT_FOUND", "capture was not found")
	}
	return record.public(), nil
}

// ListSessionCaptures is scoped to one known session id rather than being a
// global capture listing, for the same reason.
func (s *Service) ListSessionCaptures(sessionID string) ([]PublicCapture, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, domainError("SESSION_NOT_FOUND", "session was not found")
	}
	captures := make([]PublicCapture, 0, len(session.CaptureIDs))
	for _, captureID := range session.CaptureIDs {
		record, ok := s.captures[captureID]
		if !ok {
			continue
		}
		captures = append(captures, record.public())
	}
	return captures, nil
}

// mintCaptureLocked opens one stream per enrolled client when the session's
// capture policy asks for semantic events. The producer identity is the
// enrollment, so the capture needs no credential of its own: the client lease
// already names exactly one producer.
func (s *Service) mintCaptureLocked(session *sessionRecord, enrollment *enrollmentRecord, captureMethod string, now time.Time) (CaptureStreamOffer, bool, error) {
	if !session.CapturePolicy.SemanticEvents {
		return CaptureStreamOffer{}, false, nil
	}
	captureID, err := newUUIDv7(now)
	if err != nil {
		return CaptureStreamOffer{}, false, err
	}
	if captureMethod == "" {
		captureMethod = defaultCaptureMethod
	}
	record := &captureRecord{PublicCapture: PublicCapture{
		CaptureID:        captureID,
		SessionID:        session.SessionID,
		ExecutionID:      session.executionID,
		ProducerClientID: enrollment.ClientID,
		ProducerClass:    string(enrollment.ClientClass),
		CaptureMethod:    captureMethod,
		AdapterID:        enrollment.AdapterID,
		AdapterVersion:   enrollment.AdapterVersion,
		Status:           string(CaptureOpen),
		CreatedAt:        now,
	}}
	s.captures[captureID] = record
	return CaptureStreamOffer{
		SchemaVersion:  SchemaVersion,
		CaptureID:      captureID,
		ProducerClass:  enrollment.ClientClass,
		CaptureMethod:  captureMethod,
		MaxBatchBytes:  MaxCaptureBatchBytes,
		MaxBatchEvents: MaxCaptureBatchEvents,
		MaxObjectBytes: capture.MaxObjectBytes,
	}, true, nil
}

// refreshSessionCapturesLocked keeps PublicSession.CaptureIDs in agreement
// with the capture map, in a stable order so the snapshot does not churn.
func (s *Service) refreshSessionCapturesLocked(session *sessionRecord) {
	ids := make([]string, 0, len(session.CaptureIDs))
	for id, record := range s.captures {
		if record.SessionID == session.SessionID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		session.CaptureIDs = nil
		return
	}
	session.CaptureIDs = ids
}

// abandonExpiredCapturesLocked is the converger's capture duty: a producer
// whose enrollment has ended without closing its stream leaves an `abandoned`
// capture rather than an eternally `open` one. It reports whether it changed
// anything so the sweep does not rewrite the snapshot on every request.
func (s *Service) abandonExpiredCapturesLocked(now time.Time) bool {
	changed := false
	for _, record := range s.captures {
		if record.Status != string(CaptureOpen) && record.Status != string(CaptureDegraded) {
			continue
		}
		enrollment, ok := s.enrollments[record.ProducerClientID]
		if !ok {
			continue
		}
		if enrollment.Phase != EnrollmentDeparted && enrollment.Phase != EnrollmentLost && enrollment.Phase != EnrollmentExpired {
			continue
		}
		record.Status = string(CaptureAbandoned)
		closedAt := now
		record.ClosedAt = &closedAt
		changed = true
	}
	return changed
}
