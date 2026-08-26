package externalruntime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func drainCaptureEvents(t *testing.T, service *Service, captureID string, limit int) []PublicTelemetryEvent {
	t.Helper()
	var all []PublicTelemetryEvent
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("capture paging did not terminate")
		}
		page, err := service.ReadCaptureEvents(captureID, cursor, "", limit)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Events...)
		if page.NextCursor == "" {
			return all
		}
		cursor = page.NextCursor
	}
}

func TestCapturePagingReassemblesTheStreamExactlyOnce(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "paging")
	for start := uint64(0); start < 20; start += 5 {
		mustIngest(t, service, fixture.playerA, start, start+4)
	}

	events := drainCaptureEvents(t, service, fixture.playerA.capture, 3)
	if len(events) != 20 {
		t.Fatalf("paged %d events, want 20", len(events))
	}
	for index, event := range events {
		if event.Sequence != uint64(index) {
			t.Fatalf("event %d had sequence %d", index, event.Sequence)
		}
		if event.Schema != telemetryEventSchema || event.CaptureID != fixture.playerA.capture {
			t.Fatalf("published envelope = %+v", event)
		}
		if event.RawSource == nil || event.RawSource.ObjectHash == "" {
			t.Fatalf("event %d did not link back to its immutable batch", index)
		}
	}
}

func TestCapturePagingSkipsGapsAndSeesLateAppendsAtAnOldCursor(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "late")
	mustIngest(t, service, fixture.playerA, 0, 1)
	mustIngest(t, service, fixture.playerA, 4, 5)

	first, err := service.ReadCaptureEvents(fixture.playerA.capture, "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	// Sequences 2 and 3 arrive after the reader has already passed them.
	mustIngest(t, service, fixture.playerA, 2, 3)

	rest, err := service.ReadCaptureEvents(fixture.playerA.capture, first.NextCursor, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]uint64, 0, len(rest.Events))
	for _, event := range rest.Events {
		got = append(got, event.Sequence)
	}
	if fmt.Sprint(got) != "[2 3 4 5]" {
		t.Fatalf("late-appended events at an old cursor = %v", got)
	}
}

func TestSessionReadInterleavesProducersInReceiptOrderAndSaysSo(t *testing.T) {
	// Cross-producer ordering is not something the broker knows. It returns
	// the order it received things in, and labels it as such.
	service := NewService()
	now := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	fixture := newCaptureFixture(t, service, "interleaved")

	mustIngest(t, service, fixture.playerA, 0, 1)
	now = now.Add(time.Second)
	mustIngest(t, service, fixture.playerB, 0, 1)
	now = now.Add(time.Second)
	mustIngest(t, service, fixture.playerA, 2, 3)

	page, err := service.ReadSessionEvents(fixture.session.PublicSession.SessionID, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Ordering != orderingByReceipt {
		t.Fatalf("ordering = %q", page.Ordering)
	}
	if len(page.Events) != 6 {
		t.Fatalf("session events = %d, want 6", len(page.Events))
	}
	order := make([]string, 0, len(page.Events))
	for _, event := range page.Events {
		suffix := "a"
		if event.CaptureID == fixture.playerB.capture {
			suffix = "b"
		}
		order = append(order, fmt.Sprintf("%s%d", suffix, event.Sequence))
	}
	if fmt.Sprint(order) != "[a0 a1 b0 b1 a2 a3]" {
		t.Fatalf("receipt order = %v", order)
	}
}

func TestSessionPagingCoversEveryEventExactlyOnce(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 8, 26, 15, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	fixture := newCaptureFixture(t, service, "session-paging")
	for start := uint64(0); start < 8; start += 2 {
		mustIngest(t, service, fixture.playerA, start, start+1)
		now = now.Add(time.Second)
		mustIngest(t, service, fixture.playerB, start, start+1)
		now = now.Add(time.Second)
	}

	seen := make(map[string]int)
	cursor := ""
	total := 0
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("session paging did not terminate")
		}
		page, err := service.ReadSessionEvents(fixture.session.PublicSession.SessionID, cursor, "", 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range page.Events {
			seen[event.CaptureID+"#"+fmt.Sprint(event.Sequence)]++
			total++
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if total != 16 || len(seen) != 16 {
		t.Fatalf("paged %d events over %d distinct keys, want 16 and 16", total, len(seen))
	}
	for key, count := range seen {
		if count != 1 {
			t.Fatalf("%s appeared %d times", key, count)
		}
	}
}

func TestTamperedCursorIsRejectedNotIgnored(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "cursor")
	mustIngest(t, service, fixture.playerA, 0, 1)

	if _, err := service.ReadCaptureEvents(fixture.playerA.capture, "not-base64!!", "", 10); !hasCode(err, "INVALID_CURSOR") {
		t.Fatalf("tampered cursor error = %v", err)
	}
	if _, err := service.ReadSessionEvents(fixture.session.PublicSession.SessionID, "bm90LWpzb24", "", 10); !hasCode(err, "INVALID_CURSOR") {
		t.Fatalf("non-JSON cursor error = %v", err)
	}
}

func TestEventReadsAreUnauthenticatedAndLeakNothing(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	fixture := newCaptureFixture(t, service, "http-events")
	mustIngest(t, service, fixture.playerA, 0, 2)

	response := serve(handler, httptestRequest(http.MethodGet, "/v1/captures/"+fixture.playerA.capture+"/events?limit=2", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("capture events status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := ScanPublicOutput(response.Body.Bytes(), fixture.playerA.lease, fixture.playerA.transport, fixture.session.SessionJoinCredential); err != nil {
		t.Fatalf("event page leaked material: %v", err)
	}
	var page EventPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.NextCursor == "" || page.Ordering != orderingBySequence {
		t.Fatalf("capture page = %+v", page)
	}

	sessionResponse := serve(handler, httptestRequest(http.MethodGet, "/v1/sessions/"+fixture.session.PublicSession.SessionID+"/events", ""))
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("session events status = %d", sessionResponse.Code)
	}
	if err := ScanPublicOutput(sessionResponse.Body.Bytes(), fixture.playerA.lease); err != nil {
		t.Fatalf("session page leaked material: %v", err)
	}
}
