//go:build integration

// Integration tests for GatedRunner with a real Docker executor.
//
// Prerequisites:
//   - Docker daemon is running
//   - vclaw-sandbox:latest image has been built:
//     cd internal/sandbox/docker && docker build -t vclaw-sandbox:latest .
//
// Run with:
//
//	go test ./internal/sandbox/gate/... -tags integration -v -timeout 120s
package gate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vclaw/internal/audit"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
	"vclaw/internal/sandbox/gate"
	"vclaw/internal/sandbox/runtime"
)

// ─── Test setup ───────────────────────────────────────────────────────────────

func newIntegrationGate(t *testing.T, wsDir string) (*gate.GatedRunner, *audit.MemoryLogger) {
	t.Helper()
	guard, err := runtime.NewWorkspaceGuard(wsDir)
	if err != nil {
		t.Fatalf("NewWorkspaceGuard: %v", err)
	}
	logger := audit.NewMemoryLogger()
	runner := gate.NewGatedRunner(gate.Config{
		Checker:  policies.DefaultChecker,
		Detector: safety.DefaultScanner,
		Logger:   logger,
		Runner: runtime.NewDockerRunner(runtime.DockerRunnerConfig{
			Guard: guard,
		}),
	})
	return runner, logger
}

func prepareWorkspace(t *testing.T) string {
	t.Helper()
	wsRoot := t.TempDir()
	guard, err := runtime.NewWorkspaceGuard(wsRoot)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	wsDir, err := guard.PrepareSessionWorkspace("integ-session")
	if err != nil {
		t.Fatalf("PrepareSessionWorkspace: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(wsDir) })
	return wsDir
}

// ─── End-to-end: allow path ───────────────────────────────────────────────────

func TestGateIntegration_Python_HelloWorld(t *testing.T) {
	wsRoot := filepath.Dir(prepareWorkspace(t))
	wsDir := prepareWorkspace(t) // reuse pattern
	_ = wsRoot

	runner, logger := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.RunPython(ctx, &runtime.RunPythonRequest{
		RequestID:    "integ-py-01",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Code:         "print('hello from gated sandbox')",
	})

	if err != nil {
		t.Fatalf("safe Python should not be blocked or need approval: %v", err)
	}
	if result.Status != runtime.JobSuccess {
		t.Errorf("expected JobSuccess, got %s | stderr: %s", result.Status, result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code should be 0, got %d", result.ExitCode)
	}

	// Audit completeness check
	events, _ := logger.Query(audit.Filter{RequestID: "integ-py-01"})
	wantTypes := []audit.EventType{
		audit.EventToolRequest,
		audit.EventPolicyDecision,
		audit.EventExecutionStart,
		audit.EventExecutionResult,
	}
	for _, et := range wantTypes {
		found := false
		for _, ev := range events {
			if ev.EventType == et {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing audit event: %s", et)
		}
	}
}

func TestGateIntegration_Shell_ListWorkspace(t *testing.T) {
	wsDir := prepareWorkspace(t)
	runner, _ := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := runner.RunShell(ctx, &runtime.RunShellRequest{
		RequestID:    "integ-sh-01",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Command:      "ls -la /workspace && pwd",
	})

	if err != nil {
		t.Fatalf("ls should be allowed: %v", err)
	}
	if result.Status != runtime.JobSuccess {
		t.Errorf("expected success, got %s | stderr: %s", result.Status, result.Stderr)
	}
}

func TestGateIntegration_Python_WriteOutput(t *testing.T) {
	wsDir := prepareWorkspace(t)
	runner, _ := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.RunPython(ctx, &runtime.RunPythonRequest{
		RequestID:    "integ-py-02",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Code: `
with open('/workspace/output.txt', 'w') as f:
    f.write('integration test OK')
print('file written')
`,
	})

	if err != nil {
		t.Fatalf("file write should be allowed: %v", err)
	}
	if result.Status != runtime.JobSuccess {
		t.Errorf("expected success, got %s | stderr: %s", result.Status, result.Stderr)
	}

	// Verify file was created on host
	out := filepath.Join(wsDir, "output.txt")
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("output file not created: %v", readErr)
	}
	if string(data) != "integration test OK" {
		t.Errorf("unexpected file content: %q", string(data))
	}
}

// ─── End-to-end: block path ───────────────────────────────────────────────────

func TestGateIntegration_Shell_Block_Shutdown(t *testing.T) {
	wsDir := prepareWorkspace(t)
	runner, logger := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx := context.Background()

	_, err := runner.RunShell(ctx, &runtime.RunShellRequest{
		RequestID:    "integ-blk-01",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Command:      "shutdown -h now",
	})

	if !gate.IsBlocked(err) {
		t.Errorf("shutdown must be blocked, got: %v", err)
	}

	events, _ := logger.Query(audit.Filter{
		RequestID: "integ-blk-01",
		EventType: audit.EventBlocked,
	})
	if len(events) != 1 {
		t.Errorf("expected 1 blocked audit event, got %d", len(events))
	}
}

func TestGateIntegration_Python_Block_Credential(t *testing.T) {
	wsDir := prepareWorkspace(t)
	runner, _ := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx := context.Background()

	_, err := runner.RunPython(ctx, &runtime.RunPythonRequest{
		RequestID:    "integ-blk-02",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Code: `
with open('.env') as f:
    print(f.read())
`,
	})

	if !gate.IsBlocked(err) {
		t.Errorf("credential access must be blocked, got: %v", err)
	}
}

// ─── End-to-end: needs_approval path ─────────────────────────────────────────

func TestGateIntegration_Shell_NeedsApproval_Rm(t *testing.T) {
	wsDir := prepareWorkspace(t)
	runner, logger := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx := context.Background()

	_, err := runner.RunShell(ctx, &runtime.RunShellRequest{
		RequestID:    "integ-na-01",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Command:      "rm -rf /workspace/temp",
	})

	var na *gate.ErrNeedsApproval
	if !errors.As(err, &na) {
		t.Fatalf("rm -rf must return ErrNeedsApproval, got: %v", err)
	}
	if len(na.Threats) == 0 {
		t.Error("ErrNeedsApproval for rm should carry threat reports")
	}

	hitl, _ := logger.Query(audit.Filter{
		RequestID: "integ-na-01",
		EventType: audit.EventHITLProposal,
	})
	if len(hitl) != 1 {
		t.Errorf("expected 1 hitl_proposal audit event, got %d", len(hitl))
	}
	if hitl[0].HITLSummaryVI == "" {
		t.Error("hitl_summary_vi must be populated for needs_approval")
	}
}

// ─── End-to-end: timeout ─────────────────────────────────────────────────────

func TestGateIntegration_Python_Timeout(t *testing.T) {
	wsDir := prepareWorkspace(t)
	runner, _ := newIntegrationGate(t, filepath.Dir(wsDir))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := runner.RunPython(ctx, &runtime.RunPythonRequest{
		RequestID:    "integ-to-01",
		SessionID:    "integ-sess",
		UserID:       "tester",
		WorkspaceDir: wsDir,
		Code:         "import time; time.sleep(9999)",
		Timeout:      3 * time.Second,
	})

	if err != nil {
		t.Fatalf("timeout should return a result (not an error): %v", err)
	}
	if result.Status != runtime.JobTimeout {
		t.Errorf("expected JobTimeout, got %s", result.Status)
	}

	// Audit execution_result should reflect timeout
	// (result.Status is "timeout" → StatusTimeout in audit)
	// Just ensure no panic and a result was returned.
}
