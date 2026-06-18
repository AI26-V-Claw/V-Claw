ALTER TABLE agent_runs
    ADD COLUMN IF NOT EXISTS run_id text,
    ADD COLUMN IF NOT EXISTS data jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS cost_usd double precision NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS steps jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS short_label text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS category text NOT NULL DEFAULT 'chat',
    ADD COLUMN IF NOT EXISTS error_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS original_goal text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'running',
    ADD COLUMN IF NOT EXISTS iteration_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS pending_action_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pending_clarification_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

UPDATE agent_runs
SET run_id = 'run_' || regexp_replace(session_id || '_' || request_id, '[\\/\s]+', '_', 'g')
WHERE run_id IS NULL OR run_id = '';

ALTER TABLE agent_runs
    ALTER COLUMN run_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_runs_run_id ON agent_runs(run_id);
CREATE INDEX IF NOT EXISTS idx_agent_runs_session_status ON agent_runs(session_id, status);

-- Session transcript and short-term memory are file-backed JSON under
-- data/sessions. PostgreSQL is used for run/tool/approval/audit persistence,
-- so the DB session tables from the baseline schema are removed from the
-- final runtime schema.
DROP TABLE IF EXISTS session_messages CASCADE;
DROP TABLE IF EXISTS session_memory CASCADE;
DROP TABLE IF EXISTS run_events CASCADE;

ALTER TABLE risk_decisions
    ADD COLUMN IF NOT EXISTS run_id text,
    ADD COLUMN IF NOT EXISTS policy_reasons jsonb NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_risk_decisions_run_id ON risk_decisions(run_id);
CREATE INDEX IF NOT EXISTS idx_risk_decisions_tool_call_id ON risk_decisions(tool_call_id);

CREATE TABLE IF NOT EXISTS tool_calls (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id text NOT NULL,
    request_id text,
    session_id text,
    tool_call_id text NOT NULL UNIQUE,
    tool_name text NOT NULL,
    args_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL,
    reason text,
    error_message text,
    result jsonb,
    latency_ms bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_run_created_at ON tool_calls(run_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tool_calls_session_created_at ON tool_calls(session_id, created_at);

CREATE TABLE IF NOT EXISTS approval_actions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id text NOT NULL UNIQUE,
    run_id text NOT NULL,
    request_id text NOT NULL,
    session_id text NOT NULL,
    tool_call_id text NOT NULL,
    tool_name text NOT NULL,
    args_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    risk_level text NOT NULL,
    status text NOT NULL,
    approval_id text NOT NULL UNIQUE,
    approval_expires_at timestamptz NOT NULL,
    idempotency_key text UNIQUE,
    result jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_approval_actions_pending_session
    ON approval_actions(session_id, status, approval_expires_at);
CREATE INDEX IF NOT EXISTS idx_approval_actions_run_status
    ON approval_actions(run_id, status);

ALTER TABLE approval_requests
    ADD COLUMN IF NOT EXISTS action_id text,
    ADD COLUMN IF NOT EXISTS run_id text;

CREATE INDEX IF NOT EXISTS idx_approval_requests_run_status ON approval_requests(run_id, status);

ALTER TABLE approval_decisions
    ADD COLUMN IF NOT EXISTS run_id text,
    ADD COLUMN IF NOT EXISTS session_id text,
    ADD COLUMN IF NOT EXISTS channel text;

CREATE INDEX IF NOT EXISTS idx_approval_decisions_run_id ON approval_decisions(run_id);
CREATE INDEX IF NOT EXISTS idx_approval_decisions_session_id ON approval_decisions(session_id);

ALTER TABLE tool_executions
    ADD COLUMN IF NOT EXISTS run_id text,
    ADD COLUMN IF NOT EXISTS approval_id text;

CREATE INDEX IF NOT EXISTS idx_tool_executions_run_id ON tool_executions(run_id);

ALTER TABLE audit_entries
    ADD COLUMN IF NOT EXISTS event_id text,
    ADD COLUMN IF NOT EXISTS event_type text,
    ADD COLUMN IF NOT EXISTS run_id text,
    ADD COLUMN IF NOT EXISTS tool_call_id text,
    ADD COLUMN IF NOT EXISTS approval_id text,
    ADD COLUMN IF NOT EXISTS execution_id text,
    ADD COLUMN IF NOT EXISTS actor_ref text,
    ADD COLUMN IF NOT EXISTS tool_name text,
    ADD COLUMN IF NOT EXISTS action_type text,
    ADD COLUMN IF NOT EXISTS risk_level text,
    ADD COLUMN IF NOT EXISTS policy_decision text,
    ADD COLUMN IF NOT EXISTS policy_reasons jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS affected_resources jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS details jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS output_summary text,
    ADD COLUMN IF NOT EXISTS error_message text,
    ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_entries_event_id
    ON audit_entries(event_id)
    WHERE event_id IS NOT NULL AND event_id <> '';
CREATE INDEX IF NOT EXISTS idx_audit_entries_run_timestamp ON audit_entries(run_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_entries_tool_call_id ON audit_entries(tool_call_id);
CREATE INDEX IF NOT EXISTS idx_audit_entries_approval_id ON audit_entries(approval_id);

-- Drop legacy columns that no runtime code writes.
-- agent_runs: response envelope is file-backed (data/sessions transcript).
-- audit_entries: legacy Telegram-style fields replaced by structured columns above.
ALTER TABLE agent_runs
    DROP COLUMN IF EXISTS channel,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS response_status,
    DROP COLUMN IF EXISTS response_message,
    DROP COLUMN IF EXISTS plan,
    DROP COLUMN IF EXISTS error;

ALTER TABLE audit_entries
    DROP COLUMN IF EXISTS update_id,
    DROP COLUMN IF EXISTS chat_id,
    DROP COLUMN IF EXISTS input,
    DROP COLUMN IF EXISTS intent,
    DROP COLUMN IF EXISTS system_op_type,
    DROP COLUMN IF EXISTS confidence,
    DROP COLUMN IF EXISTS action_taken,
    DROP COLUMN IF EXISTS output,
    DROP COLUMN IF EXISTS error;
