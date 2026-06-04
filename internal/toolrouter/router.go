// Package toolrouter implements the Tool Router that sits between the Agent
// Planner and the sandbox execution pipeline.
//
// The Tool Router provides a single unified entry-point for all tool calls:
//
//	Agent Planner
//	    │
//	    ▼  ToolRequest{tool:"sandbox.runPython", input:{code:"..."}}
//	ToolRouter.Dispatch(ctx, req)
//	    │
//	    ├── "sandbox.runPython" → python.RunPython(ctx, input, gatedRunner)
//	    ├── "sandbox.runShell"  → shell.RunShell(ctx, input, gatedRunner)
//	    └── "file_ops"   → (Sprint 2)
//	                │
//	                ▼  GatedRunner enforces: Policy → Safety → Audit → Docker
//	           ToolResponse
//	               ├── status: "success"          (executed OK)
//	               ├── status: "failed"            (non-zero exit)
//	               ├── status: "timeout"           (killed by deadline)
//	               ├── status: "blocked"           (policy block)
//	               └── status: "pending_approval"  (needs HITL — Sprint 2)
//
// The router matches the V-Claw API contract from section 10 of the
// sandbox-scrum-plan.md.
package toolrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vclaw/internal/sandbox/gate"
	"vclaw/internal/sandbox/runtime"
	pytool "vclaw/internal/tools/os/python"
	shtool "vclaw/internal/tools/os/shell"
)

// ─── API contract types ───────────────────────────────────────────────────────

// ToolRequest is the unified request envelope sent by the Agent Planner to
// the Tool Router. It matches the "Tool Request" schema in section 10 of the
// API contract.
type ToolRequest struct {
	// RequestID is a unique identifier assigned by the Agent Planner.
	RequestID string `json:"request_id"`

	// SessionID ties the request to the active user session.
	SessionID string `json:"session_id"`

	// UserID is the authenticated user who triggered the request.
	UserID string `json:"user_id"`

	// Tool identifies which sandbox tool to invoke.
	// Currently supported: "sandbox.runPython", "sandbox.runShell".
	Tool string `json:"tool"`

	// Input holds the tool-specific payload.
	// For sandbox.runShell  → set Command (and optionally WorkspaceDir, TimeoutSeconds).
	// For sandbox.runPython → set Code or ScriptPath (and optionally WorkspaceDir, TimeoutSeconds).
	Input ToolInput `json:"input"`

	// Context carries user-intent metadata used in audit logs and HITL proposals.
	Context ToolContext `json:"context,omitempty"`
}

// ToolInput is the tool-specific payload embedded inside ToolRequest.
// Fields are used selectively depending on the Tool field.
type ToolInput struct {
	// Command is the shell expression for sandbox.runShell.
	Command string `json:"command,omitempty"`

	// Code is inline Python source for sandbox.runPython.
	Code string `json:"code,omitempty"`

	// ScriptPath is a workspace-relative path to a .py file for sandbox.runPython.
	ScriptPath string `json:"script_path,omitempty"`

	// WorkspaceDir is the absolute host path to the session workspace.
	// If empty, the router uses the workspace prepared by the session guard.
	WorkspaceDir string `json:"workspace_dir,omitempty"`

	// TimeoutSeconds overrides the default execution timeout.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// ToolContext carries request metadata used in audit logs and HITL proposals.
type ToolContext struct {
	// UserIntent is a short natural-language description of what the user asked.
	UserIntent string `json:"user_intent,omitempty"`

	// Source identifies the caller: "agent" or "user_direct".
	Source string `json:"source,omitempty"`
}

// ToolResponse is the unified response returned by the Tool Router after
// dispatching a ToolRequest. It matches the "Execution Result" schema in
// section 10 of the API contract, extended with policy fields.
type ToolResponse struct {
	// RequestID echoes the originating request.
	RequestID string `json:"request_id"`

	// JobID is the execution ID assigned by the sandbox runner.
	// Empty when the request was blocked or is pending approval.
	JobID string `json:"job_id,omitempty"`

	// Status reflects the final outcome of the request.
	//
	//   "success"          — executed, exit code 0
	//   "failed"           — executed, non-zero exit code or stderr
	//   "timeout"          — executed, killed by deadline
	//   "blocked"          — rejected by policy, never executed
	//   "pending_approval" — held for HITL (Sprint 2)
	//   "error"            — internal router/runner error
	Status string `json:"status"`

	// ExitCode is the container exit code. Zero for non-execution outcomes.
	ExitCode int `json:"exit_code"`

	// Stdout is captured standard output (possibly truncated).
	Stdout string `json:"stdout,omitempty"`

	// Stderr is captured standard error (possibly truncated).
	Stderr string `json:"stderr,omitempty"`

	// DurationMs is wall-clock execution time in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Artifacts lists workspace-relative paths written by the job.
	Artifacts []string `json:"artifacts"`

	// OutputTruncated is true when stdout/stderr was cut by the size limit.
	OutputTruncated bool `json:"output_truncated,omitempty"`

	// PolicyDecision is the outcome of the policy check: allow, requires_approval, block.
	PolicyDecision string `json:"policy_decision,omitempty"`

	// PolicyRiskLevel is the risk classification assigned by the checker.
	PolicyRiskLevel string `json:"policy_risk_level,omitempty"`

	// PolicyReasons lists the Vietnamese explanations from the checker.
	PolicyReasons []string `json:"policy_reasons,omitempty"`

	// ApprovalID is the HITL approval token, set when Status is "pending_approval".
	// Sprint 2 will use this to route the approval/rejection back to the gate.
	ApprovalID string `json:"approval_id,omitempty"`

	// ApprovalSummaryVI is the Vietnamese summary shown to the user in the
	// HITL proposal. Set when Status is "pending_approval".
	ApprovalSummaryVI string `json:"approval_summary_vi,omitempty"`

	// ErrorMessage holds a description of an internal error (not user-code failure).
	ErrorMessage string `json:"error_message,omitempty"`
}

// ─── ToolRouter ───────────────────────────────────────────────────────────────

// ToolRouter dispatches ToolRequests to the appropriate tool handler,
// routing every request through the GatedRunner pipeline.
//
// Usage:
//
//	router := toolrouter.New(toolrouter.Config{
//	    Runner: gate.NewGatedRunner(gate.Config{...}),
//	})
//	resp := router.Dispatch(ctx, req)
type ToolRouter struct {
	runner runtime.Runner
}

// Config holds the dependencies for the ToolRouter.
type Config struct {
	// Runner is the executor used for all tool calls.
	// Use gate.GatedRunner to enforce the full policy pipeline.
	// Required.
	Runner runtime.Runner
}

// New creates a ToolRouter with the given config.
// Panics if Runner is nil.
func New(cfg Config) *ToolRouter {
	if cfg.Runner == nil {
		panic("toolrouter: Runner must not be nil")
	}
	return &ToolRouter{runner: cfg.Runner}
}

// ─── Dispatch ─────────────────────────────────────────────────────────────────

// Dispatch routes the request to the correct tool handler and returns a
// unified ToolResponse. It never returns a Go error; all outcomes
// (including policy blocks and HITL holds) are represented as ToolResponse
// statuses so the Agent Planner can inspect them uniformly.
func (r *ToolRouter) Dispatch(ctx context.Context, req ToolRequest) ToolResponse {
	switch strings.ToLower(strings.TrimSpace(req.Tool)) {
	case "sandbox.runpython":
		return r.dispatchPython(ctx, req)
	case "sandbox.runshell":
		return r.dispatchShell(ctx, req)
	default:
		return ToolResponse{
			RequestID:    req.RequestID,
			Status:       "error",
			Artifacts:    []string{},
			ErrorMessage: fmt.Sprintf("unknown tool %q; supported: sandbox.runPython, sandbox.runShell", req.Tool),
		}
	}
}

// ─── Tool dispatchers ─────────────────────────────────────────────────────────

func (r *ToolRouter) dispatchPython(ctx context.Context, req ToolRequest) ToolResponse {
	input := pytool.Input{
		RequestID:      req.RequestID,
		SessionID:      req.SessionID,
		UserID:         req.UserID,
		WorkspaceDir:   req.Input.WorkspaceDir,
		Code:           req.Input.Code,
		ScriptPath:     req.Input.ScriptPath,
		TimeoutSeconds: req.Input.TimeoutSeconds,
		UserIntent:     req.Context.UserIntent,
	}

	output, err := pytool.RunPython(ctx, input, r.runner)
	if err != nil {
		return r.handleToolError(req.RequestID, output.Status, err)
	}
	return fromPythonOutput(output)
}

func (r *ToolRouter) dispatchShell(ctx context.Context, req ToolRequest) ToolResponse {
	input := shtool.Input{
		RequestID:      req.RequestID,
		SessionID:      req.SessionID,
		UserID:         req.UserID,
		WorkspaceDir:   req.Input.WorkspaceDir,
		Command:        req.Input.Command,
		TimeoutSeconds: req.Input.TimeoutSeconds,
		UserIntent:     req.Context.UserIntent,
	}

	output, err := shtool.RunShell(ctx, input, r.runner)
	if err != nil {
		return r.handleToolError(req.RequestID, output.Status, err)
	}
	return fromShellOutput(output)
}

// handleToolError converts errors from tool handlers into ToolResponse values.
// It specifically handles gate.ErrBlocked and gate.ErrNeedsApproval.
func (r *ToolRouter) handleToolError(requestID, toolStatus string, err error) ToolResponse {
	var blocked *gate.ErrBlocked
	if errors.As(err, &blocked) {
		return ToolResponse{
			RequestID:       requestID,
			Status:          "blocked",
			Artifacts:       []string{},
			PolicyDecision:  "block",
			PolicyRiskLevel: string(blocked.PolicyResult.RiskLevel),
			PolicyReasons:   blocked.PolicyResult.Reasons,
			ErrorMessage:    fmt.Sprintf("request blocked by policy: %s", strings.Join(blocked.PolicyResult.Reasons, "; ")),
		}
	}

	var needsApproval *gate.ErrNeedsApproval
	if errors.As(err, &needsApproval) {
		return ToolResponse{
			RequestID:         requestID,
			Status:            "pending_approval",
			Artifacts:         []string{},
			PolicyDecision:    "requires_approval",
			PolicyRiskLevel:   string(needsApproval.PolicyResult.RiskLevel),
			PolicyReasons:     needsApproval.PolicyResult.Reasons,
			ApprovalID:        "hitl_" + requestID,
			ApprovalSummaryVI: buildApprovalSummaryVI(needsApproval),
		}
	}

	// Other errors: input validation, runner errors, etc.
	status := toolStatus
	if status == "" {
		status = "error"
	}
	return ToolResponse{
		RequestID:    requestID,
		Status:       status,
		Artifacts:    []string{},
		ErrorMessage: err.Error(),
	}
}

// ─── Response converters ──────────────────────────────────────────────────────

func fromPythonOutput(o pytool.Output) ToolResponse {
	return ToolResponse{
		RequestID:       o.RequestID,
		JobID:           o.JobID,
		Status:          o.Status,
		ExitCode:        o.ExitCode,
		Stdout:          o.Stdout,
		Stderr:          o.Stderr,
		DurationMs:      o.DurationMs,
		Artifacts:       ensureSlice(o.Artifacts),
		OutputTruncated: o.OutputTruncated,
		PolicyDecision:  "allow",
	}
}

func fromShellOutput(o shtool.Output) ToolResponse {
	return ToolResponse{
		RequestID:       o.RequestID,
		JobID:           o.JobID,
		Status:          o.Status,
		ExitCode:        o.ExitCode,
		Stdout:          o.Stdout,
		Stderr:          o.Stderr,
		DurationMs:      o.DurationMs,
		Artifacts:       ensureSlice(o.Artifacts),
		OutputTruncated: o.OutputTruncated,
		PolicyDecision:  "allow",
	}
}

func ensureSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// buildApprovalSummaryVI generates a short Vietnamese summary for a
// requires_approval response. Sprint 2 will replace this with a rich template.
func buildApprovalSummaryVI(na *gate.ErrNeedsApproval) string {
	if len(na.PolicyResult.Reasons) > 0 {
		return fmt.Sprintf("Yêu cầu cần phê duyệt: %s", strings.Join(na.PolicyResult.Reasons, "; "))
	}
	return "Yêu cầu này cần được người dùng xác nhận trước khi thực thi."
}
