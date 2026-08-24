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
