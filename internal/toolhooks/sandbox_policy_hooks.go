package toolhooks

import (
	"context"
	"fmt"
	"strings"

	"vclaw/internal/audit"
	"vclaw/internal/policies"
	"vclaw/internal/safety"
)

type SandboxPolicyHooks struct {
	Checker          policies.Checker
	Detector         safety.Detector
	Logger           audit.AuditEventLogger
	SkipApprovalGate bool
}

func (h SandboxPolicyHooks) BeforeTool(ctx context.Context, input PreToolInput) (PreToolResult, error) {
	policyReq, text, err := sandboxPolicyRequest(input)
	if err != nil {
		return PreToolResult{}, err
	}

	base := h.baseEvent(ctx, input, text)
	if err := h.log(base); err != nil {
		return PreToolResult{}, fmt.Errorf("sandbox audit tool-request log failed: %w", err)
	}

	policyResult := h.checker().Check(policyReq)
	policyEvent := audit.NewPolicyEvent(base, string(policyResult.RiskLevel), string(policyResult.Decision), policyResult.Reasons)
	if err := h.log(policyEvent); err != nil {
		return PreToolResult{}, fmt.Errorf("sandbox audit policy log failed: %w", err)
	}

	threats := h.detector().ScanShell(text)
	if policyReq.Tool == policies.ToolRunPython {
		threats = h.detector().ScanPython(text)
	}

	result := PreToolResult{
		Decision:     DecisionAllow,
		Reason:       firstNonEmpty(strings.Join(policyResult.Reasons, "; "), "sandbox policy allowed execution"),
		PolicyResult: &policyResult,
		Threats:      threats,
	}

	switch policyResult.Decision {
	case policies.DecisionBlock:
		blocked := audit.NewBlockedEvent(base, string(policyResult.RiskLevel), policyResult.Reasons)
		if err := h.log(blocked); err != nil {
			return PreToolResult{}, fmt.Errorf("sandbox audit blocked log failed: %w", err)
		}
		result.Decision = DecisionBlock
		return result, nil
	case policies.DecisionRequiresApproval:
		if h.SkipApprovalGate {
			start := audit.NewExecutionStartEvent(base, "")
			if err := h.log(start); err != nil {
				return PreToolResult{}, fmt.Errorf("sandbox audit execution-start log failed: %w", err)
			}
			return result, nil
		}
		proposal := audit.NewHITLProposalEvent(
			base,
			"hitl_"+policyReq.RequestID,
			safety.SummariseVI(threats),
			strings.Join(policyResult.Reasons, "; "),
			nil,
		)
		if err := h.log(proposal); err != nil {
			return PreToolResult{}, fmt.Errorf("sandbox audit hitl log failed: %w", err)
		}
		result.Decision = DecisionRequiresApproval
		return result, nil
	default:
		start := audit.NewExecutionStartEvent(base, "")
		if err := h.log(start); err != nil {
			return PreToolResult{}, fmt.Errorf("sandbox audit execution-start log failed: %w", err)
		}
		return result, nil
	}
}

func (h SandboxPolicyHooks) AfterTool(ctx context.Context, input PostToolInput) error {
	base := h.baseEvent(ctx, PreToolInput{
		RequestID:  input.RequestID,
		SessionID:  input.SessionID,
		ToolCallID: input.ToolCallID,
		ToolName:   input.ToolName,
		Input:      input.Input,
		Definition: input.Definition,
	}, commandPreview(input.ToolName, input.Input))

	duration := input.FinishedAt.Sub(input.StartedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	event := audit.NewExecutionResultEvent(
		base,
		input.JobID,
		resultStatusForPostTool(input),
		input.ExitCode,
		duration,
		audit.SummariseOutput(input.Result.ContentForLLM, "", 200),
		input.OutputTruncated,
	)
	if message := executionErrorMessage(input); message != "" {
		event.ErrorMessage = message
	}
	if err := h.log(event); err != nil {
		return fmt.Errorf("sandbox audit execution-result log failed: %w", err)
	}
	return nil
}

func (h SandboxPolicyHooks) baseEvent(ctx context.Context, input PreToolInput, text string) audit.AuditEvent {
	ctxRequestID, ctxSessionID := RequestContextFrom(ctx)
	requestID := firstNonEmpty(ctxRequestID, input.RequestID, input.ToolCallID)
	sessionID := firstNonEmpty(ctxSessionID, input.SessionID)
	return audit.NewToolRequestEvent(
		requestID,
		sessionID,
		"",
		input.ToolName,
		actionTypeForDefinition(input.Definition),
		text,
	)
}

func (h SandboxPolicyHooks) checker() policies.Checker {
	if h.Checker != nil {
		return h.Checker
	}
	return policies.DefaultChecker
}

func (h SandboxPolicyHooks) detector() safety.Detector {
	if h.Detector != nil {
		return h.Detector
	}
	return safety.DefaultScanner
}

func (h SandboxPolicyHooks) log(event audit.AuditEvent) error {
	if h.Logger == nil {
		return nil
	}
	return h.Logger.Log(event)
}

func sandboxPolicyRequest(input PreToolInput) (policies.Request, string, error) {
	requestID := firstNonEmpty(input.RequestID, input.ToolCallID)
	sessionID := strings.TrimSpace(input.SessionID)
	text := commandPreview(input.ToolName, input.Input)

	switch strings.TrimSpace(input.ToolName) {
	case string(policies.ToolRunPython):
		return policies.Request{
			RequestID: requestID,
			SessionID: sessionID,
			Tool:      policies.ToolRunPython,
			Input: policies.RequestInput{
				Code:       stringValue(input.Input, "code"),
				ScriptPath: stringValue(input.Input, "script_path"),
			},
			Meta: policies.RequestMeta{Source: input.Source},
		}, text, nil
	case string(policies.ToolRunShell):
		return policies.Request{
			RequestID: requestID,
			SessionID: sessionID,
			Tool:      policies.ToolRunShell,
			Input: policies.RequestInput{
				Command: stringValue(input.Input, "command"),
			},
			Meta: policies.RequestMeta{Source: input.Source},
		}, text, nil
	default:
		return policies.Request{}, "", fmt.Errorf("sandbox policy hook does not support tool %q", input.ToolName)
	}
}

func stringValue(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}
