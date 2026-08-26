package externalruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type captureFixture struct {
	service  *Service
	identity CreateIdentityResponse
	session  CreateSessionResponse
	playerA  testEnrollmentSecrets
	playerB  testEnrollmentSecrets
}

func newCaptureFixture(t *testing.T, service *Service, name string) captureFixture {
	t.Helper()
	identity := mustIdentity(t, service, name)
	created, err := service.CreateSession(identity.AccountToken, name+"-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	return captureFixture{
		service:  service,
		identity: identity,
		session:  created,
		playerA:  mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, sessionID, name+"-a", ClientPlayer),
		playerB:  mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, sessionID, name+"-b", ClientPlayer),
	}
}

func batchRequest(first, last uint64, payload string) IngestBatchRequest {
	events := make([]TelemetryEventInput, 0, last-first+1)
	for sequence := first; sequence <= last; sequence++ {
		events = append(events, TelemetryEventInput{
			EventID:   fmt.Sprintf("event-%016d", sequence),
			Sequence:  sequence,
			EventType: "game.player.action-observed",
			Payload:   json.RawMessage(payload),
		})
	}
	return IngestBatchRequest{FirstSequence: first, LastSequence: last, Events: events}
}

func mustIngest(t *testing.T, service *Service, client testEnrollmentSecrets, first, last uint64) CaptureReceipt {
	t.Helper()
	receipt, err := service.IngestCaptureBatch(client.lease, client.capture, fmt.Sprintf("ingest-%s-%d-%d", client.id, first, last), batchRequest(first, last, `{"action":"move"}`))
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestBatchIngestAcknowledgesContiguityAndReportsGaps(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "ingest")

	first := mustIngest(t, service, fixture.playerA, 0, 1)
	if first.AcknowledgedThrough != 1 || len(first.MissingRanges) != 0 {
		t.Fatalf("first receipt = %+v", first)
	}
	late := mustIngest(t, service, fixture.playerA, 3, 3)
	if late.AcknowledgedThrough != 1 {
		t.Fatalf("acknowledged through = %d, want 1", late.AcknowledgedThrough)
	}
	if len(late.MissingRanges) != 1 || late.MissingRanges[0] != [2]uint64{2, 2} {
		t.Fatalf("missing ranges = %v", late.MissingRanges)
	}
	filled := mustIngest(t, service, fixture.playerA, 2, 2)
	if filled.AcknowledgedThrough != 3 || len(filled.MissingRanges) != 0 {
		t.Fatalf("after gap fill = %+v", filled)
	}
}

func TestIdenticalRetryIsAcknowledgedWithoutWriting(t *testing.T) {
	// At-least-once delivery means retries are normal traffic, not an error
	// path. A retry that cost a full snapshot write would turn a network blip
	// into a durability problem.
	service := NewService()
	fixture := newCaptureFixture(t, service, "retry")
	request := batchRequest(0, 2, `{"action":"build"}`)

	if _, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "retry-key", request); err != nil {
		t.Fatal(err)
	}
	indexBefore := len(service.captures[fixture.playerA.capture].Index)

	replay, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "retry-key", request)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Duplicate {
		t.Fatal("an identical retry was not marked duplicate")
	}
	if got := len(service.captures[fixture.playerA.capture].Index); got != indexBefore {
		t.Fatalf("retry grew the index from %d to %d", indexBefore, got)
	}
}

func TestDivergentAndOverlappingRangesAreSequenceConflicts(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "conflict")
	mustIngest(t, service, fixture.playerA, 0, 2)

	_, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "divergent", batchRequest(0, 2, `{"action":"rewritten"}`))
	if !hasCode(err, "SEQUENCE_CONFLICT") {
		t.Fatalf("rewritten range error = %v", err)
	}
	_, err = service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "overlapping", batchRequest(1, 4, `{"action":"move"}`))
	if !hasCode(err, "SEQUENCE_CONFLICT") {
		t.Fatalf("overlapping range error = %v", err)
	}
	_, err = service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "mismatched-hash", IngestBatchRequest{
		FirstSequence: 3, LastSequence: 3, ProducerDigest: "sha256:" + strings.Repeat("0", 64),
		Events: batchRequest(3, 3, `{"action":"move"}`).Events,
	})
	if !hasCode(err, "SEQUENCE_CONFLICT") {
		t.Fatalf("mismatched declared hash error = %v", err)
	}
}

func TestIngestRefusesForeignLeasesAndMalformedBatches(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "authz")

	_, err := service.IngestCaptureBatch(fixture.playerB.lease, fixture.playerA.capture, "foreign", batchRequest(0, 0, `{"a":1}`))
	if !hasCode(err, "TOKEN_INVALID") {
		t.Fatalf("foreign lease error = %v", err)
	}
	_, err = service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "", batchRequest(0, 0, `{"a":1}`))
	if !hasCode(err, "IDEMPOTENCY_KEY_REQUIRED") {
		t.Fatalf("missing idempotency key error = %v", err)
	}
	_, err = service.IngestCaptureBatch(fixture.playerA.lease, "0198c2c3-4d5e-7f60-8123-456789abcdef", "unknown", batchRequest(0, 0, `{"a":1}`))
	if !hasCode(err, "CAPTURE_NOT_FOUND") {
		t.Fatalf("unknown capture error = %v", err)
	}

	badType := batchRequest(0, 0, `{"a":1}`)
	badType.Events[0].EventType = "NotADottedType"
	if _, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "bad-type", badType); !hasCode(err, "BATCH_INVALID") {
		t.Fatalf("malformed event type error = %v", err)
	}

	shortID := batchRequest(0, 0, `{"a":1}`)
	shortID.Events[0].EventID = "short"
	if _, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "short-id", shortID); !hasCode(err, "BATCH_INVALID") {
		t.Fatalf("short event id error = %v", err)
	}

	noncontiguous := batchRequest(0, 1, `{"a":1}`)
	noncontiguous.Events[1].Sequence = 5
	if _, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "noncontiguous", noncontiguous); !hasCode(err, "BATCH_INVALID") {
		t.Fatalf("non-contiguous batch error = %v", err)
	}
}

func TestBatchIngestOverHTTPReturnsAReceiptAndLeaksNothing(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture := newCaptureFixture(t, service, "http-ingest")

	body, err := json.Marshal(batchRequest(0, 1, `{"action":"move"}`))
	if err != nil {
		t.Fatal(err)
	}
	request := httptestRequest(http.MethodPost, "/v1/captures/"+fixture.playerA.capture+"/batches", string(body))
	request.Header.Set("Authorization", "Bearer "+fixture.playerA.lease)
	request.Header.Set("Idempotency-Key", "http-batch-1")
	response := serve(handler, request)
	if response.Code != http.StatusOK {
		t.Fatalf("ingest status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := ScanPublicOutput(response.Body.Bytes(), fixture.playerA.lease, fixture.playerA.transport); err != nil {
		t.Fatalf("receipt leaked material: %v", err)
	}

	unauthorized := httptestRequest(http.MethodPost, "/v1/captures/"+fixture.playerA.capture+"/batches", string(body))
	unauthorized.Header.Set("Idempotency-Key", "http-batch-2")
	if code := serve(handler, unauthorized).Code; code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated ingest status = %d", code)
	}
}

// closeMatchingStreams gives two producers identical, complete accounts of the
// same execution -- the shape the RA2 slice produced, minus the part where the
// clients were the ones counting.
func closeMatchingStreams(t *testing.T, service *Service, a, b testEnrollmentSecrets, events uint64) {
	t.Helper()
	for _, client := range []testEnrollmentSecrets{a, b} {
		ingestRange(t, service, client, 0, events-1, 500)
		if _, err := service.CloseCapture(client.lease, client.capture, CaptureCloseRequest{FinalSequence: events - 1, EndReason: "match-ended"}); err != nil {
			t.Fatal(err)
		}
	}
}

func ingestRange(t *testing.T, service *Service, client testEnrollmentSecrets, first, last uint64, batchSize uint64) {
	t.Helper()
	if last < first {
		t.Fatalf("ingestRange called with an empty range %d..%d", first, last)
	}
	for start := first; start <= last; start += batchSize {
		end := start + batchSize - 1
		if end > last {
			end = last
		}
		if _, err := service.IngestCaptureBatch(client.lease, client.capture, fmt.Sprintf("range-%s-%d", client.id, start), batchRequest(start, end, `{"action":"move"}`)); err != nil {
			t.Fatal(err)
		}
	}
}
