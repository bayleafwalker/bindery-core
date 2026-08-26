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
