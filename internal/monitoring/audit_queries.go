package monitoring

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type LogQuery struct {
	Limit int
	Since time.Time
	Level string
	Tool  string
}

type LogEvent struct {
	Timestamp  time.Time
	Level      string
	EventType  string
	Status     string
	RequestID  string
	SessionID  string
	ToolName   string
	ApprovalID string
	Message    string
	Error      string
}

type ApprovalQuery struct {
	Status string
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
	argLevel := len(args) + 1
	args = append(args, nullableString(levelFilter))
	argTool := len(args) + 1
	args = append(args, nullableString(toolFilter))

	if tables["agent_runs"] {
		parts = append(parts, fmt.Sprintf(`SELECT started_at AS ts,
CASE WHEN response_status = 'failed' OR error IS NOT NULL THEN 'error' ELSE 'info' END AS level,
'run' AS event_type,
COALESCE(response_status, '') AS status,
request_id,
session_id,
'' AS tool_name,
'' AS approval_id,
COALESCE(response_message, '') AS message,
COALESCE(error::text, '') AS error_text
FROM agent_runs
WHERE ($1::timestamptz IS NULL OR started_at >= $1)
  AND ($%d::text IS NULL OR CASE WHEN response_status = 'failed' OR error IS NOT NULL THEN 'error' ELSE 'info' END = $%d)
`, argLevel, argLevel))
	}
	if tables["tool_executions"] {
		parts = append(parts, fmt.Sprintf(`SELECT requested_at AS ts,
CASE WHEN execution_status = 'failed' OR error IS NOT NULL OR result_success = false THEN 'error' ELSE 'info' END AS level,
'tool_call' AS event_type,
execution_status AS status,
COALESCE(request_id, '') AS request_id,
COALESCE(session_id, '') AS session_id,
tool_name,
tool_call_id AS approval_id,
'' AS message,
COALESCE(error::text, '') AS error_text
FROM tool_executions
WHERE ($1::timestamptz IS NULL OR requested_at >= $1)
  AND ($%d::text IS NULL OR CASE WHEN execution_status = 'failed' OR error IS NOT NULL OR result_success = false THEN 'error' ELSE 'info' END = $%d)
  AND ($%d::text IS NULL OR tool_name = $%d)
`, argLevel, argLevel, argTool, argTool))
	}
	if tables["approval_requests"] {
		parts = append(parts, fmt.Sprintf(`SELECT created_at AS ts,
'info' AS level,
'approval_request' AS event_type,
status,
request_id,
session_id,
COALESCE(tool_call->>'toolName', '') AS tool_name,
approval_id,
summary AS message,
'' AS error_text
FROM approval_requests
WHERE ($1::timestamptz IS NULL OR created_at >= $1)
  AND ($%d::text IS NULL OR $%d = 'info')
  AND ($%d::text IS NULL OR COALESCE(tool_call->>'toolName', '') = $%d)
`, argLevel, argLevel, argTool, argTool))
	}
	if tables["approval_decisions"] {
		parts = append(parts, fmt.Sprintf(`SELECT decided_at AS ts,
'info' AS level,
'approval_decision' AS event_type,
decision AS status,
request_id,
'' AS session_id,
'' AS tool_name,
approval_id,
COALESCE(comment, '') AS message,
'' AS error_text
FROM approval_decisions
WHERE ($1::timestamptz IS NULL OR decided_at >= $1)
  AND ($%d::text IS NULL OR $%d = 'info')
`, argLevel, argLevel))
	}
	if tables["audit_entries"] {
		parts = append(parts, fmt.Sprintf(`SELECT timestamp AS ts,
'error' AS level,
'error' AS event_type,
action_taken AS status,
COALESCE(request_id, '') AS request_id,
COALESCE(session_id, '') AS session_id,
system_op_type AS tool_name,
'' AS approval_id,
output AS message,
error AS error_text
FROM audit_entries
WHERE error IS NOT NULL AND error <> ''
  AND ($1::timestamptz IS NULL OR timestamp >= $1)
  AND ($%d::text IS NULL OR $%d = 'error')
  AND ($%d::text IS NULL OR system_op_type = $%d)
`, argLevel, argLevel, argTool, argTool))
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
		if err := rows.Scan(&event.Timestamp, &event.Level, &event.EventType, &event.Status, &event.RequestID, &event.SessionID, &event.ToolName, &event.ApprovalID, &event.Message, &event.Error); err != nil {
			return nil, fmt.Errorf("scan logs: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
	}
	return events, nil
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
	limit := sanitizeQueryLimit(query.Limit, 20)
	const sqlQuery = `SELECT
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
WHERE ($1::text IS NULL OR CASE
        WHEN COALESCE(ad.decision, '') = 'revised' THEN 'revised'
        ELSE ar.status
    END = $1)
ORDER BY ar.created_at DESC
LIMIT $2`
	rows, err := db.QueryContext(ctx, sqlQuery, nullableString(statusFilter), limit)
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
