package externalruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testHashA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testHashB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestIdentityCredentialIsReturnedOnlyInAuthenticatedDTO(t *testing.T) {
	service := NewService()
	created, err := service.CreateIdentity(CreateIdentityRequest{Handle: "tesla-coil-17", DisplayName: "Tesla Coil", Metadata: map[string]any{"client_family": "ra2"}}, "identity-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.AccountToken) != 43 {
		t.Fatalf("expected 256-bit raw-base64url token, got %d characters", len(created.AccountToken))
	}
	identity, err := service.GetIdentity(created.PublicIdentity.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), created.AccountToken) {
		t.Fatal("public identity contains bearer token")
	}
	if got := len(service.identities[created.PublicIdentity.AccountID].tokenVerifier); got != 32 {
		t.Fatalf("stored verifier length = %d, want 32", got)
	}
	if _, err := service.CreateIdentity(CreateIdentityRequest{Handle: "b-", DisplayName: "x"}, "identity-2"); err == nil {
		t.Fatal("expected short handle rejection")
	}
}

func TestCreateIdentityIdempotencyAndHandleUniqueness(t *testing.T) {
	service := NewService()
	req := CreateIdentityRequest{Handle: "same-handle", DisplayName: "first"}
	first, err := service.CreateIdentity(req, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.CreateIdentity(req, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicIdentity.AccountID != replay.PublicIdentity.AccountID || replay.AccountToken != "" {
		t.Fatal("idempotent replay changed the original result")
	}
	if _, err := service.CreateIdentity(CreateIdentityRequest{Handle: "same-handle", DisplayName: "second"}, "different-request"); !hasCode(err, "HANDLE_TAKEN") {
		t.Fatalf("duplicate handle error = %v", err)
	}
	if _, err := service.CreateIdentity(CreateIdentityRequest{Handle: "other-handle"}, "same-request"); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("idempotency conflict = %v", err)
	}
}

func TestSessionEnrollmentLifecycleAndObserverDegradation(t *testing.T) {
	service := NewService()
	playerA := mustIdentity(t, service, "player-alpha")
	playerB := mustIdentity(t, service, "player-bravo")
	observer := mustIdentity(t, service, "observer-one")
	created, err := service.CreateSession(playerA.AccountToken, "session-1", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	join := created.SessionJoinCredential
	a := mustEnroll(t, service, playerA.AccountToken, join, created.PublicSession.SessionID, "client-a", ClientPlayer)
	b := mustEnroll(t, service, playerB.AccountToken, join, created.PublicSession.SessionID, "client-b", ClientPlayer)
	o := mustEnroll(t, service, observer.AccountToken, join, created.PublicSession.SessionID, "client-observer", ClientObserver)
	if got := len(service.sessions[created.PublicSession.SessionID].enrollments); got != 3 {
		t.Fatalf("enrollment count = %d, want 3", got)
	}

	reportReady(t, service, a, "ready-a")
	ready := reportReady(t, service, b, "ready-b")
	if ready.PublicSession.Phase != SessionReady {
		t.Fatalf("phase after two ready players = %s", ready.PublicSession.Phase)
	}
	startedA := mustReport(t, service, a, "start-a", "started")
	if startedA.PublicSession.Phase != SessionRunning {
		t.Fatalf("phase after first start = %s", startedA.PublicSession.Phase)
	}
	mustReport(t, service, b, "start-b", "started")
	failedObserver := mustReport(t, service, o, "observer-failed", "failed")
	if failedObserver.PublicSession.Phase != SessionRunning {
		t.Fatalf("observer failure changed player session to %s", failedObserver.PublicSession.Phase)
	}
	mustReport(t, service, a, "exit-a", "exited")
	ended := mustReport(t, service, b, "exit-b", "exited")
	if ended.PublicSession.Phase != SessionEnded {
		t.Fatalf("phase after players exit = %s", ended.PublicSession.Phase)
	}
	execution, err := service.GetExecution(created.PublicSession.ExecutionID)
	if err != nil || execution.Phase != ExecutionEnded || execution.StartedAt == nil || execution.EndedAt == nil {
		t.Fatalf("execution lifecycle = %+v, error = %v", execution, err)
	}

	publicBytes, err := json.Marshal(ended.PublicSession)
	if err != nil {
		t.Fatal(err)
	}
	public := string(publicBytes)
	for _, secret := range []string{join, a.lease, a.transport, b.lease, o.lease} {
		if strings.Contains(public, secret) {
			t.Fatalf("public session contains secret %q", secret)
		}
	}
}

func TestLeaseConvergenceExpiresAdmission(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	identity := mustIdentity(t, service, "expiring-user")
	created, err := service.CreateSession(identity.AccountToken, "expiring-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	now = created.ExpiresAt.Add(time.Second)
	if err := service.Converge(now); err != nil {
		t.Fatal(err)
	}
	public, err := service.GetSession(created.PublicSession.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if public.Phase != SessionExpired {
		t.Fatalf("phase = %s, want expired", public.Phase)
	}
}

type testEnrollmentSecrets struct{ id, lease, transport string }

func mustIdentity(t *testing.T, service *Service, handle string) CreateIdentityResponse {
	t.Helper()
	response, err := service.CreateIdentity(CreateIdentityRequest{Handle: handle}, "create-"+handle)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func mustEnroll(t *testing.T, service *Service, accountToken, join, sessionID, instance string, class ClientClass) testEnrollmentSecrets {
	t.Helper()
	response, err := service.Enroll(accountToken, join, sessionID, "enroll-"+instance, EnrollmentRequest{ClientInstanceID: instance, ClientClass: class, Adapter: AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"}, Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB}})
	if err != nil {
		t.Fatal(err)
	}
	return testEnrollmentSecrets{id: response.PublicEnrollment.ClientID, lease: response.ClientLeaseToken, transport: response.TransportCredential}
}

func reportReady(t *testing.T, service *Service, enrollment testEnrollmentSecrets, key string) LifecycleReportResponse {
	t.Helper()
	return mustReport(t, service, enrollment, key, "ready")
}

func mustReport(t *testing.T, service *Service, enrollment testEnrollmentSecrets, key, kind string) LifecycleReportResponse {
	t.Helper()
	response, err := service.Report(enrollment.lease, enrollment.id, key, LifecycleReportRequest{ReportID: key, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func testSessionRequest() CreateSessionRequest {
	return CreateSessionRequest{
		Compatibility:     Compatibility{GameFamily: "ra2-yr", GameVersion: "1.001", GameHash: testHashA, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", ModID: "vanilla-yr", ModHash: testHashA, MapID: "official:sample", MapHash: testHashB},
		ParticipantPolicy: ParticipantPolicy{RequiredPlayers: 2, MaximumPlayers: 2, MaximumObservers: 1},
		Placement:         PlacementIntent{AllowedRegions: []string{"eu-north"}, LatencyP95MS: 100},
		Capture:           CapturePolicy{SemanticEvents: true, PostMatchDump: true, ObserverPreferred: true},
	}
}

func hasCode(err error, code string) bool {
	var domain *DomainError
	return err != nil && errorsAs(err, &domain) && domain.Code == code
}

func errorsAs(err error, target **DomainError) bool {
	domain, ok := err.(*DomainError)
	if !ok {
		return false
	}
	*target = domain
	return true
}
