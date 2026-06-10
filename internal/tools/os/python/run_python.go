// Package python provides the sandbox.runPython tool for the V-Claw agent.
//
// The sandbox.runPython tool allows the AI agent to execute Python code or script
// files inside an isolated Docker sandbox. All requests are validated and
// classified by the Policy Checker before the sandbox runner is invoked.
//
// Pipeline:
//
//	RunPython(input) → ValidateInput → PolicyCheck → [HITLGate] → runner.RunPython → JobResult
package python

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/sandbox/runtime"
)

// ─── Tool Input / Output ─────────────────────────────────────────────────────

// Input is the structured input accepted by the sandbox.runPython tool.
// This is what the Agent Planner sends to the Tool Router.
type Input struct {
	// RequestID is a caller-assigned unique ID for this tool invocation.
	RequestID string `json:"request_id"`

	// SessionID ties the invocation to the active session.
	SessionID string `json:"session_id"`

	// WorkspaceDir is the absolute host path to the session workspace that
	// will be mounted as /workspace inside the sandbox.
	WorkspaceDir string `json:"workspace_dir"`

	// Code is inline Python source code to execute.
	// Mutually exclusive with ScriptPath.
	Code string `json:"code,omitempty"`

	// ScriptPath is a path relative to WorkspaceDir pointing to a .py file.
	// Mutually exclusive with Code.
	ScriptPath string `json:"script_path,omitempty"`

	// TimeoutSeconds overrides the default execution timeout.
	// Zero or negative means use the default (30 s).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// UserIntent is a short natural-language description used by the policy
	// checker and included in the audit log.
	UserIntent string `json:"user_intent,omitempty"`
}

// Output is the structured result returned by the sandbox.runPython tool.
// It matches the Execution Result schema from the V-Claw API contract.
type Output struct {
	// RequestID echoes the originating request.
	RequestID string `json:"request_id"`

	// JobID is the unique execution ID assigned by the sandbox runner.
	JobID string `json:"job_id"`

	// Status is the final job state: success, failed, timeout, blocked, rejected.
	Status string `json:"status"`

	// ExitCode is the process exit code from inside the container.
	ExitCode int `json:"exit_code"`

	// Stdout is the captured standard output (possibly truncated).
	Stdout string `json:"stdout"`

	// Stderr is the captured standard error (possibly truncated).
	Stderr string `json:"stderr"`

	// DurationMs is the wall-clock execution time in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Artifacts lists paths (relative to /workspace) written by the job.
	Artifacts []string `json:"artifacts"`

	// OutputTruncated is true when output was cut due to size limits.
	OutputTruncated bool `json:"output_truncated,omitempty"`

	// ErrorMessage holds a human-readable error if the tool itself failed
	// (as opposed to the user code failing).
	ErrorMessage string `json:"error_message,omitempty"`
}

// ─── Tool Handler ─────────────────────────────────────────────────────────────

// RunPython is the entry-point function for the sandbox.runPython tool.
//
// It validates the input, converts it to the canonical sandbox request type,
// and delegates execution to the provided Runner. The caller is responsible
// for running policy checks and HITL approval before calling RunPython.
//
// Usage:
//
//	result, err := python.RunPython(ctx, input, sandboxRunner)
func RunPython(ctx context.Context, input Input, runner runtime.Runner) (Output, error) {
	if err := validateInput(input); err != nil {
		return Output{
			RequestID:    input.RequestID,
			Status:       string(runtime.JobBlocked),
			ErrorMessage: err.Error(),
		}, fmt.Errorf("sandbox.runPython: invalid input: %w", err)
	}

	req := toRuntimeRequest(input)
	if err := runtime.ValidateRunPythonRequest(req); err != nil {
		return Output{
			RequestID:    input.RequestID,
			Status:       string(runtime.JobBlocked),
			ErrorMessage: err.Error(),
		}, fmt.Errorf("sandbox.runPython: request validation failed: %w", err)
	}

	result, err := runner.RunPython(ctx, req)
	if err != nil {
		return Output{
			RequestID:    input.RequestID,
			Status:       string(runtime.JobFailed),
			ErrorMessage: err.Error(),
		}, fmt.Errorf("sandbox.runPython: runner error: %w", err)
	}

	return toOutput(result), nil
}

// ─── Input validation ────────────────────────────────────────────────────────

func validateInput(input Input) error {
	if strings.TrimSpace(input.RequestID) == "" {
		return fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(input.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(input.WorkspaceDir) == "" {
		return fmt.Errorf("workspace_dir is required")
	}
	codeEmpty := strings.TrimSpace(input.Code) == ""
	scriptEmpty := strings.TrimSpace(input.ScriptPath) == ""
	if codeEmpty && scriptEmpty {
		return fmt.Errorf("exactly one of 'code' or 'script_path' must be provided")
	}
	if !codeEmpty && !scriptEmpty {
		return fmt.Errorf("'code' and 'script_path' are mutually exclusive")
	}
	return nil
}

// ─── Type conversion ─────────────────────────────────────────────────────────

func toRuntimeRequest(input Input) *runtime.RunPythonRequest {
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	return &runtime.RunPythonRequest{
		RequestID:    input.RequestID,
		SessionID:    input.SessionID,
		WorkspaceDir: input.WorkspaceDir,
		Code:         input.Code,
		ScriptPath:   input.ScriptPath,
		Timeout:      timeout,
		Meta: runtime.RequestMeta{
			UserIntent: input.UserIntent,
			Source:     "agent",
		},
	}
}

func toOutput(r *runtime.JobResult) Output {
	artifacts := r.Artifacts
	if artifacts == nil {
		artifacts = []string{}
	}
	return Output{
		RequestID:       r.RequestID,
		JobID:           r.JobID,
		Status:          string(r.Status),
		ExitCode:        r.ExitCode,
		Stdout:          r.Stdout,
		Stderr:          r.Stderr,
		DurationMs:      r.DurationMs,
		Artifacts:       artifacts,
		OutputTruncated: r.OutputTruncated,
	}
}
