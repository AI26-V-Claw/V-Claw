//go:build integration

// Integration tests for DockerRunner.
// These tests require a running Docker daemon and the vclaw-sandbox image.
//
// Run with:
//
//	go test -tags integration -v ./internal/sandbox/runtime/... -run TestDockerRunner
//
// Skip in CI when Docker is not available.
package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sandboxWorkspace creates a temp directory to use as workspace for tests.
func sandboxWorkspace(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "vclaw-test-workspace-*")
	if err != nil {
		t.Fatalf("failed to create temp workspace: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// newTestRunner returns a DockerRunner pointed at the sandbox image.
func newTestRunner() *DockerRunner {
	return NewDockerRunner(DockerRunnerConfig{
		Image:          "vclaw-sandbox:latest",
		StopTimeoutSec: 2,
	})
}

// ─── RunPython ────────────────────────────────────────────────────────────────

func TestDockerRunner_RunPython_HelloWorld(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunPython(context.Background(), &RunPythonRequest{
		RequestID:    "req_test_001",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Code:         `print("hello sandbox")`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != JobSuccess {
		t.Errorf("expected status %q, got %q (stderr: %s)", JobSuccess, result.Status, result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello sandbox") {
		t.Errorf("expected stdout to contain 'hello sandbox', got: %q", result.Stdout)
	}
	if result.DurationMs <= 0 {
		t.Error("DurationMs should be positive")
	}
}

func TestDockerRunner_RunPython_NonZeroExit(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunPython(context.Background(), &RunPythonRequest{
		RequestID:    "req_test_002",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Code:         `import sys; sys.exit(42)`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != JobFailed {
		t.Errorf("expected %q, got %q", JobFailed, result.Status)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestDockerRunner_RunPython_NonRootUser(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunPython(context.Background(), &RunPythonRequest{
		RequestID:    "req_test_003",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Code:         `import os; print("uid:", os.getuid())`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != JobSuccess {
		t.Errorf("expected success, got %q: %s", result.Status, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "uid: 1000") {
		t.Errorf("expected uid 1000 (non-root), got stdout: %q", result.Stdout)
	}
}

func TestDockerRunner_RunPython_WorkingDir(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunPython(context.Background(), &RunPythonRequest{
		RequestID:    "req_test_004",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Code:         `import os; print(os.getcwd())`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Stdout, "/workspace") {
		t.Errorf("expected working dir /workspace, got: %q", result.Stdout)
	}
}

// ─── Timeout ──────────────────────────────────────────────────────────────────

func TestDockerRunner_RunPython_Timeout(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunPython(context.Background(), &RunPythonRequest{
		RequestID:    "req_test_timeout_py",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		// This code sleeps forever; the sandbox must kill it after 2s.
		Code:    `import time; time.sleep(9999)`,
		Timeout: 2 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != JobTimeout {
		t.Errorf("expected status %q, got %q", JobTimeout, result.Status)
	}
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1 for timeout, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "[sandbox] job killed") {
		t.Errorf("expected timeout message in stderr, got: %q", result.Stderr)
	}
	// Execution wall time should be close to the 2s timeout, not 9999s.
	if result.DurationMs > 8000 {
		t.Errorf("job took too long (%d ms), timeout did not work", result.DurationMs)
	}
}

func TestDockerRunner_RunShell_Timeout(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunShell(context.Background(), &RunShellRequest{
		RequestID:    "req_test_timeout_sh",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Command:      "sleep 9999",
		Timeout:      2 * time.Second,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != JobTimeout {
		t.Errorf("expected status %q, got %q", JobTimeout, result.Status)
	}
	if result.DurationMs > 8000 {
		t.Errorf("job took too long (%d ms), timeout did not work", result.DurationMs)
	}
}

// ─── RunShell ─────────────────────────────────────────────────────────────────

func TestDockerRunner_RunShell_ListFiles(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	// Create a test file in the workspace.
	if err := os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunShell(context.Background(), &RunShellRequest{
		RequestID:    "req_test_shell_001",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Command:      "ls /workspace",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != JobSuccess {
		t.Errorf("expected success, got %q: %s", result.Status, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "hello.txt") {
		t.Errorf("expected hello.txt in output, got: %q", result.Stdout)
	}
}

func TestDockerRunner_RunShell_NetworkBlocked(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	result, err := runner.RunShell(context.Background(), &RunShellRequest{
		RequestID:    "req_test_net",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Command:      "python3 -c \"import socket,sys; socket.setdefaulttimeout(3); socket.create_connection(('8.8.8.8',53)); print('CONNECTED')\" 2>&1 || echo 'BLOCKED'",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	combined := result.Stdout + result.Stderr
	if strings.Contains(combined, "CONNECTED") {
		t.Error("sandbox should not have network access")
	}
}

// ─── Output truncation ────────────────────────────────────────────────────────

func TestDockerRunner_RunPython_OutputTruncation(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	// Generate output larger than MaxOutputBytes (128 KB).
	// Each iteration prints 1025 bytes → 200 iterations = ~200 KB.
	code := `
for _ in range(200):
    print("A" * 1024)
`
	result, err := runner.RunPython(context.Background(), &RunPythonRequest{
		RequestID:    "req_test_trunc",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Code:         code,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Stdout) > MaxOutputBytes {
		t.Errorf("stdout exceeds MaxOutputBytes: %d > %d", len(result.Stdout), MaxOutputBytes)
	}
	if !result.OutputTruncated {
		t.Error("expected OutputTruncated to be true for large output")
	}
}

// ─── Context cancellation ─────────────────────────────────────────────────────

func TestDockerRunner_RunPython_ContextCancelled(t *testing.T) {
	runner := newTestRunner()
	ws := sandboxWorkspace(t)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 1s while Python sleeps forever.
	go func() {
		time.Sleep(1 * time.Second)
		cancel()
	}()

	result, err := runner.RunPython(ctx, &RunPythonRequest{
		RequestID:    "req_test_cancel",
		SessionID:    "sess_test",
		WorkspaceDir: ws,
		Code:         `import time; time.sleep(9999)`,
		Timeout:      60 * time.Second, // long timeout; parent context triggers first
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Parent cancellation → JobFailed (not JobTimeout, since it wasn't our timer)
	if result.Status != JobFailed {
		t.Errorf("expected %q for parent cancellation, got %q", JobFailed, result.Status)
	}
	if result.DurationMs > 8000 {
		t.Errorf("context cancel took too long: %d ms", result.DurationMs)
	}
}
