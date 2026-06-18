package monitoring

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/traceutil"
)

type LogQuery struct {
	Limit int
	Since time.Time
	Level string
	Tool  string
	RequestID string
	SessionID string
}

type LogEvent struct {
	Timestamp  time.Time
	Level      string
	EventType  string
	Status     string
	TraceID    string
	TraceURL   string
	RequestID  string
	SessionID  string
	ToolName   string
	ApprovalID string
	Message    string
	Error      string
}

type ApprovalQuery struct {
	Status string
	Since  time.Time
	Tool   string
	Limit  int
}

type ApprovalRecord struct {
	ApprovalID string
	ToolName   string
	RiskLevel  string
	Status     string
	CreatedAt  time.Time
	DecidedAt  *time.Time
}

func QueryLogs(ctx context.Context, databaseURL string, query LogQuery) ([]LogEvent, error) {
	db, err := openAuditDB(databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tables, err := availableAuditTables(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return []LogEvent{}, nil
	}

	parts := make([]string, 0, 5)
	args := []any{nullableTime(query.Since)}
	levelFilter := strings.ToLower(strings.TrimSpace(query.Level))
	toolFilter := strings.TrimSpace(query.Tool)
	requestIDFilter := strings.TrimSpace(query.RequestID)
	sessionIDFilter := strings.TrimSpace(query.SessionID)
	argLevel := len(args) + 1
	args = append(args, nullableString(levelFilter))
	argTool := len(args) + 1
	args = append(args, nullableString(toolFilter))
	argRequestID := len(args) + 1
	args = append(args, nullableString(requestIDFilter))
	argSessionID := len(args) + 1
	args = append(args, nullableString(sessionIDFilter))

	if tables["agent_runs"] {
		parts = append(parts, fmt.Sprintf(`SELECT ar.started_at AS ts,
CASE WHEN ar.status IN ('failed', 'blocked', 'max_iterations') THEN 'error' ELSE 'info' END AS level,
'run' AS event_type,
COALESCE(ar.status, '') AS status,
COALESCE(ar.data->>'trace_id', '') AS trace_id,
ar.request_id,
ar.session_id,
'' AS tool_name,
'' AS approval_id,
COALESCE(ar.original_goal, '') AS message,
'' AS error_text
FROM agent_runs ar
WHERE ($1::timestamptz IS NULL OR ar.started_at >= $1)
  AND ($%d::text IS NULL OR CASE WHEN ar.status IN ('failed', 'blocked', 'max_iterations') THEN 'error' ELSE 'info' END = $%d)
  AND ($%d::text IS NULL OR ar.request_id = $%d)
  AND ($%d::text IS NULL OR ar.session_id = $%d)
`, argLevel, argLevel, argRequestID, argRequestID, argSessionID, argSessionID))
	}
	if tables["tool_executions"] {
		parts = append(parts, fmt.Sprintf(`SELECT te.requested_at AS ts,
CASE WHEN te.execution_status = 'failed' OR te.error IS NOT NULL OR te.result_success = false THEN 'error' ELSE 'info' END AS level,
'tool_call' AS event_type,
te.execution_status AS status,
COALESCE(ar.data->>'trace_id', '') AS trace_id,
COALESCE(te.request_id, '') AS request_id,
COALESCE(te.session_id, '') AS session_id,
te.tool_name,
te.tool_call_id AS approval_id,
COALESCE(te.result_data->>'contentForUser', '') AS message,
COALESCE(te.error::text, '') AS error_text
FROM tool_executions te
LEFT JOIN agent_runs ar ON ar.run_id = te.run_id
WHERE ($1::timestamptz IS NULL OR te.requested_at >= $1)
  AND ($%d::text IS NULL OR CASE WHEN te.execution_status = 'failed' OR te.error IS NOT NULL OR te.result_success = false THEN 'error' ELSE 'info' END = $%d)
  AND ($%d::text IS NULL OR te.tool_name = $%d)
  AND ($%d::text IS NULL OR te.request_id = $%d)
  AND ($%d::text IS NULL OR te.session_id = $%d)
`, argLevel, argLevel, argTool, argTool, argRequestID, argRequestID, argSessionID, argSessionID))
	}
	if tables["approval_requests"] {
		parts = append(parts, fmt.Sprintf(`SELECT apr.created_at AS ts,
'info' AS level,
'approval_request' AS event_type,
apr.status,
COALESCE(ar.data->>'trace_id', '') AS trace_id,
apr.request_id,
apr.session_id,
COALESCE(apr.tool_call->>'toolName', '') AS tool_name,
apr.approval_id,
apr.summary AS message,
'' AS error_text
FROM approval_requests apr
LEFT JOIN agent_runs ar ON ar.run_id = apr.run_id
WHERE ($1::timestamptz IS NULL OR apr.created_at >= $1)
  AND ($%d::text IS NULL OR $%d = 'info')
  AND ($%d::text IS NULL OR COALESCE(apr.tool_call->>'toolName', '') = $%d)
  AND ($%d::text IS NULL OR apr.request_id = $%d)
  AND ($%d::text IS NULL OR apr.session_id = $%d)
`, argLevel, argLevel, argTool, argTool, argRequestID, argRequestID, argSessionID, argSessionID))
	}
	if tables["approval_decisions"] {
		parts = append(parts, fmt.Sprintf(`SELECT ad.decided_at AS ts,
'info' AS level,
'approval_decision' AS event_type,
ad.decision AS status,
COALESCE(ar.data->>'trace_id', '') AS trace_id,
ad.request_id,
'' AS session_id,
'' AS tool_name,
ad.approval_id,
COALESCE(ad.comment, '') AS message,
'' AS error_text
FROM approval_decisions ad
LEFT JOIN agent_runs ar ON ar.run_id = ad.run_id
WHERE ($1::timestamptz IS NULL OR ad.decided_at >= $1)
  AND ($%d::text IS NULL OR $%d = 'info')
  AND ($%d::text IS NULL OR ad.request_id = $%d)
`, argLevel, argLevel, argRequestID, argRequestID))
	}
	if tables["audit_entries"] {
		parts = append(parts, fmt.Sprintf(`SELECT ae.timestamp AS ts,
'error' AS level,
'error' AS event_type,
COALESCE(ae.policy_decision, '') AS status,
COALESCE(ar.data->>'trace_id', '') AS trace_id,
COALESCE(ae.request_id, '') AS request_id,
COALESCE(ae.session_id, '') AS session_id,
COALESCE(ae.tool_name, '') AS tool_name,
COALESCE(ae.approval_id, '') AS approval_id,
COALESCE(ae.output_summary, '') AS message,
ae.error_message AS error_text
FROM audit_entries ae
LEFT JOIN agent_runs ar ON ar.run_id = ae.run_id
WHERE ae.error_message IS NOT NULL AND ae.error_message <> ''
  AND ($1::timestamptz IS NULL OR ae.timestamp >= $1)
  AND ($%d::text IS NULL OR $%d = 'error')
  AND ($%d::text IS NULL OR ae.tool_name = $%d)
  AND ($%d::text IS NULL OR ae.request_id = $%d)
  AND ($%d::text IS NULL OR ae.session_id = $%d)
`, argLevel, argLevel, argTool, argTool, argRequestID, argRequestID, argSessionID, argSessionID))
	}

	if len(parts) == 0 {
		return []LogEvent{}, nil
	}

	argLimit := len(args) + 1
	args = append(args, sanitizeQueryLimit(query.Limit, 50))
	sqlQuery := strings.Join(parts, "\nUNION ALL\n") + fmt.Sprintf("\nORDER BY ts DESC LIMIT $%d", argLimit)
	rows, err := db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	var events []LogEvent
	for rows.Next() {
		var event LogEvent
		if err := rows.Scan(&event.Timestamp, &event.Level, &event.EventType, &event.Status, &event.TraceID, &event.RequestID, &event.SessionID, &event.ToolName, &event.ApprovalID, &event.Message, &event.Error); err != nil {
			return nil, fmt.Errorf("scan logs: %w", err)
		}
		event.TraceURL = traceutil.BuildTraceURL(event.TraceID)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
	}
	return events, nil
}

type LatestRun struct {
	RunID     string
	RequestID string
	SessionID string
	OriginalGoal string
	Status    string
	TraceID   string
	TraceURL  string
	StartedAt time.Time
	CompletedAt *time.Time
}

func QueryLatestRun(ctx context.Context, databaseURL string) (LatestRun, error) {
	return QueryLatestRunForSession(ctx, databaseURL, "")
}

func QueryLatestRunForSession(ctx context.Context, databaseURL string, sessionID string) (LatestRun, error) {
	runs, err := QueryRecentRunsForSession(ctx, databaseURL, sessionID, 1)
	if err != nil {
		return LatestRun{}, err
	}
	if len(runs) == 0 {
		return LatestRun{}, nil
	}
	return runs[0], nil
}

func QueryRecentRunsForSession(ctx context.Context, databaseURL string, sessionID string, limit int) ([]LatestRun, error) {
	db, err := openAuditDB(databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tables, err := availableAuditTables(ctx, db)
	if err != nil {
		return nil, err
	}
	if !tables["agent_runs"] {
		return []LatestRun{}, nil
	}

	rows, err := db.QueryContext(ctx, `SELECT run_id, COALESCE(request_id, ''), COALESCE(session_id, ''), COALESCE(original_goal, ''), COALESCE(status, ''), COALESCE(data->>'trace_id', '') AS trace_id, started_at, completed_at
		FROM agent_runs
		WHERE ($1::text IS NULL OR session_id = $1)
		ORDER BY started_at DESC
		LIMIT $2`, nullableString(strings.TrimSpace(sessionID)), sanitizeQueryLimit(limit, 5))
	if err != nil {
		return nil, fmt.Errorf("query recent runs: %w", err)
	}
	defer rows.Close()

	runs := []LatestRun{}
	for rows.Next() {
		var (
			run LatestRun
			completedAt sql.NullTime
		)
		if err := rows.Scan(&run.RunID, &run.RequestID, &run.SessionID, &run.OriginalGoal, &run.Status, &run.TraceID, &run.StartedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan recent runs: %w", err)
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}
		run.TraceURL = traceutil.BuildTraceURL(run.TraceID)
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent runs: %w", err)
	}
	return runs, nil
}

func QueryApprovals(ctx context.Context, databaseURL string, query ApprovalQuery) ([]ApprovalRecord, error) {
	db, err := openAuditDB(databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tables, err := availableAuditTables(ctx, db)
	if err != nil {
		return nil, err
	}
	if !tables["approval_requests"] {
		return []ApprovalRecord{}, nil
	}

	statusFilter := strings.ToLower(strings.TrimSpace(query.Status))
	toolFilter := strings.TrimSpace(query.Tool)
	limit := sanitizeQueryLimit(query.Limit, 20)
	args := []any{nullableTime(query.Since)}
	argStatus := len(args) + 1
	args = append(args, nullableString(statusFilter))
	argTool := len(args) + 1
	args = append(args, nullableString(toolFilter))
	argLimit := len(args) + 1
	args = append(args, limit)
	sqlQuery := fmt.Sprintf(`SELECT
    ar.approval_id,
    COALESCE(ar.tool_call->>'toolName', '') AS tool_name,
    ar.risk_level,
    CASE
        WHEN COALESCE(ad.decision, '') = 'revised' THEN 'revised'
        ELSE ar.status
    END AS status,
    ar.created_at,
    ad.decided_at
FROM approval_requests ar
LEFT JOIN LATERAL (
    SELECT decision, decided_at
    FROM approval_decisions
    WHERE approval_id = ar.approval_id
    ORDER BY decided_at DESC
    LIMIT 1
) ad ON true
WHERE ($1::timestamptz IS NULL OR ar.created_at >= $1)
  AND ($%d::text IS NULL OR CASE
        WHEN COALESCE(ad.decision, '') = 'revised' THEN 'revised'
        ELSE ar.status
    END = $%d)
  AND ($%d::text IS NULL OR COALESCE(ar.tool_call->>'toolName', '') = $%d)
ORDER BY ar.created_at DESC
LIMIT $%d`, argStatus, argStatus, argTool, argTool, argLimit)
	rows, err := db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		if isMissingRelationError(err) {
			return []ApprovalRecord{}, nil
		}
		return nil, fmt.Errorf("query approvals: %w", err)
	}
	defer rows.Close()

	var approvals []ApprovalRecord
	for rows.Next() {
		var (
			record    ApprovalRecord
			decidedAt sql.NullTime
		)
		if err := rows.Scan(&record.ApprovalID, &record.ToolName, &record.RiskLevel, &record.Status, &record.CreatedAt, &decidedAt); err != nil {
			return nil, fmt.Errorf("scan approvals: %w", err)
		}
		if decidedAt.Valid {
			record.DecidedAt = &decidedAt.Time
		}
		approvals = append(approvals, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate approvals: %w", err)
	}
	return approvals, nil
}

func openAuditDB(databaseURL string) (*sql.DB, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	return db, nil
}

func availableAuditTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ANY($1)`,
		[]string{"agent_runs", "tool_executions", "approval_requests", "approval_decisions", "audit_entries"})
	if err != nil {
		return nil, fmt.Errorf("list audit tables: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("scan audit table: %w", err)
		}
		out[table] = true
	}
	return out, rows.Err()
}

func sanitizeQueryLimit(limit int, fallback int) int {
	if limit <= 0 {
		limit = fallback
	}
	if limit > 200 {
		limit = 200
	}
	return limit
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func isMissingRelationError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "does not exist")
}
