package policies

import (
	"fmt"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

type ToolPolicy struct{}

func NewToolPolicy() ToolPolicy {
	return ToolPolicy{}
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
	if p.canUse(definition.Capability, definition.RiskLevel) && !definition.RequiresApproval {
		decision.Decision = contracts.RiskDecisionAllow
		decision.Reason = "safe read-only or compute tool"
		return decision
	}
	if definition.RequiresApproval || requiresApproval(definition.Capability, definition.RiskLevel) {
		decision.Decision = contracts.RiskDecisionRequiresApproval
		decision.RequiresApproval = true
		decision.Reason = fmt.Sprintf("tool %s requires approval for risk %s", definition.Name, definition.RiskLevel)
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
