package externalruntime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPKnownIDReadsAndNoSessionDiscovery(t *testing.T) {
	handler := NewHandler(NewService())
	identityBody := `{"handle":"http-player"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/identities", strings.NewReader(identityBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "http-identity")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("identity status = %d, body = %s", response.Code, response.Body.String())
	}
	var created CreateIdentityResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.AccountToken == "" {
		t.Fatal("identity response did not include one-time account token")
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "/v1/identities/"+created.PublicIdentity.AccountID, nil)
	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusOK {
		t.Fatalf("public identity status = %d", publicResponse.Code)
	}
	if strings.Contains(publicResponse.Body.String(), created.AccountToken) {
		t.Fatal("public identity response leaked account token")
	}

	discoveryRequest := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	discoveryResponse := httptest.NewRecorder()
	handler.ServeHTTP(discoveryResponse, discoveryRequest)
	if discoveryResponse.Code != http.StatusNotFound {
		t.Fatalf("session discovery status = %d, want 404", discoveryResponse.Code)
	}
}

func TestHTTPEnrollmentHeartbeatDoesNotEchoCredentials(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	identity := mustIdentity(t, service, "http-host")
	created, err := service.CreateSession(identity.AccountToken, "http-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(EnrollmentRequest{ClientInstanceID: "http-client", ClientClass: ClientPlayer, Adapter: AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"}, Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+created.PublicSession.SessionID+"/enrollments", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+identity.AccountToken)
	request.Header.Set("X-Session-Join-Credential", created.SessionJoinCredential)
	request.Header.Set("Idempotency-Key", "http-enrollment")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("enrollment status = %d, body = %s", response.Code, response.Body.String())
	}
	var enrollment EnrollmentCreateResponse
	if err := json.NewDecoder(response.Body).Decode(&enrollment); err != nil {
		t.Fatal(err)
	}
	if enrollment.ClientLeaseToken == "" || enrollment.TransportCredential == "" {
		t.Fatal("enrollment response did not contain scoped credentials")
	}

	heartbeat := httptest.NewRequest(http.MethodPost, "/v1/enrollments/"+enrollment.PublicEnrollment.ClientID+":heartbeat", nil)
	heartbeat.Header.Set("Authorization", "Bearer "+enrollment.ClientLeaseToken)
	heartbeatResponse := httptest.NewRecorder()
	handler.ServeHTTP(heartbeatResponse, heartbeat)
	if heartbeatResponse.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, body = %s", heartbeatResponse.Code, heartbeatResponse.Body.String())
	}
	if strings.Contains(heartbeatResponse.Body.String(), enrollment.ClientLeaseToken) || strings.Contains(heartbeatResponse.Body.String(), enrollment.TransportCredential) {
		t.Fatal("heartbeat response leaked credential")
	}
}

func TestHTTPKnownEnrollmentReadIsPublicAndNotDiscoverable(t *testing.T) {
	service := NewService()
	handler := NewHandler(service)
	identity := mustIdentity(t, service, "public-enrollment-reader")
	created, err := service.CreateSession(identity.AccountToken, "public-enrollment-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	enrollment := mustEnroll(t, service, identity.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "public-enrollment-client", ClientPlayer)

	knownID := httptest.NewRequest(http.MethodGet, "/v1/enrollments/"+enrollment.id, nil)
	knownResponse := httptest.NewRecorder()
	handler.ServeHTTP(knownResponse, knownID)
	if knownResponse.Code != http.StatusOK {
		t.Fatalf("public enrollment status = %d, body = %s", knownResponse.Code, knownResponse.Body.String())
	}
	var public PublicEnrollment
	if err := json.NewDecoder(knownResponse.Body).Decode(&public); err != nil {
		t.Fatal(err)
	}
	if public.ClientID != enrollment.id || public.ClientClass != ClientPlayer {
		t.Fatalf("public enrollment = %+v, want known player enrollment", public)
	}
	if strings.Contains(knownResponse.Body.String(), enrollment.lease) || strings.Contains(knownResponse.Body.String(), enrollment.transport) {
		t.Fatal("public enrollment response leaked scoped credentials")
	}

	discovery := httptest.NewRequest(http.MethodGet, "/v1/enrollments", nil)
	discoveryResponse := httptest.NewRecorder()
	handler.ServeHTTP(discoveryResponse, discovery)
	if discoveryResponse.Code != http.StatusNotFound {
		t.Fatalf("enrollment discovery status = %d, want 404", discoveryResponse.Code)
	}
}

func TestHTTPReconcilesAndPublishesExecutionEvidence(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	handler := NewHandler(service)
	owner := mustIdentity(t, service, "evidence-owner")
	peer := mustIdentity(t, service, "evidence-peer")
	created, err := service.CreateSession(owner.AccountToken, "evidence-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "evidence-a", ClientPlayer)
	b := mustEnroll(t, service, peer.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "evidence-b", ClientPlayer)
	closeMatchingStreams(t, service, a, b, 8)

	// The request names a method and nothing else. The counts are the
	// broker's to compute.
	request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+created.PublicSession.ExecutionID+"/evidence-sets", strings.NewReader(`{"method":"exact-count"}`))
	request.Header.Set("Authorization", "Bearer "+owner.AccountToken)
	request.Header.Set("Idempotency-Key", "evidence-reconcile")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("evidence status = %d, body = %s", response.Code, response.Body.String())
	}
	var set struct {
		EvidenceSetID string `json:"evidence_set_id"`
		Observations  []struct {
			Source     string `json:"source"`
			EventCount uint64 `json:"event_count"`
		} `json:"observations"`
		GateResults []PublicGateResult `json:"gate_results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&set); err != nil {
		t.Fatal(err)
	}
	if len(set.Observations) != 2 {
		t.Fatalf("observations = %+v", set.Observations)
	}
	for _, observation := range set.Observations {
		if observation.Source != "broker-derived" || observation.EventCount != 8 {
			t.Fatalf("observation was not derived by the broker: %+v", observation)
		}
	}
	if len(set.GateResults) != 2 {
		t.Fatalf("gate results = %+v", set.GateResults)
	}
	for _, result := range set.GateResults {
		if result.Status != "PASS" || !result.CalibrationValid {
			t.Fatalf("gate result = %+v", result)
		}
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "/v1/evidence-sets/"+set.EvidenceSetID, nil)
	publicResponse := httptest.NewRecorder()
	handler.ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusOK || !strings.Contains(publicResponse.Body.String(), `"outcome":"consistent"`) {
		t.Fatalf("public evidence status = %d, body = %s", publicResponse.Code, publicResponse.Body.String())
	}
	if strings.Contains(publicResponse.Body.String(), owner.AccountToken) {
		t.Fatal("public evidence response leaked account token")
	}
}

func TestHTTPRefusesAdapterSuppliedCountsWhereCapturesExist(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	handler := NewHandler(service)
	owner := mustIdentity(t, service, "adjudication-owner")
	created, err := service.CreateSession(owner.AccountToken, "adjudication-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "adjudication-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "adjudication-b", ClientPlayer)
	closeMatchingStreams(t, service, a, b, 4)

	body := `{"method":"exact-count","observations":[` +
		`{"observer_id":"` + a.id + `","execution_id":"` + created.PublicSession.ExecutionID + `","stream_id":"telemetry-a","event_count":6651,"source":"client-reported"},` +
		`{"observer_id":"` + b.id + `","execution_id":"` + created.PublicSession.ExecutionID + `","stream_id":"telemetry-b","event_count":6651,"source":"client-reported"}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+created.PublicSession.ExecutionID+"/evidence-sets", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+owner.AccountToken)
	request.Header.Set("Idempotency-Key", "adjudication-attempt")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "OBSERVATION_ADJUDICATION_FORBIDDEN") {
		t.Fatalf("supplied counts status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestOversizedBodyIsTooLargeRatherThanInvalidJSON(t *testing.T) {
	// Truncating an oversized body and reporting a syntax error sends the
	// client off to debug its serializer over what is really a size limit.
	handler := NewHandler(NewService())
	oversized := `{"handle":"` + strings.Repeat("a", (1<<20)+64) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/identities", strings.NewReader(oversized))
	request.Header.Set("Idempotency-Key", "http-oversized")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "PAYLOAD_TOO_LARGE") {
		t.Fatalf("oversized body code = %s", response.Body.String())
	}
}

func TestMalformedBodyIsStillInvalidJSON(t *testing.T) {
	handler := NewHandler(NewService())
	request := httptest.NewRequest(http.MethodPost, "/v1/identities", strings.NewReader(`{"handle":`))
	request.Header.Set("Idempotency-Key", "http-malformed")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "INVALID_JSON") {
		t.Fatalf("malformed body status = %d, body = %s", response.Code, response.Body.String())
	}
}
