package externalruntime

import (
	"bytes"
	"time"
)

func fixtureCreateIdentityResponse() CreateIdentityResponse {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return CreateIdentityResponse{
		PublicIdentity: PublicIdentity{
			SchemaVersion:           SchemaVersion,
			AccountID:               "acct-fixture",
			Handle:                  "player-fixture",
			ClaimedAt:               now,
			Status:                  IdentityActive,
			PublicDataNoticeVersion: "1.0",
		},
		AccountToken: "account-token-fixture",
		Recovery:     "none",
	}
}

func fixtureIdentityRecord() identityRecord {
	return identityRecord{
		PublicIdentity: PublicIdentity{
			SchemaVersion:           SchemaVersion,
			AccountID:               "acct-token-fixture",
			Handle:                  "token-fixture",
			ClaimedAt:               time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
			Status:                  IdentityActive,
			PublicDataNoticeVersion: "1.0",
		},
		tokenVerifier: bytes.Repeat([]byte{0x42}, 32),
	}
}

type redactionFixture struct {
	name      string
	public    any
	forbidden []string
	log       []byte
}

func fixtureRedactionCorpus() []redactionFixture {
	identity := fixtureCreateIdentityResponse()
	return []redactionFixture{
		{
			name:      "identity-public-dto",
			public:    identity.PublicIdentity,
			forbidden: []string{identity.AccountToken, "https://internal.invalid/upload"},
			log:       []byte(`{"event":"identity.public-read","account_id":"acct-fixture"}`),
		},
		{
			name:      "session-public-dto",
			public:    fixtureSessionWithEnrollments(),
			forbidden: []string{"session-join-fixture", "transport-credential-fixture", "https://relay.internal.invalid:9000"},
			log:       []byte(`{"event":"session.public-read","session_id":"session-fixture"}`),
		},
		{
			name:      "expired-session-public-dto",
			public:    fixtureExpiredSession(),
			forbidden: []string{"account-token-fixture", "https://source.internal.invalid:7000"},
			log:       []byte(`{"event":"session.expired","session_id":"session-expired-fixture"}`),
		},
	}
}

func fixtureSessionWithEnrollments() PublicSession {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return PublicSession{
		SchemaVersion:      SchemaVersion,
		SessionID:          "session-fixture",
		CreatedByAccountID: "acct-player-a",
		CreatedAt:          now,
		Phase:              SessionReady,
		Compatibility:      Compatibility{GameFamily: "ra2-yr", GameVersion: "1.001", AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", ModID: "vanilla-yr", ModHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", MapID: "official:sample", MapHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		ParticipantPolicy:  ParticipantPolicy{RequiredPlayers: 2, MaximumPlayers: 2, MaximumObservers: 1},
		CapturePolicy:      CapturePolicy{SemanticEvents: true, PostMatchDump: true, ObserverPreferred: true},
		Enrollments: []PublicEnrollment{
			{ClientID: "client-player-a", AccountID: "acct-player-a", ClientClass: ClientPlayer, Phase: EnrollmentReady, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", EnrolledAt: now},
			{ClientID: "client-player-b", AccountID: "acct-player-b", ClientClass: ClientPlayer, Phase: EnrollmentReady, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", EnrolledAt: now},
			{ClientID: "client-observer", AccountID: "acct-observer", ClientClass: ClientObserver, Phase: EnrollmentReady, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", EnrolledAt: now},
		},
		Transitions:             []PublicTransition{},
		PublicDataNoticeVersion: "1.0",
	}
}

func fixtureExpiredSession() PublicSession {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return PublicSession{
		SchemaVersion:           SchemaVersion,
		SessionID:               "session-expired-fixture",
		CreatedByAccountID:      "acct-player-a",
		CreatedAt:               now,
		Phase:                   SessionExpired,
		Enrollments:             []PublicEnrollment{},
		Transitions:             []PublicTransition{},
		PublicDataNoticeVersion: "1.0",
	}
}
