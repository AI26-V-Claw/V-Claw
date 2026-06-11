// Package shell provides the sandbox.runShell tool for the V-Claw agent.
//
// The sandbox.runShell tool allows the AI agent to execute shell commands inside an
// isolated Docker sandbox. Commands are classified by the Policy Checker:
//
//   - safe_read: list files, cat non-sensitive files.
//   - local_write: create new files, mkdir.
//   - destructive: delete, overwrite, bulk rename.
//   - blocked: shutdown, service control, credential access.
//
// Even low-risk sandbox.runShell classifications still require approval before
// execution because the tool itself is code_execution in docs/03-contracts.md.
//
// Pipeline:
//
//	RunShell(input) → ValidateInput → PolicyCheck → [HITLGate] → runner.RunShell → JobResult
package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vclaw/internal/sandbox/runtime"
)

// ─── Tool Input / Output ─────────────────────────────────────────────────────

// Input is the structured input accepted by the sandbox.runShell tool.
// This is what the Agent Planner sends to the Tool Router.
type Input struct {
	// RequestID is a caller-assigned unique ID for this tool invocation.
	RequestID string `json:"request_id"`

	// SessionID ties the invocation to the active session.
	SessionID string `json:"session_id"`

	// WorkspaceDir is the absolute host path to the session workspace that
	// will be mounted as /workspace inside the sandbox.
	WorkspaceDir string `json:"workspace_dir"`

	// Command is the shell expression passed to `sh -c` inside the sandbox.
	// The working directory is always /workspace.
	Command string `json:"command"`

	// TimeoutSeconds overrides the default execution timeout.
	// Zero or negative means use the default (10 s).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// UserIntent is a short natural-language description used by the policy
	// checker and included in the audit log.
	UserIntent string `json:"user_intent,omitempty"`
}

// Output is the structured result returned by the sandbox.runShell tool.
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
	// (as opposed to the user command failing).
	ErrorMessage string `json:"error_message,omitempty"`

	// WorkspaceDir is the absolute host path to the session workspace.
	WorkspaceDir string `json:"workspace_dir,omitempty"`

	// WorkspaceFiles lists the absolute host paths of all files currently in
	// the workspace. Use these paths directly as attachments or file arguments
	// for other tools — do NOT construct paths manually from workspace_dir.
	WorkspaceFiles []string `json:"workspace_files,omitempty"`
}

// ─── Tool Handler ─────────────────────────────────────────────────────────────

// RunShell is the entry-point function for the sandbox.runShell tool.
//
// It validates the input, converts it to the canonical sandbox request type,
// and delegates execution to the provided Runner. The caller is responsible
// for running policy checks and HITL approval before calling RunShell.
//
// Usage:
//
//	result, err := shell.RunShell(ctx, input, sandboxRunner)
func RunShell(ctx context.Context, input Input, runner runtime.Runner) (Output, error) {
	if err := validateInput(input); err != nil {
		return Output{
			RequestID:    input.RequestID,
			Status:       string(runtime.JobBlocked),
			ErrorMessage: err.Error(),
		}, fmt.Errorf("sandbox.runShell: invalid input: %w", err)
	}

	req := toRuntimeRequest(input)
	if err := runtime.ValidateRunShellRequest(req); err != nil {
		return Output{
			RequestID:    input.RequestID,
			Status:       string(runtime.JobBlocked),
			ErrorMessage: err.Error(),
		}, fmt.Errorf("sandbox.runShell: request validation failed: %w", err)
	}

	result, err := runner.RunShell(ctx, req)
	if err != nil {
		return Output{
			RequestID:    input.RequestID,
			Status:       string(runtime.JobFailed),
			ErrorMessage: err.Error(),
		}, fmt.Errorf("sandbox.runShell: runner error: %w", err)
	}

	out := toOutput(result)
	out.WorkspaceDir = input.WorkspaceDir
	out.WorkspaceFiles = listWorkspaceFiles(input.WorkspaceDir)
	for i, a := range out.Artifacts {
		if !filepath.IsAbs(a) {
			out.Artifacts[i] = filepath.Join(input.WorkspaceDir, a)
		}
	}
	return out, nil
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
	if strings.TrimSpace(input.Command) == "" {
		return fmt.Errorf("command is required")
	}
	return nil
}

// ─── Type conversion ─────────────────────────────────────────────────────────

func toRuntimeRequest(input Input) *runtime.RunShellRequest {
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	return &runtime.RunShellRequest{
		RequestID:    input.RequestID,
		SessionID:    input.SessionID,
		WorkspaceDir: input.WorkspaceDir,
		Command:      input.Command,
		Timeout:      timeout,
		Meta: runtime.RequestMeta{
			UserIntent: input.UserIntent,
			Source:     "agent",
		},
	}
}

func listWorkspaceFiles(dir string) []string {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths
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
