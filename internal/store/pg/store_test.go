package pg_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"vclaw/internal/agent"
	"vclaw/internal/audit"
	"vclaw/internal/contracts"
	"vclaw/internal/store/pg"
	"vclaw/internal/tools"
)

func TestStoreIntegrationPersistsRuntimeStateAndApproval(t *testing.T) {
	databaseURL := os.Getenv("VCLAW_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set VCLAW_TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL persistence integration tests")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema := "vclaw_test_" + time.Now().UTC().Format("20060102t150405000000")
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	defer db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	applyMigrations(t, ctx, db)

	store := pg.NewWithDB(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := now.Format("20060102T150405000000")
	sessionID := "sess_pg_" + suffix
	requestID := "req_pg_" + suffix
	runID := "run_" + sessionID + "_" + requestID
	toolCallID := "toolcall_pg_" + suffix
	approvalID := "appr_pg_" + suffix
	actionID := "act_pg_" + suffix

	run := agent.RunState{
		RunID:        runID,
		SessionID:    sessionID,
		RequestID:    requestID,
		OriginalGoal: "create a calendar event",
		Status:       agent.RuntimeRunStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	run.Status = agent.RuntimeRunStatusWaitingApproval
	run.PendingActionID = actionID
	if err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}
	loadedRun, err := store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if loadedRun.Status != agent.RuntimeRunStatusWaitingApproval || loadedRun.PendingActionID != actionID {
		t.Fatalf("unexpected run state: %#v", loadedRun)
	}

	decision := agent.RiskDecisionRecord{
		RunID:            runID,
		RequestID:        requestID,
		SessionID:        sessionID,
		ToolCallID:       toolCallID,
		ToolName:         "calendar.createEvent",
		RiskLevel:        contracts.RiskLevelExternalWrite,
		Decision:         contracts.RiskDecisionRequiresApproval,
		RequiresApproval: true,
		Reason:           "external write",
		CheckedAt:        now,
	}
	if err := store.RecordRiskDecision(ctx, decision); err != nil {
		t.Fatalf("RecordRiskDecision: %v", err)
	}
	if err := store.RecordToolCall(ctx, agent.ToolCallRecord{
		ToolCallID:   toolCallID,
		RunID:        runID,
		RequestID:    requestID,
		SessionID:    sessionID,
		ToolName:     "calendar.createEvent",
		ArgsSnapshot: map[string]any{"title": "Planning"},
		Status:       agent.ToolCallStatusRequiresApproval,
		ApprovalID:   approvalID,
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("RecordToolCall requires approval: %v", err)
	}

	action := agent.ActionRecord{
		ActionID:          actionID,
		RunID:             runID,
		SessionID:         sessionID,
		RequestID:         requestID,
		ToolCallID:        toolCallID,
		ToolName:          "calendar.createEvent",
		ArgsSnapshot:      map[string]any{"title": "Planning"},
		RiskLevel:         contracts.RiskLevelExternalWrite,
		Status:            agent.ActionStatusPendingApproval,
		ApprovalID:        approvalID,
		ApprovalSummary:   "Confirm calendar event",
		ApprovalDetails:   "Writes external data",
		ApprovalExpiresAt: now.Add(10 * time.Minute),
		IdempotencyKey:    "idem_" + suffix,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	stored, created, err := store.FindOrCreateAction(ctx, action)
	if err != nil {
		t.Fatalf("FindOrCreateAction: %v", err)
	}
	if !created || stored.ApprovalID != approvalID {
		t.Fatalf("unexpected created action: created=%t action=%#v", created, stored)
	}
	reopened := pg.NewWithDB(db)
	pending, err := reopened.FindLatestPendingApproval(ctx, sessionID)
	if err != nil {
		t.Fatalf("FindLatestPendingApproval after reopen: %v", err)
	}
	if pending.ApprovalID != approvalID {
		t.Fatalf("unexpected pending approval: %#v", pending)
	}

	if _, err := reopened.MarkActionApproved(ctx, actionID, agent.ApprovalDecisionRecord{
		RequestID: requestID,
		SessionID: sessionID,
		Decision:  contracts.ApprovalDecisionApproved,
		DecidedBy: "owner",
		DecidedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("MarkActionApproved: %v", err)
	}
	if _, claimed, err := reopened.TryMarkActionExecuting(ctx, actionID); err != nil || !claimed {
		t.Fatalf("TryMarkActionExecuting claimed=%t err=%v", claimed, err)
	}
	result := tools.ToolResult{
		ToolCallID:     toolCallID,
		ToolName:       "calendar.createEvent",
		Success:        true,
		ContentForLLM:  `{"eventId":"event_1"}`,
		ContentForUser: "created",
	}
	if err := reopened.RecordToolCall(ctx, agent.ToolCallRecord{
		ToolCallID:   toolCallID,
		RunID:        runID,
		RequestID:    requestID,
		SessionID:    sessionID,
		ToolName:     "calendar.createEvent",
		ArgsSnapshot: map[string]any{"title": "Planning"},
		Status:       agent.ToolCallStatusCompleted,
		ApprovalID:   approvalID,
		Result:       &result,
		LatencyMS:    12,
		CreatedAt:    now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("RecordToolCall completed: %v", err)
	}
	if _, err := reopened.CompleteAction(ctx, actionID, result); err != nil {
		t.Fatalf("CompleteAction: %v", err)
	}
	var executionApprovalID string
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(approval_id, '')
		FROM tool_executions
		WHERE tool_call_id = $1`, toolCallID).Scan(&executionApprovalID); err != nil {
		t.Fatalf("query execution approval id: %v", err)
	}
	if executionApprovalID != approvalID {
		t.Fatalf("tool execution approval_id = %q, want %q", executionApprovalID, approvalID)
	}

	calls, err := reopened.ListToolCallsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListToolCallsByRun: %v", err)
	}
	if len(calls) != 1 || calls[0].Status != agent.ToolCallStatusCompleted {
		t.Fatalf("unexpected tool calls: %#v", calls)
	}

	rejectedToolCallID := "toolcall_rejected_" + suffix
	rejectedApprovalID := "appr_rejected_" + suffix
	rejectedActionID := "act_rejected_" + suffix
	if err := reopened.RecordToolCall(ctx, agent.ToolCallRecord{
		ToolCallID:   rejectedToolCallID,
		RunID:        runID,
		RequestID:    requestID,
		SessionID:    sessionID,
		ToolName:     "gmail.createDraft",
		ArgsSnapshot: map[string]any{"subject": "Draft"},
		Status:       agent.ToolCallStatusRequiresApproval,
		ApprovalID:   rejectedApprovalID,
		CreatedAt:    now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("RecordToolCall rejected requires approval: %v", err)
	}
	if _, _, err := reopened.FindOrCreateAction(ctx, agent.ActionRecord{
		ActionID:          rejectedActionID,
		RunID:             runID,
		SessionID:         sessionID,
		RequestID:         requestID,
		ToolCallID:        rejectedToolCallID,
		ToolName:          "gmail.createDraft",
		ArgsSnapshot:      map[string]any{"subject": "Draft"},
		RiskLevel:         contracts.RiskLevelExternalWrite,
		Status:            agent.ActionStatusPendingApproval,
		ApprovalID:        rejectedApprovalID,
		ApprovalSummary:   "Confirm draft",
		ApprovalExpiresAt: now.Add(10 * time.Minute),
		CreatedAt:         now.Add(3 * time.Second),
		UpdatedAt:         now.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("FindOrCreateAction rejected: %v", err)
	}
	if _, err := reopened.MarkActionRejected(ctx, rejectedActionID, agent.ApprovalDecisionRecord{
		RequestID: requestID,
		SessionID: sessionID,
		Decision:  contracts.ApprovalDecisionRejected,
		DecidedBy: "owner",
		DecidedAt: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("MarkActionRejected: %v", err)
	}
	var rejectedStatus, rejectedError string
	if err := db.QueryRowContext(ctx, `
		SELECT status, COALESCE(error_message, '')
		FROM tool_calls
		WHERE tool_call_id = $1`, rejectedToolCallID).Scan(&rejectedStatus, &rejectedError); err != nil {
		t.Fatalf("query rejected tool call: %v", err)
	}
	if rejectedStatus != string(agent.ToolCallStatusBlocked) || rejectedError != "approval rejected" {
		t.Fatalf("rejected tool call status/error = %q/%q", rejectedStatus, rejectedError)
	}

	if err := reopened.UpsertToolRegistryEntries(ctx, []tools.ToolDefinition{{
		Name:             "calendar.createEvent",
		Owner:            "agent_core",
		Description:      "Create calendar event",
		Parameters:       tools.ToolSchema{"type": "object"},
		Capability:       tools.CapabilityMutating,
		RiskLevel:        tools.RiskLevelExternalWrite,
		RequiresApproval: true,
		Timeout:          10 * time.Second,
		Enabled:          true,
	}}); err != nil {
		t.Fatalf("UpsertToolRegistryEntries: %v", err)
	}
	var registryCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM tool_registry_entries
		WHERE name = 'calendar.createEvent'
		  AND requires_approval = true
		  AND timeout_ms = 10000`).Scan(&registryCount); err != nil {
		t.Fatalf("query tool registry: %v", err)
	}
	if registryCount != 1 {
		t.Fatalf("expected persisted tool registry row, got %d", registryCount)
	}

	event := audit.NewToolRequestEvent(requestID, sessionID, "owner", "calendar.createEvent", audit.ActionType("calendar_create"), "create")
	if err := reopened.Log(event); err != nil {
		t.Fatalf("Log audit event: %v", err)
	}
	events, err := reopened.Query(audit.Filter{SessionID: sessionID, EventType: event.EventType})
	if err != nil {
		t.Fatalf("Query audit: %v", err)
	}
	if len(events) != 1 || events[0].EventID != event.EventID {
		t.Fatalf("unexpected audit events: %#v", events)
	}
}

func TestNewAppliesEmbeddedMigrations(t *testing.T) {
	databaseURL := os.Getenv("VCLAW_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set VCLAW_TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL persistence integration tests")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	schema := "vclaw_test_auto_migrate_" + time.Now().UTC().Format("20060102t150405000000")
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	defer db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	store, err := pg.New(ctx, databaseURL+"&search_path="+schema)
	if err != nil {
		t.Fatalf("pg.New: %v", err)
	}
	defer store.Close()

	verifyDB, err := sql.Open("pgx", databaseURL+"&search_path="+schema)
	if err != nil {
		t.Fatalf("open verify db: %v", err)
	}
	defer verifyDB.Close()

	var count int
	if err := verifyDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = current_schema()
		  AND table_name = 'tool_registry_entries'`).Scan(&count); err != nil {
		t.Fatalf("query information_schema: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected tool_registry_entries table, got %d", count)
	}
	if err := applyEmbeddedMigrationsForTest(ctx, verifyDB); err != nil {
		t.Fatalf("re-apply embedded migrations: %v", err)
	}
}

func applyMigrations(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, name := range []string{
		"001_init_vclaw_schema.sql",
		"002_persistence_runtime_state.sql",
		"003_run_metadata.sql",
		"003_governance_metadata.sql",
	} {
		path := filepath.Join("..", "..", "..", "migrations", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}

func applyEmbeddedMigrationsForTest(ctx context.Context, db *sql.DB) error {
	for _, name := range []string{
		filepath.Join("migrations", "001_init_vclaw_schema.sql"),
		filepath.Join("migrations", "002_persistence_runtime_state.sql"),
		filepath.Join("migrations", "003_run_metadata.sql"),
		filepath.Join("migrations", "003_governance_metadata.sql"),
	} {
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			return err
		}
	}
	return nil
}

// TestStoreIntegrationPersistsGovernanceMetadata verifies that the five
// governance metadata fields documented in docs/03-contracts.md round-trip
// through every persistence path: agent_runs, tool_calls, risk_decisions,
// approval_actions, approval_requests, audit_entries.
//
// The test populates a single end-to-end run with non-empty governance values
// at each layer, then queries the database directly to confirm columns are not
// the empty default. Reading via store APIs would not catch a "column never
// written" regression — the COALESCE pattern in our UPDATE statements means a
// later record-without-governance won't blank an earlier write, but it would
// also mask the bug where governance is silently dropped on insert.
func TestStoreIntegrationPersistsGovernanceMetadata(t *testing.T) {
	databaseURL := os.Getenv("VCLAW_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set VCLAW_TEST_DATABASE_URL or DATABASE_URL to run PostgreSQL persistence integration tests")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema := "vclaw_gov_" + time.Now().UTC().Format("20060102t150405000000")
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	defer db.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`)
	if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	applyMigrations(t, ctx, db)

	store := pg.NewWithDB(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := now.Format("20060102T150405000000")
	sessionID := "sess_gov_" + suffix
	requestID := "req_gov_" + suffix
	runID := "run_" + sessionID + "_" + requestID
	toolCallID := "toolcall_gov_" + suffix
	approvalID := "appr_gov_" + suffix
	actionID := "act_gov_" + suffix

	// Governance bundle stamped onto every record below.
	const (
		modelID            = "claude-opus-4-8"
		promptVer          = "abc12345"
		toolSchemaVer      = "deadbeef"
		toolResultSourceID = "tool:google_workspace"
	)
	policyRef := "policy:" + runID + ":" + toolCallID + ":" + // computed via governance.PolicyRef shape
		// We don't import internal/governance here on purpose — this test
		// asserts the stored representation, not the helper. The exact unix
		// suffix is irrelevant; we check non-empty.
		"1781870400"

	// 1. agent_runs — governance set at run start.
	run := agent.RunState{
		RunID:         runID,
		SessionID:     sessionID,
		RequestID:     requestID,
		OriginalGoal:  "send draft email",
		Status:        agent.RuntimeRunStatusRunning,
		CreatedAt:     now,
		UpdatedAt:     now,
		Model:         modelID,
		PromptVersion: promptVer,
	}
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// 2. risk_decisions — policy_decision_ref propagates from runtime.
	if err := store.RecordRiskDecision(ctx, agent.RiskDecisionRecord{
		RunID:             runID,
		RequestID:         requestID,
		SessionID:         sessionID,
		ToolCallID:        toolCallID,
		ToolName:          "gmail.sendDraft",
		RiskLevel:         contracts.RiskLevelExternalWrite,
		Decision:          contracts.RiskDecisionRequiresApproval,
		RequiresApproval:  true,
		Reason:            "external write",
		CheckedAt:         now,
		PolicyDecisionRef: policyRef,
	}); err != nil {
		t.Fatalf("RecordRiskDecision: %v", err)
	}

	// 3. tool_calls — full bundle including source.
	if err := store.RecordToolCall(ctx, agent.ToolCallRecord{
		ToolCallID:        toolCallID,
		RunID:             runID,
		RequestID:         requestID,
		SessionID:         sessionID,
		ToolName:          "gmail.sendDraft",
		ArgsSnapshot:      map[string]any{"subject": "Hi"},
		Status:            agent.ToolCallStatusRequiresApproval,
		ApprovalID:        approvalID,
		CreatedAt:         now,
		Model:             modelID,
		PromptVersion:     promptVer,
		ToolSchemaVersion: toolSchemaVer,
		PolicyDecisionRef: policyRef,
		Source:            toolResultSourceID,
	}); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}

	// 4. approval_actions + approval_requests — set on action creation.
	if _, _, err := store.FindOrCreateAction(ctx, agent.ActionRecord{
		ActionID:          actionID,
		RunID:             runID,
		SessionID:         sessionID,
		RequestID:         requestID,
		ToolCallID:        toolCallID,
		ToolName:          "gmail.sendDraft",
		ArgsSnapshot:      map[string]any{"subject": "Hi"},
		RiskLevel:         contracts.RiskLevelExternalWrite,
		Status:            agent.ActionStatusPendingApproval,
		ApprovalID:        approvalID,
		ApprovalSummary:   "Send draft",
		ApprovalExpiresAt: now.Add(10 * time.Minute),
		IdempotencyKey:    "idem_gov_" + suffix,
		CreatedAt:         now,
		UpdatedAt:         now,
		Model:             modelID,
		PromptVersion:     promptVer,
		ToolSchemaVersion: toolSchemaVer,
		PolicyDecisionRef: policyRef,
	}); err != nil {
		t.Fatalf("FindOrCreateAction: %v", err)
	}

	// ── Verify the columns were actually written, not just defaults. ───────
	type row struct {
		table    string
		query    string
		args     []any
		expected map[string]string // column → expected non-empty value
	}
	checks := []row{
		{
			table: "agent_runs",
			query: `SELECT model, prompt_version FROM agent_runs WHERE run_id = $1`,
			args:  []any{runID},
			expected: map[string]string{
				"model":          modelID,
				"prompt_version": promptVer,
			},
		},
		{
			table: "risk_decisions",
			query: `SELECT policy_decision_ref FROM risk_decisions WHERE tool_call_id = $1`,
			args:  []any{toolCallID},
			expected: map[string]string{
				"policy_decision_ref": policyRef,
			},
		},
		{
			table: "tool_calls",
			query: `SELECT model, prompt_version, tool_schema_version, policy_decision_ref, source
			        FROM tool_calls WHERE tool_call_id = $1`,
			args: []any{toolCallID},
			expected: map[string]string{
				"model":               modelID,
				"prompt_version":      promptVer,
				"tool_schema_version": toolSchemaVer,
				"policy_decision_ref": policyRef,
				"source":              toolResultSourceID,
			},
		},
		{
			table: "approval_actions",
			query: `SELECT model, prompt_version, tool_schema_version, policy_decision_ref
			        FROM approval_actions WHERE action_id = $1`,
			args: []any{actionID},
			expected: map[string]string{
				"model":               modelID,
				"prompt_version":      promptVer,
				"tool_schema_version": toolSchemaVer,
				"policy_decision_ref": policyRef,
			},
		},
		{
			table: "approval_requests",
			query: `SELECT model, prompt_version FROM approval_requests WHERE approval_id = $1`,
			args:  []any{approvalID},
			expected: map[string]string{
				"model":          modelID,
				"prompt_version": promptVer,
			},
		},
	}

	for _, check := range checks {
		check := check
		t.Run(check.table, func(t *testing.T) {
			// Read column names from the result set so we can match values
			// back to expected entries regardless of map iteration order.
			rows, err := db.QueryContext(ctx, check.query, check.args...)
			if err != nil {
				t.Fatalf("query %s: %v", check.table, err)
			}
			defer rows.Close()
			columnNames, err := rows.Columns()
			if err != nil {
				t.Fatalf("columns %s: %v", check.table, err)
			}
			if !rows.Next() {
				t.Fatalf("no rows for %s", check.table)
			}
			vals := make([]sql.NullString, len(columnNames))
			scan := make([]any, len(columnNames))
			for i := range vals {
				scan[i] = &vals[i]
			}
			if err := rows.Scan(scan...); err != nil {
				t.Fatalf("scan %s: %v", check.table, err)
			}
			for i, name := range columnNames {
				want, ok := check.expected[name]
				if !ok {
					t.Errorf("%s: unexpected column %q in select", check.table, name)
					continue
				}
				got := vals[i].String
				if got == "" {
					t.Errorf("%s.%s is empty — governance field was not persisted", check.table, name)
				}
				if got != want {
					t.Errorf("%s.%s = %q, want %q", check.table, name, got, want)
				}
			}
		})
	}

	// 5. audit_entries — every emit path should carry governance.
	// We check a sample row produced by RecordToolCall above.
	t.Run("audit_entries.tool_call", func(t *testing.T) {
		var model, promptVersion, toolSchemaVersion, policyDecisionRef, source string
		if err := db.QueryRowContext(ctx, `
			SELECT model, prompt_version, tool_schema_version, policy_decision_ref, source
			FROM audit_entries
			WHERE tool_call_id = $1 AND event_type = 'tool.call.requested'`, toolCallID).
			Scan(&model, &promptVersion, &toolSchemaVersion, &policyDecisionRef, &source); err != nil {
			t.Fatalf("query audit row: %v", err)
		}
		if model != modelID || promptVersion != promptVer || toolSchemaVersion != toolSchemaVer ||
			policyDecisionRef != policyRef || source != toolResultSourceID {
			t.Errorf("audit_entries governance row mismatch: model=%q prompt_version=%q tool_schema_version=%q policy_decision_ref=%q source=%q",
				model, promptVersion, toolSchemaVersion, policyDecisionRef, source)
		}
	})

	// 6. End-to-end read path via the store — values survive a full round-trip.
	t.Run("store_round_trip", func(t *testing.T) {
		got, err := store.GetRun(ctx, runID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if got.Model != modelID || got.PromptVersion != promptVer {
			t.Errorf("RunState round-trip: got Model=%q PromptVersion=%q", got.Model, got.PromptVersion)
		}

		actionGot, err := store.GetAction(ctx, actionID)
		if err != nil {
			t.Fatalf("GetAction: %v", err)
		}
		if actionGot.Model != modelID || actionGot.PromptVersion != promptVer ||
			actionGot.ToolSchemaVersion != toolSchemaVer || actionGot.PolicyDecisionRef != policyRef {
			t.Errorf("ActionRecord round-trip missing governance: %#v", actionGot)
		}

		calls, err := store.ListToolCallsByRun(ctx, runID)
		if err != nil {
			t.Fatalf("ListToolCallsByRun: %v", err)
		}
		if len(calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(calls))
		}
		c := calls[0]
		if c.Model != modelID || c.PromptVersion != promptVer || c.ToolSchemaVersion != toolSchemaVer ||
			c.PolicyDecisionRef != policyRef || c.Source != toolResultSourceID {
			t.Errorf("ToolCallRecord round-trip missing governance: %#v", c)
		}
	})
}
