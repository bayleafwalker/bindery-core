package capture

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestBatchValidationRejectsEveryMalformedShape(t *testing.T) {
	valid := testBatch("capture-1", "player-1", 0, 2)
	if err := ValidateBatch(valid); err != nil {
		t.Fatalf("a well-formed batch was rejected: %v", err)
	}

	noncontiguous := testBatch("capture-1", "player-1", 0, 2)
	noncontiguous.Events[2].Sequence = 9
	shortRange := testBatch("capture-1", "player-1", 0, 2)
	shortRange.Events = shortRange.Events[:2]
	foreignCapture := testBatch("capture-1", "player-1", 0, 2)
	foreignCapture.Events[1].CaptureID = "capture-2"
	emptyPayload := testBatch("capture-1", "player-1", 0, 0)
	emptyPayload.Events[0].Payload = nil
	invalidPayload := testBatch("capture-1", "player-1", 0, 0)
	invalidPayload.Events[0].Payload = json.RawMessage(`{not json`)
	inverted := testBatch("capture-1", "player-1", 0, 0)
	inverted.FirstSequence, inverted.LastSequence = 3, 1

	for name, batch := range map[string]Batch{
		"non-contiguous sequence":    noncontiguous,
		"range wider than events":    shortRange,
		"event from another capture": foreignCapture,
		"empty payload":              emptyPayload,
		"invalid payload":            invalidPayload,
		"inverted range":             inverted,
		"no events":                  {CaptureID: "c", ExecutionID: "e", ProducerClientID: "p"},
	} {
		if err := ValidateBatch(batch); !errors.Is(err, ErrBatchInvalid) {
			t.Fatalf("%s was accepted: %v", name, err)
		}
	}
}

func TestSequenceArithmeticSeparatesGapsFromUndelivered(t *testing.T) {
	sequences := []uint64{0, 1, 3, 4}
	if got := Ranges(sequences); len(got) != 2 || got[0] != [2]uint64{0, 1} || got[1] != [2]uint64{3, 4} {
		t.Fatalf("ranges = %v", got)
	}
	if got := MissingRanges(sequences); len(got) != 1 || got[0] != [2]uint64{2, 2} {
		t.Fatalf("missing ranges = %v", got)
	}
	// A gap analysis over what arrived cannot see sequences the producer
	// promised at close and never sent; missingThrough can.
	if got := MissingThrough(sequences, 6); len(got) != 2 || got[0] != [2]uint64{2, 2} || got[1] != [2]uint64{5, 6} {
		t.Fatalf("missing through = %v", got)
	}
	if got := AcknowledgedThrough(sequences); got != 1 {
		t.Fatalf("acknowledged through = %d, want 1", got)
	}
	if got := AcknowledgedThrough([]uint64{1, 2}); got != -1 {
		t.Fatalf("acknowledged through without sequence zero = %d, want -1", got)
	}
}

func testBatch(captureID, producerID string, first, last uint64) Batch {
	return testBatchWithPayload(captureID, producerID, first, last, `{"event":"observation"}`)
}

func testBatchWithPayload(captureID, producerID string, first, last uint64, payload string) Batch {
	events := make([]RawEvent, 0, last-first+1)
	for sequence := first; sequence <= last; sequence++ {
		events = append(events, RawEvent{
			EventID: captureID + "-" + string(rune('a'+sequence)), SessionID: "session-1",
			ExecutionID: "execution-1", CaptureID: captureID, ProducerClientID: producerID,
			ProducerClass: "player", CaptureMethod: "test", AdapterID: "adapter", AdapterVersion: "1",
			Sequence: sequence, ReceivedAt: time.Now().UTC(), EventType: "game.event",
			Payload: []byte(payload),
		})
	}
	return Batch{CaptureID: captureID, ExecutionID: "execution-1", ProducerClientID: producerID, FirstSequence: first, LastSequence: last, Events: events}
}
