package capture

import (
	"encoding/json"
	"testing"
	"time"
)

// These vectors are frozen on purpose. The canonical encoding is what gets
// content-addressed, persisted, and published; if a change to canon.go moves
// these hashes it has reissued every capture this repository has recorded, and
// this test is the only thing that says so out loud.
const (
	goldenEventEncoding = `{"event_id":"0198c2c3-4d5e-7f60-8123-456789abcdef","session_id":"0198c2c3-4d5e-7f60-8123-456789abcdea","execution_id":"0198c2c3-4d5e-7f60-8123-456789abcdeb","capture_id":"0198c2c3-4d5e-7f60-8123-456789abcdec","producer_client_id":"0198c2c3-4d5e-7f60-8123-456789abcded","producer_class":"player","capture_method":"adapter-log-tail","adapter_id":"bindery.ra2-adapter","adapter_version":"0.1.0","sequence":0,"game_tick":4242,"producer_time":"2026-08-25T10:30:00.123456789Z","received_at":"2026-08-25T10:30:01Z","event_type":"game.match.lifecycle","payload_version":"1.0.0","payload":{"state":"started"}}`
	goldenBatchHash     = "sha256:f17eb7fb517aad93c18cd35e458d962293fb41f8a45933f2a89655cdab168dff"
	goldenOrderedHash   = "sha256:e97fc4f66b19e4c5c4f21b2b7c2bccb1d43b0c127f58ab0bb916a83b9e91caa3"
)

func goldenEvents() []RawEvent {
	tick := uint64(4242)
	producerTime := time.Date(2026, 8, 25, 10, 30, 0, 123456789, time.UTC)
	first := RawEvent{
		EventID:          "0198c2c3-4d5e-7f60-8123-456789abcdef",
		SessionID:        "0198c2c3-4d5e-7f60-8123-456789abcdea",
		ExecutionID:      "0198c2c3-4d5e-7f60-8123-456789abcdeb",
		CaptureID:        "0198c2c3-4d5e-7f60-8123-456789abcdec",
		ProducerClientID: "0198c2c3-4d5e-7f60-8123-456789abcded",
		ProducerClass:    "player",
		CaptureMethod:    "adapter-log-tail",
		AdapterID:        "bindery.ra2-adapter",
		AdapterVersion:   "0.1.0",
		Sequence:         0,
		GameTick:         &tick,
		ProducerTime:     &producerTime,
		ReceivedAt:       time.Date(2026, 8, 25, 10, 30, 1, 0, time.UTC),
		EventType:        "game.match.lifecycle",
		PayloadVersion:   "1.0.0",
		Payload:          json.RawMessage(`{ "state" :  "started" }`),
	}
	second := first
	second.EventID = "0198c2c3-4d5e-7f60-8123-456789abcdf0"
	second.Sequence = 1
	second.GameTick = nil
	second.ProducerTime = nil
	second.PayloadVersion = ""
	second.Payload = json.RawMessage(`{"units":[1,2,3]}`)
	return []RawEvent{first, second}
}

func TestCanonicalEncodingIsFrozen(t *testing.T) {
	events := goldenEvents()
	encoded, err := CanonicalEventBytes(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != goldenEventEncoding {
		t.Fatalf("canonical event encoding moved:\n got %s\nwant %s", encoded, goldenEventEncoding)
	}
	hash, err := BatchContentHash(events)
	if err != nil {
		t.Fatal(err)
	}
	if hash != goldenBatchHash {
		t.Fatalf("batch content hash moved: got %s want %s", hash, goldenBatchHash)
	}
	ordered, err := OrderedHash(events)
	if err != nil {
		t.Fatal(err)
	}
	if ordered != goldenOrderedHash {
		t.Fatalf("ordered hash moved: got %s want %s", ordered, goldenOrderedHash)
	}
}

func TestPayloadWhitespaceDoesNotChangeIdentity(t *testing.T) {
	// Two clients that agree on the facts must not disagree on the hash
	// because one of them indents.
	spaced := goldenEvents()
	compact := goldenEvents()
	compact[0].Payload = json.RawMessage(`{"state":"started"}`)
	spacedHash, err := BatchContentHash(spaced)
	if err != nil {
		t.Fatal(err)
	}
	compactHash, err := BatchContentHash(compact)
	if err != nil {
		t.Fatal(err)
	}
	if spacedHash != compactHash {
		t.Fatalf("payload whitespace changed the content hash: %s != %s", spacedHash, compactHash)
	}
}

func TestOrderedHashIgnoresBatchBoundariesButNotContent(t *testing.T) {
	events := goldenEvents()
	reversed := []RawEvent{events[1], events[0]}
	forward, err := OrderedHash(events)
	if err != nil {
		t.Fatal(err)
	}
	backward, err := OrderedHash(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if forward != backward {
		t.Fatal("ordered hash depends on supplied order rather than sequence order")
	}
	mutated := goldenEvents()
	mutated[1].Payload = json.RawMessage(`{"units":[1,2,4]}`)
	changed, err := OrderedHash(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if changed == forward {
		t.Fatal("ordered hash did not change when an event payload changed")
	}
}

func TestCanonicalBatchRoundTripsThroughDecode(t *testing.T) {
	events := goldenEvents()
	encoded, err := CanonicalBatchBytes(events)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCanonicalBatch(encoded)
	if err != nil {
		t.Fatal(err)
	}
	rehashed, err := BatchContentHash(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if rehashed != goldenBatchHash {
		t.Fatalf("decode/encode round trip is not hash stable: %s", rehashed)
	}
	if decoded[0].ProducerTime == nil || !decoded[0].ProducerTime.Equal(*events[0].ProducerTime) {
		t.Fatalf("producer time did not survive the round trip: %+v", decoded[0].ProducerTime)
	}
	if decoded[1].ProducerTime != nil || decoded[1].GameTick != nil {
		t.Fatal("absent optional fields came back populated")
	}
}

// The observed encoding is frozen for the same reason the canonical one is:
// observed_hash is published in evidence sets.
const (
	goldenObservedEncoding = `{"session_id":"0198c2c3-4d5e-7f60-8123-456789abcdea","execution_id":"0198c2c3-4d5e-7f60-8123-456789abcdeb","game_tick":4242,"event_type":"game.match.lifecycle","payload_version":"1.0.0","payload":{"state":"started"}}`
	goldenObservedHash     = "sha256:7d79895fbaa10d9d9b36cd4f0f23eae5c3412fde7018968ba1cd14f13ccc2e36"
)

func TestObservedEncodingIsFrozen(t *testing.T) {
	events := goldenEvents()
	encoded, err := ObservedEventBytes(events[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != goldenObservedEncoding {
		t.Fatalf("observed event encoding moved:\n got %s\nwant %s", encoded, goldenObservedEncoding)
	}
	observed, err := ObservedHash(events)
	if err != nil {
		t.Fatal(err)
	}
	if observed != goldenObservedHash {
		t.Fatalf("observed hash moved: got %s want %s", observed, goldenObservedHash)
	}
}

// Two producers that witnessed the same events in the same order agree, no
// matter who they are, when the broker received them, what their own clocks
// said, or what each called the event.
func TestObservedHashAgreesAcrossProducers(t *testing.T) {
	first := goldenEvents()
	second := goldenEvents()
	otherTime := time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC)
	for index := range second {
		second[index].EventID = "0198c2c3-4d5e-7f60-8123-00000000000" + string(rune('a'+index))
		second[index].CaptureID = "0198c2c3-4d5e-7f60-8123-999999999999"
		second[index].ProducerClientID = "0198c2c3-4d5e-7f60-8123-888888888888"
		second[index].ProducerClass = "observer"
		second[index].CaptureMethod = "adapter-memory-probe"
		second[index].AdapterID = "bindery.other-adapter"
		second[index].AdapterVersion = "9.9.9"
		second[index].ProducerTime = &otherTime
		second[index].ReceivedAt = otherTime
	}
	second[0].Payload = json.RawMessage(`{"state":"started"}`)
	firstHash, err := ObservedHash(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := ObservedHash(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("identical observations from two producers disagree: %s != %s", firstHash, secondHash)
	}
	streamFirst, _ := OrderedHash(first)
	streamSecond, _ := OrderedHash(second)
	if streamFirst == streamSecond {
		t.Fatal("ordered hash stopped identifying the producer's stream")
	}
}

// What was observed still moves the hash: content, tick, type, order, and
// which execution it was observed in.
func TestObservedHashChangesWithWhatWasObserved(t *testing.T) {
	base, err := ObservedHash(goldenEvents())
	if err != nil {
		t.Fatal(err)
	}
	tick := uint64(4243)
	mutations := map[string]func([]RawEvent){
		"payload":   func(events []RawEvent) { events[1].Payload = json.RawMessage(`{"units":[1,2,4]}`) },
		"tick":      func(events []RawEvent) { events[0].GameTick = &tick },
		"type":      func(events []RawEvent) { events[1].EventType = "game.match.other" },
		"version":   func(events []RawEvent) { events[1].PayloadVersion = "2.0.0" },
		"execution": func(events []RawEvent) { events[0].ExecutionID = "0198c2c3-4d5e-7f60-8123-456789abcdff" },
		"order": func(events []RawEvent) {
			events[0].Sequence, events[1].Sequence = 1, 0
		},
	}
	for name, mutate := range mutations {
		events := goldenEvents()
		mutate(events)
		changed, err := ObservedHash(events)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if changed == base {
			t.Errorf("%s: observed hash did not change", name)
		}
	}
	single, err := ObservedHash(goldenEvents()[:1])
	if err != nil {
		t.Fatal(err)
	}
	if single == base {
		t.Error("dropping an event did not change the observed hash")
	}
}
