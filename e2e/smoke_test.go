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

	// Always attempt cleanup.
	t.Cleanup(func() {
		_ = runAllow(ctx, repoRoot, nil, kindBin, "delete", "cluster", "--name", clusterName)
	})

	// Create cluster.
	runOrFail(t, ctx, repoRoot, nil, kindBin, "create", "cluster", "--name", clusterName, "--wait", "60s")

	// Write an isolated kubeconfig for this test.
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig")
	kubeconfig := runOrFail(t, ctx, repoRoot, nil, kindBin, "get", "kubeconfig", "--name", clusterName)
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	kubeEnv := append(os.Environ(), "KUBECONFIG="+kubeconfigPath)

	// Install CRDs.
	runOrFail(t, ctx, repoRoot, kubeEnv, "kubectl", "apply", "-f", "k8s/crds/")

	// Build + load demo images into kind.
	runOrFail(t, ctx, repoRoot, kubeEnv, "bash", "examples/booklet-bindery-sample/dev/build-images.sh", clusterName)

	// Start controller manager (out-of-cluster) against the kind cluster.
	managerCtx, managerCancel := context.WithCancel(ctx)
	defer managerCancel()

	managerCmd := exec.CommandContext(managerCtx, "go", "run", ".", "--metrics-bind-address=0", "--health-probe-bind-address=0")
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
		_ = managerCmd.Wait()
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

	// Find web service and port-forward to localhost.
	svc := strings.TrimSpace(runOrFail(t, ctx, repoRoot, kubeEnv,
		"kubectl", "-n", "bindery-demo", "get", "svc",
		"-l", "bindery.platform/module=core-web-client",
		"-o", "jsonpath={.items[0].metadata.name}",
	))
	if svc == "" {
		_ = runAllow(ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "svc", "-l", "bindery.platform/module=core-web-client")
		t.Fatal("web service not found")
	}

	localPort := pickFreePort(t)
	pfCtx, pfCancel := context.WithCancel(ctx)
	defer pfCancel()

	pfCmd := exec.CommandContext(pfCtx,
		"kubectl", "-n", "bindery-demo", "port-forward",
		"svc/"+svc, fmt.Sprintf("%d:8080", localPort),
	)
	pfCmd.Dir = repoRoot
	pfCmd.Env = kubeEnv
	var pfOut bytes.Buffer
	pfCmd.Stdout = &pfOut
	pfCmd.Stderr = &pfOut
	if err := pfCmd.Start(); err != nil {
		t.Fatalf("start port-forward: %v", err)
	}
	t.Cleanup(func() {
		pfCancel()
		_ = pfCmd.Wait()
	})

	httpClient := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/state", localPort)

	// Poll the web endpoint until the simulation advances.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if time.Now().After(deadline) {
			t.Logf("manager output:\n%s", managerOut.String())
			t.Logf("port-forward output:\n%s", pfOut.String())
			_ = runAllow(ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "get", "worldinstances,worldshards,capabilitybindings,pods,svc")
			_ = runAllow(ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "logs", "-l", "bindery.platform/module=core-web-client", "--tail=200")
			_ = runAllow(ctx, repoRoot, kubeEnv, "kubectl", "-n", "bindery-demo", "logs", "-l", "bindery.platform/module=core-physics-engine", "--tail=200")
			t.Fatalf("timeout waiting for /api/state to report tick>0 (%s)", url)
		}

		resp, err := httpClient.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			var state webState
			if err := json.Unmarshal(body, &state); err == nil {
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
					return
				}
			}
		}

		time.Sleep(3 * time.Second)
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
