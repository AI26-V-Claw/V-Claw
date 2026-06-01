package policies

import "vclaw/internal/tools"

type ToolPolicy struct{}

func NewToolPolicy() ToolPolicy {
	return ToolPolicy{}
}

func (p ToolPolicy) FilterTools(definitions []tools.ToolDefinition) []tools.ToolDefinition {
	allowed := make([]tools.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if p.canUse(definition.Capability, definition.RiskLevel) {
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
