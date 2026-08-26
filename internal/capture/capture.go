package capture

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrBatchInvalid covers every way a batch can be malformed. Callers that
// serve HTTP map it onto their own domain codes; this package deliberately
// does not know about them.
var ErrBatchInvalid = errors.New("capture batch is invalid")

// RawEvent is one immutable observation.
//
// Its JSON tags are load-bearing beyond serialization: canon.go derives the
// content-addressed encoding from this struct, so changing a tag, a field, or
// the field order changes every hash this repository has published. See
// canon_test.go, which freezes the result.
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

// Batch is a contiguous run of observations from one producer.
type Batch struct {
	CaptureID        string     `json:"capture_id"`
	ExecutionID      string     `json:"execution_id"`
	ProducerClientID string     `json:"producer_client_id"`
	FirstSequence    uint64     `json:"first_sequence"`
	LastSequence     uint64     `json:"last_sequence"`
	ContentHash      string     `json:"content_hash,omitempty"`
	Events           []RawEvent `json:"events"`
}

// StreamClose is a producer's own account of how its stream ended. It is a
// claim, retained beside what the broker independently observed rather than in
// place of it.
type StreamClose struct {
	ExecutionID   string      `json:"execution_id"`
	FinalSequence uint64      `json:"final_sequence"`
	ObservedGaps  [][2]uint64 `json:"observed_gaps,omitempty"`
	LocalDrops    uint64      `json:"local_drops"`
	EndReason     string      `json:"end_reason"`
	ClosedAt      time.Time   `json:"closed_at"`
}

// ValidateBatch is the single definition of a well-formed batch. The served
// control plane calls it rather than re-deriving the rules, so HTTP ingest and
// any other caller cannot disagree about what they accept.
func ValidateBatch(batch Batch) error { return validateBatch(batch) }

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

// The sequence arithmetic below is shared by the ingest receipt and the
// completeness manifest. Having one implementation is the point: two would
// eventually disagree about what is missing, and the manifest exists precisely
// to be believed about that.

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

// missingThrough also accounts for sequences the producer promised at close
// but never delivered, which a gap analysis over what arrived cannot see.
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
