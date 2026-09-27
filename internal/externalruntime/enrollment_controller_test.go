package externalruntime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func controllerEnrollmentRequest(instance string, class ClientClass, controller *EnrollmentController) EnrollmentRequest {
	return EnrollmentRequest{ClientInstanceID: instance, ClientClass: class, Adapter: AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"}, Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB}, Controller: controller}
}

func TestDeclaredControllerIsPublishedOnTheEnrollment(t *testing.T) {
	service := NewService()
	identity := mustIdentity(t, service, "controller-host")
	created, err := service.CreateSession(identity.AccountToken, "controller-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	agent := &EnrollmentController{Kind: ControllerAgent, ControllerID: "bindery.ra2-agent/scripted", ControllerVersion: "0.3.1+build.7"}
	enrolled, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "agent-seat", controllerEnrollmentRequest("agent-seat", ClientPlayer, agent))
	if err != nil {
		t.Fatal(err)
	}
	if enrolled.PublicEnrollment.Controller == nil || *enrolled.PublicEnrollment.Controller != *agent {
		t.Fatalf("enrollment controller = %+v, want %+v", enrolled.PublicEnrollment.Controller, agent)
	}
	// The response must not alias the request: mutating what the caller sent
	// cannot rewrite a published record.
	agent.ControllerVersion = "tampered"
	public, err := service.GetEnrollment(enrolled.PublicEnrollment.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if public.Controller == nil || public.Controller.ControllerVersion != "0.3.1+build.7" {
		t.Fatalf("published controller = %+v", public.Controller)
	}

	undeclared, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "undeclared-seat", controllerEnrollmentRequest("undeclared-seat", ClientPlayer, nil))
	if err != nil {
		t.Fatal(err)
	}
	if undeclared.PublicEnrollment.Controller != nil {
		t.Fatalf("an undeclared controller was defaulted to %+v", undeclared.PublicEnrollment.Controller)
	}
	encoded, err := json.Marshal(undeclared.PublicEnrollment)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "controller") {
		t.Fatalf("undeclared enrollment serialized a controller: %s", encoded)
	}

	session, err := service.GetSession(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	agentSeats := 0
	for _, enrollment := range session.Enrollments {
		if enrollment.Controller != nil && enrollment.Controller.Kind == ControllerAgent {
			agentSeats++
		}
	}
	if agentSeats != 1 {
		t.Fatalf("session names %d agent seats, want 1: %+v", agentSeats, session.Enrollments)
	}
}

func TestHumanAndBuiltinControllersCarryNoIdentity(t *testing.T) {
	for _, kind := range []ControllerKind{ControllerHuman, ControllerBuiltinAI} {
		service := NewService()
		identity := mustIdentity(t, service, "kind-"+strings.ReplaceAll(string(kind), "_", "-"))
		created, err := service.CreateSession(identity.AccountToken, "kind-session", testSessionRequest())
		if err != nil {
			t.Fatal(err)
		}
		enrolled, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "seat", controllerEnrollmentRequest("seat", ClientPlayer, &EnrollmentController{Kind: kind}))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if enrolled.PublicEnrollment.Controller == nil || enrolled.PublicEnrollment.Controller.Kind != kind {
			t.Fatalf("%s: controller = %+v", kind, enrolled.PublicEnrollment.Controller)
		}
	}
}

func TestControllerDeclarationsAreValidatedBeforeAdmission(t *testing.T) {
	cases := []struct {
		name       string
		class      ClientClass
		controller EnrollmentController
		code       string
	}{
		{"observer-any", ClientObserver, EnrollmentController{Kind: ControllerHuman}, "CONTROLLER_NOT_ALLOWED"},
		{"observer-agent", ClientObserver, EnrollmentController{Kind: ControllerAgent, ControllerID: "a", ControllerVersion: "1"}, "CONTROLLER_NOT_ALLOWED"},
		{"unknown-kind", ClientPlayer, EnrollmentController{Kind: "cyborg"}, "CONTROLLER_INVALID"},
		{"empty-kind", ClientPlayer, EnrollmentController{}, "CONTROLLER_INVALID"},
		{"agent-without-id", ClientPlayer, EnrollmentController{Kind: ControllerAgent, ControllerVersion: "1"}, "CONTROLLER_INVALID"},
		{"agent-without-version", ClientPlayer, EnrollmentController{Kind: ControllerAgent, ControllerID: "a"}, "CONTROLLER_INVALID"},
		{"agent-with-space", ClientPlayer, EnrollmentController{Kind: ControllerAgent, ControllerID: "an agent", ControllerVersion: "1"}, "CONTROLLER_INVALID"},
		{"agent-leading-dot", ClientPlayer, EnrollmentController{Kind: ControllerAgent, ControllerID: ".agent", ControllerVersion: "1"}, "CONTROLLER_INVALID"},
		{"agent-too-long", ClientPlayer, EnrollmentController{Kind: ControllerAgent, ControllerID: strings.Repeat("a", 129), ControllerVersion: "1"}, "CONTROLLER_INVALID"},
		{"human-with-id", ClientPlayer, EnrollmentController{Kind: ControllerHuman, ControllerID: "alice"}, "CONTROLLER_INVALID"},
		{"builtin-with-version", ClientPlayer, EnrollmentController{Kind: ControllerBuiltinAI, ControllerVersion: "brutal"}, "CONTROLLER_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService()
			identity := mustIdentity(t, service, "invalid-controller")
			created, err := service.CreateSession(identity.AccountToken, "invalid-controller-session", testSessionRequest())
			if err != nil {
				t.Fatal(err)
			}
			controller := tc.controller
			_, err = service.Enroll(identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "seat", controllerEnrollmentRequest("seat", tc.class, &controller))
			if !hasCode(err, tc.code) {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
			if len(service.enrollments) != 0 {
				t.Fatal("a refused controller declaration still enrolled the client")
			}
		})
	}
	longest := EnrollmentController{Kind: ControllerAgent, ControllerID: strings.Repeat("a", 128), ControllerVersion: "v1.2.3-rc.1+sha.abc:x@y/z"}
	if err := validateController(ClientPlayer, &longest); err != nil {
		t.Fatalf("a 128-character agent id was refused: %v", err)
	}
}

func TestReplayWithADifferentControllerIsAnIdempotencyConflict(t *testing.T) {
	service := NewService()
	identity := mustIdentity(t, service, "controller-replay")
	created, err := service.CreateSession(identity.AccountToken, "controller-replay-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	agent := &EnrollmentController{Kind: ControllerAgent, ControllerID: "agent", ControllerVersion: "1"}
	first, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "replay", controllerEnrollmentRequest("seat", ClientPlayer, agent))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "replay", controllerEnrollmentRequest("seat", ClientPlayer, &EnrollmentController{Kind: ControllerAgent, ControllerID: "agent", ControllerVersion: "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.PublicEnrollment.ClientID != first.PublicEnrollment.ClientID || replayed.PublicEnrollment.Controller == nil || *replayed.PublicEnrollment.Controller != *agent {
		t.Fatalf("identical replay = %+v", replayed.PublicEnrollment)
	}
	for _, changed := range []*EnrollmentController{nil, {Kind: ControllerHuman}, {Kind: ControllerAgent, ControllerID: "agent", ControllerVersion: "2"}} {
		if _, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "replay", controllerEnrollmentRequest("seat", ClientPlayer, changed)); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
			t.Fatalf("replay with controller %+v error = %v, want IDEMPOTENCY_CONFLICT", changed, err)
		}
	}
}

func TestControllerSurvivesARestartAndLegacyEnrollmentsStayUndeclared(t *testing.T) {
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	identity := mustIdentity(t, service, "controller-durable")
	created, err := service.CreateSession(identity.AccountToken, "controller-durable-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	agent, err := service.Enroll(identity.AccountToken, created.SessionJoinCredential, sessionID, "durable-agent", controllerEnrollmentRequest("durable-agent", ClientPlayer, &EnrollmentController{Kind: ControllerAgent, ControllerID: "agent", ControllerVersion: "1"}))
	if err != nil {
		t.Fatal(err)
	}
	legacy := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, sessionID, "durable-legacy", ClientPlayer)

	// An enrollment with no controller must serialize exactly as it did before
	// the field existed, so a state file written by an older broker and one
	// written now are indistinguishable for that record.
	state, err := os.ReadFile(filepath.Join(directory, "control-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Enrollments map[string]struct {
			Public map[string]json.RawMessage `json:"public"`
		} `json:"enrollments"`
	}
	if err := json.Unmarshal(state, &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.Enrollments[legacy.id].Public["controller"]; ok {
		t.Fatal("an undeclared controller was persisted")
	}
	if _, ok := snapshot.Enrollments[agent.PublicEnrollment.ClientID].Public["controller"]; !ok {
		t.Fatal("a declared controller was not persisted")
	}

	reopened := openPersistentCaptureService(t, directory)
	restoredAgent, err := reopened.GetEnrollment(agent.PublicEnrollment.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredAgent.Controller == nil || restoredAgent.Controller.Kind != ControllerAgent || restoredAgent.Controller.ControllerID != "agent" {
		t.Fatalf("restored controller = %+v", restoredAgent.Controller)
	}
	restoredLegacy, err := reopened.GetEnrollment(legacy.id)
	if err != nil {
		t.Fatal(err)
	}
	if restoredLegacy.Controller != nil {
		t.Fatalf("legacy enrollment restored with controller %+v", restoredLegacy.Controller)
	}
}

func TestRestoreRefusesAnObserverWithAController(t *testing.T) {
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	identity := mustIdentity(t, service, "controller-tamper")
	created, err := service.CreateSession(identity.AccountToken, "controller-tamper-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	observer := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "tamper-observer", ClientObserver)
	path := filepath.Join(directory, "control-state.json")
	state, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(state, &snapshot); err != nil {
		t.Fatal(err)
	}
	enrollment := snapshot["enrollments"].(map[string]any)[observer.id].(map[string]any)
	enrollment["public"].(map[string]any)["controller"] = map[string]any{"kind": "human"}
	tampered, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPersistentService(testPersistentAllocator, store); err == nil || !strings.Contains(err.Error(), "invalid controller") {
		t.Fatalf("restore of an observer with a controller error = %v", err)
	}
}

func TestHTTPEnrollmentControllerRoundTripAndErrors(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	identity := mustIdentity(t, service, "http-controller")
	created, err := service.CreateSession(identity.AccountToken, "http-controller-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	enroll := func(key, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+created.PublicSession.SessionID+"/enrollments", bytes.NewReader([]byte(body)))
		request.Header.Set("Authorization", "Bearer "+identity.AccountToken)
		request.Header.Set("X-Session-Join-Credential", created.SessionJoinCredential)
		request.Header.Set("Idempotency-Key", key)
		return serve(handler, request)
	}
	base := `"adapter":{"id":"bindery.ra2-adapter","version":"0.1.0"},"compatibility":{"game_hash":"` + testHashA + `","mod_hash":"` + testHashA + `","map_hash":"` + testHashB + `"}`

	response := enroll("agent", `{"client_instance_id":"agent","client_class":"player",`+base+`,"controller":{"kind":"agent","controller_id":"bindery.ra2-agent","controller_version":"0.1.0"}}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("agent enrollment status = %d, body = %s", response.Code, response.Body.String())
	}
	var created201 EnrollmentCreateResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created201); err != nil {
		t.Fatal(err)
	}
	read := serve(handler, httptest.NewRequest(http.MethodGet, "/v1/enrollments/"+created201.PublicEnrollment.ClientID, nil))
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"controller":{"kind":"agent","controller_id":"bindery.ra2-agent","controller_version":"0.1.0"}`) {
		t.Fatalf("public enrollment read = %d %s", read.Code, read.Body.String())
	}
	if err := ScanPublicOutput(read.Body.Bytes(), created201.ClientLeaseToken, created201.TransportCredential); err != nil {
		t.Fatalf("public enrollment with controller failed the redaction oracle: %v", err)
	}

	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"observer", `{"client_instance_id":"observer","client_class":"observer",` + base + `,"controller":{"kind":"human"}}`, "CONTROLLER_NOT_ALLOWED", http.StatusBadRequest},
		{"unknown-kind", `{"client_instance_id":"unknown","client_class":"player",` + base + `,"controller":{"kind":"oracle"}}`, "CONTROLLER_INVALID", http.StatusBadRequest},
		{"unknown-field", `{"client_instance_id":"field","client_class":"player",` + base + `,"controller":{"kind":"human","model":"x"}}`, "INVALID_JSON", http.StatusBadRequest},
	} {
		response := enroll(tc.name, tc.body)
		if response.Code != tc.status || !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%s: status = %d, body = %s", tc.name, response.Code, response.Body.String())
		}
	}
}
