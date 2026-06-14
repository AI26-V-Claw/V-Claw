package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"vclaw/internal/agent"
	"vclaw/internal/audit"
	"vclaw/internal/contracts"
	"vclaw/internal/tools"
)

type Store struct {
	db *sql.DB
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, errors.New("database url is required")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func NewWithDB(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) CreateRun(ctx context.Context, state agent.RunState) error {
	if err := s.upsertRun(ctx, state); err != nil {
		return err
	}
	return s.insertAuditEntry(ctx, auditEntry{
		EventID:   state.RunID + ":agent.run.started",
		EventType: "agent.run.started",
		RunID:     state.RunID,
		RequestID: state.RequestID,
		SessionID: state.SessionID,
		Details: map[string]any{
			"status": string(state.Status),
			"goal":   state.OriginalGoal,
		},
		Timestamp: zeroNow(state.CreatedAt),
	})
}

func (s *Store) GetRun(ctx context.Context, runID string) (agent.RunState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT run_id, session_id, request_id, original_goal, status, iteration_count,
		       pending_action_id, pending_clarification_id, created_at, updated_at, completed_at
		FROM agent_runs
		WHERE run_id = $1`, runID)
	return scanRunState(row)
}

func (s *Store) UpdateRun(ctx context.Context, state agent.RunState) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET session_id = $2, request_id = $3, original_goal = $4, status = $5,
		    iteration_count = $6, pending_action_id = $7, pending_clarification_id = $8,
		    updated_at = $9, completed_at = $10
		WHERE run_id = $1`,
		state.RunID, state.SessionID, state.RequestID, state.OriginalGoal, string(state.Status),
		state.IterationCount, state.PendingActionID, state.PendingClarificationID, zeroNow(state.UpdatedAt), state.CompletedAt)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return agent.ErrRuntimeStateNotFound
	}
	if err != nil {
		return err
	}
	eventType, ok := runStatusEventType(state.Status)
	if !ok {
		return nil
	}
	return s.insertAuditEntry(ctx, auditEntry{
		EventID:   state.RunID + ":" + eventType,
		EventType: eventType,
		RunID:     state.RunID,
		RequestID: state.RequestID,
		SessionID: state.SessionID,
		Details: map[string]any{
			"status":                 string(state.Status),
			"iterationCount":         state.IterationCount,
			"pendingActionId":        state.PendingActionID,
			"pendingClarificationId": state.PendingClarificationID,
		},
		Timestamp: zeroNow(state.UpdatedAt),
	})
}

// runStatusEventType maps a terminal run status to the contract run event
// (docs/03-contracts.md §5). Non-terminal statuses such as waiting_approval and
// waiting_clarification are not run events — they are persisted in
// agent_runs.status and surfaced via approval.requested / clarify.requested.
func runStatusEventType(status agent.RuntimeRunStatus) (string, bool) {
	switch status {
	case agent.RuntimeRunStatusCompleted:
		return "agent.run.completed", true
	case agent.RuntimeRunStatusFailed, agent.RuntimeRunStatusMaxIterations:
		return "agent.run.failed", true
	case agent.RuntimeRunStatusBlocked:
		return "agent.run.cancelled", true
	default:
		return "", false
	}
}

func (s *Store) FindOrCreateAction(ctx context.Context, record agent.ActionRecord) (agent.ActionRecord, bool, error) {
	if record.IdempotencyKey != "" {
		if existing, err := s.getActionByQuery(ctx, `idempotency_key = $1`, record.IdempotencyKey); err == nil {
			return existing, false, nil
		} else if !errors.Is(err, agent.ErrRuntimeStateNotFound) {
			return agent.ActionRecord{}, false, err
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return agent.ActionRecord{}, false, err
	}
	defer rollback(tx)
	if record.Status == agent.ActionStatusPendingApproval && record.SessionID != "" {
		if err := supersedePendingApprovalsTx(ctx, tx, record.SessionID, record.ActionID); err != nil {
			return agent.ActionRecord{}, false, err
		}
	}
	actionJSON, err := json.Marshal(record.ArgsSnapshot)
	if err != nil {
		return agent.ActionRecord{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO approval_actions (
			action_id, run_id, request_id, session_id, tool_call_id, tool_name, args_snapshot,
			risk_level, status, approval_id, approval_expires_at, idempotency_key, result, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, nullif($12, ''), $13::jsonb, $14, $15)
		ON CONFLICT (action_id) DO NOTHING`,
		record.ActionID, record.RunID, record.RequestID, record.SessionID, record.ToolCallID, record.ToolName,
		string(actionJSON), string(record.RiskLevel), string(record.Status), record.ApprovalID,
		record.ApprovalExpiresAt, record.IdempotencyKey, toolResultJSON(record.Result),
		zeroNow(record.CreatedAt), zeroNow(record.UpdatedAt))
	if err != nil {
		return agent.ActionRecord{}, false, err
	}
	affected, _ := result.RowsAffected()
	created := affected > 0
	if created {
		if err := upsertApprovalRequestTx(ctx, tx, record, contracts.ApprovalStatusPending); err != nil {
			return agent.ActionRecord{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return agent.ActionRecord{}, false, err
	}
	if created {
		if err := s.insertAuditEntry(ctx, auditEntry{
			EventID:        record.ApprovalID + ":approval.requested",
			EventType:      "approval.requested",
			RunID:          record.RunID,
			RequestID:      record.RequestID,
			SessionID:      record.SessionID,
			ToolCallID:     record.ToolCallID,
			ApprovalID:     record.ApprovalID,
			ToolName:       record.ToolName,
			RiskLevel:      string(record.RiskLevel),
			PolicyDecision: string(contracts.RiskDecisionRequiresApproval),
			Details: map[string]any{
				"actionId": record.ActionID,
				"summary":  record.ApprovalSummary,
				"details":  record.ApprovalDetails,
			},
			Timestamp: zeroNow(record.CreatedAt),
		}); err != nil {
			return agent.ActionRecord{}, false, err
		}
	}
	stored, err := s.GetAction(ctx, record.ActionID)
	return stored, created, err
}

func (s *Store) GetAction(ctx context.Context, actionID string) (agent.ActionRecord, error) {
	return s.getActionByQuery(ctx, `action_id = $1`, actionID)
}

func (s *Store) GetActionByApprovalID(ctx context.Context, approvalID string) (agent.ActionRecord, error) {
	return s.getActionByQuery(ctx, `approval_id = $1`, approvalID)
}

func (s *Store) FindLatestPendingApproval(ctx context.Context, sessionID string) (agent.ActionRecord, error) {
	row := s.db.QueryRowContext(ctx, actionSelectSQL()+`
		WHERE session_id = $1
		  AND status = 'pending_approval'
		  AND approval_expires_at > now()
		ORDER BY created_at DESC
		LIMIT 1`, sessionID)
	return scanAction(row)
}

func (s *Store) MarkActionApproved(ctx context.Context, actionID string, decision agent.ApprovalDecisionRecord) (agent.ActionRecord, error) {
	return s.markActionDecision(ctx, actionID, decision, agent.ActionStatusApproved, contracts.ApprovalStatusApproved)
}

func (s *Store) MarkActionRejected(ctx context.Context, actionID string, decision agent.ApprovalDecisionRecord) (agent.ActionRecord, error) {
	return s.markActionDecision(ctx, actionID, decision, agent.ActionStatusRejected, contracts.ApprovalStatusRejected)
}

func (s *Store) MarkActionExpired(ctx context.Context, actionID string) (agent.ActionRecord, error) {
	record, err := s.GetAction(ctx, actionID)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	now := time.Now()
	_, err = s.db.ExecContext(ctx, `
		UPDATE approval_actions
		SET status = 'expired', updated_at = $2
		WHERE action_id = $1 AND status IN ('pending_approval', 'approved')`, actionID, now)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = 'expired', resolved_at = $2
		WHERE approval_id = $1 AND status IN ('pending', 'approved')`, record.ApprovalID, now)
	if err := s.insertAuditEntry(ctx, auditEntry{
		EventID:    record.ApprovalID + ":approval.expired",
		EventType:  "approval.expired",
		RunID:      record.RunID,
		RequestID:  record.RequestID,
		SessionID:  record.SessionID,
		ToolCallID: record.ToolCallID,
		ApprovalID: record.ApprovalID,
		ToolName:   record.ToolName,
		RiskLevel:  string(record.RiskLevel),
		Details:    map[string]any{"actionId": record.ActionID},
		Timestamp:  now,
	}); err != nil {
		return agent.ActionRecord{}, err
	}
	return s.GetAction(ctx, actionID)
}

func (s *Store) TryMarkActionExecuting(ctx context.Context, actionID string) (agent.ActionRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE approval_actions
		SET status = 'executing', updated_at = now()
		WHERE action_id = $1 AND status = 'approved'
		RETURNING action_id`, actionID)
	var returned string
	err := row.Scan(&returned)
	if errors.Is(err, sql.ErrNoRows) {
		record, getErr := s.GetAction(ctx, actionID)
		return record, false, getErr
	}
	if err != nil {
		return agent.ActionRecord{}, false, err
	}
	record, err := s.GetAction(ctx, actionID)
	return record, true, err
}

func (s *Store) CompleteAction(ctx context.Context, actionID string, result tools.ToolResult) (agent.ActionRecord, error) {
	return s.updateActionResult(ctx, actionID, agent.ActionStatusCompleted, result)
}

func (s *Store) FailAction(ctx context.Context, actionID string, result tools.ToolResult) (agent.ActionRecord, error) {
	return s.updateActionResult(ctx, actionID, agent.ActionStatusFailed, result)
}

func (s *Store) RecordToolCall(ctx context.Context, record agent.ToolCallRecord) error {
	args, err := json.Marshal(record.ArgsSnapshot)
	if err != nil {
		return err
	}
	resultJSON := toolResultJSON(record.Result)
	now := zeroNow(record.CreatedAt)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tool_calls (
			run_id, request_id, session_id, tool_call_id, tool_name, args_snapshot,
			status, reason, error_message, result, latency_ms, created_at, updated_at, completed_at
		)
		VALUES ($1, nullif($2, ''), nullif($3, ''), $4, $5, $6::jsonb, $7::text, nullif($8, ''),
		        nullif($9, ''), $10::jsonb, $11::bigint, $12::timestamptz, $12::timestamptz,
		        CASE WHEN $7::text IN ('completed', 'failed') THEN $12::timestamptz ELSE NULL::timestamptz END)
		ON CONFLICT (tool_call_id) DO UPDATE
		SET run_id = EXCLUDED.run_id,
		    request_id = COALESCE(EXCLUDED.request_id, tool_calls.request_id),
		    session_id = COALESCE(EXCLUDED.session_id, tool_calls.session_id),
		    tool_name = EXCLUDED.tool_name,
		    args_snapshot = EXCLUDED.args_snapshot,
		    status = EXCLUDED.status,
		    reason = COALESCE(EXCLUDED.reason, tool_calls.reason),
		    error_message = COALESCE(EXCLUDED.error_message, tool_calls.error_message),
		    result = COALESCE(EXCLUDED.result, tool_calls.result),
		    latency_ms = COALESCE(EXCLUDED.latency_ms, tool_calls.latency_ms),
		    updated_at = EXCLUDED.updated_at,
		    completed_at = COALESCE(EXCLUDED.completed_at, tool_calls.completed_at)`,
		record.RunID, record.RequestID, record.SessionID, record.ToolCallID, record.ToolName,
		string(args), string(record.Status), record.Reason, record.ErrorMessage, resultJSON,
		nullInt64(record.LatencyMS), now)
	if err != nil {
		return err
	}
	eventType := "tool.call.requested"
	if record.Status == agent.ToolCallStatusCompleted {
		eventType = "tool.call.completed"
	} else if record.Status == agent.ToolCallStatusFailed {
		eventType = "tool.call.failed"
	} else if record.Status == agent.ToolCallStatusBlocked {
		eventType = "safety.action.blocked"
	}
	if err := s.insertAuditEntry(ctx, auditEntry{
		EventID:    record.ToolCallID + ":" + eventType,
		EventType:  eventType,
		RunID:      record.RunID,
		RequestID:  record.RequestID,
		SessionID:  record.SessionID,
		ToolCallID: record.ToolCallID,
		ApprovalID: record.ApprovalID,
		ToolName:   record.ToolName,
		Details: map[string]any{
			"status":       string(record.Status),
			"reason":       record.Reason,
			"errorMessage": record.ErrorMessage,
			"latencyMs":    record.LatencyMS,
		},
		ErrorMessage: record.ErrorMessage,
		Timestamp:    now,
	}); err != nil {
		return err
	}
	if record.Status == agent.ToolCallStatusCompleted || record.Status == agent.ToolCallStatusFailed {
		return s.recordToolExecution(ctx, record)
	}
	return nil
}

func (s *Store) RecordRiskDecision(ctx context.Context, record agent.RiskDecisionRecord) error {
	reasons, err := json.Marshal(record.PolicyReasons)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO risk_decisions (
			run_id, request_id, session_id, tool_call_id, tool_name, risk_level,
			decision, requires_approval, reason, policy_reasons, checked_at
		)
		VALUES (nullif($1, ''), nullif($2, ''), nullif($3, ''), $4, $5, $6, $7, $8, nullif($9, ''), $10::jsonb, $11)`,
		record.RunID, record.RequestID, record.SessionID, record.ToolCallID, record.ToolName,
		string(record.RiskLevel), string(record.Decision), record.RequiresApproval, record.Reason,
		string(reasons), zeroNow(record.CheckedAt))
	if err != nil {
		return err
	}
	return s.insertAuditEntry(ctx, auditEntry{
		EventID:        record.ToolCallID + ":safety.risk.checked",
		EventType:      "safety.risk.checked",
		RunID:          record.RunID,
		RequestID:      record.RequestID,
		SessionID:      record.SessionID,
		ToolCallID:     record.ToolCallID,
		ToolName:       record.ToolName,
		RiskLevel:      string(record.RiskLevel),
		PolicyDecision: string(record.Decision),
		PolicyReasons:  record.PolicyReasons,
		Details: map[string]any{
			"requiresApproval": record.RequiresApproval,
			"reason":           record.Reason,
		},
		Timestamp: zeroNow(record.CheckedAt),
	})
}

func (s *Store) UpsertToolRegistryEntries(ctx context.Context, definitions []tools.ToolDefinition) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	for _, definition := range definitions {
		parameters, err := json.Marshal(definition.Parameters)
		if err != nil {
			return err
		}
		timeoutMS := sql.NullInt64{}
		if definition.Timeout > 0 {
			timeoutMS = sql.NullInt64{Int64: definition.Timeout.Milliseconds(), Valid: true}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO tool_registry_entries (
				name, owner, description, parameters, capability, risk_level,
				requires_approval, timeout_ms, enabled, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, now(), now())
			ON CONFLICT (name) DO UPDATE
			SET owner = EXCLUDED.owner,
			    description = EXCLUDED.description,
			    parameters = EXCLUDED.parameters,
			    capability = EXCLUDED.capability,
			    risk_level = EXCLUDED.risk_level,
			    requires_approval = EXCLUDED.requires_approval,
			    timeout_ms = EXCLUDED.timeout_ms,
			    enabled = EXCLUDED.enabled,
			    updated_at = now()`,
			definition.Name, definition.Owner, definition.Description, string(parameters),
			string(definition.Capability), string(definition.RiskLevel), definition.RequiresApproval,
			timeoutMS, definition.Enabled)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AppendRunEvent is a no-op for the PostgreSQL store. The run-event stream is
// not part of the persistence schema (see docs/05-erd.md); durable run/tool/
// approval lifecycle is traced through audit_entries instead. The File and
// in-memory state stores keep this stream for local debugging.
func (s *Store) AppendRunEvent(_ context.Context, _ agent.RunEvent) error {
	return nil
}

func (s *Store) Log(event audit.AuditEvent) error {
	reasons, _ := json.Marshal(event.PolicyReasons)
	resources, _ := json.Marshal(event.AffectedPaths)
	details, _ := json.Marshal(event)
	_, err := s.db.Exec(`
		INSERT INTO audit_entries (
			event_id, event_type, timestamp, request_id, session_id, tool_call_id,
			approval_id, execution_id, channel, actor_ref, tool_name, action_type,
			risk_level, policy_decision, policy_reasons, affected_resources, details,
			output_summary, error_message
		)
		VALUES (nullif($1, ''), nullif($2, ''), $3, nullif($4, ''), nullif($5, ''), nullif($6, ''),
		        nullif($7, ''), nullif($8, ''), nullif($9, ''), nullif($10, ''), nullif($11, ''),
		        nullif($12, ''), nullif($13, ''), nullif($14, ''), $15::jsonb, $16::jsonb,
		        $17::jsonb, nullif($18, ''), nullif($19, ''))
		ON CONFLICT (event_id) WHERE event_id IS NOT NULL AND event_id <> '' DO NOTHING`,
		event.EventID, string(event.EventType), zeroNow(event.Timestamp), event.RequestID, event.SessionID,
		"", event.HITLApprovalID, event.JobID, "", event.UserID, event.Tool, string(event.ActionType),
		event.RiskLevel, event.PolicyDecision, string(reasons), string(resources), string(details),
		event.OutputSummary, event.ErrorMessage)
	return err
}

func (s *Store) Query(filter audit.Filter) ([]audit.AuditEvent, error) {
	query := `SELECT details FROM audit_entries WHERE true`
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		query += fmt.Sprintf(" AND %s $%d", clause, len(args))
	}
	if filter.RequestID != "" {
		add("request_id =", filter.RequestID)
	}
	if filter.SessionID != "" {
		add("session_id =", filter.SessionID)
	}
	if filter.EventType != "" {
		add("event_type =", string(filter.EventType))
	}
	if filter.RiskLevel != "" {
		add("risk_level =", filter.RiskLevel)
	}
	if !filter.Since.IsZero() {
		add("timestamp >=", filter.Since)
	}
	if !filter.Until.IsZero() {
		add("timestamp <=", filter.Until)
	}
	query += " ORDER BY timestamp, id"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audit.AuditEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event audit.AuditEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			continue
		}
		if filter.Status != "" && event.Status != filter.Status {
			continue
		}
		if filter.UserID != "" && event.UserID != filter.UserID {
			continue
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) ListToolCallsByRun(ctx context.Context, runID string) ([]agent.ToolCallRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tool_call_id, run_id, COALESCE(request_id, ''), COALESCE(session_id, ''), tool_name,
		       args_snapshot, status, COALESCE(reason, ''), COALESCE(error_message, ''), result,
		       COALESCE(latency_ms, 0), created_at
		FROM tool_calls
		WHERE run_id = $1
		ORDER BY created_at, id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []agent.ToolCallRecord
	for rows.Next() {
		record, err := scanToolCallRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) upsertRun(ctx context.Context, state agent.RunState) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_runs (
			run_id, request_id, session_id, channel, input_text, original_goal, status,
			iteration_count, pending_action_id, pending_clarification_id, created_at, updated_at, completed_at
		)
		VALUES ($1, $2, $3, '', $4, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (run_id) DO UPDATE
		SET request_id = EXCLUDED.request_id,
		    session_id = EXCLUDED.session_id,
		    original_goal = COALESCE(NULLIF(EXCLUDED.original_goal, ''), agent_runs.original_goal),
		    status = EXCLUDED.status,
		    iteration_count = EXCLUDED.iteration_count,
		    pending_action_id = EXCLUDED.pending_action_id,
		    pending_clarification_id = EXCLUDED.pending_clarification_id,
		    updated_at = EXCLUDED.updated_at,
		    completed_at = EXCLUDED.completed_at`,
		state.RunID, state.RequestID, state.SessionID, state.OriginalGoal, string(state.Status),
		state.IterationCount, state.PendingActionID, state.PendingClarificationID,
		zeroNow(state.CreatedAt), zeroNow(state.UpdatedAt), state.CompletedAt)
	return err
}

func (s *Store) getActionByQuery(ctx context.Context, where string, arg any) (agent.ActionRecord, error) {
	row := s.db.QueryRowContext(ctx, actionSelectSQL()+" WHERE "+where, arg)
	return scanAction(row)
}

func (s *Store) markActionDecision(ctx context.Context, actionID string, decision agent.ApprovalDecisionRecord, status agent.ActionStatus, approvalStatus contracts.ApprovalStatus) (agent.ActionRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	defer rollback(tx)
	record, err := getActionTx(ctx, tx, actionID)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	now := zeroNow(decision.DecidedAt)
	_, err = tx.ExecContext(ctx, `
		UPDATE approval_actions
		SET status = $2, updated_at = $3
		WHERE action_id = $1 AND status IN ('pending_approval', 'approved')`, actionID, string(status), now)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = $2, resolved_at = $3
		WHERE approval_id = $1`, record.ApprovalID, string(approvalStatus), now)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_decisions (approval_id, run_id, request_id, session_id, decision, decided_by, channel, comment, decided_at)
		VALUES ($1, $2, $3, $4, $5, nullif($6, ''), nullif($7, ''), nullif($8, ''), $9)`,
		record.ApprovalID, record.RunID, coalesce(decision.RequestID, record.RequestID),
		coalesce(decision.SessionID, record.SessionID), string(decision.Decision), decision.DecidedBy,
		decision.Channel, decision.Comment, now)
	if err != nil {
		return agent.ActionRecord{}, err
	}
	if status == agent.ActionStatusRejected {
		_, err = tx.ExecContext(ctx, `
			UPDATE tool_calls
			SET status = 'blocked',
			    error_message = 'approval rejected',
			    updated_at = $2,
			    completed_at = $2
			WHERE tool_call_id = $1
			  AND status = 'requires_approval'`, record.ToolCallID, now)
		if err != nil {
			return agent.ActionRecord{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return agent.ActionRecord{}, err
	}
	eventType := "approval.approved"
	if status == agent.ActionStatusRejected {
		eventType = "approval.rejected"
	}
	if err := s.insertAuditEntry(ctx, auditEntry{
		EventID:    record.ApprovalID + ":" + eventType,
		EventType:  eventType,
		RunID:      record.RunID,
		RequestID:  coalesce(decision.RequestID, record.RequestID),
		SessionID:  coalesce(decision.SessionID, record.SessionID),
		ToolCallID: record.ToolCallID,
		ApprovalID: record.ApprovalID,
		ToolName:   record.ToolName,
		RiskLevel:  string(record.RiskLevel),
		Details: map[string]any{
			"actionId":  record.ActionID,
			"decision":  string(decision.Decision),
			"decidedBy": decision.DecidedBy,
			"comment":   decision.Comment,
		},
		Timestamp: zeroNow(decision.DecidedAt),
	}); err != nil {
		return agent.ActionRecord{}, err
	}
	return s.GetAction(ctx, actionID)
}

func (s *Store) updateActionResult(ctx context.Context, actionID string, status agent.ActionStatus, result tools.ToolResult) (agent.ActionRecord, error) {
	_, err := s.db.ExecContext(ctx, `
		UPDATE approval_actions
		SET status = $2, result = $3::jsonb, updated_at = now()
		WHERE action_id = $1`, actionID, string(status), toolResultJSON(&result))
	if err != nil {
		return agent.ActionRecord{}, err
	}
	return s.GetAction(ctx, actionID)
}

func (s *Store) recordToolExecution(ctx context.Context, record agent.ToolCallRecord) error {
	resultSuccess := sql.NullBool{}
	resultData := "null"
	errorData := "null"
	status := "completed"
	if record.Status == agent.ToolCallStatusFailed {
		status = "failed"
	}
	if record.Result != nil {
		resultSuccess.Valid = true
		resultSuccess.Bool = record.Result.Success
		data, _ := json.Marshal(map[string]any{
			"contentForLLM":  record.Result.ContentForLLM,
			"contentForUser": record.Result.ContentForUser,
		})
		resultData = string(data)
		if record.Result.Error != nil {
			errJSON, _ := json.Marshal(record.Result.Error)
			errorData = string(errJSON)
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_executions (
			run_id, request_id, session_id, tool_call_id, approval_id, tool_name,
			input, execution_status, result_success, result_data, error, requested_at, started_at, completed_at
		)
		VALUES (nullif($1, ''), nullif($2, ''), nullif($3, ''), $4, nullif($5, ''), $6,
		        $7::jsonb, $8, $9, $10::jsonb, $11::jsonb, $12, $12, $12)
		ON CONFLICT (tool_call_id) DO UPDATE
		SET run_id = COALESCE(EXCLUDED.run_id, tool_executions.run_id),
		    request_id = COALESCE(EXCLUDED.request_id, tool_executions.request_id),
		    session_id = COALESCE(EXCLUDED.session_id, tool_executions.session_id),
		    approval_id = COALESCE(EXCLUDED.approval_id, tool_executions.approval_id),
		    execution_status = EXCLUDED.execution_status,
		    result_success = EXCLUDED.result_success,
		    result_data = EXCLUDED.result_data,
		    error = EXCLUDED.error,
		    completed_at = EXCLUDED.completed_at`,
		record.RunID, record.RequestID, record.SessionID, record.ToolCallID, record.ApprovalID,
		record.ToolName, mustJSON(record.ArgsSnapshot), status, resultSuccess, resultData, errorData, zeroNow(record.CreatedAt))
	return err
}

func scanRunState(scanner interface{ Scan(dest ...any) error }) (agent.RunState, error) {
	var state agent.RunState
	var status string
	var completedAt sql.NullTime
	err := scanner.Scan(&state.RunID, &state.SessionID, &state.RequestID, &state.OriginalGoal,
		&status, &state.IterationCount, &state.PendingActionID, &state.PendingClarificationID,
		&state.CreatedAt, &state.UpdatedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.RunState{}, agent.ErrRuntimeStateNotFound
	}
	if err != nil {
		return agent.RunState{}, err
	}
	state.Status = agent.RuntimeRunStatus(status)
	if completedAt.Valid {
		state.CompletedAt = &completedAt.Time
	}
	return state, nil
}

func actionSelectSQL() string {
	return `
		SELECT action_id, run_id, request_id, session_id, tool_call_id, tool_name, args_snapshot,
		       risk_level, status, approval_id, approval_expires_at, COALESCE(idempotency_key, ''),
		       result, created_at, updated_at
		FROM approval_actions`
}

func scanAction(scanner interface{ Scan(dest ...any) error }) (agent.ActionRecord, error) {
	var record agent.ActionRecord
	var argsRaw, resultRaw []byte
	var riskLevel, status string
	err := scanner.Scan(&record.ActionID, &record.RunID, &record.RequestID, &record.SessionID,
		&record.ToolCallID, &record.ToolName, &argsRaw, &riskLevel, &status, &record.ApprovalID,
		&record.ApprovalExpiresAt, &record.IdempotencyKey, &resultRaw, &record.CreatedAt, &record.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.ActionRecord{}, agent.ErrRuntimeStateNotFound
	}
	if err != nil {
		return agent.ActionRecord{}, err
	}
	record.RiskLevel = contracts.RiskLevel(riskLevel)
	record.Status = agent.ActionStatus(status)
	if len(argsRaw) > 0 {
		_ = json.Unmarshal(argsRaw, &record.ArgsSnapshot)
	}
	if len(resultRaw) > 0 && string(resultRaw) != "null" {
		var result tools.ToolResult
		if err := json.Unmarshal(resultRaw, &result); err == nil {
			record.Result = &result
		}
	}
	return record, nil
}

func scanToolCallRecord(scanner interface{ Scan(dest ...any) error }) (agent.ToolCallRecord, error) {
	var record agent.ToolCallRecord
	var argsRaw, resultRaw []byte
	var status string
	if err := scanner.Scan(&record.ToolCallID, &record.RunID, &record.RequestID, &record.SessionID,
		&record.ToolName, &argsRaw, &status, &record.Reason, &record.ErrorMessage, &resultRaw,
		&record.LatencyMS, &record.CreatedAt); err != nil {
		return agent.ToolCallRecord{}, err
	}
	record.Status = agent.ToolCallStatus(status)
	if len(argsRaw) > 0 {
		_ = json.Unmarshal(argsRaw, &record.ArgsSnapshot)
	}
	if len(resultRaw) > 0 && string(resultRaw) != "null" {
		var result tools.ToolResult
		if err := json.Unmarshal(resultRaw, &result); err == nil {
			record.Result = &result
		}
	}
	return record, nil
}

func getActionTx(ctx context.Context, tx *sql.Tx, actionID string) (agent.ActionRecord, error) {
	row := tx.QueryRowContext(ctx, actionSelectSQL()+` WHERE action_id = $1`, actionID)
	return scanAction(row)
}

func supersedePendingApprovalsTx(ctx context.Context, tx *sql.Tx, sessionID string, exceptActionID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT action_id, run_id
		FROM approval_actions
		WHERE session_id = $1 AND status = 'pending_approval' AND action_id <> $2`, sessionID, exceptActionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pending struct{ actionID, runID string }
	var records []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.actionID, &item.runID); err != nil {
			return err
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			UPDATE approval_actions
			SET status = 'superseded', updated_at = now()
			WHERE action_id = $1`, record.actionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE approval_requests
			SET status = 'cancelled', resolved_at = now()
			WHERE action_id = $1`, record.actionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_runs
			SET status = 'blocked', pending_action_id = '', pending_clarification_id = '',
			    updated_at = now(), completed_at = now()
			WHERE run_id = $1 AND status = 'waiting_approval' AND pending_action_id = $2`, record.runID, record.actionID); err != nil {
			return err
		}
	}
	return nil
}

func upsertApprovalRequestTx(ctx context.Context, tx *sql.Tx, record agent.ActionRecord, status contracts.ApprovalStatus) error {
	toolCall := contracts.ToolCall{
		ToolCallID: record.ToolCallID,
		RequestID:  record.RequestID,
		SessionID:  record.SessionID,
		ToolName:   record.ToolName,
		Input:      record.ArgsSnapshot,
	}
	toolCallJSON, err := json.Marshal(toolCall)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_requests (
			approval_id, action_id, run_id, request_id, session_id, tool_call_id,
			status, risk_level, summary, details, tool_call, created_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, nullif($10, ''), $11::jsonb, $12, $13)
		ON CONFLICT (approval_id) DO UPDATE
		SET action_id = EXCLUDED.action_id,
		    run_id = EXCLUDED.run_id,
		    status = EXCLUDED.status,
		    risk_level = EXCLUDED.risk_level,
		    summary = EXCLUDED.summary,
		    details = EXCLUDED.details,
		    tool_call = EXCLUDED.tool_call,
		    expires_at = EXCLUDED.expires_at`,
		record.ApprovalID, record.ActionID, record.RunID, record.RequestID, record.SessionID,
		record.ToolCallID, string(status), string(record.RiskLevel), record.ApprovalSummary,
		record.ApprovalDetails, string(toolCallJSON), zeroNow(record.CreatedAt), record.ApprovalExpiresAt)
	return err
}

func toolResultJSON(result *tools.ToolResult) string {
	if result == nil {
		return "null"
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "null"
	}
	return string(data)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func zeroNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

func nullInt64(value int64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type auditEntry struct {
	EventID        string
	EventType      string
	RunID          string
	RequestID      string
	SessionID      string
	ToolCallID     string
	ApprovalID     string
	ExecutionID    string
	Channel        string
	ActorRef       string
	ToolName       string
	ActionType     string
	RiskLevel      string
	PolicyDecision string
	PolicyReasons  []string
	Resources      []string
	Details        map[string]any
	OutputSummary  string
	ErrorMessage   string
	Timestamp      time.Time
}

func (s *Store) insertAuditEntry(ctx context.Context, entry auditEntry) error {
	policyReasons, err := json.Marshal(entry.PolicyReasons)
	if err != nil {
		return err
	}
	resources, err := json.Marshal(entry.Resources)
	if err != nil {
		return err
	}
	details, err := json.Marshal(entry.Details)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_entries (
			event_id, event_type, run_id, request_id, session_id, tool_call_id,
			approval_id, execution_id, channel, actor_ref, tool_name, action_type,
			risk_level, policy_decision, policy_reasons, affected_resources, details,
			output_summary, error_message, timestamp
		)
		VALUES (nullif($1, ''), nullif($2, ''), nullif($3, ''), nullif($4, ''), nullif($5, ''),
		        nullif($6, ''), nullif($7, ''), nullif($8, ''), nullif($9, ''), nullif($10, ''),
		        nullif($11, ''), nullif($12, ''), nullif($13, ''), nullif($14, ''), $15::jsonb,
		        $16::jsonb, $17::jsonb, nullif($18, ''), nullif($19, ''), $20)
		ON CONFLICT (event_id) WHERE event_id IS NOT NULL AND event_id <> '' DO NOTHING`,
		entry.EventID, entry.EventType, entry.RunID, entry.RequestID, entry.SessionID, entry.ToolCallID,
		entry.ApprovalID, entry.ExecutionID, entry.Channel, entry.ActorRef, entry.ToolName, entry.ActionType,
		entry.RiskLevel, entry.PolicyDecision, string(policyReasons), string(resources), string(details),
		entry.OutputSummary, entry.ErrorMessage, zeroNow(entry.Timestamp))
	return err
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

var _ agent.RuntimeStateStore = (*Store)(nil)
var _ audit.AuditEventLogger = (*Store)(nil)
