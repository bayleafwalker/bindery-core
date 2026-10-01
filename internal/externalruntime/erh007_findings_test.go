package externalruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
	"github.com/bayleafwalker/bindery-core/pkg/gatev1"
)

// These cover what running a second, non-RA2 runtime and a third-party game
// found in core. Each finding was first pinned by a test written to fail
// when it was fixed; every one is now resolved, and its test was retired in
// favour of one that holds the fix, with a negative control where a fix
// could pass vacuously. game_tick in the canonical encoding is the one leak
// accepted permanently, and its test still fails if the encoding moves. See
// docs/assessments/2026-08-26-erh-007-second-runtime.md and
// docs/assessments/2026-08-26-erh-007-third-party-runtime.md.

// RETIRED FINDING: ordered-hash could not report agreement between two
// producers, ever. capture.OrderedHash covers producer_client_id, capture_id
// and received_at, so two producers that observed exactly the same thing
// hashed differently by construction and always reconciled as inconsistent.
//
// Broker-derived summaries now also carry capture.ObservedHash, which covers
// only what was observed, and ordered-hash reconciliation compares that. The
// stream hashes still differ, because they still identify who produced each
// stream; agreement is about the observations. The negative control keeps the
// fix from being a method that agrees with everything.
func TestOrderedHashAgreesAcrossProducersOnIdenticalObservations(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "finding-owner")
	created, err := service.CreateSession(owner.AccountToken, "finding-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "finding-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "finding-b", ClientPlayer)
	// The fixture files byte-identical observations on both streams.
	closeMatchingStreams(t, service, a, b, 8)

	result, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "finding-evidence",
		ReconcileEvidenceRequest{Method: evidencev1.MethodOrderedHash})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 2 {
		t.Fatalf("observations = %d", len(result.Observations))
	}
	first, second := result.Observations[0], result.Observations[1]
	if first.OrderedHash == second.OrderedHash {
		t.Fatal("stream hashes agree across producers: ordered_hash stopped identifying the stream")
	}
	if first.ObservedHash == "" || first.ObservedHash != second.ObservedHash {
		t.Fatalf("identical observations, divergent observed hashes: %q vs %q", first.ObservedHash, second.ObservedHash)
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeConsistent {
		t.Fatalf("outcome = %s, want consistent", result.Reconciliation.Outcome)
	}
}

func TestOrderedHashStillReportsDivergentObservations(t *testing.T) {
	service := NewServiceWithPlacementAllocator(testPersistentAllocator)
	owner := mustIdentity(t, service, "divergent-owner")
	created, err := service.CreateSession(owner.AccountToken, "divergent-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "divergent-a", ClientPlayer)
	b := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "divergent-b", ClientPlayer)
	for client, payload := range map[*testEnrollmentSecrets]string{&a: `{"action":"move"}`, &b: `{"action":"attack"}`} {
		if _, err := service.IngestCaptureBatch(client.lease, client.capture, "divergent-"+client.id, batchRequest(0, 7, payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := service.CloseCapture(client.lease, client.capture, CaptureCloseRequest{FinalSequence: through(7), EndReason: "match-ended"}); err != nil {
			t.Fatal(err)
		}
	}

	result, err := service.CreateEvidenceSet(owner.AccountToken, created.PublicSession.ExecutionID, "divergent-evidence",
		ReconcileEvidenceRequest{Method: evidencev1.MethodOrderedHash})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeInconsistent || len(result.Reconciliation.DistinctHashes) != 2 {
		t.Fatalf("divergent observations reconciled as %+v", result.Reconciliation)
	}
}

// FINDING, accepted permanently: game_tick is in the canonical encoding.
//
// Every published event hash covers a field that only a tick-based game can
// fill. A runtime without ticks sends null, so the field is not a barrier to
// entry -- but it cannot be removed either, because the canonical encoding is
// frozen and dropping a field would reissue every hash this repository has
// published. It is recorded as a permanent leak rather than fixed.
func TestFindingGameTickIsFrozenIntoEveryPublishedHash(t *testing.T) {
	event := capture.RawEvent{
		EventID: "finding-event", SessionID: "s", ExecutionID: "e", CaptureID: "c",
		ProducerClientID: "p", ProducerClass: "player", CaptureMethod: "m",
		AdapterID: "a", AdapterVersion: "1", Sequence: 0,
		ReceivedAt: time.Unix(0, 0).UTC(), EventType: "t", Payload: json.RawMessage(`{}`),
	}
	encoded, err := capture.CanonicalEventBytes(event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"game_tick":null`) {
		t.Fatalf("game_tick is no longer emitted for a runtime that has no ticks; "+
			"if the canonical encoding changed, every published hash moved: %s", encoded)
	}
	// A runtime with ticks fills it, and the hash changes -- which is correct,
	// and is why the field cannot simply be dropped.
	tick := uint64(7)
	ticked := event
	ticked.GameTick = &tick
	tickedBytes, err := capture.CanonicalEventBytes(ticked)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == string(tickedBytes) {
		t.Fatal("game_tick does not affect the canonical encoding, so it could be removed after all")
	}
}

// RETIRED FINDING: a producer could not close a stream as legitimately empty.
// final_sequence was an unsigned sequence number, so the smallest claim was
// that sequence 0 exists, and a client that honestly observed nothing closed
// with a gap it did not have. final_sequence is now nullable: null says "this
// producer observed nothing", and is refused from a producer that did.
func TestAnEmptyStreamClosesWithoutAPhantomGap(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "empty-stream")
	request := CaptureCloseRequest{FinalSequence: nil, EndReason: "client produced no observations"}
	closed, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, request)
	if err != nil {
		t.Fatal(err)
	}
	completeness := closed.Completeness
	if completeness == nil || !completeness.Closed {
		t.Fatalf("completeness = %+v, want a closed manifest", completeness)
	}
	if len(completeness.MissingRanges) != 0 || completeness.ExpectedThrough != nil || completeness.EventCount != 0 {
		t.Fatalf("an empty stream closed as %+v, want no expected range, no gap and no events", completeness)
	}
	replay, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, request)
	if err != nil || replay.Completeness.ExpectedThrough != nil {
		t.Fatalf("replaying the empty close = %+v, %v", replay.Completeness, err)
	}
	if _, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture,
		CaptureCloseRequest{FinalSequence: through(0), EndReason: request.EndReason}); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("an empty close was silently changed to a claim of sequence 0: %v", err)
	}
	gate := evaluateCaptureCompleteness(service.captures[fixture.playerA.capture], service.objects, gatev1.Context{
		Phase: GatePhaseEvidenceReconciliation, ArtifactType: GateArtifactCaptureStream, CapabilitiesKnown: true,
	})
	if gate.Status != string(gatev1.StatusPass) {
		t.Fatalf("an honestly empty stream fails completeness: %s %s", gate.Status, gate.Reason)
	}
}

// The control that keeps the empty close honest: a producer that ingested
// anything, or reports gaps, cannot claim it observed nothing.
func TestAnEmptyCloseIsRefusedFromAProducerThatObserved(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "empty-refused")
	mustIngest(t, service, fixture.playerA, 0, 1)
	_, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture,
		CaptureCloseRequest{FinalSequence: nil, EndReason: "claims nothing"})
	if !hasCode(err, "CLOSE_CONTRADICTS_OBSERVATIONS") {
		t.Fatalf("error = %v, want CLOSE_CONTRADICTS_OBSERVATIONS", err)
	}
	_, err = service.CloseCapture(fixture.playerB.lease, fixture.playerB.capture,
		CaptureCloseRequest{FinalSequence: nil, ObservedGaps: [][2]uint64{{0, 0}}, EndReason: "claims a gap in nothing"})
	if !hasCode(err, "CLOSE_CONTRADICTS_OBSERVATIONS") {
		t.Fatalf("error = %v, want CLOSE_CONTRADICTS_OBSERVATIONS", err)
	}
	// Once empty, a stream stays empty.
	if _, err := service.CloseCapture(fixture.playerB.lease, fixture.playerB.capture,
		CaptureCloseRequest{EndReason: "nothing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.IngestCaptureBatch(fixture.playerB.lease, fixture.playerB.capture, "after-empty", batchRequest(0, 0, `{}`)); err == nil {
		t.Fatal("a stream closed as empty accepted an observation afterwards")
	}
}

func TestAnEmptyCloseSurvivesARestart(t *testing.T) {
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	fixture := newCaptureFixture(t, service, "empty-restart")
	if _, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture,
		CaptureCloseRequest{EndReason: "nothing"}); err != nil {
		t.Fatal(err)
	}
	reopened := openPersistentCaptureService(t, directory)
	record, err := reopened.GetCapture(fixture.playerA.capture)
	if err != nil {
		t.Fatal(err)
	}
	if !record.Completeness.Closed || record.Completeness.ExpectedThrough != nil || len(record.Completeness.MissingRanges) != 0 {
		t.Fatalf("empty close after restart = %+v", record.Completeness)
	}
}

func TestHTTPAcceptsANullFinalSequence(t *testing.T) {
	var request CaptureCloseRequest
	if err := json.Unmarshal([]byte(`{"final_sequence":null,"local_drops":0,"end_reason":"nothing"}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.FinalSequence != nil {
		t.Fatalf("final_sequence null decoded as %d", *request.FinalSequence)
	}
	if err := json.Unmarshal([]byte(`{"final_sequence":0,"local_drops":0,"end_reason":"one"}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.FinalSequence == nil || *request.FinalSequence != 0 {
		t.Fatal("final_sequence 0 no longer claims sequence 0")
	}
}

// RETIRED FINDING: capture streams were minted for every enrollment, gated
// only by the session-wide semantic_events switch, so clients that observe
// nothing by design -- every player in a server-authoritative runtime --
// each held a stream they could not honestly close. An enrollment may now
// decline its stream with capture: false.
func TestAnEnrollmentCanDeclineItsCaptureStream(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "decline-owner")
	created, err := service.CreateSession(owner.AccountToken, "decline-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	declined := false
	request := EnrollmentRequest{
		ClientInstanceID: "no-stream", ClientClass: ClientPlayer, Capture: &declined,
		Adapter:       AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	}
	response, err := service.Enroll(owner.AccountToken, created.SessionJoinCredential, sessionID, "enroll-no-stream", request)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.CaptureStreamOffers) != 0 {
		t.Fatalf("a declined enrollment was offered %d capture streams", len(response.CaptureStreamOffers))
	}
	replay, err := service.Enroll(owner.AccountToken, created.SessionJoinCredential, sessionID, "enroll-no-stream", request)
	if err != nil || replay.PublicEnrollment.ClientID != response.PublicEnrollment.ClientID || len(replay.CaptureStreamOffers) != 0 {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	request.Capture = nil
	if _, err := service.Enroll(owner.AccountToken, created.SessionJoinCredential, sessionID, "enroll-no-stream", request); !hasCode(err, "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("changing capture on a replay = %v, want IDEMPOTENCY_CONFLICT", err)
	}

	// The default is unchanged: a session that captures semantic events
	// still offers a stream to an enrollment that does not decline one.
	streamed := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, sessionID, "with-stream", ClientPlayer)
	if streamed.capture == "" {
		t.Fatal("an enrollment that did not decline its stream was not offered one")
	}
	captures, err := service.ListSessionCaptures(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range captures {
		if record.ProducerClientID == response.PublicEnrollment.ClientID {
			t.Fatal("a declined enrollment holds a capture stream")
		}
	}
	if len(captures) != 1 {
		t.Fatalf("session holds %d captures, want 1", len(captures))
	}
}

func TestDecliningCaptureRefusesACaptureMethod(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "decline-method-owner")
	created, err := service.CreateSession(owner.AccountToken, "decline-method-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	declined := false
	_, err = service.Enroll(owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "enroll-contradiction",
		EnrollmentRequest{
			ClientInstanceID: "contradiction", ClientClass: ClientPlayer, Capture: &declined, CaptureMethod: "adapter-log-tail",
			Adapter:       AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
			Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
		})
	if !hasCode(err, "CAPTURE_INVALID") {
		t.Fatalf("error = %v, want CAPTURE_INVALID", err)
	}
}

func TestEnrollmentRequestDecodesCapture(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"client_instance_id":"x","client_class":"player","capture":false,` +
		`"adapter":{"id":"a","version":"1"},"compatibility":{"game_hash":"` + testHashA + `"}}`))
	decoder.DisallowUnknownFields()
	var request EnrollmentRequest
	if err := decoder.Decode(&request); err != nil {
		t.Fatal(err)
	}
	if request.Capture == nil || *request.Capture {
		t.Fatal("capture: false did not decode as a declined stream")
	}
	// Omitting the field must not change an existing enrollment's identity.
	if encoded, _ := json.Marshal(EnrollmentRequest{ClientInstanceID: "x"}); strings.Contains(string(encoded), `"capture"`) {
		t.Fatalf("an enrollment that does not decline still serializes capture: %s", encoded)
	}
}

// RETIRED FINDING: a single-authority execution could publish no evidence at
// all, because reconciliation required two observers and "evidence set"
// conflated the record of what was observed with the cross-check between
// observers. The record method publishes the broker-derived observations
// uncompared; the cross-check methods still refuse a single observer.
func TestASingleAuthorityExecutionPublishesARecord(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "authority-owner")
	created, err := service.CreateSession(owner.AccountToken, "authority-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	authority := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, sessionID, "authority", ClientPlayer)
	declined := false
	if _, err := service.Enroll(owner.AccountToken, created.SessionJoinCredential, sessionID, "enroll-seat", EnrollmentRequest{
		ClientInstanceID: "seat", ClientClass: ClientPlayer, Capture: &declined,
		Adapter:       AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
		Compatibility: ClientHashes{GameHash: testHashA, ModHash: testHashA, MapHash: testHashB},
	}); err != nil {
		t.Fatal(err)
	}
	ingestRange(t, service, authority, 0, 31, 500)
	if _, err := service.CloseCapture(authority.lease, authority.capture, CaptureCloseRequest{FinalSequence: through(31), EndReason: "simulation complete"}); err != nil {
		t.Fatal(err)
	}
	executionID := created.PublicSession.ExecutionID

	if _, err := service.CreateEvidenceSet(owner.AccountToken, executionID, "authority-count",
		ReconcileEvidenceRequest{Method: evidencev1.MethodExactCount}); !hasCode(err, "RECONCILIATION_INVALID") {
		t.Fatalf("a single observer was cross-checked: %v", err)
	}
	record, err := service.CreateEvidenceSet(owner.AccountToken, executionID, "authority-record",
		ReconcileEvidenceRequest{Method: evidencev1.MethodRecord})
	if err != nil {
		t.Fatal(err)
	}
	if record.Reconciliation.Outcome != evidencev1.OutcomeUncompared || record.Reconciliation.ComparedObservers != 0 {
		t.Fatalf("reconciliation = %+v, want uncompared", record.Reconciliation)
	}
	if len(record.Observations) != 1 {
		t.Fatalf("observations = %d, want the authority's", len(record.Observations))
	}
	observation := record.Observations[0]
	if observation.ObserverID != authority.id || observation.EventCount != 32 || observation.Source != evidencev1.SourceBrokerDerived {
		t.Fatalf("observation = %+v", observation)
	}
	if len(record.GateResults) != 1 || record.GateResults[0].Status != string(gatev1.StatusPass) {
		t.Fatalf("gate results = %+v", record.GateResults)
	}
	published, err := service.GetEvidenceSet(record.EvidenceSetID)
	if err != nil || published.EvidenceSetID != record.EvidenceSetID {
		t.Fatalf("the record is not publicly readable: %+v, %v", published, err)
	}
}

// Without captured streams the only observations are the client's own
// account, and a record will not publish those uncompared.
func TestARecordRefusesClientReportedObservations(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "record-claim-owner")
	request := testSessionRequest()
	request.Capture.SemanticEvents = false
	created, err := service.CreateSession(owner.AccountToken, "record-claim-session", request)
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "claim-a", ClientPlayer)
	executionID := created.PublicSession.ExecutionID
	_, err = service.CreateEvidenceSet(owner.AccountToken, executionID, "record-claim", ReconcileEvidenceRequest{
		Method: evidencev1.MethodRecord,
		Observations: []evidencev1.ObservationSummary{
			{ObserverID: a.id, ExecutionID: executionID, StreamID: "claimed", EventCount: 9, Source: evidencev1.SourceClientReported},
		},
	})
	if !hasCode(err, "RECONCILIATION_INVALID") {
		t.Fatalf("error = %v, want RECONCILIATION_INVALID", err)
	}
}

// The counterpart to the findings: what ERH-007 caused to be removed from core
// must stay removed. These are the shapes a non-RA2 runtime needs and that the
// control plane refused before 2026-08-26.
func TestNonRA2SessionShapesAreAccepted(t *testing.T) {
	base := func() CreateSessionRequest {
		return CreateSessionRequest{
			Compatibility: Compatibility{
				GameFamily: "bindery.dedicated", GameVersion: "1.0.0", GameHash: testHashA,
				AdapterID: "bindery.dedicated-adapter", AdapterVersion: "0.1.0",
			},
			ParticipantPolicy: ParticipantPolicy{RequiredPlayers: 2, MaximumPlayers: 2, MaximumObservers: 1},
			Placement:         PlacementIntent{AllowedRegions: []string{"eu-north"}, LatencyP95MS: 100},
			Capture:           CapturePolicy{SemanticEvents: true},
		}
	}
	cases := []struct {
		name    string
		mutate  func(*CreateSessionRequest)
		wantErr bool
	}{
		{name: "no mod and no map", mutate: func(*CreateSessionRequest) {}},
		{name: "more seats than one game's cap", mutate: func(r *CreateSessionRequest) {
			r.ParticipantPolicy = ParticipantPolicy{RequiredPlayers: 10, MaximumPlayers: 16, MaximumObservers: 2}
		}},
		{name: "a single player against a server", mutate: func(r *CreateSessionRequest) {
			r.ParticipantPolicy = ParticipantPolicy{RequiredPlayers: 1, MaximumPlayers: 1, MaximumObservers: 1}
		}},
		{name: "a mod with its hash", mutate: func(r *CreateSessionRequest) {
			r.Compatibility.ModID, r.Compatibility.ModHash = "vanilla", testHashB
		}},
		// Still refused: half a content identity names something unverifiable.
		{name: "a mod id with no hash", wantErr: true, mutate: func(r *CreateSessionRequest) {
			r.Compatibility.ModID = "vanilla"
		}},
		{name: "a map hash with no id", wantErr: true, mutate: func(r *CreateSessionRequest) {
			r.Compatibility.MapHash = testHashB
		}},
		{name: "more seats than the control plane bound", wantErr: true, mutate: func(r *CreateSessionRequest) {
			r.ParticipantPolicy = ParticipantPolicy{RequiredPlayers: 2, MaximumPlayers: MaximumParticipantsPerSession + 1, MaximumObservers: 0}
		}},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService()
			identity := mustIdentity(t, service, fmt.Sprintf("shape-host-%d", index))
			request := base()
			testCase.mutate(&request)
			_, err := service.CreateSession(identity.AccountToken, "shape-session", request)
			if testCase.wantErr && err == nil {
				t.Fatal("expected the session to be refused")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("session refused: %v", err)
			}
		})
	}
}

// The findings below came from the third-party run of 2026-08-26: OpenTTD,
// driven through its admin network by adapters/bindery-openttd-runtime. They
// are pinned here rather than there because they are properties of core, and
// core is testable without a game installed. See
// docs/assessments/2026-08-26-erh-007-third-party-runtime.md.

// RETIRED FINDING: enrollment refused any client whose game_hash differed
// from the session's, so a game that ships a Windows, a macOS and a Linux
// build of one release -- which play together -- could not enroll its second
// platform. game_hash carried provenance and compatibility at once. A session
// may now declare compatible_game_hashes: the check is membership in that
// set, and each enrollment records the build its client declared.
func TestASessionAdmitsTheBuildsItDeclaresCompatible(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "cross-build-owner")
	request := testSessionRequest()
	request.Compatibility.CompatibleGameHashes = []string{testHashB}
	created, err := service.CreateSession(owner.AccountToken, "cross-build-session", request)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.PublicSession.SessionID
	enroll := func(instance, gameHash string) (EnrollmentCreateResponse, error) {
		return service.Enroll(owner.AccountToken, created.SessionJoinCredential, sessionID, "enroll-"+instance, EnrollmentRequest{
			ClientInstanceID: instance, ClientClass: ClientPlayer,
			Adapter:       AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
			Compatibility: ClientHashes{GameHash: gameHash, ModHash: testHashA, MapHash: testHashB},
		})
	}
	primary, err := enroll("primary-platform", testHashA)
	if err != nil {
		t.Fatal(err)
	}
	other, err := enroll("other-platform", testHashB)
	if err != nil {
		t.Fatalf("a declared compatible build was refused: %v", err)
	}
	if primary.PublicEnrollment.GameHash != testHashA || other.PublicEnrollment.GameHash != testHashB {
		t.Fatalf("enrollments do not record their builds: %q and %q", primary.PublicEnrollment.GameHash, other.PublicEnrollment.GameHash)
	}
	if _, err := enroll("undeclared-build", testHashC); !hasCode(err, "COMPATIBILITY_MISMATCH") {
		t.Fatalf("an undeclared build = %v, want COMPATIBILITY_MISMATCH", err)
	}
	session, err := service.GetSession(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Compatibility.CompatibleGameHashes) != 1 || session.Compatibility.CompatibleGameHashes[0] != testHashB {
		t.Fatalf("the session does not publish its compatible builds: %+v", session.Compatibility)
	}
}

// The default is unchanged: a session that declares no other build admits
// only its own.
func TestASessionWithoutCompatibleBuildsAdmitsOnlyItsOwn(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "one-build-owner")
	created, err := service.CreateSession(owner.AccountToken, "one-build-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Enroll(owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID,
		"enroll-other-platform", EnrollmentRequest{
			ClientInstanceID: "other-platform",
			ClientClass:      ClientPlayer,
			Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
			Compatibility:    ClientHashes{GameHash: testHashB, ModHash: testHashA, MapHash: testHashB},
		})
	if !hasCode(err, "COMPATIBILITY_MISMATCH") {
		t.Fatalf("refusal code = %v, want COMPATIBILITY_MISMATCH", err)
	}
}

func TestCompatibleGameHashesAreValidated(t *testing.T) {
	for name, hashes := range map[string][]string{
		"malformed": {"sha256:nope"},
		"duplicate": {testHashB, testHashB},
		"own-build": {testHashA},
	} {
		service := NewService()
		owner := mustIdentity(t, service, "validate-"+strings.ReplaceAll(name, " ", "-"))
		request := testSessionRequest()
		request.Compatibility.CompatibleGameHashes = hashes
		if _, err := service.CreateSession(owner.AccountToken, "validate-session", request); !hasCode(err, "COMPATIBILITY_INVALID") {
			t.Errorf("%s: error = %v, want COMPATIBILITY_INVALID", name, err)
		}
	}
}

// RETIRED FINDING: an evidence set recorded no observation interval, so two
// honest observers who watched different intervals of one execution were
// indistinguishable from two who disagree about the same interval. Each
// broker-derived summary now records the interval its stream covers: first
// and last game tick, and first and last broker receive time.
func TestEvidenceRecordsTheIntervalEachObserverWatched(t *testing.T) {
	service := NewService()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	fixture := newCaptureFixture(t, service, "interval")
	// One observer files ticks 100..107; the other, having started later,
	// files ticks 102..107 of the same execution. Neither is lying.
	for _, watched := range []struct {
		client    testEnrollmentSecrets
		firstTick uint64
	}{{fixture.playerA, 100}, {fixture.playerB, 102}} {
		events := make([]TelemetryEventInput, 0, 8)
		for tick := watched.firstTick; tick <= 107; tick++ {
			tick := tick
			sequence := uint64(len(events))
			events = append(events, TelemetryEventInput{
				EventID: fmt.Sprintf("event-%016d", sequence), Sequence: sequence, GameTick: &tick,
				EventType: "game.player.action-observed", Payload: json.RawMessage(`{"action":"move"}`),
			})
		}
		last := uint64(len(events) - 1)
		if _, err := service.IngestCaptureBatch(watched.client.lease, watched.client.capture, "interval-"+watched.client.id,
			IngestBatchRequest{FirstSequence: 0, LastSequence: last, Events: events}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(10 * time.Second)
		if _, err := service.CloseCapture(watched.client.lease, watched.client.capture, CaptureCloseRequest{
			FinalSequence: through(last), EndReason: "the observer stopped watching",
		}); err != nil {
			t.Fatal(err)
		}
	}

	result, err := service.CreateEvidenceSet(fixture.identity.AccountToken,
		fixture.session.PublicSession.ExecutionID, "interval-evidence",
		ReconcileEvidenceRequest{Method: evidencev1.MethodExactCount})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeInconsistent {
		t.Fatalf("outcome = %s, want inconsistent", result.Reconciliation.Outcome)
	}
	firstTicks := map[string]uint64{}
	for _, observation := range result.Observations {
		interval := observation.Interval
		if interval == nil || interval.FirstGameTick == nil || interval.LastGameTick == nil {
			t.Fatalf("observation %s records no interval: %+v", observation.StreamID, interval)
		}
		if *interval.LastGameTick != 107 {
			t.Fatalf("observation %s last tick = %d, want 107", observation.StreamID, *interval.LastGameTick)
		}
		if interval.FirstReceivedAt.IsZero() || interval.LastReceivedAt.Before(interval.FirstReceivedAt) {
			t.Fatalf("observation %s receive interval = %+v", observation.StreamID, interval)
		}
		firstTicks[observation.ObserverID] = *interval.FirstGameTick
	}
	if firstTicks[fixture.playerA.id] != 100 || firstTicks[fixture.playerB.id] != 102 {
		t.Fatalf("first ticks = %v, want 100 for the early observer and 102 for the late one", firstTicks)
	}
	// The stored set is not aliased by what was returned.
	*result.Observations[0].Interval.FirstGameTick = 999
	stored, err := service.GetEvidenceSet(result.EvidenceSetID)
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range stored.Observations {
		if *observation.Interval.FirstGameTick == 999 {
			t.Fatal("mutating a returned evidence set changed the stored one")
		}
	}
}

// A runtime without ticks still gets a receive-time interval, and an empty
// stream records none.
func TestIntervalWithoutTicksAndForAnEmptyStream(t *testing.T) {
	events := []capture.RawEvent{
		{Sequence: 0, ReceivedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
		{Sequence: 1, ReceivedAt: time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)},
	}
	interval := observationInterval(events)
	if interval == nil || interval.FirstGameTick != nil || interval.LastGameTick != nil {
		t.Fatalf("interval without ticks = %+v", interval)
	}
	if !interval.FirstReceivedAt.Equal(events[0].ReceivedAt) || !interval.LastReceivedAt.Equal(events[1].ReceivedAt) {
		t.Fatalf("receive interval = %+v", interval)
	}
	if observationInterval(nil) != nil {
		t.Fatal("an empty stream recorded an interval")
	}
}

const testHashC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
