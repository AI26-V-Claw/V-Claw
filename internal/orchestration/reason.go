package orchestration

import "context"

type FailureReason string

const (
	FailureReasonNone                FailureReason = ""
	FailureReasonTimeout             FailureReason = "timeout"
	FailureReasonCanceled            FailureReason = "canceled"
	FailureReasonIterationBudget     FailureReason = "iteration_budget"
	FailureReasonProviderError       FailureReason = "provider_error"
	FailureReasonProviderUnavailable FailureReason = "provider_unavailable"
	FailureReasonToolError           FailureReason = "tool_error"
	FailureReasonApprovalExpired     FailureReason = "approval_expired"
	FailureReasonApprovalRejected    FailureReason = "approval_rejected"
	FailureReasonPolicyBlocked       FailureReason = "policy_blocked"
	FailureReasonAborted             FailureReason = "aborted"
)

func FromContextError(err error) FailureReason {
	switch err {
	case nil:
		return FailureReasonNone
	case context.DeadlineExceeded:
		return FailureReasonTimeout
	case context.Canceled:
		return FailureReasonCanceled
	default:
		return FailureReasonAborted
	}
}
