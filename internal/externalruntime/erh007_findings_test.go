package externalruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
)

// These pin what running a second, non-RA2 runtime found in core. They are
// written to fail if the behaviour changes, so a fix retires a finding
// loudly instead of leaving the assessment quietly wrong. See
// docs/assessments/2026-08-26-erh-007-second-runtime.md.

// FINDING: ordered-hash cannot report agreement between two producers, ever.
//
// capture.OrderedHash composes per-event digests of the canonical encoding,
// and that encoding binds producer_client_id, capture_id and received_at into
// every event. Two producers that observed exactly the same thing therefore
// hash differently by construction. OrderedHash's own doc comment states the
// opposite intent -- "two producers that batched the same events differently
// must still agree" -- so this is an implementation that contradicts its
// documented purpose, not a design choice.
//
// It is not specific to a second runtime; RA2's two-client cross-check has the
// same problem. A second runtime is simply what made anyone look.
func TestFindingOrderedHashAlwaysDivergesAcrossProducers(t *testing.T) {
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
	first, second := result.Observations[0].OrderedHash, result.Observations[1].OrderedHash
	if first == second {
		t.Fatal("ordered hashes now agree across producers: the finding is fixed and this test, " +
			"the assessment, and the roadmap note must be retired")
	}
	if result.Reconciliation.Outcome != evidencev1.OutcomeInconsistent {
		t.Fatalf("outcome = %s, want inconsistent while the finding stands", result.Reconciliation.Outcome)
	}
	t.Logf("identical observations, divergent hashes: %s vs %s", first[:20], second[:20])
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

// FINDING: a producer cannot close a stream as legitimately empty.
//
// CaptureCloseRequest.FinalSequence is an unsigned sequence number, so the
// smallest thing a producer can claim is that sequence 0 exists. A client that
// honestly observed nothing -- every player in a server-authoritative runtime
// -- must therefore close with a gap it does not have, and is indistinguishable
// from a producer that lost its first event.
func TestFindingAnEmptyStreamCannotCloseCleanly(t *testing.T) {
	service := NewService()
	fixture := newCaptureFixture(t, service, "empty-stream")
	closed, err := service.CloseCapture(fixture.playerA.lease, fixture.playerA.capture, CaptureCloseRequest{
		FinalSequence: 0, LocalDrops: 0, EndReason: "client produced no observations",
	})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Completeness == nil {
		t.Fatal("no completeness manifest")
	}
	if len(closed.Completeness.MissingRanges) == 0 {
		t.Fatal("an empty stream now closes without a phantom gap: the finding is fixed and must be retired")
	}
	if closed.Completeness.EventCount != 0 {
		t.Fatalf("event count = %d, want 0", closed.Completeness.EventCount)
	}
	t.Logf("a stream that produced nothing closes reporting missing %v", closed.Completeness.MissingRanges)
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
			FinalSequence: watched.events - 1, EndReason: "the observer stopped watching",
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
