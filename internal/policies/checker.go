package policies

import (
	"fmt"
	"strings"
)

// ─── RuleBasedChecker ─────────────────────────────────────────────────────────

// RuleBasedChecker is the default implementation of Checker.
// It applies the policy matrix defined in shell_rules.go and python_rules.go
// using first-match pattern scanning on normalised (lowercase) input text.
//
// Matching strategy:
//  1. The input (command or code) is lowercased.
//  2. Rules are scanned in order; the first matching rule wins.
//  3. If no rule matches, the request falls back to the default policy
//     configured for the tool (see defaultPolicies).
//
// Create with NewRuleBasedChecker(). The zero value is not valid.
type RuleBasedChecker struct {
	// cfg holds optional overrides.
	cfg RuleBasedConfig
}

// RuleBasedConfig allows callers to tune the checker behaviour.
type RuleBasedConfig struct {
	// LocalWriteRequiresConfirm, when true, changes the decision for
	// local_write rules from DecisionAllow to DecisionRequiresApproval before
	// the sandbox-wide approval invariant is applied.
	LocalWriteRequiresConfirm bool
}

// NewRuleBasedChecker creates a RuleBasedChecker with the given config.
func NewRuleBasedChecker(cfg RuleBasedConfig) *RuleBasedChecker {
	return &RuleBasedChecker{cfg: cfg}
}

// DefaultChecker is a ready-to-use RuleBasedChecker with default config.
var DefaultChecker = NewRuleBasedChecker(RuleBasedConfig{})

// ─── Checker interface ────────────────────────────────────────────────────────

// Check classifies req and returns a policy Result.
// It dispatches to the tool-specific sub-checker based on req.Tool.
func (c *RuleBasedChecker) Check(req Request) Result {
	switch req.Tool {
	case ToolRunShell:
		return c.checkShell(req)
	case ToolRunPython:
		return c.checkPython(req)
	default:
		return c.unknown(req)
	}
}

// ─── Tool sub-checkers ────────────────────────────────────────────────────────

// checkShell classifies a sandbox.runShell request.
func (c *RuleBasedChecker) checkShell(req Request) Result {
	cmd := strings.ToLower(req.Input.Command)

	for _, rule := range shellRules {
		if strings.Contains(cmd, strings.ToLower(rule.Pattern)) {
			return requireApprovalForCodeExecution(c.applyRule(req.RequestID, rule))
		}
	}

	// No rule matched -> keep the classification, but sandbox.runShell is still
	// code execution and must wait for approval before dispatch.
	return requireApprovalForCodeExecution(Result{
		RequestID: req.RequestID,
		Decision:  DecisionAllow,
		RiskLevel: RiskSafeRead,
		Reasons:   []string{"Lệnh không khớp với bất kỳ rule nào. Phân loại safe_read; sandbox.runShell vẫn cần phê duyệt trước khi thực thi."},
	})
}

// checkPython classifies a sandbox.runPython request.
// It scans both Code (inline) and ScriptPath for dangerous patterns.
func (c *RuleBasedChecker) checkPython(req Request) Result {
	// Build the text to analyse: inline code takes priority; fall back to path.
	text := req.Input.Code
	if strings.TrimSpace(text) == "" {
		text = req.Input.ScriptPath
	}
	lower := strings.ToLower(text)

	for _, rule := range pythonRules {
		if strings.Contains(lower, strings.ToLower(rule.Pattern)) {
			return requireApprovalForCodeExecution(c.applyRule(req.RequestID, rule))
		}
	}

	// No rule matched -> keep the classification, but sandbox.runPython is still
	// code execution and must wait for approval before dispatch.
	// Python code always creates a temporary script file in the workspace.
	return requireApprovalForCodeExecution(Result{
		RequestID: req.RequestID,
		Decision:  DecisionAllow,
		RiskLevel: RiskLocalWrite,
		Reasons:   []string{"Code Python không chứa pattern nguy hiểm. Phân loại local_write; sandbox.runPython vẫn cần phê duyệt trước khi thực thi."},
	})
}

// unknown handles an unrecognised tool name.
func (c *RuleBasedChecker) unknown(req Request) Result {
	return Result{
		RequestID: req.RequestID,
		Decision:  DecisionBlock,
		RiskLevel: RiskDestructive,
		Reasons:   []string{fmt.Sprintf("Tool không được nhận dạng: %q. Bị chặn.", req.Tool)},
	}
}

// ─── Rule application ─────────────────────────────────────────────────────────

// applyRule converts a MatrixEntry into a Result, honouring the
// LocalWriteRequiresConfirm config override.
func (c *RuleBasedChecker) applyRule(requestID string, rule MatrixEntry) Result {
	decision := rule.Decision
	// Config override: local_write -> requires_approval.
	if c.cfg.LocalWriteRequiresConfirm && rule.RiskLevel == RiskLocalWrite && decision == DecisionAllow {
		decision = DecisionRequiresApproval
	}
	return Result{
		RequestID: requestID,
		Decision:  decision,
		RiskLevel: rule.RiskLevel,
		Reasons:   []string{rule.ReasonVI},
	}
}

func requireApprovalForCodeExecution(result Result) Result {
	if result.Decision != DecisionAllow {
		return result
	}
	result.Decision = DecisionRequiresApproval
	result.Reasons = append(result.Reasons, "sandbox.runPython/sandbox.runShell là code_execution theo contract và phải được phê duyệt trước khi thực thi.")
	return result
}

// ─── Helper: Explain ──────────────────────────────────────────────────────────

// Explain returns a formatted Vietnamese summary of a Result suitable for
// inclusion in a HITL proposal or audit log.
func Explain(r Result) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Quyết định: %s | Mức rủi ro: %s\n", r.Decision, r.RiskLevel))
	sb.WriteString("Lý do:\n")
	for _, reason := range r.Reasons {
		sb.WriteString("  - ")
		sb.WriteString(reason)
		sb.WriteString("\n")
	}
	return sb.String()
}
