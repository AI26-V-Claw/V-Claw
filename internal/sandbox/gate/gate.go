// Package gate provides the final execution boundary for sandbox runners.
//
// GatedRunner is intentionally thin: it always calls pre/post hooks around the
// underlying runner and turns hook decisions into sandbox-specific errors.
package gate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/audit"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
	"vclaw/internal/sandbox/runtime"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

// ErrBlocked is returned when the request is rejected before execution.
type ErrBlocked struct {
	RequestID    string
	PolicyResult policies.Result
}

func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("gate: request %q blocked by policy (risk=%s): %s",
		e.RequestID, e.PolicyResult.RiskLevel,
		strings.Join(e.PolicyResult.Reasons, "; "))
}

// ErrNeedsApproval is returned when the request must be approved before execution.
type ErrNeedsApproval struct {
	RequestID    string
	PolicyResult policies.Result
	Threats      []safety.DangerReport
}

func (e *ErrNeedsApproval) Error() string {
	return fmt.Sprintf("gate: request %q needs approval (risk=%s): %s",
		e.RequestID, e.PolicyResult.RiskLevel,
		strings.Join(e.PolicyResult.Reasons, "; "))
}

func IsBlocked(err error) bool {
	var e *ErrBlocked
	return errors.As(err, &e)
}

func IsNeedsApproval(err error) bool {
	var e *ErrNeedsApproval
	return errors.As(err, &e)
}

// Config keeps backward compatibility for constructing sandbox execution hooks.
type Config struct {
	Checker          policies.Checker
	Detector         safety.Detector
	Logger           audit.AuditEventLogger
	ToolHooks        toolhooks.Hooks
	Runner           runtime.Runner
	SkipApprovalGate bool
}

// GatedRunner always executes hooks before and after the underlying runner.
type GatedRunner struct {
	toolHooks toolhooks.Hooks
	runner    runtime.Runner
}

func (g *GatedRunner) Hooks() toolhooks.Hooks {
	if g == nil {
		return nil
	}
	return g.toolHooks
}

func (g *GatedRunner) Runner() runtime.Runner {
	if g == nil {
		return nil
	}
	return g.runner
}

func NewGatedRunner(cfg Config) *GatedRunner {
	if cfg.Runner == nil {
		panic("gate: Runner must not be nil")
	}

	hooks := cfg.ToolHooks
	if hooks == nil {
		if cfg.Checker == nil {
			panic("gate: Checker must not be nil when ToolHooks is nil")
		}
		hooks = toolhooks.SandboxPolicyHooks{
			Checker:          cfg.Checker,
			Detector:         cfg.Detector,
			Logger:           cfg.Logger,
			SkipApprovalGate: cfg.SkipApprovalGate,
		}
	}

	return &GatedRunner{
		toolHooks: hooks,
		runner:    cfg.Runner,
	}
}

func (g *GatedRunner) RunPython(ctx context.Context, req *runtime.RunPythonRequest) (*runtime.JobResult, error) {
	definition := sandboxToolDefinition(string(policies.ToolRunPython))
	input := map[string]any{
		"code":        req.Code,
		"script_path": req.ScriptPath,
		"timeout":     req.Timeout.String(),
	}
	if blocked, err := g.beforeTool(ctx, req.RequestID, req.SessionID, definition, input); blocked != nil || err != nil {
		return blocked, err
	}
	return g.execute(ctx, definition, input, req.RequestID, req.SessionID, func() (*runtime.JobResult, error) {
		return g.runner.RunPython(ctx, req)
	})
}

func (g *GatedRunner) RunShell(ctx context.Context, req *runtime.RunShellRequest) (*runtime.JobResult, error) {
	definition := sandboxToolDefinition(string(policies.ToolRunShell))
	input := map[string]any{
		"command": req.Command,
		"timeout": req.Timeout.String(),
	}
	if blocked, err := g.beforeTool(ctx, req.RequestID, req.SessionID, definition, input); blocked != nil || err != nil {
		return blocked, err
	}
	return g.execute(ctx, definition, input, req.RequestID, req.SessionID, func() (*runtime.JobResult, error) {
		return g.runner.RunShell(ctx, req)
	})
}

func (g *GatedRunner) execute(
	ctx context.Context,
	definition tools.ToolDefinition,
	input map[string]any,
	requestID string,
	sessionID string,
	fn func() (*runtime.JobResult, error),
) (*runtime.JobResult, error) {
	startedAt := time.Now()

	result, err := fn()
	if err != nil {
		g.afterTool(ctx, requestID, sessionID, definition, input, tools.ToolResult{
			ToolCallID:     requestID,
			ToolName:       definition.Name,
			Success:        false,
			ContentForLLM:  err.Error(),
			ContentForUser: err.Error(),
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: err.Error(),
			},
		}, err, "", 0, false, startedAt)
		return nil, err
	}

	outputSummary := audit.SummariseOutput(result.Stdout, result.Stderr, 200)
	toolResult := tools.ToolResult{
		ToolCallID:     requestID,
		ToolName:       definition.Name,
		Success:        result.Status == runtime.JobSuccess,
		ContentForLLM:  outputSummary,
		ContentForUser: outputSummary,
		Error:          sandboxToolError(result),
	}
	g.afterTool(ctx, requestID, sessionID, definition, input, tools.ToolResult{
		ToolCallID:     toolResult.ToolCallID,
		ToolName:       toolResult.ToolName,
		Success:        toolResult.Success,
		ContentForLLM:  toolResult.ContentForLLM,
		ContentForUser: toolResult.ContentForUser,
		Error:          toolResult.Error,
	}, nil, result.JobID, result.ExitCode, result.OutputTruncated, startedAt)

	return result, nil
}

func (g *GatedRunner) beforeTool(
	ctx context.Context,
	requestID string,
	sessionID string,
	definition tools.ToolDefinition,
	input map[string]any,
) (*runtime.JobResult, error) {
	if g == nil || g.toolHooks == nil {
		return nil, nil
	}

	result, err := g.toolHooks.BeforeTool(ctx, toolhooks.PreToolInput{
		RequestID:  requestID,
		SessionID:  sessionID,
		ToolCallID: requestID,
		ToolName:   definition.Name,
		Input:      cloneMap(input),
		Definition: definition,
		OccurredAt: time.Now(),
		Source:     "sandbox_gate",
	})
	if err != nil {
		return nil, &ErrBlocked{
			RequestID:    requestID,
			PolicyResult: blockedResult(requestID, definition, "pre-tool hook failed: "+err.Error()),
		}
	}

	switch result.Decision {
	case toolhooks.DecisionBlock:
		return nil, &ErrBlocked{
			RequestID:    requestID,
			PolicyResult: blockedPolicyResult(requestID, definition, result),
		}
	case toolhooks.DecisionRequiresApproval:
		return nil, &ErrNeedsApproval{
			RequestID:    requestID,
			PolicyResult: approvalPolicyResult(requestID, definition, result),
			Threats:      result.Threats,
		}
	default:
		return nil, nil
	}
}

func (g *GatedRunner) afterTool(
	ctx context.Context,
	requestID string,
	sessionID string,
	definition tools.ToolDefinition,
	input map[string]any,
	result tools.ToolResult,
	execErr error,
	jobID string,
	exitCode int,
	outputTruncated bool,
	startedAt time.Time,
) {
	if g == nil || g.toolHooks == nil {
		return
	}

	_ = g.toolHooks.AfterTool(ctx, toolhooks.PostToolInput{
		RequestID:       requestID,
		SessionID:       sessionID,
		ToolCallID:      requestID,
		ToolName:        definition.Name,
		Input:           cloneMap(input),
		Definition:      definition,
		Result:          result,
		Err:             execErr,
		JobID:           jobID,
		ExitCode:        exitCode,
		StartedAt:       startedAt,
		FinishedAt:      time.Now(),
		OutputTruncated: outputTruncated,
		Source:          "sandbox_gate",
	})
}

func blockedPolicyResult(requestID string, definition tools.ToolDefinition, result toolhooks.PreToolResult) policies.Result {
	if result.PolicyResult != nil {
		return *result.PolicyResult
	}
	reason := firstNonEmpty(result.Reason, "sandbox policy blocked the request")
	return blockedResult(requestID, definition, reason)
}

func approvalPolicyResult(requestID string, definition tools.ToolDefinition, result toolhooks.PreToolResult) policies.Result {
	if result.PolicyResult != nil {
		return *result.PolicyResult
	}
	reason := firstNonEmpty(result.Reason, "sandbox policy requires approval")
	return policies.Result{
		RequestID: requestID,
		Decision:  policies.DecisionRequiresApproval,
		RiskLevel: policies.RiskLevel(definition.RiskLevel),
		Reasons:   []string{reason},
	}
}

func blockedResult(requestID string, definition tools.ToolDefinition, reason string) policies.Result {
	return policies.Result{
		RequestID: requestID,
		Decision:  policies.DecisionBlock,
		RiskLevel: policies.RiskLevel(definition.RiskLevel),
		Reasons:   []string{reason},
	}
}

func sandboxToolError(result *runtime.JobResult) *tools.ToolError {
	if result == nil || result.Status == runtime.JobSuccess {
		return nil
	}
	return &tools.ToolError{
		Code:    sandboxToolErrorCode(result.Status),
		Message: sandboxToolErrorMessage(result),
	}
}

func sandboxToolErrorCode(status runtime.JobStatus) string {
	switch status {
	case runtime.JobBlocked, runtime.JobRejected:
		return tools.ErrorBlockedByPolicy
	case runtime.JobTimeout:
		return tools.ErrorTimeout
	default:
		return tools.ErrorExecutionFailed
	}
}

func sandboxToolErrorMessage(result *runtime.JobResult) string {
	if result == nil {
		return "sandbox execution failed"
	}
	if strings.TrimSpace(result.Stderr) != "" {
		return strings.TrimSpace(result.Stderr)
	}
	return "sandbox job finished with status " + string(result.Status)
}

func sandboxToolDefinition(name string) tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:             name,
		Group:            "sandbox",
		Capability:       tools.CapabilityMutating,
		RiskLevel:        tools.RiskLevelCodeExecution,
		RequiresApproval: true,
		Enabled:          true,
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
