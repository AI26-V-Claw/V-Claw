// Package gate implements the policy gate that sits between the tool router
// and the sandbox executor.
//
// Every tool request must flow through GatedRunner before reaching the Docker
// sandbox. The gate enforces the following pipeline:
//
//	Tool Request
//	    │
//	    ▼
//	PolicyChecker ─── block ────────────► ErrBlocked   (never executed, logged)
//	    │
//	    ├── requires_approval ───────────► ErrNeedsApproval (logged, HITL in Sprint 2)
//	    │
//	    └── allow
//	            │
//	            ▼
//	        SafetyScanner (threats logged; used for HITL proposals in Sprint 2)
//	            │
//	            ▼
//	        AuditLogger (EventExecutionStart)
//	            │
//	            ▼
//	        DockerRunner.RunPython / RunShell
//	            │
//	            ▼
//	        AuditLogger (EventExecutionResult)
//	            │
//	            ▼
//	        JobResult returned to caller
//
// Usage:
//
//	guard, _ := runtime.NewWorkspaceGuard("/var/vclaw/workspaces")
//	runner := gate.NewGatedRunner(gate.Config{
//	    Checker:  policies.DefaultChecker,
//	    Detector: safety.DefaultScanner,
//	    Logger:   auditLogger,
//	    Runner:   runtime.NewDockerRunner(runtime.DockerRunnerConfig{Guard: guard}),
//	})
//	result, err := runner.RunPython(ctx, req)
//	if errors.As(err, &gate.ErrBlocked{}) { ... }
//	if errors.As(err, &gate.ErrNeedsApproval{}) { ... }
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

// ─── Error types ──────────────────────────────────────────────────────────────

// ErrBlocked is returned when the PolicyChecker rejects a request outright.
// The request was never sent to the sandbox executor.
type ErrBlocked struct {
	// RequestID identifies the blocked request.
	RequestID string

	// PolicyResult contains the full policy decision with reasons.
	PolicyResult policies.Result
}

func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("gate: request %q blocked by policy (risk=%s): %s",
		e.RequestID, e.PolicyResult.RiskLevel,
		strings.Join(e.PolicyResult.Reasons, "; "))
}

// ErrNeedsApproval is returned when the PolicyChecker classifies the request
// as requiring human approval before execution.
// Sprint 2 will handle this by surfacing a HITL proposal to the user.
type ErrNeedsApproval struct {
	// RequestID identifies the held request.
	RequestID string

	// PolicyResult contains the full policy decision with reasons.
	PolicyResult policies.Result

	// Threats are any additional danger reports from the safety scanner.
	Threats []safety.DangerReport
}

func (e *ErrNeedsApproval) Error() string {
	return fmt.Sprintf("gate: request %q needs approval (risk=%s): %s",
		e.RequestID, e.PolicyResult.RiskLevel,
		strings.Join(e.PolicyResult.Reasons, "; "))
}

// IsBlocked returns true if err is or wraps an ErrBlocked.
func IsBlocked(err error) bool {
	var e *ErrBlocked
	return errors.As(err, &e)
}

// IsNeedsApproval returns true if err is or wraps an ErrNeedsApproval.
func IsNeedsApproval(err error) bool {
	var e *ErrNeedsApproval
	return errors.As(err, &e)
}

// ─── Config ───────────────────────────────────────────────────────────────────

// Config holds the dependencies for GatedRunner.
type Config struct {
	// Checker is the policy engine that classifies every incoming request.
	// Required.
	Checker policies.Checker

	// Detector scans for dangerous patterns to enrich audit logs and
	// HITL proposals. Required.
	Detector safety.Detector

	// Logger records all pipeline events to the audit log.
	// Optional — if nil, a NopLogger is used.
	Logger audit.AuditEventLogger

	// ToolHooks runs common pre/post tool hooks around sandbox executions.
	// Optional — if nil, a no-op implementation is used.
	ToolHooks toolhooks.Hooks

	// Runner is the underlying executor that dispatches jobs to Docker.
	// Required.
	Runner runtime.Runner

	// SkipApprovalGate, when true, allows requests classified as
	// requires_approval to proceed to execution without returning
	// ErrNeedsApproval. Block decisions are still enforced unconditionally.
	//
	// Set this to true when the caller (e.g. the agent's ToolPolicy HITL
	// flow) has already obtained user approval before invoking the runner.
	// In that case the gate acts as a content-based block guard only.
	//
	// Leave false (default) for the toolrouter path where the gate itself
	// owns the HITL proposal flow (Sprint 2+).
	SkipApprovalGate bool
}

// ─── GatedRunner ──────────────────────────────────────────────────────────────

// GatedRunner wraps a runtime.Runner and requires every request to pass
// through the PolicyChecker before execution.
//
// It implements the runtime.Runner interface, so it is a drop-in replacement
// wherever a Runner is accepted. Callers should use GatedRunner instead of
// DockerRunner directly.
type GatedRunner struct {
	checker          policies.Checker
	detector         safety.Detector
	logger           audit.AuditEventLogger
	toolHooks        toolhooks.Hooks
	runner           runtime.Runner
	skipApprovalGate bool
}

// NewGatedRunner creates a GatedRunner from the given Config.
// Panics if Checker, Detector, or Runner is nil.
func NewGatedRunner(cfg Config) *GatedRunner {
	if cfg.Checker == nil {
		panic("gate: Checker must not be nil")
	}
	if cfg.Detector == nil {
		panic("gate: Detector must not be nil")
	}
	if cfg.Runner == nil {
		panic("gate: Runner must not be nil")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = &audit.NopLogger{}
	}
	hooks := cfg.ToolHooks
	if hooks == nil {
		hooks = toolhooks.NoopHooks{}
	}
	return &GatedRunner{
		checker:          cfg.Checker,
		detector:         cfg.Detector,
		logger:           logger,
		toolHooks:        hooks,
		runner:           cfg.Runner,
		skipApprovalGate: cfg.SkipApprovalGate,
	}
}

// ─── Runner interface ─────────────────────────────────────────────────────────

// RunPython gates a sandbox.runPython request through the policy layer before
// dispatching it to the underlying runner.
//
// Returns ErrBlocked if the policy rejects the request.
// Returns ErrNeedsApproval if the policy requires human approval.
// (Sprint 2 will convert ErrNeedsApproval into a HITL proposal flow.)
func (g *GatedRunner) RunPython(ctx context.Context, req *runtime.RunPythonRequest) (*runtime.JobResult, error) {
	// ── Build policy request ───────────────────────────────────────────────
	text := req.Code
	if strings.TrimSpace(text) == "" {
		text = req.ScriptPath
	}
	policyReq := policies.Request{
		RequestID: req.RequestID,
		SessionID: req.SessionID,
		Tool:      policies.ToolRunPython,
		Input:     policies.RequestInput{Code: text, ScriptPath: req.ScriptPath},
		Meta:      policies.RequestMeta{UserIntent: req.Meta.UserIntent, Source: req.Meta.Source},
	}

	// ── Log tool request ──────────────────────────────────────────────────
	base := audit.NewToolRequestEvent(
		req.RequestID, req.SessionID, "",
		string(policies.ToolRunPython), audit.ActionRunPython,
		text,
	)
	_ = g.logger.Log(base)

	definition := sandboxToolDefinition(string(policies.ToolRunPython))
	input := map[string]any{
		"code":        req.Code,
		"script_path": req.ScriptPath,
		"timeout":     req.Timeout.String(),
	}
	if blocked, err := g.beforeTool(ctx, req.RequestID, req.SessionID, definition, input, base); blocked != nil || err != nil {
		return blocked, err
	}

	// ── Policy check ──────────────────────────────────────────────────────
	policyResult := g.checker.Check(policyReq)
	policyEv := audit.NewPolicyEvent(base, string(policyResult.RiskLevel),
		string(policyResult.Decision), policyResult.Reasons)
	_ = g.logger.Log(policyEv)

	// ── Safety scan (always, for audit/HITL enrichment) ───────────────────
	threats := g.detector.ScanPython(text)

	// ── Dispatch based on policy decision ─────────────────────────────────
	switch policyResult.Decision {
	case policies.DecisionBlock:
		_ = g.logger.Log(audit.NewBlockedEvent(base,
			string(policyResult.RiskLevel), policyResult.Reasons))
		return nil, &ErrBlocked{RequestID: req.RequestID, PolicyResult: policyResult}

	case policies.DecisionRequiresApproval:
		if g.skipApprovalGate {
			// Approval was already granted by the caller (e.g. agent ToolPolicy
			// HITL). Proceed to execution; block decisions still apply above.
			break
		}
		summaryVI := safety.SummariseVI(threats)
		reasonVI := strings.Join(policyResult.Reasons, "; ")
		_ = g.logger.Log(audit.NewHITLProposalEvent(base,
			"hitl_"+req.RequestID, summaryVI, reasonVI, nil))
		return nil, &ErrNeedsApproval{
			RequestID:    req.RequestID,
			PolicyResult: policyResult,
			Threats:      threats,
		}

	default: // DecisionAllow — proceed to execution
	}

	// ── Execute ───────────────────────────────────────────────────────────
	return g.execute(ctx, base, definition, input, req.RequestID, req.SessionID, func() (*runtime.JobResult, error) {
		return g.runner.RunPython(ctx, req)
	})
}

// RunShell gates a sandbox.runShell request through the policy layer before
// dispatching it to the underlying runner.
func (g *GatedRunner) RunShell(ctx context.Context, req *runtime.RunShellRequest) (*runtime.JobResult, error) {
	// ── Build policy request ───────────────────────────────────────────────
	policyReq := policies.Request{
		RequestID: req.RequestID,
		SessionID: req.SessionID,
		Tool:      policies.ToolRunShell,
		Input:     policies.RequestInput{Command: req.Command},
		Meta:      policies.RequestMeta{UserIntent: req.Meta.UserIntent, Source: req.Meta.Source},
	}

	// ── Log tool request ──────────────────────────────────────────────────
	base := audit.NewToolRequestEvent(
		req.RequestID, req.SessionID, "",
		string(policies.ToolRunShell), audit.ActionRunShell,
		req.Command,
	)
	_ = g.logger.Log(base)

	definition := sandboxToolDefinition(string(policies.ToolRunShell))
	input := map[string]any{
		"command": req.Command,
		"timeout": req.Timeout.String(),
	}
	if blocked, err := g.beforeTool(ctx, req.RequestID, req.SessionID, definition, input, base); blocked != nil || err != nil {
		return blocked, err
	}

	// ── Policy check ──────────────────────────────────────────────────────
	policyResult := g.checker.Check(policyReq)
	policyEv := audit.NewPolicyEvent(base, string(policyResult.RiskLevel),
		string(policyResult.Decision), policyResult.Reasons)
	_ = g.logger.Log(policyEv)

	// ── Safety scan ───────────────────────────────────────────────────────
	threats := g.detector.ScanShell(req.Command)

	// ── Dispatch based on policy decision ─────────────────────────────────
	switch policyResult.Decision {
	case policies.DecisionBlock:
		_ = g.logger.Log(audit.NewBlockedEvent(base,
			string(policyResult.RiskLevel), policyResult.Reasons))
		return nil, &ErrBlocked{RequestID: req.RequestID, PolicyResult: policyResult}

	case policies.DecisionRequiresApproval:
		if g.skipApprovalGate {
			// Approval was already granted by the caller (e.g. agent ToolPolicy
			// HITL). Proceed to execution; block decisions still apply above.
			break
		}
		summaryVI := safety.SummariseVI(threats)
		reasonVI := strings.Join(policyResult.Reasons, "; ")
		_ = g.logger.Log(audit.NewHITLProposalEvent(base,
			"hitl_"+req.RequestID, summaryVI, reasonVI, nil))
		return nil, &ErrNeedsApproval{
			RequestID:    req.RequestID,
			PolicyResult: policyResult,
			Threats:      threats,
		}

	default: // DecisionAllow
	}

	// ── Execute ───────────────────────────────────────────────────────────
	return g.execute(ctx, base, definition, input, req.RequestID, req.SessionID, func() (*runtime.JobResult, error) {
		return g.runner.RunShell(ctx, req)
	})
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// execute logs execution start, calls fn (the runner dispatch), then logs the result.
func (g *GatedRunner) execute(
	ctx context.Context,
	base audit.AuditEvent,
	definition tools.ToolDefinition,
	input map[string]any,
	requestID string,
	sessionID string,
	fn func() (*runtime.JobResult, error),
) (*runtime.JobResult, error) {
	startedAt := time.Now()

	startEv := audit.NewExecutionStartEvent(base, "")
	_ = g.logger.Log(startEv)

	result, err := fn()
	if err != nil {
		// Runner-level error (Docker daemon down, path guard rejection, etc.)
		errEv := base
		errEv.ErrorMessage = err.Error()
		errEv.Status = audit.StatusFailed
		_ = g.logger.Log(errEv)
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
		}, err, startedAt)
		return nil, err
	}

	resultStatus := string(result.Status)
	outputSummary := audit.SummariseOutput(result.Stdout, result.Stderr, 200)
	resultEv := audit.NewExecutionResultEvent(
		base, result.JobID, resultStatus,
		result.ExitCode, result.DurationMs,
		outputSummary, result.OutputTruncated,
	)
	_ = g.logger.Log(resultEv)
	g.afterTool(ctx, requestID, sessionID, definition, input, tools.ToolResult{
		ToolCallID:     requestID,
		ToolName:       definition.Name,
		Success:        result.Status == runtime.JobSuccess,
		ContentForLLM:  outputSummary,
		ContentForUser: outputSummary,
		Error:          nil,
	}, nil, startedAt)

	return result, nil
}

func (g *GatedRunner) beforeTool(
	ctx context.Context,
	requestID string,
	sessionID string,
	definition tools.ToolDefinition,
	input map[string]any,
	base audit.AuditEvent,
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
		reasons := []string{"pre-tool hook failed: " + err.Error()}
		_ = g.logger.Log(audit.NewBlockedEvent(base, string(definition.RiskLevel), reasons))
		return nil, &ErrBlocked{
			RequestID: requestID,
			PolicyResult: policies.Result{
				RequestID: requestID,
				Decision:  policies.DecisionBlock,
				RiskLevel: policies.RiskLevel(definition.RiskLevel),
				Reasons:   reasons,
			},
		}
	}
	switch result.Decision {
	case toolhooks.DecisionBlock:
		reason := firstNonEmpty(strings.TrimSpace(result.Reason), "pre-tool hook blocked the request")
		reasons := []string{reason}
		_ = g.logger.Log(audit.NewBlockedEvent(base, string(definition.RiskLevel), reasons))
		return nil, &ErrBlocked{
			RequestID: requestID,
			PolicyResult: policies.Result{
				RequestID: requestID,
				Decision:  policies.DecisionBlock,
				RiskLevel: policies.RiskLevel(definition.RiskLevel),
				Reasons:   reasons,
			},
		}
	case toolhooks.DecisionRequiresApproval:
		reason := firstNonEmpty(strings.TrimSpace(result.Reason), "pre-tool hook requires approval")
		_ = g.logger.Log(audit.NewHITLProposalEvent(base, "hitl_"+requestID, "", reason, nil))
		if g.skipApprovalGate {
			return nil, nil
		}
		return nil, &ErrNeedsApproval{
			RequestID: requestID,
			PolicyResult: policies.Result{
				RequestID: requestID,
				Decision:  policies.DecisionRequiresApproval,
				RiskLevel: policies.RiskLevel(definition.RiskLevel),
				Reasons:   []string{reason},
			},
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
	startedAt time.Time,
) {
	if g == nil || g.toolHooks == nil {
		return
	}
	_ = g.toolHooks.AfterTool(ctx, toolhooks.PostToolInput{
		RequestID:  requestID,
		SessionID:  sessionID,
		ToolCallID: requestID,
		ToolName:   definition.Name,
		Input:      cloneMap(input),
		Definition: definition,
		Result:     result,
		Err:        execErr,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
		Source:     "sandbox_gate",
	})
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
