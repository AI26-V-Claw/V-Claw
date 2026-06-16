package toolhooks

import (
	"context"

	"vclaw/internal/safety"
)

type ChainHooks []Hooks

func (c ChainHooks) BeforeTool(ctx context.Context, input PreToolInput) (PreToolResult, error) {
	result := PreToolResult{Decision: DecisionAllow}
	var firstErr error
	for _, hook := range c {
		if hook == nil {
			continue
		}
		next, err := hook.BeforeTool(ctx, input)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result = mergePreToolResult(result, next)
	}
	if firstErr != nil {
		return PreToolResult{}, firstErr
	}
	return result, nil
}

func (c ChainHooks) AfterTool(ctx context.Context, input PostToolInput) error {
	var firstErr error
	for _, hook := range c {
		if hook == nil {
			continue
		}
		if err := hook.AfterTool(ctx, input); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func mergePreToolResult(current PreToolResult, next PreToolResult) PreToolResult {
	if decisionRank(next.Decision) > decisionRank(current.Decision) {
		merged := next
		if merged.Decision == "" {
			merged.Decision = DecisionAllow
		}
		if len(current.Threats) > 0 {
			merged.Threats = append(append([]safety.DangerReport(nil), current.Threats...), merged.Threats...)
		}
		return merged
	}

	if current.Decision == "" {
		current.Decision = DecisionAllow
	}
	if current.Reason == "" {
		current.Reason = next.Reason
	}
	if current.PolicyResult == nil {
		current.PolicyResult = next.PolicyResult
	}
	if len(next.Threats) > 0 {
		current.Threats = append(current.Threats, next.Threats...)
	}
	return current
}

func decisionRank(decision Decision) int {
	switch decision {
	case DecisionBlock:
		return 2
	case DecisionRequiresApproval:
		return 1
	default:
		return 0
	}
}
