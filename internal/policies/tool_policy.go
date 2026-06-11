package policies

import (
	"fmt"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

type ToolPolicy struct {
	userConfig UserPolicyConfig
	userStore  *UserPolicyStore
}

func NewToolPolicy() ToolPolicy {
	return ToolPolicy{}
}

func NewToolPolicyWithConfig(cfg UserPolicyConfig) ToolPolicy {
	normalized, err := normalizeUserPolicyConfig(cfg)
	if err != nil {
		return ToolPolicy{}
	}
	return ToolPolicy{userConfig: normalized}
}

func NewToolPolicyWithStore(store *UserPolicyStore) ToolPolicy {
	if store == nil {
		return ToolPolicy{}
	}
	return ToolPolicy{userStore: store}
}

func (p ToolPolicy) FilterTools(definitions []tools.ToolDefinition) []tools.ToolDefinition {
	allowed := make([]tools.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Enabled && p.canUse(definition.Capability, definition.RiskLevel) {
			allowed = append(allowed, definition)
		}
	}
	return allowed
}

func (p ToolPolicy) CanExecute(tool tools.Tool) bool {
	if tool == nil {
		return false
	}
	return p.canUse(tool.Capability(), tool.RiskLevel())
}

// CanRunInParallel reports whether a tool is safe to execute in a parallel batch.
// Only read-only tools with safe-read or safe-compute risk levels are allowed.
// RequiresApproval is checked by the caller via ToolDefinition before this is called.
func (p ToolPolicy) CanRunInParallel(tool tools.Tool) bool {
	if tool == nil {
		return false
	}
	if tool.Capability() != tools.CapabilityReadOnly {
		return false
	}
	switch tool.RiskLevel() {
	case tools.RiskLevelSafeRead, tools.RiskLevelSafeCompute:
		return true
	default:
		return false
	}
}

func (p ToolPolicy) DecideToolCall(toolCallID string, definition tools.ToolDefinition, found bool, checkedAt time.Time) contracts.RiskDecision {
	if !found {
		return contracts.RiskDecision{
			ToolCallID: toolCallID,
			ToolName:   definition.Name,
			RiskLevel:  contracts.RiskLevelDestructive,
			Decision:   contracts.RiskDecisionBlock,
			Reason:     "tool not found",
			CheckedAt:  checkedAt,
		}
	}

	decision := contracts.RiskDecision{
		ToolCallID: toolCallID,
		ToolName:   definition.Name,
		RiskLevel:  contracts.RiskLevel(definition.RiskLevel),
		CheckedAt:  checkedAt,
	}
	if !definition.Enabled {
		decision.Decision = contracts.RiskDecisionBlock
		decision.Reason = "tool is disabled"
		return decision
	}
	userConfig := p.currentUserConfig()
	if policyDecision, matched := userPolicyDecision(userConfig, contracts.RiskLevel(definition.RiskLevel)); matched {
		decision.Decision = policyDecision
		decision.RequiresApproval = policyDecision == contracts.RiskDecisionRequiresApproval
		decision.Reason = userPolicyReason(definition.Name, definition.RiskLevel, policyDecision)
		return decision
	}
	if definition.RequiresApproval || requiresApproval(definition.Capability, definition.RiskLevel) {
		decision.Decision = contracts.RiskDecisionRequiresApproval
		decision.RequiresApproval = true
		decision.Reason = fmt.Sprintf("tool %s requires approval for risk %s", definition.Name, definition.RiskLevel)
		return decision
	}
	if p.canUse(definition.Capability, definition.RiskLevel) {
		decision.Decision = contracts.RiskDecisionAllow
		decision.Reason = "safe read-only or compute tool"
		return decision
	}
	decision.Decision = contracts.RiskDecisionBlock
	decision.Reason = "tool blocked by policy"
	return decision
}

func (ToolPolicy) canUse(capability tools.Capability, riskLevel tools.RiskLevel) bool {
	if capability != tools.CapabilityReadOnly {
		return false
	}

	switch riskLevel {
	case tools.RiskLevelSafeRead, tools.RiskLevelSafeCompute:
		return true
	default:
		return false
	}
}

func requiresApproval(capability tools.Capability, riskLevel tools.RiskLevel) bool {
	if capability != tools.CapabilityReadOnly {
		return true
	}
	switch riskLevel {
	case tools.RiskLevelSafeRead, tools.RiskLevelSafeCompute:
		return false
	default:
		return true
	}
}

func (p ToolPolicy) currentUserConfig() UserPolicyConfig {
	if p.userStore != nil {
		return p.userStore.Snapshot()
	}
	return p.userConfig
}

func userPolicyDecision(cfg UserPolicyConfig, riskLevel contracts.RiskLevel) (contracts.RiskDecisionStatus, bool) {
	if containsRiskLevel(riskLevel, cfg.AlwaysBlock) {
		return contracts.RiskDecisionBlock, true
	}
	if containsRiskLevel(riskLevel, cfg.RequireApproval) {
		return contracts.RiskDecisionRequiresApproval, true
	}
	if isLowRiskLevel(riskLevel) && containsRiskLevel(riskLevel, cfg.AutoAllow) {
		return contracts.RiskDecisionAllow, true
	}
	return "", false
}

func isLowRiskLevel(riskLevel contracts.RiskLevel) bool {
	switch riskLevel {
	case contracts.RiskLevelSafeRead, contracts.RiskLevelSafeCompute:
		return true
	default:
		return false
	}
}

func userPolicyReason(toolName string, riskLevel tools.RiskLevel, decision contracts.RiskDecisionStatus) string {
	switch decision {
	case contracts.RiskDecisionAllow:
		return fmt.Sprintf("tool %s is auto-allowed by user policy for risk %s", toolName, riskLevel)
	case contracts.RiskDecisionBlock:
		return fmt.Sprintf("tool %s is blocked by user policy for risk %s", toolName, riskLevel)
	case contracts.RiskDecisionRequiresApproval:
		return fmt.Sprintf("tool %s requires approval by user policy for risk %s", toolName, riskLevel)
	default:
		return "tool blocked by policy"
	}
}
