package monitoring

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestQueryLogsPopulatesTraceURLForToolCall(t *testing.T) {
	databaseURL := os.Getenv("VCLAW_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set VCLAW_TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL monitoring tests")
	}

	t.Setenv("LANGFUSE_HOST", "https://us.cloud.langfuse.com")
	t.Setenv("LANGFUSE_PROJECT_ID", "proj_123")

	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema := "vclaw_monitoring_test_" + time.Now().UTC().Format("20060102t150405000000")
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	for _, name := range []string{"001_init_vclaw_schema.sql", "002_persistence_runtime_state.sql"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
	queryURL := databaseURL
	if strings.Contains(queryURL, "?") {
		queryURL += "&search_path=" + schema
	} else {
		queryURL += "?search_path=" + schema
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	runID := "run_sess_monitoring_req_monitoring"
	requestID := "req_monitoring"
	sessionID := "sess_monitoring"
	traceID := "trace_monitoring_123"
	toolCallID := "toolcall_monitoring"
	toolName := "calendar.createEvent"
	approvalID := "approval_monitoring"
	auditID := "audit_monitoring"

	if _, err := db.ExecContext(ctx, `
		INSERT INTO agent_runs (run_id, request_id, session_id, input_text, original_goal, status, iteration_count, pending_action_id, pending_clarification_id, created_at, updated_at, started_at, completed_at, data, plan)
		VALUES ($1, $2, $3, $4, $5, 'completed', 0, '', '', $6, $6, $6, $6, jsonb_build_object('trace_id', $7), '{}'::jsonb)`,
		runID, requestID, sessionID, "create a calendar event", "create a calendar event", now, traceID); err != nil {
		t.Fatalf("insert agent run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tool_executions (run_id, request_id, session_id, tool_call_id, tool_name, input, execution_status, result_success, result_data, error, requested_at, started_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, 'completed', true, jsonb_build_object('contentForUser', 'done', 'contentForLLM', 'technical'), NULL, $6, $6, $6)`,
		runID, requestID, sessionID, toolCallID, toolName, now); err != nil {
		t.Fatalf("insert tool execution: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO approval_requests (approval_id, run_id, request_id, session_id, tool_call_id, status, risk_level, summary, details, tool_call, created_at, expires_at, resolved_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', 'external_write', 'approve calendar event', 'details', jsonb_build_object('toolName', $6), $7, $7, NULL)`,
		approvalID, runID, requestID, sessionID, toolCallID, toolName, now); err != nil {
		t.Fatalf("insert approval request: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO approval_decisions (approval_id, run_id, request_id, decision, decided_by, decided_at, comment)
		VALUES ($1, $2, $3, 'approved', 'tester', $4, 'ok')`,
		approvalID, runID, requestID, now); err != nil {
		t.Fatalf("insert approval decision: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO audit_entries (event_id, timestamp, request_id, session_id, run_id, tool_call_id, approval_id, tool_name, policy_decision, output_summary, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'blocked', 'audit summary', 'boom')`,
		auditID, now, requestID, sessionID, runID, toolCallID, approvalID, toolName); err != nil {
		t.Fatalf("insert audit entry: %v", err)
	}

	events, err := QueryLogs(ctx, queryURL, LogQuery{
		Limit: 10,
		Since: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("len(events) = %d, want 5", len(events))
	}
	wantURL := "https://us.cloud.langfuse.com/project/proj_123/traces/" + traceID
	for _, event := range events {
		if event.EventType == "run" || event.EventType == "tool_call" || event.EventType == "approval_request" || event.EventType == "approval_decision" || event.EventType == "error" {
			if event.TraceID != traceID {
				t.Fatalf("TraceID for %s = %q, want %q", event.EventType, event.TraceID, traceID)
			}
			if event.TraceURL != wantURL {
				t.Fatalf("TraceURL for %s = %q, want %q", event.EventType, event.TraceURL, wantURL)
			}
		}
	}

	latest, err := QueryLatestRunForSession(ctx, queryURL, sessionID)
	if err != nil {
		t.Fatalf("QueryLatestRunForSession: %v", err)
	}
	if latest.TraceID != traceID {
		t.Fatalf("Latest TraceID = %q, want %q", latest.TraceID, traceID)
	}
	if latest.TraceURL != wantURL {
		t.Fatalf("Latest TraceURL = %q, want %q", latest.TraceURL, wantURL)
	}
}
