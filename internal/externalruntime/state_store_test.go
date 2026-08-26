package externalruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/pkg/evidencev1"
)

func TestPersistentServiceRestoresResolvableControlAndEvidenceGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control-state.json")
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := OpenPersistentService(testPersistentAllocator, store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 19, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }

	playerA := mustIdentity(t, service, "persistent-alpha")
	playerB := mustIdentity(t, service, "persistent-bravo")
	created, err := service.CreateSession(playerA.AccountToken, "persistent-session", testSessionRequest())
	if err != nil {
		t.Fatal(err)
	}
	a := mustEnroll(t, service, playerA.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "persistent-client-a", ClientPlayer)
	b := mustEnroll(t, service, playerB.AccountToken, created.SessionJoinCredential, created.PublicSession.SessionID, "persistent-client-b", ClientPlayer)
	reportReady(t, service, a, "persistent-ready-a")
	reportReady(t, service, b, "persistent-ready-b")
	mustReport(t, service, a, "persistent-start-a", "started")
	mustReport(t, service, b, "persistent-start-b", "started")

	set, err := service.CreateEvidenceSet(playerA.AccountToken, created.PublicSession.ExecutionID, "persistent-evidence", ReconcileEvidenceRequest{
		Method: evidencev1.MethodExactCount,
		Observations: []evidencev1.ObservationSummary{
			{ObserverID: a.id, ExecutionID: created.PublicSession.ExecutionID, StreamID: "telemetry-a", EventCount: 6651},
			{ObserverID: b.id, ExecutionID: created.PublicSession.ExecutionID, StreamID: "telemetry-b", EventCount: 6651},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	reopenedStore, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPersistentService(testPersistentAllocator, reopenedStore)
	if err != nil {
		t.Fatal(err)
	}
	reopened.clock = func() time.Time { return now.Add(time.Second) }

	identity, err := reopened.GetIdentity(playerA.PublicIdentity.AccountID)
	if err != nil || identity.Handle != "persistent-alpha" {
		t.Fatalf("identity after restart = %+v, error = %v", identity, err)
	}
	session, err := reopened.GetSession(created.PublicSession.SessionID)
	if err != nil || session.ExecutionID != created.PublicSession.ExecutionID || session.PlacementID == "" {
		t.Fatalf("session after restart = %+v, error = %v", session, err)
	}
	placement, err := reopened.GetPlacement(session.PlacementID)
	if err != nil || placement.SessionID != session.SessionID || placement.Allocator.Revision == "" {
		t.Fatalf("placement after restart = %+v, error = %v", placement, err)
	}
	execution, err := reopened.GetExecution(session.ExecutionID)
	if err != nil || execution.Phase != ExecutionRunning || len(execution.EvidenceSetIDs) != 1 {
		t.Fatalf("execution after restart = %+v, error = %v", execution, err)
	}
	restoredSet, err := reopened.GetEvidenceSet(set.EvidenceSetID)
	if err != nil || restoredSet.ExecutionID != execution.ExecutionID || restoredSet.Reconciliation.Outcome != evidencev1.OutcomeConsistent {
		t.Fatalf("evidence after restart = %+v, error = %v", restoredSet, err)
	}
	if _, err := reopened.CreateSession(playerA.AccountToken, "after-restart", testSessionRequest()); err != nil {
		t.Fatalf("persisted account verifier no longer authenticates: %v", err)
	}
	if _, err := reopened.Heartbeat(a.lease, a.id); err != nil {
		t.Fatalf("persisted client lease no longer authenticates: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestPersistenceFailureRollsBackMutation(t *testing.T) {
	store := &failingStateStore{snapshot: emptyServiceSnapshot(), fail: true}
	service, err := OpenPersistentService(nil, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateIdentity(CreateIdentityRequest{Handle: "rolled-back"}, "rollback-id"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if len(service.identities) != 0 || len(service.handles) != 0 || len(service.identityIdempotency) != 0 {
		t.Fatalf("failed mutation remained in memory: identities=%d handles=%d idempotency=%d", len(service.identities), len(service.handles), len(service.identityIdempotency))
	}
}

func testPersistentAllocator(PlacementIntent) (PublicPlacement, error) {
	return PublicPlacement{
		Region:            "eu-north",
		RelayProviderID:   "cncnet-private",
		RelayAllocationID: "0198c2c3-4d5e-7f60-8123-456789abcdef",
		RelayEndpoint:     "192.0.2.10:50001",
		PolicyVersion:     "cncnet-private-lab-v1",
		Allocator:         fixtureAllocatorIdentity(),
	}, nil
}

type failingStateStore struct {
	snapshot serviceSnapshot
	fail     bool
}

func (s *failingStateStore) Load() (serviceSnapshot, error) { return s.snapshot, nil }

func (s *failingStateStore) Save(snapshot serviceSnapshot) error {
	if s.fail {
		return errors.New("injected persistence failure")
	}
	s.snapshot = snapshot
	return nil
}
