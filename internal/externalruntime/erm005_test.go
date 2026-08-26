package externalruntime

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestERM005RedactionAndIdempotencyGates(t *testing.T) {
	for _, fixture := range fixtureRedactionCorpus() {
		t.Run(fixture.name, func(t *testing.T) {
			encoded, err := json.Marshal(fixture.public)
			if err != nil {
				t.Fatal(err)
			}
			if err := ScanPublicOutput(encoded, fixture.forbidden...); err != nil {
				t.Fatalf("public DTO failed redaction scan: %v; bytes=%s", err, encoded)
			}
			if err := ScanStructuredLog(fixture.log, fixture.forbidden...); err != nil {
				t.Fatalf("structured log failed redaction scan: %v; bytes=%s", err, fixture.log)
			}
		})
	}

	service := NewService()
	identityRequest := CreateIdentityRequest{Handle: "erm005-player", DisplayName: "ERM-005"}
	identity, err := service.CreateIdentity(identityRequest, "erm005-identity")
	if err != nil {
		t.Fatal(err)
	}
	identityReplay, err := service.CreateIdentity(identityRequest, "erm005-identity")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(identity.PublicIdentity, identityReplay.PublicIdentity) || identityReplay.AccountToken != "" {
		t.Fatalf("identity replay = %+v, want original public identity without a second token", identityReplay)
	}
	if _, err := service.CreateIdentity(CreateIdentityRequest{Handle: "erm005-other"}, "erm005-identity"); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("identity conflict = %v", err)
	}

	sessionRequest := testSessionRequest()
	session, err := service.CreateSession(identity.AccountToken, "erm005-session", sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	sessionReplay, err := service.CreateSession(identity.AccountToken, "erm005-session", sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(session.PublicSession, sessionReplay.PublicSession) || sessionReplay.SessionJoinCredential != "" {
		t.Fatalf("session replay = %+v, want original public session without a second credential", sessionReplay)
	}
	if changed := sessionRequest; func() bool {
		changed.Capture.ObserverPreferred = !changed.Capture.ObserverPreferred
		_, conflictErr := service.CreateSession(identity.AccountToken, "erm005-session", changed)
		return hasCode(conflictErr, "IDEMPOTENCY_CONFLICT")
	}() == false {
		t.Fatal("session idempotency conflict was not rejected")
	}

	enrollmentRequest := EnrollmentRequest{
		ClientInstanceID: "erm005-client",
		ClientClass:      ClientPlayer,
		Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility:    ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	}
	enrollment, err := service.Enroll(identity.AccountToken, session.SessionJoinCredential, session.PublicSession.SessionID, "erm005-enrollment", enrollmentRequest)
	if err != nil {
		t.Fatal(err)
	}
	enrollmentReplay, err := service.Enroll(identity.AccountToken, session.SessionJoinCredential, session.PublicSession.SessionID, "erm005-enrollment", enrollmentRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(enrollment.PublicEnrollment, enrollmentReplay.PublicEnrollment) || enrollmentReplay.ClientLeaseToken != "" || enrollmentReplay.TransportCredential != "" {
		t.Fatalf("enrollment replay = %+v, want original public enrollment without scoped credentials", enrollmentReplay)
	}
	if changed := enrollmentRequest; func() bool {
		changed.ClientClass = ClientObserver
		_, conflictErr := service.Enroll(identity.AccountToken, session.SessionJoinCredential, session.PublicSession.SessionID, "erm005-enrollment", changed)
		return hasCode(conflictErr, "IDEMPOTENCY_CONFLICT")
	}() == false {
		t.Fatal("enrollment idempotency conflict was not rejected")
	}

	reportRequest := LifecycleReportRequest{ReportID: "erm005-ready", Kind: "ready"}
	report, err := service.Report(enrollment.ClientLeaseToken, enrollment.PublicEnrollment.ClientID, "erm005-report", reportRequest)
	if err != nil {
		t.Fatal(err)
	}
	reportReplay, err := service.Report(enrollment.ClientLeaseToken, enrollment.PublicEnrollment.ClientID, "erm005-report", reportRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report, reportReplay) {
		t.Fatalf("report replay = %+v, want original response", reportReplay)
	}
	if _, err := service.Report(enrollment.ClientLeaseToken, enrollment.PublicEnrollment.ClientID, "erm005-report", LifecycleReportRequest{ReportID: "erm005-ready", Kind: "started"}); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("report conflict = %v", err)
	}
}
