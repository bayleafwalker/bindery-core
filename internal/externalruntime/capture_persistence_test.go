package externalruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bayleafwalker/bindery-core/internal/capture"
)

func openPersistentCaptureService(t *testing.T, directory string) *Service {
	t.Helper()
	store, err := NewFileStateStore(filepath.Join(directory, "control-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := OpenPersistentService(testPersistentAllocator, store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	return service
}

func TestCapturesAndTheirObjectsSurviveARestart(t *testing.T) {
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	fixture := newCaptureFixture(t, service, "durable")
	mustIngest(t, service, fixture.playerA, 0, 1)
	mustIngest(t, service, fixture.playerA, 3, 3)
	mustIngest(t, service, fixture.playerB, 0, 3)
	if _, err := service.CloseCapture(fixture.playerB.lease, fixture.playerB.capture, CaptureCloseRequest{FinalSequence: 3, EndReason: "match-ended"}); err != nil {
		t.Fatal(err)
	}
	beforeA, err := service.GetCapture(fixture.playerA.capture)
	if err != nil {
		t.Fatal(err)
	}

	reopened := openPersistentCaptureService(t, directory)
	afterA, err := reopened.GetCapture(fixture.playerA.capture)
	if err != nil {
		t.Fatal(err)
	}
	if afterA.Completeness.EventCount != beforeA.Completeness.EventCount {
		t.Fatalf("event count %d became %d across restart", beforeA.Completeness.EventCount, afterA.Completeness.EventCount)
	}
	if len(afterA.Completeness.MissingRanges) != 1 || afterA.Completeness.MissingRanges[0] != [2]uint64{2, 2} {
		t.Fatalf("gap did not survive restart: %v", afterA.Completeness.MissingRanges)
	}
	afterB, err := reopened.GetCapture(fixture.playerB.capture)
	if err != nil {
		t.Fatal(err)
	}
	if !afterB.Completeness.Closed || afterB.Status != string(CaptureClosed) {
		t.Fatalf("closed capture came back as %+v", afterB)
	}
	session, err := reopened.GetSession(fixture.session.PublicSession.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.CaptureIDs) != 2 {
		t.Fatalf("session capture ids after restart = %v", session.CaptureIDs)
	}

	// The persisted bytes, not just the index, must still be there.
	for _, entry := range reopened.captures[fixture.playerA.capture].Index {
		body, err := reopened.objects.Get(entry.ContentHash)
		if err != nil {
			t.Fatalf("object %s did not survive restart: %v", entry.ContentHash, err)
		}
		events, err := capture.DecodeCanonicalBatch(body)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(len(events)) != entry.EventCount {
			t.Fatalf("object %s holds %d events, index says %d", entry.ContentHash, len(events), entry.EventCount)
		}
	}

	// And a retry across the restart boundary is still recognised as a retry.
	replay, err := reopened.IngestCaptureBatch(fixture.playerA.lease, fixture.playerA.capture, "post-restart-retry", batchRequest(0, 1, `{"action":"move"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Duplicate {
		t.Fatal("a retry after restart was recorded as a new batch")
	}
}

func TestRestoreRefusesACaptureWhoseBytesAreGone(t *testing.T) {
	// A control plane that silently forgets one capture is worse than one that
	// will not start: the manifest would then under-report without saying so.
	directory := t.TempDir()
	service := openPersistentCaptureService(t, directory)
	fixture := newCaptureFixture(t, service, "missing-object")
	mustIngest(t, service, fixture.playerA, 0, 1)
	hash := service.captures[fixture.playerA.capture].Index[0].ContentHash

	digest := strings.TrimPrefix(hash, capture.HashPrefix)
	if err := os.Remove(filepath.Join(directory, "objects", digest[:2], digest)); err != nil {
		t.Fatal(err)
	}

	store, err := NewFileStateStore(filepath.Join(directory, "control-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPersistentService(testPersistentAllocator, store); err == nil {
		t.Fatal("the service started with a capture whose observations are gone")
	} else if !strings.Contains(err.Error(), "unresolvable object") {
		t.Fatalf("restore error = %v", err)
	}
}

func TestPreCaptureStateFileStillLoads(t *testing.T) {
	// Adding capture records to the snapshot must not strand an existing lab
	// state file, so the schema version deliberately did not move.
	directory := t.TempDir()
	path := filepath.Join(directory, "control-state.json")
	legacy := `{"schema_version":"` + stateSnapshotVersion + `","identities":{},"handles":{},"sessions":{},"enrollments":{},"placements":{},"executions":{},"evidence_sets":{},"identity_idempotency":{},"session_idempotency":{},"enrollment_idempotency":{},"evidence_idempotency":{}}` + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := OpenPersistentService(testPersistentAllocator, store)
	if err != nil {
		t.Fatalf("a pre-capture state file was rejected: %v", err)
	}
	if service.captures == nil {
		t.Fatal("restore left the capture map nil")
	}
	if _, err := service.GetCapture("0198c2c3-4d5e-7f60-8123-456789abcdef"); !hasCode(err, "CAPTURE_NOT_FOUND") {
		t.Fatalf("capture lookup after legacy load = %v", err)
	}
}
