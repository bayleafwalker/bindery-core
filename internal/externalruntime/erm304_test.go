package externalruntime

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCompletenessManifestMakesGapsAndDropsExplicit(t *testing.T) {
	// "complete: true" with nothing behind it is a cheerful shrug. The point
	// of the manifest is that a reader can see exactly what is missing.
	service := NewService()
	fixture := newCaptureFixture(t, service, "manifest")
	mustIngest(t, service, fixture.playerA, 0, 1)
	mustIngest(t, service, fixture.playerA, 3, 3)

	open, err := service.GetCapture(fixture.playerA.capture)
	if err != nil {
		t.Fatal(err)
	}
	if open.Completeness.Closed || open.Completeness.ExpectedThrough != nil {
		t.Fatalf("open capture claimed a final sequence: %+v", open.Completeness)
	}
	if open.Completeness.EventCount != 3 {
		t.Fatalf("observed event count = %d, want 3", open.Completeness.EventCount)
	}

	closed, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, CaptureCloseRequest{
		FinalSequence: 4, ObservedGaps: [][2]uint64{{2, 2}}, LocalDrops: 1, EndReason: "client-exit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != string(CaptureClosed) || closed.ClosedAt == nil {
		t.Fatalf("closed capture = %+v", closed)
	}
	completeness := closed.Completeness
	if completeness == nil || !completeness.Closed {
		t.Fatalf("completeness = %+v", completeness)
	}
	if completeness.ExpectedThrough == nil || *completeness.ExpectedThrough != 4 {
		t.Fatalf("expected through = %v", completeness.ExpectedThrough)
	}
	// Sequence 2 was never received and sequence 4 was promised but never
	// arrived. Both are missing and both must be named.
	if len(completeness.MissingRanges) != 2 ||
		completeness.MissingRanges[0] != [2]uint64{2, 2} ||
		completeness.MissingRanges[1] != [2]uint64{4, 4} {
		t.Fatalf("missing ranges = %v", completeness.MissingRanges)
	}
	if completeness.LocalDrops != 1 || completeness.EndReason != "client-exit" {
		t.Fatalf("producer account was not retained: %+v", completeness)
	}
	if len(completeness.RawObjectHashes) != 2 {
		t.Fatalf("raw object hashes = %v", completeness.RawObjectHashes)
	}
	if completeness.SourceCoverage != string(ClientPlayer) {
		t.Fatalf("source coverage = %q", completeness.SourceCoverage)
	}
	if completeness.ClockQuality != ClockReceiveOnly {
		t.Fatalf("clock quality = %q", completeness.ClockQuality)
	}
}

func TestCloseIsIdempotentAndRefusesADifferentAccount(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "close-twice")
	mustIngest(t, service, fixture.playerA, 0, 2)
	request := CaptureCloseRequest{FinalSequence: 2, LocalDrops: 0, EndReason: "match-ended"}

	first, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, request)
	if err != nil {
		t.Fatal(err)
	}
	if !first.ClosedAt.Equal(*second.ClosedAt) {
		t.Fatal("a repeated close moved the close time")
	}
	if _, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, CaptureCloseRequest{FinalSequence: 9, EndReason: "match-ended"}); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("contradictory close error = %v", err)
	}
	if _, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "after-close", batchRequest(3, 3, `{"a":1}`)); !hasCode(err, "CAPTURE_NOT_OPEN") {
		t.Fatalf("ingest after close error = %v", err)
	}
}

func TestDegradedReportMarksTheStreamNotTheMatch(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "degraded")
	mustReport(t, service, fixture.playerA, "degraded-report", "capture_degraded")

	record, err := service.GetCapture(fixture.playerA.capture)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != string(CaptureDegraded) {
		t.Fatalf("capture status = %q", record.Status)
	}
	session, err := service.GetSession(fixture.session.PublicSession.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Phase == SessionFailed {
		t.Fatal("a degraded capture failed the session")
	}
	// A degraded stream is still a stream: it keeps accepting observations.
	if _, err := service.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "degraded-batch", batchRequest(0, 0, `{"a":1}`)); err != nil {
		t.Fatalf("degraded capture refused a batch: %v", err)
	}
}

func TestCloseOverHTTPUsesTheColonVerbAndLeaksNothing(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture := newCaptureFixture(t, service, "http-close")
	mustIngest(t, service, fixture.playerA, 0, 1)

	body, err := json.Marshal(CaptureCloseRequest{FinalSequence: 1, EndReason: "match-ended"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptestRequest(http.MethodPost, "/v1/captures/"+fixture.playerA.capture+":close", string(body))
	request.Header.Set("Authorization", "Bearer "+fixture.playerA.lease)
	response := serve(handler, request)
	if response.Code != http.StatusOK {
		t.Fatalf("close status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := ScanPublicOutput(response.Body.Bytes(), fixture.playerA.lease, fixture.playerA.transport); err != nil {
		t.Fatalf("close response leaked material: %v", err)
	}
	var public PublicCapture
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public.Completeness == nil || !public.Completeness.Closed || len(public.Completeness.MissingRanges) != 0 {
		t.Fatalf("closed capture manifest = %+v", public.Completeness)
	}
}
