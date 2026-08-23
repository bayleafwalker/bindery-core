package capture

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBatchIngestIsAtLeastOnceWithGapsAndLateAppend(t *testing.T) {
	store := NewStore()
	initialBatch := testBatch("capture-1", "player-1", 0, 1)
	first, err := store.Ingest(initialBatch)
	if err != nil {
		t.Fatal(err)
	}
	if first.AcknowledgedThrough != 1 {
		t.Fatalf("ack = %d", first.AcknowledgedThrough)
	}
	lateBatch := testBatch("capture-1", "player-1", 3, 3)
	late, err := store.Ingest(lateBatch)
	if err != nil {
		t.Fatal(err)
	}
	if late.AcknowledgedThrough != 1 || len(late.MissingRanges) != 1 || late.MissingRanges[0] != [2]uint64{2, 2} {
		t.Fatalf("late receipt = %+v", late)
	}
	replay, err := store.Ingest(lateBatch)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Duplicate {
		t.Fatal("retry was not marked duplicate")
	}
	if _, err := store.Ingest(testBatchWithPayload("capture-1", "player-1", 3, 3, `{"different":true}`)); !errors.Is(err, ErrSequenceConflict) {
		t.Fatalf("conflicting sequence = %v", err)
	}
	if err := store.Close("capture-1", "player-1", StreamClose{FinalSequence: 3, LocalDrops: 1, EndReason: "client-exit", ClosedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	manifest, err := store.Manifest("capture-1", "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Closed || manifest.LocalDrops != 1 || len(manifest.MissingRanges) != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.MissingRanges[0] != [2]uint64{2, 2} {
		t.Fatalf("manifest gaps = %+v", manifest.MissingRanges)
	}
}

func TestObjectManifestNeverSerializesPrivateStorageKey(t *testing.T) {
	store := NewObjectStore()
	manifest, err := store.Put("capture-1", "relay", "application/octet-stream", []byte("raw"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private/") {
		t.Fatal("public object manifest leaked private key")
	}
}

func testBatch(captureID, producerID string, first, last uint64) Batch {
	return testBatchWithPayload(captureID, producerID, first, last, `{"event":"observation"}`)
}
func testBatchWithPayload(captureID, producerID string, first, last uint64, payload string) Batch {
	events := make([]RawEvent, 0, last-first+1)
	for sequence := first; sequence <= last; sequence++ {
		events = append(events, RawEvent{EventID: captureID + "-" + string(rune('a'+sequence)), SessionID: "session-1", CaptureID: captureID, ProducerClientID: producerID, ProducerClass: "player", CaptureMethod: "test", AdapterID: "adapter", AdapterVersion: "1", Sequence: sequence, ReceivedAt: time.Now().UTC(), EventType: "game.event", Payload: []byte(payload)})
	}
	return Batch{CaptureID: captureID, ProducerClientID: producerID, FirstSequence: first, LastSequence: last, Events: events}
}
