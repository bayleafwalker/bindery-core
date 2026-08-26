package externalruntime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// The durability ceiling is a consequence of two properties held together.
// Nothing is ever deleted -- there is no delete in the non-test
// external-runtime sources -- and commitLocked rewrites the entire snapshot on
// every mutation. So the state file's size is a function of total history, and
// the cost of one mutation is a function of all the mutations before it.
//
// The tests below exist to replace "unbounded" with a number, because
// docs/decisions/operator-gates.md declares retention indefinite and owes the
// limit that qualifies the claim.

// growthSample is one measurement of the state file as history accumulates.
type growthSample struct {
	Sessions      int
	SnapshotBytes int
	BytesPerUnit  int
	CommitLatency time.Duration
}

// TestDurabilityCeilingIsMeasured records how the snapshot grows and what one
// mutation costs at each size, then extrapolates the history that fits under
// MaxStateSnapshotBytes. It asserts the shape of the growth rather than a
// specific machine's timings, which vary; the numbers are reported so a human
// can read them out of the test log.
func TestDurabilityCeilingIsMeasured(t *testing.T) {
	store, err := NewFileStateStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := OpenPersistentService(func(PlacementIntent) (PublicPlacement, error) {
		return *fixtureRelayPlacement(), nil
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	identity := mustIdentity(t, service, "ceiling-host")

	const step = 40
	const steps = 5
	samples := make([]growthSample, 0, steps)
	sessions := 0
	for sample := 0; sample < steps; sample++ {
		for n := 0; n < step; n++ {
			sessions++
			addSessionWithTwoPlayers(t, service, identity.AccountToken, sessions)
		}
		encoded, err := json.Marshal(service.snapshotLocked())
		if err != nil {
			t.Fatal(err)
		}
		// One more real mutation, timed end to end: deep copy, marshal, write,
		// fsync, rename, fsync the directory. This is what a client waits for.
		sessions++
		start := time.Now()
		addSessionWithTwoPlayers(t, service, identity.AccountToken, sessions)
		latency := time.Since(start)

		samples = append(samples, growthSample{
			Sessions:      sessions,
			SnapshotBytes: len(encoded),
			BytesPerUnit:  len(encoded) / sessions,
			CommitLatency: latency,
		})
	}

	t.Log("sessions | snapshot bytes | bytes/session | one commit")
	for _, sample := range samples {
		t.Logf("%8d | %14d | %13d | %10s", sample.Sessions, sample.SnapshotBytes, sample.BytesPerUnit, sample.CommitLatency)
	}

	first, last := samples[0], samples[len(samples)-1]
	if last.SnapshotBytes <= first.SnapshotBytes {
		t.Fatalf("snapshot did not grow with history: %d then %d bytes", first.SnapshotBytes, last.SnapshotBytes)
	}

	// Per-unit cost settles, which is what makes the extrapolation meaningful:
	// growth is linear in history, so the ceiling divides cleanly.
	drift := float64(last.BytesPerUnit-first.BytesPerUnit) / float64(first.BytesPerUnit)
	if drift > 0.25 || drift < -0.25 {
		t.Fatalf("bytes per session moved %.0f%% across the run (%d then %d); growth is not linear and the extrapolation below would be wrong",
			drift*100, first.BytesPerUnit, last.BytesPerUnit)
	}

	ceiling := MaxStateSnapshotBytes / last.BytesPerUnit
	t.Logf("measured: %d bytes per session of two players", last.BytesPerUnit)
	t.Logf("ceiling:  ~%d such sessions reach MaxStateSnapshotBytes (%d MiB), after which Load refuses the state file",
		ceiling, MaxStateSnapshotBytes>>20)
	t.Logf("note:     every mutation rewrites the whole snapshot, so the last session before the ceiling costs a %d MiB write",
		MaxStateSnapshotBytes>>20)

	if ceiling < 100 {
		t.Fatalf("extrapolated ceiling of %d sessions is implausibly low; the measurement is wrong", ceiling)
	}
}

// TestStateBeyondTheCeilingCannotBeRestored proves the ceiling is a hard stop
// rather than a slowdown, and records how it presents. Load wraps the file in
// an io.LimitReader, so an oversized snapshot is not reported as oversized: it
// is truncated mid-value and surfaces as a decode error. An operator meeting
// this for the first time sees corruption, not a capacity limit.
func TestStateBeyondTheCeilingCannotBeRestored(t *testing.T) {
	if testing.Short() {
		t.Skip("writes a state file larger than MaxStateSnapshotBytes")
	}
	path := filepath.Join(t.TempDir(), "state")
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := emptyServiceSnapshot()
	// Identities are the cheapest record to accumulate, so this reaches the
	// limit with the least work; the ceiling does not care which kind of
	// history filled it.
	for len(snapshot.Identities) < 200_000 {
		id := fmt.Sprintf("00000000-0000-7000-8000-%012d", len(snapshot.Identities))
		snapshot.Identities[id] = storedIdentity{
			Public: PublicIdentity{
				SchemaVersion: SchemaVersion, AccountID: id,
				Handle:    fmt.Sprintf("ceiling-filler-%d", len(snapshot.Identities)),
				ClaimedAt: time.Unix(0, 0).UTC(), Status: IdentityActive,
				PublicDataNoticeVersion: "1.0",
			},
			TokenVerifier: make([]byte, 32),
		}
		if len(snapshot.Identities)%20_000 == 0 {
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) > MaxStateSnapshotBytes {
				break
			}
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= MaxStateSnapshotBytes {
		t.Skipf("could not exceed the %d MiB limit with %d identities (%d bytes); raise the bound if records shrank",
			MaxStateSnapshotBytes>>20, len(snapshot.Identities), len(encoded))
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("save oversized state: %v", err)
	}
	t.Logf("wrote a %d MiB snapshot holding %d identities", len(encoded)>>20, len(snapshot.Identities))

	// Save accepted it. The state is now written and unreadable, which is the
	// property worth knowing: the ceiling is enforced on the way in, not out.
	if _, err := store.Load(); err == nil {
		t.Fatal("a snapshot past MaxStateSnapshotBytes was loaded; the ceiling is not where it is documented")
	} else {
		t.Logf("Load refused it as: %v", err)
	}
}

// TestCommitCostNearTheCeiling measures rather than extrapolates. It restores a
// state file close to MaxStateSnapshotBytes and times one ordinary mutation, so
// the cost of a single client request at the far end of the service's life is
// an observation instead of a projection from small samples.
func TestCommitCostNearTheCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a state file close to MaxStateSnapshotBytes")
	}
	// Deliberately under the limit: the point is a service that still works.
	const target = MaxStateSnapshotBytes / 2

	path := filepath.Join(t.TempDir(), "state")
	store, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := emptyServiceSnapshot()
	for {
		index := len(snapshot.Identities)
		id := fmt.Sprintf("00000000-0000-7000-8000-%012d", index)
		handle := fmt.Sprintf("ceiling-filler-%d", index)
		snapshot.Identities[id] = storedIdentity{
			Public: PublicIdentity{
				SchemaVersion: SchemaVersion, AccountID: id, Handle: handle,
				ClaimedAt: time.Unix(0, 0).UTC(), Status: IdentityActive,
				PublicDataNoticeVersion: "1.0",
			},
			TokenVerifier: make([]byte, 32),
		}
		snapshot.Handles[handle] = id
		if index%10_000 == 0 {
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded) >= target {
				break
			}
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	service, err := OpenPersistentService(nil, store)
	if err != nil {
		t.Fatalf("restore %d MiB of state: %v", len(encoded)>>20, err)
	}
	restore := time.Since(start)

	start = time.Now()
	if _, err := service.CreateIdentity(CreateIdentityRequest{Handle: "one-more-client"}, "ceiling-one-more"); err != nil {
		t.Fatalf("mutate at %d MiB: %v", len(encoded)>>20, err)
	}
	mutation := time.Since(start)

	t.Logf("state:    %d MiB holding %d identities (half the %d MiB ceiling)", len(encoded)>>20, len(snapshot.Identities), MaxStateSnapshotBytes>>20)
	t.Logf("restore:  %s to start the service on it", restore)
	t.Logf("mutation: %s for one identity claim, because the whole snapshot is rewritten", mutation)

	if mutation < time.Millisecond {
		t.Fatalf("one mutation over %d MiB of state took %s; the snapshot cannot have been rewritten", len(encoded)>>20, mutation)
	}
}

func addSessionWithTwoPlayers(t *testing.T, service *Service, accountToken string, n int) {
	t.Helper()
	created, err := service.CreateSession(accountToken, fmt.Sprintf("ceiling-session-%d", n), testSessionRequest())
	if err != nil {
		t.Fatalf("create session %d: %v", n, err)
	}
	sessionID := created.PublicSession.SessionID
	mustEnroll(t, service, accountToken, created.SessionJoinCredential, sessionID, fmt.Sprintf("ceiling-%d-a", n), ClientPlayer)
	mustEnroll(t, service, accountToken, created.SessionJoinCredential, sessionID, fmt.Sprintf("ceiling-%d-b", n), ClientPlayer)
}
