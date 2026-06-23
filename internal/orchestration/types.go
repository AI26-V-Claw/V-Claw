package orchestration

import "time"

type RunStatus string

const (
	RunStatusRunning              RunStatus = "running"
	RunStatusWaitingApproval      RunStatus = "waiting_approval"
	RunStatusWaitingClarification RunStatus = "waiting_clarification"
	RunStatusCompleted            RunStatus = "completed"
	RunStatusFailed               RunStatus = "failed"
	RunStatusBlocked              RunStatus = "blocked"
	RunStatusTimeout              RunStatus = "timeout"
	RunStatusCanceled             RunStatus = "cancelled"
	RunStatusIterationBudget      RunStatus = "iteration_budget"
)

type RunResult struct {
	RunID         string
	SessionID     string
	RequestID     string
	Status        RunStatus
	FailureReason FailureReason
	Message       string
	OriginalGoal  string
	StartedAt     time.Time
	TerminalAt    time.Time
	Iterations    int
}

type ChildResult struct {
	RunID         string
	SessionID     string
	Status        RunStatus
	FailureReason FailureReason
	Content       string
	Iterations    int
	Runtime       time.Duration
}

func (r RunResult) IsTerminal() bool {
	switch r.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusBlocked, RunStatusTimeout, RunStatusCanceled, RunStatusIterationBudget:
		return true
	default:
		return false
	}
}

func (r RunResult) IsSuccess() bool {
	return r.Status == RunStatusCompleted && r.FailureReason == FailureReasonNone
}
