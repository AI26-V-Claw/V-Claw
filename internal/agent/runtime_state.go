package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

var ErrRuntimeStateNotFound = errors.New("runtime state not found")

type RuntimeRunStatus string

const (
	RuntimeRunStatusRunning              RuntimeRunStatus = "running"
	RuntimeRunStatusWaitingApproval      RuntimeRunStatus = "waiting_approval"
	RuntimeRunStatusWaitingClarification RuntimeRunStatus = "waiting_clarification"
	RuntimeRunStatusCompleted            RuntimeRunStatus = "completed"
	RuntimeRunStatusFailed               RuntimeRunStatus = "failed"
	RuntimeRunStatusBlocked              RuntimeRunStatus = "blocked"
	RuntimeRunStatusMaxIterations        RuntimeRunStatus = "max_iterations"
	RuntimeRunStatusCancelled            RuntimeRunStatus = "cancelled"
)

type ActionStatus string

const (
	ActionStatusPendingApproval ActionStatus = "pending_approval"
	ActionStatusApproved        ActionStatus = "approved"
	ActionStatusExecuting       ActionStatus = "executing"
	ActionStatusCompleted       ActionStatus = "completed"
	ActionStatusFailed          ActionStatus = "failed"
	ActionStatusRejected        ActionStatus = "rejected"
	ActionStatusExpired         ActionStatus = "expired"
	ActionStatusSuperseded      ActionStatus = "superseded"
)

type ToolCallStatus string

const (
	ToolCallStatusProposed             ToolCallStatus = "proposed"
	ToolCallStatusAllowed              ToolCallStatus = "allowed"
	ToolCallStatusRequiresApproval     ToolCallStatus = "requires_approval"
	ToolCallStatusBlocked              ToolCallStatus = "blocked"
	ToolCallStatusWaitingClarification ToolCallStatus = "waiting_clarification"
	ToolCallStatusSkipped              ToolCallStatus = "skipped"
	ToolCallStatusExecuting            ToolCallStatus = "executing"
	ToolCallStatusCompleted            ToolCallStatus = "completed"
	ToolCallStatusFailed               ToolCallStatus = "failed"
)

type RunState struct {
	RunID                  string
	SessionID              string
	RequestID              string
	OriginalGoal           string
	Status                 RuntimeRunStatus
	FailureReason          string
	IterationCount         int
	PendingActionID        string
	PendingClarificationID string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	CompletedAt            *time.Time
	// Governance fields — set once when the run starts, carried through every
	// record that belongs to this run so N4 can filter/group without joining.
	Model         string // LLM model ID, e.g. "claude-opus-4-8"
	PromptVersion string // content-hash fingerprint of the effective system prompt
}

type ActionRecord struct {
	ActionID          string
	RunID             string
	SessionID         string
	RequestID         string
	ToolCallID        string
	ToolName          string
	ArgsSnapshot      map[string]any
	RiskLevel         contracts.RiskLevel
	Status            ActionStatus
	ApprovalID        string
	ApprovalSummary   string
	ApprovalDetails   string
	ApprovalExpiresAt time.Time
	IdempotencyKey    string
	Result            *tools.ToolResult
	CreatedAt         time.Time
	UpdatedAt         time.Time
	// Governance fields — copied from the run at action-creation time.
	Model             string // LLM model ID
	PromptVersion     string // content-hash fingerprint of system prompt
	ToolSchemaVersion string // content-hash fingerprint of the tool's parameter schema
	PolicyDecisionRef string // "policy:<runID>:<toolCallID>:<unixSec>"
}

type ToolCallRecord struct {
	ToolCallID   string
	RunID        string
	RequestID    string
	SessionID    string
	ToolName     string
	ArgsSnapshot map[string]any
	Status       ToolCallStatus
	Reason       string
	ApprovalID   string
	Result       *tools.ToolResult
	ErrorMessage string
	LatencyMS    int64
	CreatedAt    time.Time
	// Governance fields — same provenance bundle as ActionRecord, but populated
	// for every tool call (not only those that need approval).
	Model             string
	PromptVersion     string
	ToolSchemaVersion string
	PolicyDecisionRef string
	// Source identifies the origin layer that produced this call's result.
	// Mirrors contracts.ToolResult.Source so audit records keep a single
	// source-of-truth for attribution.
	Source string
}

type RiskDecisionRecord struct {
	RunID            string
	RequestID        string
	SessionID        string
	ToolCallID       string
	ToolName         string
	RiskLevel        contracts.RiskLevel
	Decision         contracts.RiskDecisionStatus
	RequiresApproval bool
	Reason           string
	PolicyReasons    []string
	CheckedAt        time.Time
	// PolicyDecisionRef is the composite reference shared with every record
	// (tool call, action, audit) that descends from this risk decision.
	// Format: "policy:<runID>:<toolCallID>:<unixSec>".
	PolicyDecisionRef string
}

type ApprovalDecisionRecord struct {
	RequestID string
	SessionID string
	Decision  contracts.ApprovalDecisionStatus
	DecidedBy string
	Channel   string
	Comment   string
	DecidedAt time.Time
}

type RunEvent struct {
	RunID     string
	Type      string
	Data      map[string]any
	CreatedAt time.Time
}

type RuntimeStateStore interface {
	CreateRun(ctx context.Context, state RunState) error
	GetRun(ctx context.Context, runID string) (RunState, error)
	UpdateRun(ctx context.Context, state RunState) error

	FindOrCreateAction(ctx context.Context, record ActionRecord) (ActionRecord, bool, error)
	GetAction(ctx context.Context, actionID string) (ActionRecord, error)
	GetActionByApprovalID(ctx context.Context, approvalID string) (ActionRecord, error)
	FindLatestPendingApproval(ctx context.Context, sessionID string) (ActionRecord, error)
	MarkActionApproved(ctx context.Context, actionID string, decision ApprovalDecisionRecord) (ActionRecord, error)
	MarkActionRejected(ctx context.Context, actionID string, decision ApprovalDecisionRecord) (ActionRecord, error)
	MarkActionExpired(ctx context.Context, actionID string) (ActionRecord, error)
	TryMarkActionExecuting(ctx context.Context, actionID string) (ActionRecord, bool, error)
	CompleteAction(ctx context.Context, actionID string, result tools.ToolResult) (ActionRecord, error)
	FailAction(ctx context.Context, actionID string, result tools.ToolResult) (ActionRecord, error)

	RecordToolCall(ctx context.Context, record ToolCallRecord) error
	RecordRiskDecision(ctx context.Context, record RiskDecisionRecord) error
	AppendRunEvent(ctx context.Context, event RunEvent) error
}

type InMemoryRuntimeStateStore struct {
	mu                   sync.Mutex
	runs                 map[string]RunState
	actions              map[string]ActionRecord
	actionsByApprovalID  map[string]string
	actionsByIdempotency map[string]string
	toolCalls            []ToolCallRecord
	riskDecisions        []RiskDecisionRecord
	approvalDecisions    []ApprovalDecisionRecord
	events               []RunEvent
}

func NewInMemoryRuntimeStateStore() *InMemoryRuntimeStateStore {
	return &InMemoryRuntimeStateStore{
		runs:                 map[string]RunState{},
		actions:              map[string]ActionRecord{},
		actionsByApprovalID:  map[string]string{},
		actionsByIdempotency: map[string]string{},
	}
}

func (s *InMemoryRuntimeStateStore) CreateRun(_ context.Context, state RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.runs[state.RunID]; ok {
		s.runs[state.RunID] = cloneRunState(mergeRunState(existing, state))
		return nil
	}
	s.runs[state.RunID] = cloneRunState(state)
	return nil
}

func (s *InMemoryRuntimeStateStore) GetRun(_ context.Context, runID string) (RunState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.runs[runID]
	if !ok {
		return RunState{}, ErrRuntimeStateNotFound
	}
	return cloneRunState(state), nil
}

func (s *InMemoryRuntimeStateStore) UpdateRun(_ context.Context, state RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[state.RunID]; !ok {
		return ErrRuntimeStateNotFound
	}
	s.runs[state.RunID] = cloneRunState(state)
	return nil
}

func (s *InMemoryRuntimeStateStore) FindOrCreateAction(_ context.Context, record ActionRecord) (ActionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.IdempotencyKey != "" {
		if actionID := s.actionsByIdempotency[record.IdempotencyKey]; actionID != "" {
			existing, ok := s.actions[actionID]
			if !ok {
				return ActionRecord{}, false, ErrRuntimeStateNotFound
			}
			return cloneActionRecord(existing), false, nil
		}
	}
	if existing, ok := s.actions[record.ActionID]; ok {
		return cloneActionRecord(existing), false, nil
	}
	record = cloneActionRecord(record)
	if record.Status == ActionStatusPendingApproval {
		s.supersedePendingApprovalsLocked(record.SessionID, record.ActionID)
	}
	s.actions[record.ActionID] = record
	if record.ApprovalID != "" {
		s.actionsByApprovalID[record.ApprovalID] = record.ActionID
	}
	if record.IdempotencyKey != "" {
		s.actionsByIdempotency[record.IdempotencyKey] = record.ActionID
	}
	return cloneActionRecord(record), true, nil
}

func (s *InMemoryRuntimeStateStore) GetAction(_ context.Context, actionID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	return cloneActionRecord(record), nil
}

func (s *InMemoryRuntimeStateStore) GetActionByApprovalID(_ context.Context, approvalID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	actionID := s.actionsByApprovalID[approvalID]
	if actionID == "" {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	return cloneActionRecord(record), nil
}

func (s *InMemoryRuntimeStateStore) FindLatestPendingApproval(_ context.Context, sessionID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest ActionRecord
	for _, record := range s.actions {
		if record.SessionID != sessionID || record.Status != ActionStatusPendingApproval {
			continue
		}
		if latest.ActionID == "" || actionRecordTime(record).After(actionRecordTime(latest)) {
			latest = record
		}
	}
	if latest.ActionID == "" {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	return cloneActionRecord(latest), nil
}

func (s *InMemoryRuntimeStateStore) MarkActionApproved(_ context.Context, actionID string, decision ApprovalDecisionRecord) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	if record.Status == ActionStatusPendingApproval {
		record.Status = ActionStatusApproved
		record.UpdatedAt = time.Now()
		s.actions[actionID] = record
	}
	decision.SessionID = record.SessionID
	s.approvalDecisions = append(s.approvalDecisions, decision)
	return cloneActionRecord(record), nil
}

func (s *InMemoryRuntimeStateStore) MarkActionRejected(_ context.Context, actionID string, decision ApprovalDecisionRecord) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	if record.Status == ActionStatusPendingApproval || record.Status == ActionStatusApproved {
		record.Status = ActionStatusRejected
		record.UpdatedAt = time.Now()
		s.actions[actionID] = record
	}
	for i, call := range s.toolCalls {
		if call.ToolCallID == record.ToolCallID && call.Status == ToolCallStatusRequiresApproval {
			call.Status = ToolCallStatusBlocked
			call.ErrorMessage = "approval rejected"
			call.CreatedAt = time.Now()
			s.toolCalls[i] = call
		}
	}
	decision.SessionID = record.SessionID
	s.approvalDecisions = append(s.approvalDecisions, decision)
	return cloneActionRecord(record), nil
}

func (s *InMemoryRuntimeStateStore) MarkActionExpired(_ context.Context, actionID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	if record.Status == ActionStatusPendingApproval || record.Status == ActionStatusApproved {
		record.Status = ActionStatusExpired
		record.UpdatedAt = time.Now()
		s.actions[actionID] = record
	}
	return cloneActionRecord(record), nil
}

func (s *InMemoryRuntimeStateStore) TryMarkActionExecuting(_ context.Context, actionID string) (ActionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, false, ErrRuntimeStateNotFound
	}
	if record.Status != ActionStatusApproved {
		return cloneActionRecord(record), false, nil
	}
	record.Status = ActionStatusExecuting
	record.UpdatedAt = time.Now()
	s.actions[actionID] = record
	return cloneActionRecord(record), true, nil
}

func (s *InMemoryRuntimeStateStore) CompleteAction(_ context.Context, actionID string, result tools.ToolResult) (ActionRecord, error) {
	return s.updateActionStatus(actionID, ActionStatusCompleted, &result)
}

func (s *InMemoryRuntimeStateStore) FailAction(_ context.Context, actionID string, result tools.ToolResult) (ActionRecord, error) {
	return s.updateActionStatus(actionID, ActionStatusFailed, &result)
}

func (s *InMemoryRuntimeStateStore) RecordToolCall(_ context.Context, record ToolCallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCalls = append(s.toolCalls, cloneToolCallRecord(record))
	return nil
}

func (s *InMemoryRuntimeStateStore) RecordRiskDecision(_ context.Context, record RiskDecisionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.riskDecisions = append(s.riskDecisions, cloneRiskDecisionRecord(record))
	return nil
}

func (s *InMemoryRuntimeStateStore) AppendRunEvent(_ context.Context, event RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, cloneRunEvent(event))
	return nil
}

func (s *InMemoryRuntimeStateStore) updateActionStatus(actionID string, status ActionStatus, result *tools.ToolResult) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.actions[actionID]
	if !ok {
		return ActionRecord{}, ErrRuntimeStateNotFound
	}
	record.Status = status
	record.UpdatedAt = time.Now()
	record.Result = cloneToolResultPtr(result)
	s.actions[actionID] = record
	return cloneActionRecord(record), nil
}

func (s *InMemoryRuntimeStateStore) supersedePendingApprovalsLocked(sessionID string, exceptActionID string) {
	if sessionID == "" {
		return
	}
	for actionID, record := range s.actions {
		if actionID == exceptActionID || record.SessionID != sessionID || record.Status != ActionStatusPendingApproval {
			continue
		}
		now := time.Now()
		record.Status = ActionStatusSuperseded
		record.UpdatedAt = now
		s.actions[actionID] = record
		run, ok := s.runs[record.RunID]
		if !ok || run.Status != RuntimeRunStatusWaitingApproval || run.PendingActionID != actionID {
			continue
		}
		run.Status = RuntimeRunStatusBlocked
		run.PendingActionID = ""
		run.PendingClarificationID = ""
		run.UpdatedAt = now
		run.CompletedAt = &now
		s.runs[record.RunID] = run
	}
}

func actionRecordTime(record ActionRecord) time.Time {
	if !record.CreatedAt.IsZero() {
		return record.CreatedAt
	}
	return record.UpdatedAt
}

func mergeRunState(existing RunState, next RunState) RunState {
	if next.CreatedAt.IsZero() {
		next.CreatedAt = existing.CreatedAt
	}
	return next
}

func cloneRunState(state RunState) RunState {
	if state.CompletedAt != nil {
		completedAt := *state.CompletedAt
		state.CompletedAt = &completedAt
	}
	return state
}

func cloneActionRecord(record ActionRecord) ActionRecord {
	record.ArgsSnapshot = cloneArguments(record.ArgsSnapshot)
	record.Result = cloneToolResultPtr(record.Result)
	return record
}

func cloneToolCallRecord(record ToolCallRecord) ToolCallRecord {
	record.ArgsSnapshot = cloneArguments(record.ArgsSnapshot)
	record.Result = cloneToolResultPtr(record.Result)
	return record
}

func cloneRiskDecisionRecord(record RiskDecisionRecord) RiskDecisionRecord {
	if len(record.PolicyReasons) > 0 {
		record.PolicyReasons = append([]string(nil), record.PolicyReasons...)
	}
	return record
}

func cloneRunEvent(event RunEvent) RunEvent {
	event.Data = cloneArguments(event.Data)
	return event
}

func cloneToolResultPtr(result *tools.ToolResult) *tools.ToolResult {
	if result == nil {
		return nil
	}
	cloned := *result
	if result.Error != nil {
		errCopy := *result.Error
		cloned.Error = &errCopy
	}
	return &cloned
}


