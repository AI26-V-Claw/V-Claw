package runtime

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Runner is the core interface for the V-Claw sandbox execution engine.
//
// Implementations back this interface against the Docker sandbox image
// (vclaw-sandbox:latest). The interface is intentionally narrow so that
// callers (the sandbox.runPython and sandbox.runShell tools) only interact with the
// sandbox through well-typed request/result structs.
//
// Callers MUST NOT invoke Runner directly; every request must first
// pass through the Policy Checker and, if needed, the HITL Gate.
type Runner interface {
	// RunPython executes Python code or a script file inside an isolated
	// Docker sandbox container. The container is destroyed after the job
	// completes regardless of outcome.
	RunPython(ctx context.Context, req *RunPythonRequest) (*JobResult, error)

	// RunShell executes a shell command inside an isolated Docker sandbox
	// container via `sh -c`. The container is destroyed after the job
	// completes regardless of outcome.
	RunShell(ctx context.Context, req *RunShellRequest) (*JobResult, error)
}

// ─── Validation helpers ───────────────────────────────────────────────────────

// ValidateRunPythonRequest checks that a RunPythonRequest is well-formed
// before it reaches the Runner. The Policy Checker calls this early so that
// malformed requests are rejected without consuming Docker resources.
func ValidateRunPythonRequest(req *RunPythonRequest) error {
	if req == nil {
		return errors.New("sandbox.runPython: request must not be nil")
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return errors.New("sandbox.runPython: request_id is required")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return errors.New("sandbox.runPython: session_id is required")
	}
	if strings.TrimSpace(req.WorkspaceDir) == "" {
		return errors.New("sandbox.runPython: workspace_dir is required")
	}
	codeEmpty := strings.TrimSpace(req.Code) == ""
	scriptEmpty := strings.TrimSpace(req.ScriptPath) == ""
	if codeEmpty && scriptEmpty {
		return errors.New("sandbox.runPython: exactly one of code or script_path must be set")
	}
	if !codeEmpty && !scriptEmpty {
		return errors.New("sandbox.runPython: code and script_path are mutually exclusive")
	}
	return nil
}

// ValidateRunShellRequest checks that a RunShellRequest is well-formed
// before it reaches the Runner.
func ValidateRunShellRequest(req *RunShellRequest) error {
	if req == nil {
		return errors.New("sandbox.runShell: request must not be nil")
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return errors.New("sandbox.runShell: request_id is required")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return errors.New("sandbox.runShell: session_id is required")
	}
	if strings.TrimSpace(req.WorkspaceDir) == "" {
		return errors.New("sandbox.runShell: workspace_dir is required")
	}
	if strings.TrimSpace(req.Command) == "" {
		return errors.New("sandbox.runShell: command is required")
	}
	return nil
}

// ─── Timeout helpers ─────────────────────────────────────────────────────────

// EffectivePythonTimeout returns the timeout to use for a RunPythonRequest,
// falling back to DefaultPythonTimeout when req.Timeout is zero.
func EffectivePythonTimeout(req *RunPythonRequest) time.Duration {
	if req.Timeout > 0 {
		return req.Timeout
	}
	return DefaultPythonTimeout
}

// EffectiveShellTimeout returns the timeout to use for a RunShellRequest,
// falling back to DefaultShellTimeout when req.Timeout is zero.
func EffectiveShellTimeout(req *RunShellRequest) time.Duration {
	if req.Timeout > 0 {
		return req.Timeout
	}
	return DefaultShellTimeout
}

// ─── Output helpers ───────────────────────────────────────────────────────────

// TruncateOutput cuts s to at most MaxOutputBytes and returns it together with
// a flag indicating whether truncation occurred.
func TruncateOutput(s string) (out string, truncated bool) {
	if len(s) <= MaxOutputBytes {
		return s, false
	}
	return s[:MaxOutputBytes], true
}
