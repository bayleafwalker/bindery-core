package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Canonical encoding is frozen. These bytes are what gets content-addressed,
// written to the object store, and published as a hash in evidence sets and
// completeness manifests. Changing the struct below -- its field order, its
// tags, or the set of fields -- changes every hash this repository has ever
// published. canon_test.go pins the result against golden vectors so that a
// well-meaning refactor fails loudly instead of silently reissuing history.
//
// encoding/json emits struct fields in declaration order, so this struct is
// the specification: there is no separate key-sorting pass to disagree with.
type canonicalEvent struct {
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
	GameTick         *uint64         `json:"game_tick"`
	ProducerTime     *string         `json:"producer_time"`
	ReceivedAt       string          `json:"received_at"`
	EventType        string          `json:"event_type"`
	PayloadVersion   string          `json:"payload_version"`
	Payload          json.RawMessage `json:"payload"`
}

// HashPrefix is carried by every hash this package emits, matching the
// `^sha256:[0-9a-f]{64}$` pattern the public contract schemas require.
const HashPrefix = "sha256:"

func formatDigest(digest [sha256.Size]byte) string {
	return HashPrefix + hex.EncodeToString(digest[:])
}

func canonicalTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func canonicalize(event RawEvent) (canonicalEvent, error) {
	// Payload is compacted rather than passed through: two clients that agree
	// on the facts must not disagree on the hash because one of them indents.
	var payload bytes.Buffer
	if err := json.Compact(&payload, event.Payload); err != nil {
		return canonicalEvent{}, fmt.Errorf("%w: payload is not valid JSON", ErrBatchInvalid)
	}
	canonical := canonicalEvent{
		EventID:          event.EventID,
		SessionID:        event.SessionID,
		ExecutionID:      event.ExecutionID,
		CaptureID:        event.CaptureID,
		ProducerClientID: event.ProducerClientID,
		ProducerClass:    event.ProducerClass,
		CaptureMethod:    event.CaptureMethod,
		AdapterID:        event.AdapterID,
		AdapterVersion:   event.AdapterVersion,
		Sequence:         event.Sequence,
		GameTick:         event.GameTick,
		ReceivedAt:       canonicalTime(event.ReceivedAt),
		EventType:        event.EventType,
		PayloadVersion:   event.PayloadVersion,
		Payload:          json.RawMessage(payload.Bytes()),
	}
	if event.ProducerTime != nil {
		producerTime := canonicalTime(*event.ProducerTime)
		canonical.ProducerTime = &producerTime
	}
	return canonical, nil
}

// CanonicalEventBytes returns the frozen encoding of a single raw event.
func CanonicalEventBytes(event RawEvent) ([]byte, error) {
	canonical, err := canonicalize(event)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("%w: event is not encodable", ErrBatchInvalid)
	}
	return encoded, nil
}

// EventDigest is the raw sha256 of one canonical event. OrderedHash composes
// these, so it is exposed as the digest rather than the formatted string.
func EventDigest(event RawEvent) ([sha256.Size]byte, error) {
	encoded, err := CanonicalEventBytes(event)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// CanonicalBatchBytes is what the object store persists for a batch: a JSON
// array of canonical events in the order supplied. Ingest validates that the
// order is ascending and contiguous before this is called.
func CanonicalBatchBytes(events []RawEvent) ([]byte, error) {
	canonical := make([]canonicalEvent, 0, len(events))
	for _, event := range events {
		encoded, err := canonicalize(event)
		if err != nil {
			return nil, err
		}
		canonical = append(canonical, encoded)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("%w: batch is not encodable", ErrBatchInvalid)
	}
	return encoded, nil
}

// BatchContentHash content-addresses a batch body.
func BatchContentHash(events []RawEvent) (string, error) {
	encoded, err := CanonicalBatchBytes(events)
	if err != nil {
		return "", err
	}
	return formatDigest(sha256.Sum256(encoded)), nil
}

// OrderedHash is the stream identity used by the `ordered-hash` reconciliation
// method and by broker-derived observation summaries: the sha256 over the
// concatenated event digests in ascending sequence order. It is deliberately
// not the hash of the concatenated batch bodies -- two producers that batched
// the same events differently must still agree.
func OrderedHash(events []RawEvent) (string, error) {
	ordered := make([]RawEvent, len(events))
	copy(ordered, events)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })
	accumulator := sha256.New()
	for _, event := range ordered {
		digest, err := EventDigest(event)
		if err != nil {
			return "", err
		}
		accumulator.Write(digest[:])
	}
	var digest [sha256.Size]byte
	copy(digest[:], accumulator.Sum(nil))
	return formatDigest(digest), nil
}

// DecodeCanonicalBatch reverses CanonicalBatchBytes. Reads go through this so
// that a persisted batch is interpreted by exactly the encoding that produced
// its hash.
func DecodeCanonicalBatch(encoded []byte) ([]RawEvent, error) {
	var canonical []canonicalEvent
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, fmt.Errorf("%w: persisted batch is not canonical", ErrBatchInvalid)
	}
	events := make([]RawEvent, 0, len(canonical))
	for _, entry := range canonical {
		event := RawEvent{
			EventID:          entry.EventID,
			SessionID:        entry.SessionID,
			ExecutionID:      entry.ExecutionID,
			CaptureID:        entry.CaptureID,
			ProducerClientID: entry.ProducerClientID,
			ProducerClass:    entry.ProducerClass,
			CaptureMethod:    entry.CaptureMethod,
			AdapterID:        entry.AdapterID,
			AdapterVersion:   entry.AdapterVersion,
			Sequence:         entry.Sequence,
			GameTick:         entry.GameTick,
			EventType:        entry.EventType,
			PayloadVersion:   entry.PayloadVersion,
			Payload:          append(json.RawMessage(nil), entry.Payload...),
		}
		receivedAt, err := time.Parse(time.RFC3339Nano, entry.ReceivedAt)
		if err != nil {
			return nil, fmt.Errorf("%w: persisted batch has an unparseable receive time", ErrBatchInvalid)
		}
		event.ReceivedAt = receivedAt.UTC()
		if entry.ProducerTime != nil {
			producerTime, err := time.Parse(time.RFC3339Nano, *entry.ProducerTime)
			if err != nil {
				return nil, fmt.Errorf("%w: persisted batch has an unparseable producer time", ErrBatchInvalid)
			}
			producerTime = producerTime.UTC()
			event.ProducerTime = &producerTime
		}
		events = append(events, event)
	}
	return events, nil
}

// Ranges, MissingRanges and MissingThrough are the sequence arithmetic the
// completeness manifest and the ingest receipt both depend on. They are
// exported so the served control plane reuses them rather than growing a
// second, subtly different implementation of "what is missing".
func Ranges(sequences []uint64) [][2]uint64 { return toRanges(sequences) }

func MissingRanges(sequences []uint64) [][2]uint64 { return missingRanges(sequences) }

func MissingThrough(sequences []uint64, final uint64) [][2]uint64 {
	return missingThrough(sequences, final)
}

// AcknowledgedThrough is the highest contiguous sequence held from zero, or -1
// when sequence zero itself is missing.
func AcknowledgedThrough(sequences []uint64) int64 {
	present := make(map[uint64]struct{}, len(sequences))
	for _, sequence := range sequences {
		present[sequence] = struct{}{}
	}
	sequence := uint64(0)
	for {
		if _, ok := present[sequence]; !ok {
			return int64(sequence) - 1
		}
		sequence++
	}
}

// ProducerDigest identifies a batch by what the producer actually sent,
// excluding the fields the broker assigns.
//
// This matters for idempotency. Receive time is stamped on arrival, so a
// retried batch is not byte-identical to its first attempt -- content-
// addressing the stored bytes therefore cannot tell a retry from a rewrite.
// The producer digest can, because it covers exactly what the client is able
// to reproduce.
func ProducerDigest(events []RawEvent) (string, error) {
	stripped := make([]RawEvent, len(events))
	copy(stripped, events)
	for index := range stripped {
		stripped[index].ReceivedAt = time.Time{}
	}
	return BatchContentHash(stripped)
}
