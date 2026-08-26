package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestE2ESmoke_BinderySample(t *testing.T) {
	if os.Getenv("BINDERY_E2E") == "" {
		t.Skip("set BINDERY_E2E=1 to run Kind-based smoke test")
	}

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not found in PATH")
	}

	repoRoot := findRepoRoot(t)
	kindBin := "kind"
	if _, err := exec.LookPath("kind"); err != nil {
		fallback := filepath.Join(repoRoot, ".tools", "kind")
		if info, statErr := os.Stat(fallback); statErr == nil && info.Mode()&0o111 != 0 {
			kindBin = fallback
		} else {
			t.Skip("kind not found in PATH (and .tools/kind not usable)")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	clusterName := fmt.Sprintf("bindery-e2e-%d", time.Now().UnixNano())
	t.Logf("cluster=%s", clusterName)

	// Keep the cluster entirely inside this test's own kubeconfig. Without
	// --kubeconfig, `kind create` writes into the caller's real kubeconfig and
	// switches their current-context, so running the suite silently repoints
	// whatever cluster they were working against.
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")

	// Always attempt cleanup.
	t.Cleanup(func() {
		// Deliberately not the test's ctx: its `defer cancel()` fires when the
		// test function returns, which is BEFORE cleanups run, so deleting the
		// cluster with it is cancelled instantly and leaks the cluster. The
		// previous code also discarded the error, so the leak was silent.
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancelCleanup()

		if out, err := runOut(cleanupCtx, repoRoot, nil, kindBin,
			"delete", "cluster", "--name", clusterName, "--kubeconfig", kubeconfigPath); err != nil {
			t.Logf("kind delete cluster %s failed: %v\n%s", clusterName, err, out)
		}
	})

	// Create cluster.
	runOrFail(t, ctx, repoRoot, nil, kindBin, "create", "cluster",
		"--name", clusterName, "--kubeconfig", kubeconfigPath, "--wait", "60s")

	kubeEnv := append(os.Environ(), "KUBECONFIG="+kubeconfigPath)

	// Install CRDs.
	runOrFail(t, ctx, repoRoot, kubeEnv, "kubectl", "apply", "-f", "k8s/crds/")

	// Build + load demo images into kind.
	runOrFail(t, ctx, repoRoot, kubeEnv, "bash", "examples/booklet-bindery-sample/dev/build-images.sh", clusterName)

	// Start controller manager (out-of-cluster) against the kind cluster.
	//
	// Build the binary and exec it directly rather than using `go run .`.
	// CommandContext kills only the process it started, so under `go run` the
	// compiled manager is a grandchild that survives cancellation, keeps the
	// inherited stdout/stderr pipe open, and leaves Cmd.Wait blocked forever in
	// awaitGoroutines -- hanging cleanup and leaking a manager per run.
	managerBin := filepath.Join(t.TempDir(), "bindery-manager")
	runOrFail(t, ctx, repoRoot, nil, "go", "build", "-o", managerBin, ".")

	managerCtx, managerCancel := context.WithCancel(ctx)
	defer managerCancel()

	managerCmd := exec.CommandContext(managerCtx, managerBin, "--metrics-bind-address=0", "--health-probe-bind-address=0")
	managerCmd.Dir = repoRoot
	managerCmd.Env = kubeEnv
	var managerOut bytes.Buffer
	managerCmd.Stdout = &managerOut
	managerCmd.Stderr = &managerOut
	if err := managerCmd.Start(); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		managerCancel()
		// Bound the wait regardless: a cleanup that can block forever turns any
		// failure into a timeout with no diagnostics.
		done := make(chan struct{})
		go func() {
			_ = managerCmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Logf("manager did not exit within 30s of cancellation; killing")
			if managerCmd.Process != nil {
				_ = managerCmd.Process.Kill()
			}
		}
	})

	// Apply sample game resources.
	runOrFail(t, ctx, repoRoot, kubeEnv, "bash", "examples/booklet-bindery-sample/dev/apply.sh")

	// Wait for world to report bindings resolved and runtime ready.
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "wait",
		"--for=condition=BindingsResolved=True",
		"worldinstance/bindery-sample-world",
		"--timeout=180s",
	)
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "wait",
		"--for=condition=RuntimeReady=True",
		"worldinstance/bindery-sample-world",
		"--timeout=240s",
	)

	// Ensure web deployment is up.
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "wait",
		"--for=condition=Available=True",
		"deployment",
		"-l", "bindery.platform/module=core-web-client",
		"--timeout=240s",
	)

	// The web client Deployment is created before its physics.engine binding
	// resolves, carrying only its own BINDERY_SAMPLE_* env. Once the provider
	// Service exists the orchestrator injects BINDERY_CAPABILITY_* into the pod
	// template, which rolls the Deployment a second time. Becoming Available
	// therefore does not mean the pod is settled: a port-forward opened against
	// the first generation dies mid-poll with "lost connection to pod".
	//
	// Wait for the injected endpoint to appear, then for that generation to
	// finish rolling out, before forwarding to anything.
	webDeploy := strings.TrimSpace(runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "get", "deployment",
		"-l", "bindery.platform/module=core-web-client",
		"-o", "jsonpath={.items[0].metadata.name}",
	))
	if webDeploy == "" {
		t.Fatal("web deployment not found")
	}
	waitForCapabilityEnv(t, ctx, repoRoot, kubeEnv, webDeploy, "BINDERY_CAPABILITY_PHYSICS_ENGINE_ENDPOINT", 240*time.Second)
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "rollout", "status",
		"deployment/"+webDeploy, "--timeout=240s",
	)

	// Find web service and port-forward to localhost.
	svc := strings.TrimSpace(runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "get", "svc",
		"-l", "bindery.platform/module=core-web-client",
		"-o", "jsonpath={.items[0].metadata.name}",
	))
	if svc == "" {
		logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "svc", "-l", "bindery.platform/module=core-web-client")
		t.Fatal("web service not found")
	}

	// Even settled, a port-forward is a single long-lived connection through a
	// pod that can be replaced at any time. Treat it as disposable: the poller
	// restarts it whenever it drops rather than failing the run.
	pf := newPortForward(t, ctx, repoRoot, kubeEnv, svc)
	t.Cleanup(pf.stop)

	httpClient := &http.Client{Timeout: 2 * time.Second}

	// Poll the web endpoint until the simulation advances.
	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
poll:
	for {
		if time.Now().After(deadline) {
			t.Logf("manager output:\n%s", managerOut.String())
			t.Logf("port-forward output:\n%s", pf.output())
			if lastErr != nil {
				t.Logf("last poll error: %v", lastErr)
			}
			logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "worldinstances,worldshards,capabilitybindings,pods,svc")
			logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "logs", "-l", "bindery.platform/module=core-web-client", "--tail=200")
			logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "logs", "-l", "bindery.platform/module=core-physics-engine", "--tail=200")
			t.Fatalf("timeout waiting for /api/state to report tick>0 (%s)", pf.url())
		}

		// Re-establish the forward if it dropped since the last attempt.
		pf.ensure()

		resp, err := httpClient.Get(pf.url())
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else {
				var state webState
				if err := json.Unmarshal(body, &state); err != nil {
					lastErr = fmt.Errorf("decode %q: %w", string(body), err)
				} else {
					planets, ships := 0, 0
					for _, e := range state.Entities {
						switch e.Kind {
						case "planet":
							planets++
						case "ship":
							ships++
						}
					}
					if state.Error == "" && state.Tick > 0 && planets >= 2 && ships > 0 {
						t.Logf("simulation advancing: tick=%d planets=%d ships=%d", state.Tick, planets, ships)
						break poll
					}
					lastErr = fmt.Errorf("state not ready: tick=%d planets=%d ships=%d err=%q",
						state.Tick, planets, ships, state.Error)
				}
			}
		}

		time.Sleep(3 * time.Second)
	}

	// With a single shard proven healthy, exercise the sharding path end to end:
	// ShardAutoscaler -> WorldInstance.spec.shardCount -> WorldShard objects ->
	// per-shard CapabilityBindings -> per-shard runtime Deployments.
	assertShardingScalesWorld(t, ctx, repoRoot, kubeEnv)
}

// assertShardingScalesWorld drives the world wide and back through the
// ShardAutoscaler, asserting each stage of the chain reacts.
//
// The autoscaler is left with no spec.metrics, so the controller never contacts
// the metrics API and the shard count is decided purely by the min/max clamp.
// That keeps this deterministic on a Kind cluster, which has no metrics-server.
func assertShardingScalesWorld(t *testing.T, ctx context.Context, repoRoot string, kubeEnv []string) {
	t.Helper()

	const world = "bindery-sample-world"
	const autoscaler = "bindery-sample-world-autoscaler"

	// Scale out: raising minShards to 2 forces the clamp upward.
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "patch", "shardautoscaler", autoscaler,
		"--type=merge", "-p", `{"spec":{"minShards":2}}`,
	)

	waitForShardCount(t, ctx, repoRoot, kubeEnv, world, 2, 2*time.Minute)
	waitForShardObjects(t, ctx, repoRoot, kubeEnv, world, 2, 2*time.Minute)

	// The new shard must produce real runtime workloads, not just objects.
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "wait",
		"--for=condition=Available=True", "deployment",
		"-l", "bindery.platform/shard=1",
		"--timeout=240s",
	)
	t.Logf("shard 1 runtime deployments available")

	// The autoscaler should converge on reporting what it did. status.current is
	// written from the shard count observed at the START of a reconcile, and the
	// world is updated after, so the pass that scales 1 -> 2 records "1/2". Only
	// the following pass (RequeueAfter is 30s) observes the settled "2/2".
	waitForAutoscalerStatus(t, ctx, repoRoot, kubeEnv, autoscaler, "2/2", 2*time.Minute)

	// Scale back in: shard 1 must be reclaimed, not merely orphaned.
	//
	// Lowering minShards alone would NOT do this. The controller clamps as
	//
	//	if desired < min { desired = min }   // raises only
	//	if desired > max { desired = max }   // lowers only
	//
	// and with no metrics the calculated count is just the current count, so
	// min is a floor that exerts no downward pull. maxShards is the clamp that
	// can actually reduce the world, so drop it to 1.
	runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "patch", "shardautoscaler", autoscaler,
		"--type=merge", "-p", `{"spec":{"minShards":1,"maxShards":1}}`,
	)

	waitForShardCount(t, ctx, repoRoot, kubeEnv, world, 1, 2*time.Minute)
	waitForShardObjects(t, ctx, repoRoot, kubeEnv, world, 1, 2*time.Minute)
	t.Logf("world scaled back to a single shard")
}

// waitForAutoscalerStatus blocks until the ShardAutoscaler reports the wanted
// current/desired shard pair.
func waitForAutoscalerStatus(t *testing.T, ctx context.Context, repoRoot string, kubeEnv []string, autoscaler, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		out, err := runOut(ctx, repoRoot, kubeEnv,
			"kubectl", "-n", "bindery-demo", "get", "shardautoscaler", autoscaler,
			"-o", "jsonpath={.status.currentShards}/{.status.desiredShards}")
		got := strings.TrimSpace(out)
		if err == nil && got == want {
			t.Logf("shardautoscaler %s status current/desired=%s", autoscaler, got)
			return
		}
		if time.Now().After(deadline) {
			logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "shardautoscaler", autoscaler, "-o", "yaml")
			t.Fatalf("timeout waiting for shardautoscaler status %s (last=%q, err=%v)", want, got, err)
		}
		time.Sleep(3 * time.Second)
	}
}

// waitForShardCount blocks until the WorldInstance reports the wanted shardCount.
func waitForShardCount(t *testing.T, ctx context.Context, repoRoot string, kubeEnv []string, world string, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		out, err := runOut(ctx, repoRoot, kubeEnv,
			"kubectl", "-n", "bindery-demo", "get", "worldinstance", world,
			"-o", "jsonpath={.spec.shardCount}")
		got := strings.TrimSpace(out)
		if err == nil && got == fmt.Sprintf("%d", want) {
			t.Logf("worldinstance %s shardCount=%s", world, got)
			return
		}
		if time.Now().After(deadline) {
			logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "shardautoscalers,worldinstances,worldshards")
			t.Fatalf("timeout waiting for %s shardCount=%d (last=%q, err=%v)", world, want, got, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForShardObjects blocks until exactly want WorldShards exist for the world.
func waitForShardObjects(t *testing.T, ctx context.Context, repoRoot string, kubeEnv []string, world string, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		out, err := runOut(ctx, repoRoot, kubeEnv,
			"kubectl", "-n", "bindery-demo", "get", "worldshards",
			"-l", "bindery.platform/world="+world,
			"-o", "jsonpath={.items[*].metadata.name}")
		names := strings.Fields(strings.TrimSpace(out))
		if err == nil && len(names) == want {
			sort.Strings(names)
			t.Logf("worldshards for %s: %v", world, names)
			return
		}
		if time.Now().After(deadline) {
			logCmd(t, ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "worldshards", "-o", "wide")
			t.Fatalf("timeout waiting for %d worldshards on %s (have %d: %v, err=%v)", want, world, len(names), names, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForCapabilityEnv blocks until the named env var appears in the
// deployment's pod template, meaning the orchestrator has published the
// resolved capability endpoint and will not roll the Deployment again for it.
func waitForCapabilityEnv(t *testing.T, ctx context.Context, dir string, env []string, deploy, envName string, timeout time.Duration) {
	t.Helper()

	jsonPath := fmt.Sprintf("jsonpath={.spec.template.spec.containers[0].env[?(@.name==%q)].value}", envName)
	deadline := time.Now().Add(timeout)
	for {
		out, err := runOut(ctx, dir, env, "kubectl", "-n", "bindery-demo", "get", "deployment", deploy, "-o", jsonPath)
		if err == nil && strings.TrimSpace(out) != "" {
			t.Logf("%s resolved on %s: %s", envName, deploy, strings.TrimSpace(out))
			return
		}
		if time.Now().After(deadline) {
			logCmd(t, ctx, dir, env, "kubectl", "-n", "bindery-demo", "get", "capabilitybindings", "-o", "wide")
			t.Fatalf("timeout waiting for %s on deployment/%s", envName, deploy)
		}
		time.Sleep(2 * time.Second)
	}
}

// portForward keeps a kubectl port-forward alive for the life of a poll loop.
// The forwarded pod can be replaced at any time, which kills the forward; the
// poller calls ensure() to bring it back rather than treating that as fatal.
type portForward struct {
	t       *testing.T
	ctx     context.Context
	dir     string
	env     []string
	svc     string
	port    int
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	out     *bytes.Buffer
	restart int
}

func newPortForward(t *testing.T, ctx context.Context, dir string, env []string, svc string) *portForward {
	t.Helper()

	pf := &portForward{t: t, ctx: ctx, dir: dir, env: env, svc: svc, out: &bytes.Buffer{}}
	pf.start()
	return pf
}

func (p *portForward) start() {
	p.t.Helper()

	p.port = pickFreePort(p.t)
	ctx, cancel := context.WithCancel(p.ctx)
	p.cancel = cancel

	cmd := exec.CommandContext(ctx, "kubectl", "-n", "bindery-demo", "port-forward",
		"svc/"+p.svc, fmt.Sprintf("%d:8080", p.port))
	cmd.Dir = p.dir
	cmd.Env = p.env
	cmd.Stdout = p.out
	cmd.Stderr = p.out
	if err := cmd.Start(); err != nil {
		p.t.Fatalf("start port-forward: %v", err)
	}
	p.cmd = cmd

	// Reap the process in the background so exit is observable. cmd.ProcessState
	// is only populated by Wait, and signalling the pid cannot distinguish a
	// live process from an unreaped zombie, so neither is usable as a liveness
	// check here.
	done := make(chan struct{})
	p.done = done
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	// Give kubectl a moment to bind the local port before the first request.
	time.Sleep(2 * time.Second)
}

// ensure restarts the forward if the kubectl process has exited.
func (p *portForward) ensure() {
	p.t.Helper()

	select {
	case <-p.done:
		p.restart++
		p.t.Logf("port-forward dropped, restarting (attempt %d)", p.restart)
		p.cancel()
		p.start()
	default:
	}
}

func (p *portForward) url() string {
	return fmt.Sprintf("http://127.0.0.1:%d/api/state", p.port)
}

func (p *portForward) output() string {
	return p.out.String()
}

func (p *portForward) stop() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
}

type webState struct {
	Tick     int64       `json:"tick"`
	Error    string      `json:"error"`
	Entities []webEntity `json:"entities"`
	WorldID  string      `json:"worldId"`
}

type webEntity struct {
	Kind string `json:"kind"`
}

func pickFreePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// e2e/smoke_test.go -> repo root
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func runOrFail(t *testing.T, ctx context.Context, dir string, env []string, name string, args ...string) string {
	t.Helper()

	out, err := runOut(ctx, dir, env, name, args...)
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func runAllow(ctx context.Context, dir string, env []string, name string, args ...string) error {
	_, err := runOut(ctx, dir, env, name, args...)
	return err
}

// logCmd runs a diagnostic command and surfaces its output through the test
// log. The failure paths previously called runAllow, which discards stdout, so
// every "dump the cluster state" branch produced nothing to read.
func logCmd(t *testing.T, ctx context.Context, dir string, env []string, name string, args ...string) {
	t.Helper()

	out, err := runOut(ctx, dir, env, name, args...)
	if err != nil {
		t.Logf("$ %s %s -> error: %v", name, strings.Join(args, " "), err)
	}
	t.Logf("$ %s %s\n%s", name, strings.Join(args, " "), out)
}

func runOut(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
