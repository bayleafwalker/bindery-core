package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

var (
	ErrSequenceConflict = errors.New("capture sequence conflicts with immutable raw observation")
	ErrBatchInvalid     = errors.New("capture batch is invalid")
)

type RawEvent struct {
	EventID          string          `json:"event_id"`
	SessionID        string          `json:"session_id"`
	ExecutionID      string          `json:"execution_id"`
	CaptureID        string          `json:"capture_id"`
	ProducerClientID string          `json:"producer_client_id"`
	ProducerClass    string          `json:"producer_class"`
	CaptureMethod    string          `json:"capture_method"`
	AdapterID        string          `json:"adapter_id"`
	AdapterVersion   string          `json:"adapter_version"`
	Sequence         uint64          `json:"sequence"`
	GameTick         *uint64         `json:"game_tick,omitempty"`
	ProducerTime     *time.Time      `json:"producer_time,omitempty"`
	ReceivedAt       time.Time       `json:"received_at"`
	EventType        string          `json:"event_type"`
	PayloadVersion   string          `json:"payload_version,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}

type Batch struct {
	CaptureID        string     `json:"capture_id"`
	ExecutionID      string     `json:"execution_id"`
	ProducerClientID string     `json:"producer_client_id"`
	FirstSequence    uint64     `json:"first_sequence"`
	LastSequence     uint64     `json:"last_sequence"`
	ContentHash      string     `json:"content_hash,omitempty"`
	Events           []RawEvent `json:"events"`
}

type Receipt struct {
	CaptureID           string      `json:"capture_id"`
	ExecutionID         string      `json:"execution_id"`
	ProducerClientID    string      `json:"producer_client_id"`
	FirstSequence       uint64      `json:"first_sequence"`
	LastSequence        uint64      `json:"last_sequence"`
	AcknowledgedThrough int64       `json:"acknowledged_through"`
	MissingRanges       [][2]uint64 `json:"missing_ranges,omitempty"`
	RawObjectHash       string      `json:"raw_object_hash"`
	Duplicate           bool        `json:"duplicate,omitempty"`
}

type StreamClose struct {
	ExecutionID   string      `json:"execution_id"`
	FinalSequence uint64      `json:"final_sequence"`
	ObservedGaps  [][2]uint64 `json:"observed_gaps,omitempty"`
	LocalDrops    uint64      `json:"local_drops"`
	EndReason     string      `json:"end_reason"`
	ClosedAt      time.Time   `json:"closed_at"`
}

type CompletenessManifest struct {
	CaptureID        string      `json:"capture_id"`
	ExecutionID      string      `json:"execution_id"`
	ProducerClientID string      `json:"producer_client_id"`
	ExpectedThrough  *uint64     `json:"expected_through,omitempty"`
	ObservedRanges   [][2]uint64 `json:"observed_ranges"`
	MissingRanges    [][2]uint64 `json:"missing_ranges"`
	LocalDrops       uint64      `json:"local_drops"`
	RawObjectHashes  []string    `json:"raw_object_hashes"`
	DerivationIDs    []string    `json:"derivation_ids"`
	Closed           bool        `json:"closed"`
	EndReason        string      `json:"end_reason,omitempty"`
}

type stream struct {
	executionID string
	events      map[uint64]RawEvent
	batches     map[string]string
	objects     []string
	close       *StreamClose
	derivations []string
}

type Store struct {
	mu      sync.RWMutex
	streams map[string]*stream
}

func NewStore() *Store { return &Store{streams: make(map[string]*stream)} }

func (s *Store) Ingest(batch Batch) (Receipt, error) {
	if err := validateBatch(batch); err != nil {
		return Receipt{}, err
	}
	if batch.ContentHash == "" {
		batch.ContentHash = hashEvents(batch.Events)
	}
	key := streamKey(batch.CaptureID, batch.ProducerClientID)
	batchKey := fmt.Sprintf("%s:%d:%d", key, batch.FirstSequence, batch.LastSequence)
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.streams[key]
	if state == nil {
		state = &stream{executionID: batch.ExecutionID, events: make(map[uint64]RawEvent), batches: make(map[string]string)}
		s.streams[key] = state
	}
	if state.executionID != batch.ExecutionID {
		return Receipt{}, ErrSequenceConflict
	}
	if previous, ok := state.batches[batchKey]; ok {
		if previous != batch.ContentHash {
			return Receipt{}, ErrSequenceConflict
		}
		return s.receiptLocked(batch, state, true), nil
	}
	for _, event := range batch.Events {
		if previous, ok := state.events[event.Sequence]; ok && !bytes.Equal(previous.Payload, event.Payload) {
			return Receipt{}, ErrSequenceConflict
		}
	}
	for _, event := range batch.Events {
		state.events[event.Sequence] = cloneEvent(event)
	}
	state.batches[batchKey] = batch.ContentHash
	state.objects = append(state.objects, batch.ContentHash)
	return s.receiptLocked(batch, state, false), nil
}

func (s *Store) Close(captureID, producerID string, close StreamClose) error {
	key := streamKey(captureID, producerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.streams[key]
	if state == nil {
		state = &stream{executionID: close.ExecutionID, events: make(map[uint64]RawEvent), batches: make(map[string]string)}
		s.streams[key] = state
	}
	if close.ExecutionID == "" || close.EndReason == "" || state.executionID != close.ExecutionID {
		return ErrBatchInvalid
	}
	state.close = &close
	return nil
}

func (s *Store) AppendDerivation(captureID, producerID, derivationID string) error {
	if derivationID == "" {
		return ErrBatchInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := streamKey(captureID, producerID)
	state := s.streams[key]
	if state == nil {
		return errors.New("capture stream not found")
	}
	state.derivations = append(state.derivations, derivationID)
	return nil
}

func (s *Store) Manifest(captureID, producerID string) (CompletenessManifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.streams[streamKey(captureID, producerID)]
	if !ok {
		return CompletenessManifest{}, errors.New("capture stream not found")
	}
	sequences := make([]uint64, 0, len(state.events))
	for sequence := range state.events {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	ranges := toRanges(sequences)
	manifest := CompletenessManifest{CaptureID: captureID, ExecutionID: state.executionID, ProducerClientID: producerID, ObservedRanges: ranges, MissingRanges: missingRanges(sequences), RawObjectHashes: unique(state.objects), DerivationIDs: append([]string(nil), state.derivations...)}
	if state.close != nil {
		expected := state.close.FinalSequence
		manifest.ExpectedThrough = &expected
		manifest.LocalDrops = state.close.LocalDrops
		manifest.Closed = true
		manifest.EndReason = state.close.EndReason
		manifest.MissingRanges = missingThrough(sequences, expected)
	}
	return manifest, nil
}

func (s *Store) receiptLocked(batch Batch, state *stream, duplicate bool) Receipt {
	return Receipt{CaptureID: batch.CaptureID, ExecutionID: batch.ExecutionID, ProducerClientID: batch.ProducerClientID, FirstSequence: batch.FirstSequence, LastSequence: batch.LastSequence, AcknowledgedThrough: acknowledgedThrough(state.events), MissingRanges: missingRangesFrom(state.events), RawObjectHash: batch.ContentHash, Duplicate: duplicate}
}

func validateBatch(batch Batch) error {
	if batch.CaptureID == "" || batch.ExecutionID == "" || batch.ProducerClientID == "" || len(batch.Events) == 0 || batch.FirstSequence > batch.LastSequence {
		return ErrBatchInvalid
	}
	if uint64(len(batch.Events)) != batch.LastSequence-batch.FirstSequence+1 {
		return ErrBatchInvalid
	}
	for index, event := range batch.Events {
		if event.CaptureID != batch.CaptureID || event.ExecutionID != batch.ExecutionID || event.ProducerClientID != batch.ProducerClientID || event.Sequence != batch.FirstSequence+uint64(index) || len(event.Payload) == 0 || !json.Valid(event.Payload) {
			return ErrBatchInvalid
		}
	}
	return nil
}

func streamKey(captureID, producerID string) string { return captureID + ":" + producerID }

func hashEvents(events []RawEvent) string {
	encoded, _ := json.Marshal(events)
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func cloneEvent(event RawEvent) RawEvent {
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	return event
}

func acknowledgedThrough(events map[uint64]RawEvent) int64 {
	sequence := uint64(0)
	for {
		if _, ok := events[sequence]; !ok {
			return int64(sequence) - 1
		}
		sequence++
	}
}

func missingRangesFrom(events map[uint64]RawEvent) [][2]uint64 {
	sequences := make([]uint64, 0, len(events))
	for sequence := range events {
		sequences = append(sequences, sequence)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	return missingRanges(sequences)
}

func missingRanges(sequences []uint64) [][2]uint64 {
	if len(sequences) < 2 {
		return nil
	}
	result := make([][2]uint64, 0)
	for i := 1; i < len(sequences); i++ {
		if sequences[i] > sequences[i-1]+1 {
			result = append(result, [2]uint64{sequences[i-1] + 1, sequences[i] - 1})
		}
	}
	return result
}

func missingThrough(sequences []uint64, final uint64) [][2]uint64 {
	present := make(map[uint64]struct{}, len(sequences))
	for _, sequence := range sequences {
		if sequence <= final {
			present[sequence] = struct{}{}
		}
	}
	result := make([][2]uint64, 0)
	start, inGap := uint64(0), false
	for sequence := uint64(0); sequence <= final; sequence++ {
		_, exists := present[sequence]
		if !exists && !inGap {
			start, inGap = sequence, true
		}
		if exists && inGap {
			result = append(result, [2]uint64{start, sequence - 1})
			inGap = false
		}
		if sequence == final && inGap {
			result = append(result, [2]uint64{start, final})
		}
	}
	return result
}

func toRanges(sequences []uint64) [][2]uint64 {
	if len(sequences) == 0 {
		return nil
	}
	result := make([][2]uint64, 0)
	start, previous := sequences[0], sequences[0]
	for _, sequence := range sequences[1:] {
		if sequence != previous+1 {
			result = append(result, [2]uint64{start, previous})
			start = sequence
		}
		previous = sequence
	}
	return append(result, [2]uint64{start, previous})
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
