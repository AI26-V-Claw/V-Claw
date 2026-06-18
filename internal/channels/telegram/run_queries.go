package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"vclaw/internal/agent"
)

var queryTelegramRunByID = loadTelegramRunByID

func loadTelegramRunByID(ctx context.Context, databaseURL string, runID string) (*agent.RunState, error) {
	runID = strings.TrimSpace(runID)
	if databaseURL == "" || runID == "" {
		return nil, nil
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var (
		state    agent.RunState
		status   string
		dataRaw  []byte
		stepsRaw []byte
	)
	row := db.QueryRowContext(ctx, `
		SELECT run_id, COALESCE(request_id, ''), COALESCE(session_id, ''), COALESCE(original_goal, input_text, ''),
		       COALESCE(status, ''), COALESCE(data, '{}'::jsonb), COALESCE(cost_usd, 0), COALESCE(steps, '[]'::jsonb),
		       COALESCE(short_label, ''), COALESCE(category, ''), COALESCE(error_ref, ''),
		       COALESCE(iteration_count, 0), COALESCE(pending_action_id, ''), COALESCE(pending_clarification_id, ''),
		       created_at, updated_at, completed_at
		FROM agent_runs
		WHERE run_id = $1`, runID)
	var completedAt sql.NullTime
	if err := row.Scan(&state.RunID, &state.RequestID, &state.SessionID, &state.OriginalGoal, &status, &dataRaw, &state.CostUSD, &stepsRaw, &state.ShortLabel, &state.Category, &state.ErrorRef, &state.IterationCount, &state.PendingActionID, &state.PendingClarificationID, &state.CreatedAt, &state.UpdatedAt, &completedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load telegram run by id: %w", err)
	}
	state.Status = agent.RuntimeRunStatus(status)
	if len(dataRaw) > 0 && string(dataRaw) != "null" {
		if err := json.Unmarshal(dataRaw, &state.Data); err != nil {
			return nil, err
		}
	}
	if len(stepsRaw) > 0 && string(stepsRaw) != "null" {
		if err := json.Unmarshal(stepsRaw, &state.Steps); err != nil {
			return nil, err
		}
	}
	if completedAt.Valid {
		state.CompletedAt = &completedAt.Time
	}
	return &state, nil
}
