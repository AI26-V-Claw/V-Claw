// Package runtime defines the core types and interfaces for the V-Claw sandbox
// execution engine.
//
// Architecture flow:
//
//	Tool Request → Policy Checker → HITL Gate → Runner → Docker Sandbox → Audit Log
//
// The Runner interface is the boundary between the policy/approval layer and the
// actual Docker execution backend. Callers (run_python, run_shell tools) must
// not invoke the Runner directly; they go through the policy pipeline first.
package runtime

import "time"

// ─── Risk Levels ─────────────────────────────────────────────────────────────

// RiskLevel classifies how dangerous a requested action is.
// The Policy Checker assigns a RiskLevel to every incoming tool request.
type RiskLevel string

const (
	// RiskSafeRead - read-only operations, e.g. list files, read CSV.
	RiskSafeRead RiskLevel = "safe_read"

	// RiskSafeWrite - creates new files in workspace, e.g. write report.
	// May require a light confirmation depending on config.
	RiskSafeWrite RiskLevel = "safe_write"

	// RiskNeedsApproval - modifies or deletes existing data.
	// Mandatory HITL before execution.
	RiskNeedsApproval RiskLevel = "needs_approval"

	// RiskHighRisk - deep system commands or actions outside sandbox scope.
	// Always blocked or requires strict HITL.
	RiskHighRisk RiskLevel = "high_risk"

	// RiskExternalNetwork - sends or receives data over the network.
	// Requires HITL and full audit.
	RiskExternalNetwork RiskLevel = "external_network"

	// RiskCredentialAccess - attempts to read tokens, keys, or passwords.
	// Blocked by default.
	RiskCredentialAccess RiskLevel = "credential_access"
)

// ─── Job Status ───────────────────────────────────────────────────────────────

// JobStatus represents the lifecycle state of a sandbox execution job.
type JobStatus string

const (
	// JobPending - job is queued, not yet dispatched to the sandbox.
	JobPending JobStatus = "pending"

	// JobRunning - job is actively executing inside the Docker sandbox.
	JobRunning JobStatus = "running"

	// JobSuccess - job completed with exit code 0.
	JobSuccess JobStatus = "success"

	// JobFailed - job completed with non-zero exit code.
	JobFailed JobStatus = "failed"

	// JobTimeout - job was killed because it exceeded the allowed duration.
	JobTimeout JobStatus = "timeout"

	// JobBlocked - job was not dispatched because the policy checker blocked it.
	JobBlocked JobStatus = "blocked"

	// JobRejected - job was not dispatched because the user rejected the HITL proposal.
	JobRejected JobStatus = "rejected"
)

// ─── Request Context ─────────────────────────────────────────────────────────

// RequestMeta carries session/user metadata attached to every tool request.
// It is used by the policy checker, HITL gate, and audit log.
type RequestMeta struct {
	// UserIntent is a short natural-language description of what the user asked.
	UserIntent string

	// Source identifies who triggered the request: "agent" or "user_direct".
	Source string
}

// ─── Run Python ──────────────────────────────────────────────────────────────

// RunPythonRequest is the input schema for the run_python tool.
//
// Exactly one of Code or ScriptPath must be non-empty:
//   - Code: inline Python source to execute.
//   - ScriptPath: absolute path to a .py file that already exists inside WorkspaceDir.
type RunPythonRequest struct {
	// RequestID is a unique identifier assigned by the tool router.
	RequestID string

	// SessionID ties the request to a user session.
	SessionID string

	// UserID is the authenticated user who triggered the action.
	UserID string

	// WorkspaceDir is the absolute host path to the session-scoped workspace
	// volume that will be bind-mounted as /workspace inside the container.
	WorkspaceDir string

	// Code is inline Python source code to execute.
	// Mutually exclusive with ScriptPath.
	Code string

	// ScriptPath is a path relative to WorkspaceDir pointing to a .py file.
	// Mutually exclusive with Code.
	ScriptPath string

	// Timeout limits execution duration. Zero means use the default (30s).
	Timeout time.Duration

	// Meta carries user intent and audit metadata.
	Meta RequestMeta
}

// ─── Run Shell ───────────────────────────────────────────────────────────────

// RunShellRequest is the input schema for the run_shell tool.
//
// Command is executed via `sh -c` inside the sandbox container.
// The working directory inside the container is always /workspace.
type RunShellRequest struct {
	// RequestID is a unique identifier assigned by the tool router.
	RequestID string

	// SessionID ties the request to a user session.
	SessionID string

	// UserID is the authenticated user who triggered the action.
	UserID string

	// WorkspaceDir is the absolute host path to the session-scoped workspace
	// volume that will be bind-mounted as /workspace inside the container.
	WorkspaceDir string

	// Command is the shell expression to execute, passed to `sh -c`.
	Command string

	// Timeout limits execution duration. Zero means use the default (10s).
	Timeout time.Duration

	// Meta carries user intent and audit metadata.
	Meta RequestMeta
}

// ─── Job Result ──────────────────────────────────────────────────────────────

// JobResult is the output schema returned by the Runner after execution.
// It corresponds to the Execution Result section of the V-Claw API contract.
type JobResult struct {
	// RequestID echoes the originating request for correlation.
	RequestID string

	// JobID is a unique identifier for this specific execution attempt,
	// assigned by the Runner at dispatch time.
	JobID string

	// Status is the final lifecycle state of the job.
	Status JobStatus

	// ExitCode is the process exit code from inside the container.
	// Meaningful only when Status is JobSuccess or JobFailed.
	ExitCode int

	// Stdout is the captured standard output, possibly truncated.
	Stdout string

	// Stderr is the captured standard error, possibly truncated.
	Stderr string

	// DurationMs is the wall-clock execution time in milliseconds.
	DurationMs int64

	// Artifacts lists paths (relative to /workspace) of files written
	// by the job that callers may want to reference.
	Artifacts []string

	// OutputTruncated is true when Stdout or Stderr was cut because it
	// exceeded the output size limit.
	OutputTruncated bool
}

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	// DefaultPythonTimeout is the default execution limit for run_python jobs.
	DefaultPythonTimeout = 30 * time.Second

	// DefaultShellTimeout is the default execution limit for run_shell jobs.
	DefaultShellTimeout = 10 * time.Second

	// MaxOutputBytes is the maximum number of bytes kept from stdout or stderr
	// before truncation.
	MaxOutputBytes = 128 * 1024 // 128 KB
)
