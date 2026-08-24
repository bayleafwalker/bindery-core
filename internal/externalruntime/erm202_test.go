package externalruntime

import "testing"

func TestEnrollmentRejectsEveryCompatibilityMismatchBeforeLease(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EnrollmentRequest, *CreateSessionRequest)
	}{
		{
			name: "game",
			mutate: func(enrollment *EnrollmentRequest, session *CreateSessionRequest) {
				enrollment.Compatibility.GameHash = testHashB
				session.Compatibility.GameHash = testHashA
			},
		},
		{
			name: "mod",
			mutate: func(enrollment *EnrollmentRequest, session *CreateSessionRequest) {
				enrollment.Compatibility.ModHash = testHashB
				session.Compatibility.ModHash = testHashA
			},
		},
		{
			name: "map",
			mutate: func(enrollment *EnrollmentRequest, session *CreateSessionRequest) {
				enrollment.Compatibility.MapHash = testHashA
				session.Compatibility.MapHash = testHashB
			},
		},
		{
			name: "adapter",
			mutate: func(enrollment *EnrollmentRequest, session *CreateSessionRequest) {
				enrollment.Adapter = AdapterRef{ID: "other.adapter", Version: "9.9.9"}
				session.Compatibility.AdapterID = "bindery.ra2-adapter"
				session.Compatibility.AdapterVersion = "0.1.0"
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := NewService()
			identity := mustIdentity(t, service, "probe-"+test.name)
			sessionRequest := testSessionRequest()
			session, err := service.CreateSession(identity.AccountToken, "session-"+test.name, sessionRequest)
			if err != nil {
				t.Fatal(err)
			}
			enrollmentRequest := EnrollmentRequest{
				ClientInstanceID: "probe-client",
				ClientClass:      ClientPlayer,
				Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
				Compatibility:    ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
			}
			test.mutate(&enrollmentRequest, &sessionRequest)
			_, err = service.Enroll(identity.AccountToken, session.SessionJoinCredential, session.PublicSession.SessionID, "enroll-probe", enrollmentRequest)
			if !hasCode(err, "COMPATIBILITY_MISMATCH") {
				t.Fatalf("enrollment error = %v, want COMPATIBILITY_MISMATCH", err)
			}
			if len(service.enrollments) != 0 {
				t.Fatalf("compatibility rejection issued %d lease records", len(service.enrollments))
			}
		})
	}
}

func TestEnrollmentRejectsMalformedGameHashBeforeAdmission(t *testing.T) {
	service := NewService()
	identity := mustIdentity(t, service, "malformed-game-hash")
	session, err := service.CreateSession(identity.AccountToken, "malformed-game-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Enroll(identity.AccountToken, session.SessionJoinCredential, session.PublicSession.SessionID, "malformed-game-enroll", EnrollmentRequest{
		ClientInstanceID: "malformed-game-client",
		ClientClass:      ClientPlayer,
		Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility:    ClientHashes{GameHash: "not-a-hash", ModHash: testHashA, MapHash: testHashB},
	})
	if !hasCode(err, "COMPATIBILITY_MISMATCH") {
		t.Fatalf("enrollment error = %v, want COMPATIBILITY_MISMATCH", err)
	}
}
