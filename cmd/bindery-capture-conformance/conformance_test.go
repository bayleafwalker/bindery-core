package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testBuildRevision = "738e9f752ad1d892bdad8852cd4bd4e29182c16a"

// TestCapturePlaneConformanceAcrossAProcessBoundary runs the real control
// plane and the real producer as two operating system processes talking over
// TCP.
//
// Every other capture test in this repository calls the service or hands a
// synthetic request to http.Handler.ServeHTTP, so none of them exercises
// serialization over a socket, HTTP status handling, or a producer built
// without access to Bindery's Go types. This one does, which is why it builds
// and executes binaries rather than importing anything.
func TestCapturePlaneConformanceAcrossAProcessBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and executes two binaries")
	}
	workspace := t.TempDir()
	broker := buildBinary(t, workspace, "bindery-external-runtime", "../bindery-external-runtime")
	producer := buildBinary(t, workspace, "bindery-capture-conformance", ".")

	address := reserveLoopbackPort(t)
	server := exec.Command(broker)
	server.Env = append(os.Environ(),
		"BINDERY_EXTERNAL_RUNTIME_ADDR="+address,
		"BINDERY_STATE_PATH="+filepath.Join(workspace, "state"),
		"BINDERY_RELAY_ENDPOINT=192.168.122.1:50001",
		"BINDERY_BUILD_REVISION="+testBuildRevision,
	)
	var serverLog strings.Builder
	server.Stdout = &serverLog
	server.Stderr = &serverLog
	if err := server.Start(); err != nil {
		t.Fatalf("start control plane: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
		if t.Failed() {
			t.Logf("control plane log:\n%s", serverLog.String())
		}
	})
	waitForListener(t, address, server, &serverLog)

	reportPath := filepath.Join(workspace, "conformance.json")
	command := exec.Command(producer,
		"-base-url", "http://"+address,
		"-events", "12",
		"-batch-size", "5",
		"-report", reportPath,
	)
	output, err := command.CombinedOutput()

	raw, readErr := os.ReadFile(reportPath)
	if readErr != nil {
		t.Fatalf("producer wrote no report (%v); exit: %v; output:\n%s", readErr, err, output)
	}
	var result report
	if decodeErr := json.Unmarshal(raw, &result); decodeErr != nil {
		t.Fatalf("decode report: %v\nraw:\n%s", decodeErr, raw)
	}
	for _, step := range result.Steps {
		if step.OK {
			t.Logf("PASS %-42s %s", step.Name, step.Detail)
			continue
		}
		t.Errorf("FAIL %-42s %s", step.Name, step.Error)
	}
	if err != nil {
		t.Fatalf("producer exited with %v", err)
	}
	if !result.OK || result.Failed != 0 {
		t.Fatalf("conformance report: %d passed, %d failed", result.Passed, result.Failed)
	}
	// A run that silently skipped its obligations would otherwise pass.
	if result.Passed < 10 {
		t.Fatalf("only %d steps ran; the run did not reach the end of the capture lifecycle", result.Passed)
	}
}

func buildBinary(t *testing.T, workspace, name, pkg string) string {
	t.Helper()
	path := filepath.Join(workspace, name)
	build := exec.Command("go", "build", "-o", path, pkg)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, output)
	}
	return path
}

// reserveLoopbackPort asks the kernel for a free port and releases it. The
// window between release and the server's bind is small and the alternative --
// a hard-coded port -- fails whenever anything else on the machine holds it.
func reserveLoopbackPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return address
}

func waitForListener(t *testing.T, address string, server *exec.Cmd, log fmt.Stringer) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if server.ProcessState != nil && server.ProcessState.Exited() {
			t.Fatalf("control plane exited before listening:\n%s", log.String())
		}
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("control plane did not listen on %s within 20s:\n%s", address, log.String())
}
