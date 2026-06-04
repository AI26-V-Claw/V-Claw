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
	// SafeWriteRequiresConfirm, when true, changes the decision for
	// safe_write from allow to requires_approval. Useful in conservative
	// environments where every write should be reviewed.
	SafeWriteRequiresConfirm bool
}

// NewRuleBasedChecker creates a RuleBasedChecker with the given config.
func NewRuleBasedChecker(cfg RuleBasedConfig) *RuleBasedChecker {
	return &RuleBasedChecker{cfg: cfg}
}

// DefaultChecker is a ready-to-use RuleBasedChecker with default config.
// Suitable for most environments; safe_write decisions are allowed directly.
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
	case ToolFileOps:
		return c.checkFileOps(req)
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
			return c.applyRule(req.RequestID, rule)
		}
	}

	// No rule matched → default for shell: safe_read / allow.
	return Result{
		RequestID: req.RequestID,
		Decision:  DecisionAllow,
		RiskLevel: RiskSafeRead,
		Reasons:   []string{"Lệnh không khớp với bất kỳ rule nào. Mặc định cho phép (safe_read)."},
	}
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
			return c.applyRule(req.RequestID, rule)
		}
	}

	// No rule matched → default for Python: safe_write / allow.
	// Python code always creates a temporary script file in the workspace.
	return Result{
		RequestID: req.RequestID,
		Decision:  DecisionAllow,
		RiskLevel: RiskSafeWrite,
		Reasons:   []string{"Code Python không chứa pattern nguy hiểm. Được phép chạy trong sandbox."},
	}
}

// checkFileOps classifies a file_ops request by operation type.
func (c *RuleBasedChecker) checkFileOps(req Request) Result {
	op := strings.ToLower(strings.TrimSpace(req.Input.FileOp))
	if op == "" {
		return Result{
			RequestID: req.RequestID,
			Decision:  DecisionBlock,
			RiskLevel: RiskHighRisk,
			Reasons:   []string{"file_ops: loại thao tác (file_op) bị thiếu hoặc rỗng."},
		}
	}

	entry, ok := fileOpsRules[op]
	if !ok {
		return Result{
			RequestID: req.RequestID,
			Decision:  DecisionBlock,
			RiskLevel: RiskHighRisk,
			Reasons:   []string{fmt.Sprintf("file_ops: loại thao tác không hợp lệ: %q.", op)},
		}
	}

	return c.applyRule(req.RequestID, entry)
}

// unknown handles an unrecognised tool name.
func (c *RuleBasedChecker) unknown(req Request) Result {
	return Result{
		RequestID: req.RequestID,
		Decision:  DecisionBlock,
		RiskLevel: RiskHighRisk,
		Reasons:   []string{fmt.Sprintf("Tool không được nhận dạng: %q. Bị chặn.", req.Tool)},
	}
}

// ─── Rule application ─────────────────────────────────────────────────────────

// applyRule converts a MatrixEntry into a Result, honouring the
// SafeWriteRequiresConfirm config override.
func (c *RuleBasedChecker) applyRule(requestID string, rule MatrixEntry) Result {
	decision := rule.Decision
	// Config override: safe_write -> requires_approval.
	if c.cfg.SafeWriteRequiresConfirm && rule.RiskLevel == RiskSafeWrite && decision == DecisionAllow {
		decision = DecisionRequiresApproval
	}
	return Result{
		RequestID: requestID,
		Decision:  decision,
		RiskLevel: rule.RiskLevel,
		Reasons:   []string{rule.ReasonVI},
	}
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
