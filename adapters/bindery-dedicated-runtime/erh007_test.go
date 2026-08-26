package dedicated_test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dedicated "github.com/bayleafwalker/bindery-core/adapters/bindery-dedicated-runtime"
)

const buildRevision = "738e9f752ad1d892bdad8852cd4bd4e29182c16a"

// broker is the real control plane, run as its own process so the restart
// drill below restarts something rather than reconstructing it in memory.
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

// restart is the ERH-007 restart drill: the process is killed outright, not
// asked politely, and brought back on the same state file.
func (b *broker) restart() {
	b.stop()
	b.start()
}

// TestERH007SecondRuntime runs a Linux-native, server-authoritative runtime
// through the same session, placement, execution, observation and evidence
// contracts the RA2 adapter uses, with only the adapter swapped.
//
// What it establishes and what it does not is stated in
// docs/assessments/2026-08-26-erh-007-second-runtime.md. In short: it is a
// second adapter against a second integration mechanism, not a second
// commercial game, so it cannot surface the constraints of a codebase nobody
// here controls.
func TestERH007SecondRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the control plane as a separate process")
	}
	const (
		players = 10 // more than the 8 the control plane used to allow
		ticks   = 8
		convoys = 4
	)
	b := startBroker(t)
	client := dedicated.NewClient("http://" + b.address)
	server := dedicated.NewServer(client, dedicated.NewWorld(0x5eed, 6, convoys))

	if err := server.ClaimIdentity("dedicated-operator"); err != nil {
		t.Fatalf("claim identity: %v", err)
	}

	// Session contract: no mod, no map, and a seat count no RA2 session has.
	if err := server.CreateSession(players, 2); err != nil {
		t.Fatalf("create session without mod or map: %v", err)
	}
	t.Logf("session %s execution %s", server.SessionID, server.ExecutionID)

	if err := server.EnrollServer(); err != nil {
		t.Fatalf("enroll the simulation owner: %v", err)
	}
	if err := server.EnrollPlayers(players); err != nil {
		t.Fatalf("enroll players: %v", err)
	}

	produced, err := server.Simulate(ticks, 7)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if produced != ticks*convoys {
		t.Fatalf("produced %d events, want %d", produced, ticks*convoys)
	}
	t.Logf("server produced %d observations; %d player clients produced none", produced, players)

	// Positive control: the authoritative stream closes complete.
	closed, err := server.CloseServerCapture(uint64(produced - 1))
	if err != nil {
		t.Fatalf("close server capture: %v", err)
	}
	if closed.Completeness == nil || len(closed.Completeness.MissingRanges) != 0 {
		t.Fatalf("authoritative stream did not close complete: %+v", closed.Completeness)
	}

	// Negative control: streams whose producers legitimately observed nothing.
	emptyStreams, err := server.ClosePlayerCaptures()
	if err != nil {
		t.Fatalf("close player captures: %v", err)
	}
	if len(emptyStreams) != players {
		t.Fatalf("closed %d player captures, want %d", len(emptyStreams), players)
	}

	// FINDING, pinned rather than worked around: with one authority there is
	// nothing to reconcile against, and the contract has no way to publish an
	// execution's observations without also cross-checking them. A runtime
	// whose server is the only honest witness therefore produces no evidence
	// set at all. See the assessment.
	code, err := server.ReconcileExpectingRefusal("exact-count", "dedicated-single-authority")
	if err != nil {
		t.Fatalf("single-authority reconciliation was not refused as expected: %v", err)
	}
	if code != "RECONCILIATION_INVALID" {
		t.Fatalf("single-authority refusal code = %q", code)
	}
	t.Logf("finding: a single-authority execution cannot produce an evidence set (%s)", code)

	// The server-authoritative answer to "two independent observers" is a hot
	// standby running the same deterministic world, which is the divergence
	// this shape of runtime actually needs to detect.
	if err := server.EnrollReplica(); err != nil {
		t.Fatalf("enroll replica: %v", err)
	}
	replicaProduced, err := server.SimulateReplica(dedicated.NewWorld(0x5eed, 6, convoys), ticks, 7)
	if err != nil {
		t.Fatalf("simulate replica: %v", err)
	}
	if replicaProduced != produced {
		t.Fatalf("replica produced %d events, primary produced %d; the world is not deterministic", replicaProduced, produced)
	}
	if _, err := server.CloseReplicaCapture(uint64(replicaProduced - 1)); err != nil {
		t.Fatalf("close replica capture: %v", err)
	}

	// The restart drill happens between production and reconciliation, which
	// is the interesting place for it: everything the evidence set is about
	// has to have survived a process death.
	b.restart()

	afterRestart, err := server.GetCapture(server.CaptureID)
	if err != nil {
		t.Fatalf("read the authoritative capture after restart: %v", err)
	}
	if afterRestart.Completeness == nil || afterRestart.Completeness.EventCount != uint64(produced) {
		t.Fatalf("restart lost observations: %+v", afterRestart.Completeness)
	}
	if !afterRestart.Completeness.Closed || afterRestart.Status != "closed" {
		t.Fatalf("restart lost the close: %+v", afterRestart)
	}
	t.Logf("restart drill: %d observations and the close survived", afterRestart.Completeness.EventCount)

	evidence, err := server.ReconcileEvidence("exact-count", "dedicated-evidence")
	if err != nil {
		t.Fatalf("reconcile evidence after restart: %v", err)
	}

	// Evidence contract: only streams that passed the gate contribute, so the
	// primary and the replica are compared and the ten empty seats are not.
	if len(evidence.Observations) != 2 {
		t.Fatalf("observations = %d, want the primary and the replica", len(evidence.Observations))
	}
	byStream := map[string]uint64{}
	for _, observation := range evidence.Observations {
		if observation.Source != "broker-derived" {
			t.Fatalf("observation source = %q, want broker-derived", observation.Source)
		}
		if observation.OrderedHash == "" {
			t.Fatal("observation carried no ordered hash")
		}
		byStream[observation.StreamID] = observation.EventCount
	}
	if byStream[server.CaptureID] != uint64(produced) || byStream[server.ReplicaCap] != uint64(produced) {
		t.Fatalf("counts = %v, want %d on both authoritative streams", byStream, produced)
	}
	if evidence.Reconciliation.Outcome != "consistent" {
		t.Fatalf("primary and replica disagreed on a deterministic world: %s", evidence.Reconciliation.Outcome)
	}

	// Gate controls: exactly one PASS, and every empty stream refused.
	passed, failed := 0, 0
	for _, gate := range evidence.GateResults {
		if !gate.CalibrationValid {
			t.Fatalf("gate ran without valid calibration: %+v", gate)
		}
		if gate.GateID != "bindery.capture.completeness" {
			t.Fatalf("unexpected gate %q", gate.GateID)
		}
		switch gate.Status {
		case "PASS":
			passed++
			if gate.CaptureID != server.CaptureID && gate.CaptureID != server.ReplicaCap {
				t.Fatalf("a stream other than the authoritative ones passed: %s", gate.CaptureID)
			}
		case "FAIL":
			failed++
		default:
			t.Fatalf("gate status = %q on %s", gate.Status, gate.CaptureID)
		}
	}
	if passed != 2 || failed != players {
		t.Fatalf("gate outcomes: %d pass, %d fail; want 2 and %d", passed, failed, players)
	}
	t.Logf("gate controls: 2 PASS on the authoritative streams, %d FAIL on streams that produced nothing", failed)

	if evidence.Reconciliation.ComparedObservers != 2 {
		t.Fatalf("compared observers = %d, want 2", evidence.Reconciliation.ComparedObservers)
	}
	t.Logf("reconciliation: %s over %d observer, outcome %s",
		evidence.Reconciliation.Method, evidence.Reconciliation.ComparedObservers, evidence.Reconciliation.Outcome)

	// Idempotency survives the restart too.
	replay, err := server.ReconcileEvidence("exact-count", "dedicated-evidence")
	if err != nil {
		t.Fatalf("replay evidence set: %v", err)
	}
	if replay.EvidenceSetID != evidence.EvidenceSetID {
		t.Fatalf("replay minted a second evidence set: %s then %s", evidence.EvidenceSetID, replay.EvidenceSetID)
	}
}
