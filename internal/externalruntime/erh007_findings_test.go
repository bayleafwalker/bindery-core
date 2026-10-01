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

// These pin what running a second, non-RA2 runtime found in core. They are
// written to fail if the behaviour changes, so a fix retires a finding
// loudly instead of leaving the assessment quietly wrong. See
// docs/assessments/2026-08-26-erh-007-second-runtime.md.

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

// FINDING: every participant must run a byte-identical build of the game.
//
// Enrollment refuses any client whose game_hash differs from the session's.
// For Red Alert 2 that is nearly free -- one platform, one executable -- but
// most games ship a different binary per platform, and OpenTTD's Windows,
// macOS and Linux builds of the same release play together. Under this rule a
// cross-platform match cannot be enrolled at all: the second platform's client
// is refused as incompatible with the first.
//
// The fix is a contract decision rather than a patch. game_hash currently
// carries two meanings at once -- "which build am I running" and "are we
// playing the same thing" -- and only the second belongs in a compatibility
// check. Recorded rather than fixed, because deciding what makes two builds
// the same game is not an adapter's call.
func TestFindingEnrollmentRequiresByteIdenticalGameBuilds(t *testing.T) {
	service := NewService()
	owner := mustIdentity(t, service, "cross-build-owner")
	created, err := service.CreateSession(owner.AccountToken, "cross-build-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	// testHashB stands in for the same game, same version, other platform.
	_, err = service.Enroll(owner.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID,
		"enroll-other-platform", EnrollmentRequest{
			ClientInstanceID: "other-platform",
			ClientClass:      ClientPlayer,
			Adapter:          AdapterRef{ID: "bindery.ra2-adapter", Version: "0.1.0"},
			Compatibility:    ClientHashes{GameHash: testHashB, ModHash: testHashA, MapHash: testHashB},
		})
	if err == nil {
		t.Fatal("a client running another platform's build of the same game now enrolls: " +
			"the finding is fixed and this test and the assessment must be retired")
	}
	if !hasCode(err, "COMPATIBILITY_MISMATCH") {
		t.Fatalf("refusal code = %v, want COMPATIBILITY_MISMATCH", err)
	}
}

// FINDING: an evidence set does not record what interval each observer watched.
//
// Two honest observers of one execution can watch different intervals of it --
// the second connected later, the first was disconnected early -- and produce
// different counts of the same execution. Reconciliation calls that
// `inconsistent`, and the evidence set holds nothing that distinguishes it from
// two observers of the same interval who genuinely disagree. Anyone reading the
// set later cannot tell "they saw different things" from "they saw different
// amounts of it".
//
// This surfaced against a real game: two admin connections to the same OpenTTD
// server differ by exactly one event, because the earlier one observes the
// later one arriving. The adapter works around it by bounding both recordings
// between two facts in the game's own history, which is a thing an adapter can
// do only because it controls both observers.
func TestFindingEvidenceSetsRecordNoObservationInterval(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "interval")
	// One observer files eight observations; the other, having started later,
	// files six of the same execution. Neither is lying.
	for _, watched := range []struct {
		client testEnrollmentSecrets
		events uint64
	}{{fixture.playerA, 8}, {fixture.playerB, 6}} {
		ingestRange(t, service, watched.client, 0, watched.events-1, 500)
		if _, err := service.CloseCapture(watched.client.lease, watched.client.capture, CaptureCloseRequest{
			FinalSequence: through(watched.events - 1), EndReason: "the observer stopped watching",
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
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"observed_from", "observed_until", "observation_interval", "interval"} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("evidence sets now record %q: the finding is fixed and must be retired", field)
		}
	}
	t.Logf("two honest observers of different intervals are recorded as a disagreement: %v",
		result.Reconciliation.DistinctCounts)
}
