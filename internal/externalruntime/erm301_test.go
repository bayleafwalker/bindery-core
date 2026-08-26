package externalruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentOpensOneCaptureStreamPerClient(t *testing.T) {
	service := NewService()
	identity := mustIdentity(t, service, "capture-host")
	created, err := service.CreateSession(identity.AccountToken, "capture-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	playerA := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, sessionID, "capture-a", ClientPlayer)
	playerB := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, sessionID, "capture-b", ClientPlayer)

	if playerA.capture == "" || playerB.capture == "" {
		t.Fatal("enrollment did not return a capture stream offer")
	}
	if playerA.capture == playerB.capture {
		t.Fatal("two clients were handed the same capture stream")
	}

	record, err := service.GetCapture(playerA.capture)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != string(CaptureOpen) {
		t.Fatalf("capture status = %q", record.Status)
	}
	if record.ProducerClientID != playerA.id || record.SessionID != sessionID {
		t.Fatalf("capture is not attributed to its producer: %+v", record)
	}
	if record.CaptureMethod != defaultCaptureMethod {
		t.Fatalf("capture method = %q", record.CaptureMethod)
	}
	if record.ClosedAt != nil {
		t.Fatal("a freshly opened capture reported a close time")
	}
	if record.Completeness == nil || record.Completeness.Closed || record.Completeness.ExpectedThrough != nil {
		t.Fatalf("completeness on an open capture = %+v", record.Completeness)
	}

	session, err := service.GetSession(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.CaptureIDs) != 2 {
		t.Fatalf("session capture ids = %v", session.CaptureIDs)
	}

	captures, err := service.ListSessionCaptures(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(captures) != 2 {
		t.Fatalf("listed captures = %d", len(captures))
	}
}

func TestCapturePolicyWithoutSemanticEventsOpensNoStream(t *testing.T) {
	service := NewService()
	identity := mustIdentity(t, service, "no-capture-host")
	request := testSessionRequest()
	request.Capture = CapturePolicy{}
	created, err := service.CreateSession(identity.AccountToken, "no-capture-session", request)
	if err != nil {
		t.Fatal(err)
	}
	player := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "quiet", ClientPlayer)
	if player.capture != "" {
		t.Fatal("a session that asked for no semantic events was given a capture stream")
	}
	session, err := service.GetSession(created.PublicSession.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.CaptureIDs) != 0 {
		t.Fatalf("session capture ids = %v", session.CaptureIDs)
	}
}

func TestReplayedEnrollmentReturnsTheSameCaptureStream(t *testing.T) {
	service := NewService()
	identity := mustIdentity(t, service, "replay-host")
	created, err := service.CreateSession(identity.AccountToken, "replay-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	request := EnrollmentRequest{ClientInstanceID: "replay-client", ClientClass: ClientPlayer, CaptureMethod: "adapter-log-tail", Adapter: AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"}, Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB}}
	first, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "replay-key", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "replay-key", request)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.CaptureStreamOffers) != 1 || second.CaptureStreamOffers[0].CaptureID != first.CaptureStreamOffers[0].CaptureID {
		t.Fatalf("replayed enrollment opened a second stream: %+v", second.CaptureStreamOffers)
	}
	if first.CaptureStreamOffers[0].CaptureMethod != "adapter-log-tail" {
		t.Fatalf("declared capture method was discarded: %+v", first.CaptureStreamOffers[0])
	}
	if len(service.captures) != 1 {
		t.Fatalf("captures held = %d, want 1", len(service.captures))
	}
}

func TestExpiredEnrollmentAbandonsItsOpenCapture(t *testing.T) {
	// An eternally `open` stream belonging to a client that has gone away is a
	// lie the manifest would then repeat.
	service := NewService()
	now := time.Date(2026, 8, 26, 9, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	identity := mustIdentity(t, service, "abandon-host")
	created, err := service.CreateSession(identity.AccountToken, "abandon-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	player := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "abandoned", ClientPlayer)

	now = now.Add(10 * time.Minute)
	if err := service.Converge(now); err != nil {
		t.Fatal(err)
	}
	record, err := service.GetCapture(player.capture)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != string(CaptureAbandoned) {
		t.Fatalf("capture status after lease expiry = %q", record.Status)
	}
	if record.ClosedAt == nil || !record.ClosedAt.Equal(now) {
		t.Fatalf("abandoned capture close time = %v", record.ClosedAt)
	}
}

func TestCaptureReadsAreKnownIDOnlyAndLeakNothing(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	identity := mustIdentity(t, service, "http-capture")
	created, err := service.CreateSession(identity.AccountToken, "http-capture-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	player := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "http-producer", ClientPlayer)

	response := doRequest(t, handler, http.MethodGet, "/v1/captures/"+player.capture, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("capture read status = %d, body = %s", response.Code, response.Body.String())
	}
	body := response.Body.Bytes()
	if err := ScanPublicOutput(body, player.lease, player.transport, created.SessionJoinCredential, identity.AccountToken); err != nil {
		t.Fatalf("public capture leaked material: %v (%s)", err, body)
	}
	var public PublicCapture
	if err := json.Unmarshal(body, &public); err != nil {
		t.Fatal(err)
	}
	if public.CaptureID != player.capture {
		t.Fatalf("capture read returned %q", public.CaptureID)
	}

	listed := doRequest(t, handler, http.MethodGet, "/v1/sessions/"+created.PublicSession.SessionID+"/captures", nil)
	if listed.Code != http.StatusOK {
		t.Fatalf("session capture list status = %d", listed.Code)
	}
	if err := ScanPublicOutput(listed.Body.Bytes(), player.lease, player.transport); err != nil {
		t.Fatalf("session capture list leaked material: %v", err)
	}

	missing := doRequest(t, handler, http.MethodGet, "/v1/captures/0198c2c3-4d5e-7f60-8123-456789abcdef", nil)
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "CAPTURE_NOT_FOUND") {
		t.Fatalf("unknown capture status = %d, body = %s", missing.Code, missing.Body.String())
	}

	discovery := doRequest(t, handler, http.MethodGet, "/v1/captures", nil)
	if discovery.Code != http.StatusNotFound {
		t.Fatalf("capture discovery status = %d, want 404", discovery.Code)
	}
}

func doRequest(t *testing.T, handler http.Handler, method, target string, body *strings.Reader) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, body)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func httptestRequest(method, target, body string) *http.Request {
	if body == "" {
		return httptest.NewRequest(method, target, nil)
	}
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
