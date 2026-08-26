package externalruntime

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

const (
	defaultEventPageSize = 500
	maxEventPageSize     = 1000
)

// PublicTelemetryEvent is the published form of one observation. The envelope
// is fixed by contracts/externalruntime/v1/telemetry-event.schema.json;
// `payload` stays opaque, because a core that understood game payloads would
// be a core with game-specific fields in it.
type PublicTelemetryEvent struct {
	Schema           string          `json:"schema"`
	SchemaVersion    string          `json:"schema_version"`
	EventID          string          `json:"event_id"`
	SessionID        string          `json:"session_id"`
	ExecutionID      string          `json:"execution_id"`
	CaptureID        string          `json:"capture_id"`
	ProducerClientID string          `json:"producer_client_id"`
	ProducerClass    string          `json:"producer_class"`
	CaptureMethod    string          `json:"capture_method"`
	Adapter          AdapterRef      `json:"adapter"`
	Sequence         uint64          `json:"sequence"`
	GameTick         *uint64         `json:"game_tick,omitempty"`
	ProducerTime     *time.Time      `json:"producer_time,omitempty"`
	ReceivedAt       time.Time       `json:"received_at"`
	EventType        string          `json:"event_type"`
	PayloadVersion   string          `json:"payload_version,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	RawSource        *RawSourceRef   `json:"raw_source,omitempty"`
	// Derivation is present only on derived events, and names the normalizer
	// and the observations the fact was computed from. Its absence is what
	// marks an event as an observation rather than an interpretation.
	Derivation *capture.Derivation `json:"derivation,omitempty"`
}

// RawSourceRef links a published event back to the immutable batch object it
// was read out of, so a reader can re-derive the event rather than trust this
// rendering of it.
type RawSourceRef struct {
	ObjectHash string `json:"object_hash"`
}

// EventPage is a cursor-paged read. `next_cursor` is absent at the tail rather
// than empty, so "there is more" and "there is not" are different shapes.
type EventPage struct {
	SchemaVersion string                 `json:"schema_version"`
	Events        []PublicTelemetryEvent `json:"events"`
	NextCursor    string                 `json:"next_cursor,omitempty"`
	// Ordering names what the sequence of events means. For a session read it
	// is explicitly a retrieval order and not a causal one: cross-producer
	// ordering is not something the broker is in a position to know.
	Ordering string `json:"ordering"`
}

const (
	orderingBySequence   = "capture-sequence"
	orderingByReceipt    = "broker-receipt-order"
	telemetryEventSchema = "bindery.telemetry.event"
)

type captureCursor struct {
	AfterSequence *uint64 `json:"after_sequence"`
}

type sessionCursor struct {
	ReceivedAt time.Time `json:"received_at"`
	CaptureID  string    `json:"capture_id"`
	Sequence   uint64    `json:"sequence"`
}

func encodeCursor(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCursor(raw string, destination any) error {
	if raw == "" {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return domainError("INVALID_CURSOR", "cursor is not a valid page token")
	}
	if err := json.Unmarshal(decoded, destination); err != nil {
		return domainError("INVALID_CURSOR", "cursor is not a valid page token")
	}
	return nil
}

func clampPageSize(limit int) int {
	if limit <= 0 {
		return defaultEventPageSize
	}
	if limit > maxEventPageSize {
		return maxEventPageSize
	}
	return limit
}

// readPlan is the snapshot of index state a read needs. It is taken under the
// lock and then released: object bytes are immutable and content-addressed, so
// there is no reason to hold the control-plane lock while reading a
// six-thousand-event page off disk.
type readPlan struct {
	metadata PublicCapture
	entries  []CaptureIndexEntry
}

// EventKindRaw and EventKindDerived select which side of a capture to read.
const (
	EventKindRaw     = "raw"
	EventKindDerived = "derived"
)

func entryKindFor(kind string) (string, error) {
	switch kind {
	case "", EventKindRaw:
		return CaptureEntryRaw, nil
	case EventKindDerived:
		return CaptureEntryDerived, nil
	default:
		return "", domainError("EVENT_KIND_INVALID", "kind must be raw or derived")
	}
}

func (s *Service) captureReadPlans(captureIDs []string, entryKind string) ([]readPlan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plans := make([]readPlan, 0, len(captureIDs))
	for _, captureID := range captureIDs {
		record, ok := s.captures[captureID]
		if !ok {
			return nil, domainError("CAPTURE_NOT_FOUND", "capture was not found")
		}
		entries := make([]CaptureIndexEntry, 0, len(record.Index))
		for _, entry := range record.Index {
			if entry.Kind == entryKind {
				entries = append(entries, entry)
			}
		}
		plans = append(plans, readPlan{metadata: clonePublicCapture(record.PublicCapture), entries: entries})
	}
	return plans, nil
}

// ReadCaptureEvents pages one producer's stream in sequence order, the only
// ordering the contract guarantees.
func (s *Service) ReadCaptureEvents(captureID, cursor, kind string, limit int) (EventPage, error) {
	entryKind, err := entryKindFor(kind)
	if err != nil {
		return EventPage{}, err
	}
	var position captureCursor
	if err := decodeCursor(cursor, &position); err != nil {
		return EventPage{}, err
	}
	plans, err := s.captureReadPlans([]string{captureID}, entryKind)
	if err != nil {
		return EventPage{}, err
	}
	plan := plans[0]
	sort.Slice(plan.entries, func(i, j int) bool { return plan.entries[i].FirstSequence < plan.entries[j].FirstSequence })
	limit = clampPageSize(limit)

	page := EventPage{SchemaVersion: SchemaVersion, Events: make([]PublicTelemetryEvent, 0, limit), Ordering: orderingBySequence}
	for _, entry := range plan.entries {
		if position.AfterSequence != nil && entry.LastSequence <= *position.AfterSequence {
			continue
		}
		events, err := s.readBatch(entry)
		if err != nil {
			return EventPage{}, err
		}
		for _, event := range events {
			if position.AfterSequence != nil && event.Event.Sequence <= *position.AfterSequence {
				continue
			}
			if len(page.Events) == limit {
				last := page.Events[len(page.Events)-1].Sequence
				next, err := encodeCursor(captureCursor{AfterSequence: &last})
				if err != nil {
					return EventPage{}, err
				}
				page.NextCursor = next
				return page, nil
			}
			page.Events = append(page.Events, publishEvent(plan.metadata, event, entry.ContentHash))
		}
	}
	return page, nil
}

// ReadSessionEvents interleaves every producer on a session.
//
// There is no total order across producers and the broker does not invent one:
// events are returned in the order the broker received their batches, with
// capture id and sequence as deterministic tie-breaks. That is a retrieval
// order. Reconstructing what actually happened first is the reader's problem,
// and game tick is the field to do it with.
func (s *Service) ReadSessionEvents(sessionID, cursor, kind string, limit int) (EventPage, error) {
	entryKind, err := entryKindFor(kind)
	if err != nil {
		return EventPage{}, err
	}
	var position sessionCursor
	if err := decodeCursor(cursor, &position); err != nil {
		return EventPage{}, err
	}
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	var captureIDs []string
	if ok {
		captureIDs = append([]string(nil), session.CaptureIDs...)
	}
	s.mu.RUnlock()
	if !ok {
		return EventPage{}, domainError("SESSION_NOT_FOUND", "session was not found")
	}
	plans, err := s.captureReadPlans(captureIDs, entryKind)
	if err != nil {
		return EventPage{}, err
	}

	type placedEntry struct {
		entry    CaptureIndexEntry
		metadata PublicCapture
	}
	placed := make([]placedEntry, 0)
	for _, plan := range plans {
		for _, entry := range plan.entries {
			placed = append(placed, placedEntry{entry: entry, metadata: plan.metadata})
		}
	}
	sort.Slice(placed, func(i, j int) bool {
		if !placed[i].entry.ReceivedAt.Equal(placed[j].entry.ReceivedAt) {
			return placed[i].entry.ReceivedAt.Before(placed[j].entry.ReceivedAt)
		}
		if placed[i].metadata.CaptureID != placed[j].metadata.CaptureID {
			return placed[i].metadata.CaptureID < placed[j].metadata.CaptureID
		}
		return placed[i].entry.FirstSequence < placed[j].entry.FirstSequence
	})

	limit = clampPageSize(limit)
	started := cursor == ""
	page := EventPage{SchemaVersion: SchemaVersion, Events: make([]PublicTelemetryEvent, 0, limit), Ordering: orderingByReceipt}
	for _, item := range placed {
		if !started && precedesCursor(item.entry, item.metadata.CaptureID, position) {
			continue
		}
		events, err := s.readBatch(item.entry)
		if err != nil {
			return EventPage{}, err
		}
		for _, event := range events {
			if !started {
				if item.metadata.CaptureID == position.CaptureID && item.entry.ReceivedAt.Equal(position.ReceivedAt) && event.Event.Sequence <= position.Sequence {
					continue
				}
			}
			if len(page.Events) == limit {
				last := page.Events[len(page.Events)-1]
				next, err := encodeCursor(sessionCursor{ReceivedAt: last.ReceivedAt, CaptureID: last.CaptureID, Sequence: last.Sequence})
				if err != nil {
					return EventPage{}, err
				}
				page.NextCursor = next
				return page, nil
			}
			page.Events = append(page.Events, publishEvent(item.metadata, event, item.entry.ContentHash))
		}
	}
	return page, nil
}

func precedesCursor(entry CaptureIndexEntry, captureID string, position sessionCursor) bool {
	if entry.ReceivedAt.Before(position.ReceivedAt) {
		return true
	}
	if !entry.ReceivedAt.Equal(position.ReceivedAt) {
		return false
	}
	if captureID < position.CaptureID {
		return true
	}
	if captureID > position.CaptureID {
		return false
	}
	return entry.LastSequence <= position.Sequence
}

// readBatch returns one persisted batch, raw or derived, in sequence order.
// Raw events come back with a zero derivation, which is exactly the
// distinction the published envelope preserves.
func (s *Service) readBatch(entry CaptureIndexEntry) ([]capture.DerivedEvent, error) {
	body, err := s.objects.Get(entry.ContentHash)
	if err != nil {
		return nil, domainError("OBSERVATION_UNREADABLE", "a persisted observation batch could not be read")
	}
	var events []capture.DerivedEvent
	if entry.Kind == CaptureEntryDerived {
		events, err = capture.DecodeCanonicalDerivedBatch(body)
		if err != nil {
			return nil, domainError("OBSERVATION_UNREADABLE", "a persisted derivation is not canonical")
		}
	} else {
		raw, decodeErr := capture.DecodeCanonicalBatch(body)
		if decodeErr != nil {
			return nil, domainError("OBSERVATION_UNREADABLE", "a persisted observation batch is not canonical")
		}
		events = make([]capture.DerivedEvent, 0, len(raw))
		for _, event := range raw {
			events = append(events, capture.DerivedEvent{Event: event})
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Event.Sequence < events[j].Event.Sequence })
	return events, nil
}

func publishEvent(metadata PublicCapture, derived capture.DerivedEvent, objectHash string) PublicTelemetryEvent {
	event := derived.Event
	published := PublicTelemetryEvent{
		Schema:           telemetryEventSchema,
		SchemaVersion:    SchemaVersion,
		EventID:          event.EventID,
		SessionID:        metadata.SessionID,
		ExecutionID:      metadata.ExecutionID,
		CaptureID:        metadata.CaptureID,
		ProducerClientID: metadata.ProducerClientID,
		ProducerClass:    metadata.ProducerClass,
		CaptureMethod:    event.CaptureMethod,
		Adapter:          AdapterRef{ID: event.AdapterID, Version: event.AdapterVersion},
		Sequence:         event.Sequence,
		GameTick:         event.GameTick,
		ProducerTime:     event.ProducerTime,
		ReceivedAt:       event.ReceivedAt,
		EventType:        event.EventType,
		PayloadVersion:   event.PayloadVersion,
		Payload:          event.Payload,
		RawSource:        &RawSourceRef{ObjectHash: objectHash},
	}
	if derived.Derivation.NormalizerID != "" {
		derivation := derived.Derivation
		derivation.SourceEventIDs = append([]string(nil), derived.Derivation.SourceEventIDs...)
		published.Derivation = &derivation
	}
	return published
}
