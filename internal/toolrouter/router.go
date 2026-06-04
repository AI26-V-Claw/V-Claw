// Package toolrouter implements the Tool Router that sits between the Agent
// Planner and the sandbox execution pipeline.
//
// The Tool Router provides a single unified entry-point for all tool calls:
//
//	Agent Planner
//	    │
//	    ▼  contracts.ToolCall{toolName:"sandbox.runPython", input:{code:"..."}}
//	ToolRouter.Dispatch(ctx, call)
//	    │
//	    ├── "sandbox.runPython" → python.RunPython(ctx, input, gatedRunner)
//	    └── "sandbox.runShell"  → shell.RunShell(ctx, input, gatedRunner)
//	                              │
//	                              ▼  GatedRunner enforces: Policy → Safety → Audit → Docker
//	           contracts.ToolResult
//
// The router supports the sandbox tool names defined in docs/03-contracts.md.
package toolrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vclaw/internal/contracts"
	"vclaw/internal/sandbox/gate"
	"vclaw/internal/sandbox/runtime"
	pytool "vclaw/internal/tools/os/python"
	shtool "vclaw/internal/tools/os/shell"
)

// ─── ToolRouter ───────────────────────────────────────────────────────────────

// ToolRouter dispatches canonical contract ToolCalls to the appropriate handler,
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
// canonical ToolResult. It never returns a Go error; all outcomes
// (including policy blocks and HITL holds) are represented in ToolResult.
func (r *ToolRouter) Dispatch(ctx context.Context, call contracts.ToolCall) contracts.ToolResult {
	switch strings.ToLower(strings.TrimSpace(call.ToolName)) {
	case "sandbox.runpython":
		return r.dispatchPython(ctx, call)
	case "sandbox.runshell":
		return r.dispatchShell(ctx, call)
	default:
		return errorResult(call, contracts.ErrorToolNotFound, contracts.ErrorSourceTool,
			fmt.Sprintf("unknown tool %q; supported: sandbox.runPython, sandbox.runShell", call.ToolName))
	}
}

// ─── Tool dispatchers ─────────────────────────────────────────────────────────

func (r *ToolRouter) dispatchPython(ctx context.Context, call contracts.ToolCall) contracts.ToolResult {
	input := pytool.Input{
		RequestID:      call.RequestID,
		SessionID:      call.SessionID,
		WorkspaceDir:   stringInput(call.Input, "workspace_dir", "workspaceDir", "workingDir"),
		Code:           stringInput(call.Input, "code"),
		ScriptPath:     stringInput(call.Input, "script_path", "scriptPath"),
		TimeoutSeconds: intInput(call.Input, "timeout_seconds", "timeoutSeconds"),
		UserIntent:     stringInput(call.Input, "user_intent", "userIntent"),
	}

	output, err := pytool.RunPython(ctx, input, r.runner)
	if err != nil {
		return r.handleToolError(call, output.Status, err)
	}
	return resultFromPythonOutput(call, output)
}

func (r *ToolRouter) dispatchShell(ctx context.Context, call contracts.ToolCall) contracts.ToolResult {
	input := shtool.Input{
		RequestID:      call.RequestID,
		SessionID:      call.SessionID,
		WorkspaceDir:   stringInput(call.Input, "workspace_dir", "workspaceDir", "workingDir"),
		Command:        stringInput(call.Input, "command"),
		TimeoutSeconds: intInput(call.Input, "timeout_seconds", "timeoutSeconds"),
		UserIntent:     stringInput(call.Input, "user_intent", "userIntent"),
	}

	output, err := shtool.RunShell(ctx, input, r.runner)
	if err != nil {
		return r.handleToolError(call, output.Status, err)
	}
	return resultFromShellOutput(call, output)
}

// handleToolError converts errors from tool handlers into ToolResult values.
// It specifically handles gate.ErrBlocked and gate.ErrNeedsApproval.
func (r *ToolRouter) handleToolError(call contracts.ToolCall, toolStatus string, err error) contracts.ToolResult {
	var blocked *gate.ErrBlocked
	if errors.As(err, &blocked) {
		return contracts.ToolResult{
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Success:    false,
			Data: map[string]any{
				"status":          "blocked",
				"artifacts":       []string{},
				"policyDecision":  string(contracts.RiskDecisionBlock),
				"policyRiskLevel": string(blocked.PolicyResult.RiskLevel),
				"policyReasons":   blocked.PolicyResult.Reasons,
			},
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorActionBlockedByPolicy,
				Message:   fmt.Sprintf("request blocked by policy: %s", strings.Join(blocked.PolicyResult.Reasons, "; ")),
				Source:    contracts.ErrorSourcePolicy,
				Retryable: false,
			},
		}
	}

	var needsApproval *gate.ErrNeedsApproval
	if errors.As(err, &needsApproval) {
		approvalID := "hitl_" + call.RequestID
		return contracts.ToolResult{
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Success:    false,
			Data: map[string]any{
				"status":            "pending_approval",
				"artifacts":         []string{},
				"policyDecision":    string(contracts.RiskDecisionRequiresApproval),
				"policyRiskLevel":   string(needsApproval.PolicyResult.RiskLevel),
				"policyReasons":     needsApproval.PolicyResult.Reasons,
				"approvalId":        approvalID,
				"approvalSummaryVi": buildApprovalSummaryVI(needsApproval),
			},
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorActionRequiresApproval,
				Message:   "action requires approval",
				Source:    contracts.ErrorSourcePolicy,
				Retryable: false,
			},
		}
	}

	// Other errors: input validation, runner errors, etc.
	status := toolStatus
	if status == "" {
		status = "error"
	}
	return errorResultWithData(call, contracts.ErrorToolInputInvalid, contracts.ErrorSourceTool, err.Error(), map[string]any{
		"status":    status,
		"artifacts": []string{},
	})
}

// ─── Response converters ──────────────────────────────────────────────────────

func resultFromPythonOutput(call contracts.ToolCall, o pytool.Output) contracts.ToolResult {
	return contracts.ToolResult{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Success:    o.Status == string(runtime.JobSuccess),
		Data:       executionData(o.RequestID, o.JobID, o.Status, o.ExitCode, o.Stdout, o.Stderr, o.DurationMs, o.Artifacts, o.OutputTruncated),
		Error:      executionError(o.Status, o.Stderr, o.ErrorMessage),
	}
}

func resultFromShellOutput(call contracts.ToolCall, o shtool.Output) contracts.ToolResult {
	return contracts.ToolResult{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Success:    o.Status == string(runtime.JobSuccess),
		Data:       executionData(o.RequestID, o.JobID, o.Status, o.ExitCode, o.Stdout, o.Stderr, o.DurationMs, o.Artifacts, o.OutputTruncated),
		Error:      executionError(o.Status, o.Stderr, o.ErrorMessage),
	}
}

func executionData(requestID, jobID, status string, exitCode int, stdout, stderr string, durationMs int64, artifacts []string, outputTruncated bool) map[string]any {
	return map[string]any{
		"requestId":       requestID,
		"jobId":           jobID,
		"status":          status,
		"exitCode":        exitCode,
		"stdout":          stdout,
		"stderr":          stderr,
		"durationMs":      durationMs,
		"artifacts":       ensureSlice(artifacts),
		"outputTruncated": outputTruncated,
		"policyDecision":  string(contracts.RiskDecisionAllow),
	}
}

func executionError(status, stderr, message string) *contracts.ErrorShape {
	if status == string(runtime.JobSuccess) {
		return nil
	}
	if strings.TrimSpace(message) == "" {
		message = strings.TrimSpace(stderr)
	}
	if message == "" {
		message = "sandbox job finished with status " + status
	}
	code := contracts.ErrorInternal
	if status == string(runtime.JobTimeout) {
		code = contracts.ErrorProviderTimeout
	}
	return &contracts.ErrorShape{
		Code:      code,
		Message:   message,
		Source:    contracts.ErrorSourceTool,
		Retryable: false,
	}
}

func ensureSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func errorResult(call contracts.ToolCall, code string, source contracts.ErrorSource, message string) contracts.ToolResult {
	return errorResultWithData(call, code, source, message, nil)
}

func errorResultWithData(call contracts.ToolCall, code string, source contracts.ErrorSource, message string, data map[string]any) contracts.ToolResult {
	return contracts.ToolResult{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Success:    false,
		Data:       data,
		Error: &contracts.ErrorShape{
			Code:      code,
			Message:   message,
			Source:    source,
			Retryable: false,
		},
	}
}

func stringInput(input map[string]any, names ...string) string {
	for _, name := range names {
		value, ok := input[name]
		if !ok {
			continue
		}
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func intInput(input map[string]any, names ...string) int {
	for _, name := range names {
		value, ok := input[name]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case int:
			return v
		case int32:
			return int(v)
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return 0
}

// buildApprovalSummaryVI generates a short Vietnamese summary for a
// requires_approval response. Sprint 2 will replace this with a rich template.
func buildApprovalSummaryVI(na *gate.ErrNeedsApproval) string {
	if len(na.PolicyResult.Reasons) > 0 {
		return fmt.Sprintf("Yêu cầu cần phê duyệt: %s", strings.Join(na.PolicyResult.Reasons, "; "))
	}
	return "Yêu cầu này cần được người dùng xác nhận trước khi thực thi."
}
