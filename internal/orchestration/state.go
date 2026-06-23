package orchestration

import "time"

func CaptureFromAgentStatus(
	runID string,
	sessionID string,
	requestID string,
	originalGoal string,
	message string,
	agentStatus string,
	failureReason FailureReason,
	iterations int,
	startedAt time.Time,
) RunResult {
	return RunResult{
		RunID:         runID,
		SessionID:     sessionID,
		RequestID:     requestID,
		Status:        mapAgentStatus(agentStatus),
		FailureReason: failureReason,
		Message:       message,
		OriginalGoal:  originalGoal,
		StartedAt:     startedAt,
		TerminalAt:    time.Now().UTC(),
		Iterations:    iterations,
	}
}

func CaptureChildResult(
	runID string,
	sessionID string,
	content string,
	agentStatus string,
	failureReason FailureReason,
	iterations int,
	runtime time.Duration,
) ChildResult {
	return ChildResult{
		RunID:         runID,
		SessionID:     sessionID,
		Status:        mapAgentStatus(agentStatus),
		FailureReason: failureReason,
		Content:       content,
		Iterations:    iterations,
		Runtime:       runtime,
	}
}

func FailureReasonFromErrorCode(code string) FailureReason {
	switch code {
	case "PROVIDER_ERROR":
		return FailureReasonProviderError
	case "PROVIDER_TIMEOUT":
		return FailureReasonTimeout
	case "PROVIDER_UNAVAILABLE":
		return FailureReasonProviderUnavailable
	case "ITERATION_BUDGET_EXHAUSTED":
		return FailureReasonIterationBudget
	case "APPROVAL_EXPIRED":
		return FailureReasonApprovalExpired
	case "ACTION_BLOCKED_BY_POLICY":
		return FailureReasonPolicyBlocked
	case "TOOL_NOT_FOUND", "TOOL_INPUT_INVALID":
		return FailureReasonToolError
	default:
		return FailureReasonAborted
	}
}

func mapAgentStatus(agentStatus string) RunStatus {
	switch agentStatus {
	case "completed":
		return RunStatusCompleted
	case "failed":
		return RunStatusFailed
	case "blocked":
		return RunStatusBlocked
	case "iteration_budget":
		return RunStatusIterationBudget
	case "waiting_approval":
		return RunStatusWaitingApproval
	case "waiting_clarification":
		return RunStatusWaitingClarification
	case "timeout":
		return RunStatusTimeout
	case "canceled":
		return RunStatusCanceled
	default:
		return RunStatusFailed
	}
}
