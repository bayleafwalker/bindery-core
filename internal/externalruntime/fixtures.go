package externalruntime

import (
	"bytes"
	"encoding/json"
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

func fixtureRelayPlacement() *PublicPlacement {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return &PublicPlacement{
		SchemaVersion:     SchemaVersion,
		PlacementID:       "0198c2c3-4d5e-7f60-8123-456789abcdea",
		SessionID:         "0198c2c3-4d5e-7f60-8123-456789abcdeb",
		Region:            "eu-north",
		RelayProviderID:   "bindery-native",
		RelayAllocationID: "0198c2c3-4d5e-7f60-8123-456789abcdef",
		RelayEndpoint:     "127.0.0.1:40000",
		PolicyVersion:     "relay-placement/v1",
		Allocator:         fixtureAllocatorIdentity(),
		CreatedAt:         now,
	}
}

func fixtureAllocatorIdentity() ImplementationIdentity {
	return ImplementationIdentity{
		Implementation: "bindery-native",
		Repository:     "https://github.com/bayleafwalker/bindery-core",
		Revision:       "738e9f752ad1d892bdad8852cd4bd4e29182c16a",
		ConfigDigest:   "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
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
			name:      "capture-public-dto",
			public:    fixtureClosedCapture(),
			forbidden: []string{"client-lease-fixture", "objects/sha256", "https://ingest.internal.invalid/batches"},
			log:       []byte(`{"event":"capture.public-read","capture_id":"capture-fixture"}`),
		},
		{
			name:      "capture-object-manifest",
			public:    fixtureCaptureObjectManifest(),
			forbidden: []string{"client-lease-fixture", "private/", "objects/sha256"},
			log:       []byte(`{"event":"capture.object-stored","capture_id":"capture-fixture"}`),
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
		ExecutionID:        "execution-fixture",
		PlacementID:        "0198c2c3-4d5e-7f60-8123-456789abcdea",
		CreatedByAccountID: "acct-player-a",
		CreatedAt:          now,
		Phase:              SessionReady,
		Compatibility:      Compatibility{GameFamily: "ra2-yr", GameVersion: "1.001", GameHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", ModID: "vanilla-yr", ModHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", MapID: "official:sample", MapHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		ParticipantPolicy:  ParticipantPolicy{RequiredPlayers: 2, MaximumPlayers: 2, MaximumObservers: 1},
		CapturePolicy:      CapturePolicy{SemanticEvents: true, PostMatchDump: true, ObserverPreferred: true},
		Enrollments: []PublicEnrollment{
			{ClientID: "client-player-a", AccountID: "acct-player-a", ClientClass: ClientPlayer, Phase: EnrollmentReady, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", EnrolledAt: now},
			{ClientID: "client-player-b", AccountID: "acct-player-b", ClientClass: ClientPlayer, Phase: EnrollmentReady, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", EnrolledAt: now},
			{ClientID: "client-observer", AccountID: "acct-observer", ClientClass: ClientObserver, Phase: EnrollmentReady, AdapterID: "bindery.ra2-adapter", AdapterVersion: "0.1.0", EnrolledAt: now},
		},
		Placement:               fixtureRelayPlacement(),
		Transitions:             []PublicTransition{},
		PublicDataNoticeVersion: "1.0",
	}
}

func fixtureExpiredSession() PublicSession {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return PublicSession{
		SchemaVersion:           SchemaVersion,
		SessionID:               "session-expired-fixture",
		ExecutionID:             "execution-expired-fixture",
		CreatedByAccountID:      "acct-player-a",
		CreatedAt:               now,
		Phase:                   SessionExpired,
		Enrollments:             []PublicEnrollment{},
		Transitions:             []PublicTransition{},
		PublicDataNoticeVersion: "1.0",
	}
}

// fixtureClosedCapture is a fully populated public capture, completeness
// manifest included, so the redaction oracle scans the shape a real read
// returns rather than a minimal one.
func fixtureClosedCapture() PublicCapture {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	closedAt := now.Add(20 * time.Minute)
	expected := uint64(6650)
	return PublicCapture{
		CaptureID:        "capture-fixture",
		SessionID:        "session-fixture",
		ExecutionID:      "execution-fixture",
		ProducerClientID: "client-player-a",
		ProducerClass:    string(ClientPlayer),
		CaptureMethod:    "adapter-log-tail",
		AdapterID:        "bindery.ra2-adapter",
		AdapterVersion:   "0.1.0",
		Status:           string(CaptureClosed),
		CreatedAt:        now,
		ClosedAt:         &closedAt,
		Objects:          []string{"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
		Completeness: &CaptureCompleteness{
			ExpectedThrough: &expected,
			ObservedRanges:  [][2]uint64{{0, 6650}},
			MissingRanges:   [][2]uint64{},
			EventCount:      6651,
			RawObjectHashes: []string{"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
			Closed:          true,
			EndReason:       "match-ended",
			SourceCoverage:  string(ClientPlayer),
			ClockQuality:    ClockProducerTimed,
		},
	}
}

func fixtureCaptureObjectManifest() PublicObjectManifest {
	return PublicObjectManifest{
		SchemaVersion:    SchemaVersion,
		ContentHash:      "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		MediaType:        "application/octet-stream",
		Bytes:            4096,
		CaptureID:        "capture-fixture",
		ProducerClientID: "client-player-a",
		CaptureMethod:    "adapter-log-tail",
		ReceivedAt:       time.Date(2026, 8, 26, 12, 20, 0, 0, time.UTC),
	}
}

// RedactionCorpus serializes the public DTO shapes that actually cross the
// boundary, so the release gate can scan real bytes from a binary rather than
// only from a test. Forbidden fixture values are emitted alongside them so a
// scan can prove the values are absent, not merely that no field is named
// suspiciously.
func RedactionCorpus() ([]byte, []string, error) {
	var serialized []byte
	forbidden := make([]string, 0)
	for _, fixture := range fixtureRedactionCorpus() {
		encoded, err := json.Marshal(fixture.public)
		if err != nil {
			return nil, nil, err
		}
		serialized = append(serialized, encoded...)
		serialized = append(serialized, '\n')
		serialized = append(serialized, fixture.log...)
		serialized = append(serialized, '\n')
		forbidden = append(forbidden, fixture.forbidden...)
	}
	return serialized, forbidden, nil
}
