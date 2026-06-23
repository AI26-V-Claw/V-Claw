package agent

import "vclaw/internal/providers"

func onlyPlanToolCalls(calls []providers.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if call.Name != PlanToolName {
			return false
		}
	}
	return true
}
