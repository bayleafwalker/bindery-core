package openttd_test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	openttd "github.com/bayleafwalker/bindery-core/adapters/bindery-openttd-runtime"
)

const buildRevision = "19db3c2f00d3b6c2126e6fadd1f911f42521d3b8"

// The sha256 the OpenTTD project publishes for the Windows x64 build of
// release 15.3, from its own release manifest. It is the same game and the
// same release as the Linux binary this run drives, and the two play together
// over the network -- which is the whole point of using it here.
const publishedWindowsBuildHash = "sha256:61c0a6a43d81008c7ff4330fb56351bbf66a980cdad041f1d0e08b51f2eeb34c"

// broker is the real control plane, run as its own process so the restart
// drill restarts something rather than reconstructing it in memory.
type broker struct {
	binary    string
	address   string
	statePath string
	command   *exec.Cmd
	log       *strings.Builder
	t         *testing.T
}

func startBroker(t *testing.T) *broker {
	t.Helper()
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "bindery-external-runtime")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/bindery-external-runtime")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build control plane: %v\n%s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()

	b := &broker{binary: binary, address: address, statePath: filepath.Join(workspace, "state"), t: t}
	b.start()
	t.Cleanup(b.stop)
	return b
}

func (b *broker) start() {
	b.t.Helper()
	b.log = &strings.Builder{}
	command := exec.Command(b.binary)
	command.Env = append(os.Environ(),
		"BINDERY_EXTERNAL_RUNTIME_ADDR="+b.address,
		"BINDERY_STATE_PATH="+b.statePath,
		"BINDERY_RELAY_ENDPOINT=192.168.122.1:50001",
		"BINDERY_BUILD_REVISION="+buildRevision,
	)
	command.Stdout = b.log
	command.Stderr = b.log
	if err := command.Start(); err != nil {
		b.t.Fatalf("start control plane: %v", err)
	}
	b.command = command
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", b.address, 200*time.Millisecond)
		if err == nil {
			connection.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	b.t.Fatalf("control plane did not listen on %s:\n%s", b.address, b.log.String())
}

func (b *broker) stop() {
	if b.command == nil || b.command.Process == nil {
		return
	}
	_ = b.command.Process.Kill()
	_ = b.command.Wait()
	b.command = nil
}

func (b *broker) restart() {
	b.stop()
	b.start()
}

// dumpDivergence prints both recordings side by side from the first place they
// differ, which is the only useful thing to look at when two observers of the
// same game disagree.
func dumpDivergence(t *testing.T, left, right []openttd.Observation) {
	t.Helper()
	limit := len(left)
	if len(right) > limit {
		limit = len(right)
	}
	describe := func(stream []openttd.Observation, index int) string {
		if index >= len(stream) {
			return "(nothing)"
		}
		return stream[index].Kind + " " + string(stream[index].Payload)
	}
	first := -1
	for index := 0; index < limit; index++ {
		if describe(left, index) != describe(right, index) {
			first = index
			break
		}
	}
	if first < 0 {
		t.Logf("the recordings share a common prefix of %d observations", limit)
		return
	}
	for index := first - 2; index < first+6 && index < limit; index++ {
		if index < 0 {
			continue
		}
		marker := " "
		if describe(left, index) != describe(right, index) {
			marker = "!"
		}
		t.Logf("%s %3d a: %s", marker, index, describe(left, index))
		t.Logf("%s %3d b: %s", marker, index, describe(right, index))
	}
}

func requireGame(t *testing.T) string {
	t.Helper()
	binary, err := openttd.LocateBinary()
	if err != nil {
		t.Skipf("no OpenTTD binary: %v; run hack/fetch-openttd.sh and set %s", err, openttd.BinaryEnvVar)
	}
	return binary
}

// TestERH007ThirdPartyRuntime runs Bindery's contracts against OpenTTD: a real
// multiplayer game, in its own processes, which has never heard of this project
// and offers no way to teach it. The dedicated-runtime run of 2026-08-26 met
// ERH-007's acceptance criteria but recorded its own limit -- a second adapter
// written by someone who could read the contracts is a lower bound on what a
// third-party runtime hits. This is the run that tests the bound.
//
// The shape of the run: an unmodified OpenTTD dedicated server generates a map
// from a seed; two admin-network applications observe it independently; real
// game clients join over the network and start companies; both observers file
// what they saw on their own capture streams; the broker is killed and
// restarted; and the evidence is reconciled with gate controls.
func TestERH007ThirdPartyRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a game server, game clients, and the control plane as separate processes")
	}
	binary := requireGame(t)
	const players = 3

	workspace := t.TempDir()
	game, err := openttd.StartServer(binary, workspace, "bindery-admin-password", players+2, 8)
	if err != nil {
		t.Fatalf("start the game server: %v", err)
	}
	defer game.Stop()
	t.Logf("openttd dedicated server: game %s, admin %s", game.GameAddress(), game.AdminAddress())

	// Two independent admin applications. They share no state, hold separate
	// sockets, and decode separately; neither can see what the other received.
	observers := make([]*openttd.Observer, 0, 2)
	var welcome openttd.Welcome
	for _, name := range []string{"bindery-observer-a", "bindery-observer-b"} {
		admin, gameWelcome, err := openttd.DialAdmin(name, game.AdminAddress(), game.Password)
		if err != nil {
			t.Fatalf("connect admin %s: %v\n%s", name, err, game.LogTail())
		}
		defer admin.Close()
		welcome = gameWelcome
		observer := openttd.NewObserver(admin)
		if err := observer.Subscribe(); err != nil {
			t.Fatalf("subscribe %s: %v", name, err)
		}
		observers = append(observers, observer)
	}
	t.Logf("game reports: %s revision %s, dedicated=%v, %dx%d map from seed %d",
		welcome.ServerName, welcome.Revision, welcome.Dedicated, welcome.MapWidth, welcome.MapHeight, welcome.Seed)

	// Both observers stop on the same logical event: the moment the last of the
	// game's clients has left. Nothing time-driven is subscribed, so this is a
	// point in the game's history rather than a point in wall-clock time.
	departures := func() func(openttd.Observation) bool {
		seen := 0
		return func(observation openttd.Observation) bool {
			if observation.Kind == "openttd.client-quit" || observation.Kind == "openttd.client-error" {
				seen++
			}
			return seen == players
		}
	}
	// Both recordings begin at the same fact: a throwaway admin application
	// connects and leaves, and the game announces it to everyone already
	// watching. Without it observer A records observer B's arrival and B does
	// not, and the two streams differ by exactly that one event.
	for _, observer := range observers {
		observer.Start(openttd.RunMarker("bindery-run-marker"), departures())
	}
	if err := openttd.MarkRun("bindery-run-marker", game.AdminAddress(), game.Password); err != nil {
		t.Fatalf("mark the start of the run: %v", err)
	}

	b := startBroker(t)
	runtime, err := openttd.NewRuntime(openttd.NewClient("http://"+b.address), binary)
	if err != nil {
		t.Fatalf("hash the game binary: %v", err)
	}
	if err := runtime.ClaimIdentity("openttd-operator"); err != nil {
		t.Fatalf("claim identity: %v", err)
	}
	if err := runtime.CreateSession(welcome, players, len(observers)); err != nil {
		t.Fatalf("create a session for a game with no mod and no map: %v", err)
	}
	t.Logf("session %s execution %s, game_hash %s", runtime.SessionID, runtime.ExecutionID, runtime.GameHash)
	for _, observer := range []string{"bindery-observer-a", "bindery-observer-b"} {
		if err := runtime.EnrollObserver(observer); err != nil {
			t.Fatalf("enroll observer %s: %v", observer, err)
		}
	}

	// FINDING, and one only a cross-platform game can surface: enrollment
	// requires every participant to run a byte-identical build. OpenTTD's
	// Windows, macOS and Linux builds of one release play together; Bindery
	// refuses the second of them. Red Alert 2 ships one platform's executable,
	// so nothing in the RA2 slice could have found this.
	code, err := runtime.EnrollExpectingRefusal("bindery-player-windows", publishedWindowsBuildHash)
	if err != nil {
		t.Fatalf("offering another platform's build of the same game: %v", err)
	}
	if code != "COMPATIBILITY_MISMATCH" {
		t.Fatalf("refusal code = %q, want COMPATIBILITY_MISMATCH", code)
	}
	t.Logf("finding: a client running the published Windows build of the same release is refused (%s)", code)

	// Real game clients, joining over the network. Each creates a company on
	// arrival, which is what an OpenTTD client does when it joins with no
	// company selected.
	processes := make([]*openttd.PlayerProcess, 0, players)
	defer func() {
		for _, process := range processes {
			process.Stop()
		}
	}()
	for index := 0; index < players; index++ {
		name := "bindery-player-" + string(rune('a'+index))
		if err := runtime.EnrollPlayer(name); err != nil {
			t.Fatalf("enroll player %s: %v", name, err)
		}
		process, err := openttd.StartPlayer(binary, filepath.Join(workspace, name), name, game.GameAddress())
		if err != nil {
			t.Fatalf("start game client %s: %v", name, err)
		}
		processes = append(processes, process)
		// The game pauses while a client connects, so joins are staggered
		// rather than raced; this is the game's pacing, not a fudge factor.
		time.Sleep(4 * time.Second)
	}

	// Let the joined game run, then end it the way the run intends to.
	time.Sleep(5 * time.Second)
	for _, process := range processes {
		process.Stop()
	}

	recordings := make([][]openttd.Observation, len(observers))
	for index, observer := range observers {
		recorded, err := observer.Wait(60 * time.Second)
		if err != nil {
			t.Fatalf("observer %d: %v\ngame server log:\n%s", index, err, game.LogTail())
		}
		recordings[index] = recorded
	}
	if len(recordings[0]) == 0 {
		t.Fatal("the game published nothing over the admin network")
	}
	t.Logf("observer a recorded %d observations, observer b recorded %d", len(recordings[0]), len(recordings[1]))

	// A question this runtime can ask and the dedicated one could not: do two
	// independent observers of a game nobody here wrote actually see the same
	// thing? The event ids are derived from content, so if they do, the two
	// streams are identical event by event.
	if len(recordings[0]) != len(recordings[1]) {
		t.Errorf("the two admin connections disagreed on how much happened: %d against %d",
			len(recordings[0]), len(recordings[1]))
		dumpDivergence(t, recordings[0], recordings[1])
		t.FailNow()
	}
	for index := range recordings[0] {
		left, right := recordings[0][index], recordings[1][index]
		if left.Kind != right.Kind || string(left.Payload) != string(right.Payload) {
			t.Fatalf("observation %d differed between admin connections:\n a: %s %s\n b: %s %s",
				index, left.Kind, left.Payload, right.Kind, right.Payload)
		}
		if openttd.EventID(index, left) != openttd.EventID(index, right) {
			t.Fatalf("observation %d minted different event ids for the same fact", index)
		}
	}
	t.Logf("the two admin connections agreed on every one of the %d observations, event id included", len(recordings[0]))

	for index, observer := range runtime.Observers {
		published, err := runtime.Publish(observer, recordings[index], 16)
		if err != nil {
			t.Fatalf("publish %s: %v", observer.Name, err)
		}
		if published != len(recordings[index]) {
			t.Fatalf("published %d of %d observations for %s", published, len(recordings[index]), observer.Name)
		}
		closed, err := runtime.CloseCapture(observer, uint64(published-1), "the game's clients have all left")
		if err != nil {
			t.Fatalf("close %s: %v", observer.Name, err)
		}
		if closed.Completeness == nil || len(closed.Completeness.MissingRanges) != 0 {
			t.Fatalf("observer %s did not close complete: %+v", observer.Name, closed.Completeness)
		}
	}

	// Negative controls: the game's own clients hold streams they cannot write
	// to, because OpenTTD's clients are told what happened rather than
	// witnessing it.
	empty, err := runtime.ClosePlayerCaptures()
	if err != nil {
		t.Fatalf("close the game clients' streams: %v", err)
	}
	if len(empty) != players {
		t.Fatalf("closed %d client streams, want %d", len(empty), players)
	}

	// The restart drill sits between production and reconciliation: everything
	// the evidence set is about has to survive the broker dying. The game keeps
	// running throughout, which is the honest arrangement -- a game server does
	// not stop because a control plane fell over.
	b.restart()

	afterRestart, err := runtime.GetCapture(runtime.Observers[0].CaptureID)
	if err != nil {
		t.Fatalf("read a capture after the restart: %v", err)
	}
	if afterRestart.Completeness == nil || afterRestart.Completeness.EventCount != uint64(len(recordings[0])) {
		t.Fatalf("the restart lost observations: %+v", afterRestart.Completeness)
	}
	if !afterRestart.Completeness.Closed || afterRestart.Status != "closed" {
		t.Fatalf("the restart lost the close: %+v", afterRestart)
	}
	t.Logf("restart drill: %d observations and the close survived a killed broker", afterRestart.Completeness.EventCount)

	evidence, err := runtime.Reconcile("exact-count", "openttd-exact-count")
	if err != nil {
		t.Fatalf("reconcile after the restart: %v", err)
	}
	if len(evidence.Observations) != len(observers) {
		t.Fatalf("observations = %d, want one per admin application", len(evidence.Observations))
	}
	for _, observation := range evidence.Observations {
		if observation.Source != "broker-derived" {
			t.Fatalf("observation source = %q, want broker-derived", observation.Source)
		}
		if observation.EventCount != uint64(len(recordings[0])) {
			t.Fatalf("stream %s counted %d, the observer recorded %d",
				observation.StreamID, observation.EventCount, len(recordings[0]))
		}
	}
	if evidence.Reconciliation.Outcome != "consistent" {
		t.Fatalf("two admin connections that saw the same events reconciled as %s", evidence.Reconciliation.Outcome)
	}
	if evidence.Reconciliation.ComparedObservers != len(observers) {
		t.Fatalf("compared observers = %d, want %d", evidence.Reconciliation.ComparedObservers, len(observers))
	}

	passed, failed := 0, 0
	for _, gate := range evidence.GateResults {
		if !gate.CalibrationValid {
			t.Fatalf("a gate ran without valid calibration: %+v", gate)
		}
		if gate.GateID != "bindery.capture.completeness" {
			t.Fatalf("unexpected gate %q", gate.GateID)
		}
		switch gate.Status {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		default:
			t.Fatalf("gate status = %q on %s", gate.Status, gate.CaptureID)
		}
	}
	if passed != len(observers) || failed != players {
		t.Fatalf("gate outcomes: %d pass, %d fail; want %d and %d", passed, failed, len(observers), players)
	}
	t.Logf("gate controls: %d PASS on the observing streams, %d FAIL on the game clients' empty ones", passed, failed)

	// FINDING, confirmed against a game this repository does not control:
	// ordered-hash reconciliation cannot report agreement. These two streams
	// are the same events, in the same order, with the same content-derived
	// event ids -- and they still hash differently, because the canonical
	// encoding binds producer_client_id, capture_id and received_at into every
	// event. The dedicated runtime found this; a real game confirms it is not
	// an artefact of a runtime written alongside the contracts.
	status, orderedHash, failure, err := runtime.ReconcileRaw("ordered-hash", "openttd-ordered-hash")
	if err != nil {
		t.Fatalf("ordered-hash reconciliation: %v", err)
	}
	if status != 201 {
		t.Fatalf("ordered-hash reconciliation was refused (%d %s); the finding this pins has changed shape", status, failure.Code)
	}
	if orderedHash.Reconciliation.Outcome != "inconsistent" {
		t.Fatalf("ordered-hash now reports %q for identical streams -- the finding is fixed and this test must be retired",
			orderedHash.Reconciliation.Outcome)
	}
	if len(orderedHash.Reconciliation.DistinctHashes) != len(observers) {
		t.Fatalf("distinct hashes = %d, want one per producer", len(orderedHash.Reconciliation.DistinctHashes))
	}
	t.Logf("finding confirmed on a third-party game: identical observations, %d distinct ordered hashes, outcome %s",
		len(orderedHash.Reconciliation.DistinctHashes), orderedHash.Reconciliation.Outcome)

	// Idempotency survives the restart.
	replay, err := runtime.Reconcile("exact-count", "openttd-exact-count")
	if err != nil {
		t.Fatalf("replay the evidence set: %v", err)
	}
	if replay.EvidenceSetID != evidence.EvidenceSetID {
		t.Fatalf("the replay minted a second evidence set: %s then %s", evidence.EvidenceSetID, replay.EvidenceSetID)
	}
}
