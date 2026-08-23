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
