CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE agent_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id text NOT NULL UNIQUE,
    session_id text NOT NULL,
    channel text NOT NULL,
    input_text text NOT NULL,
    locale text,
    response_status text,
    response_message text,
    data jsonb NOT NULL DEFAULT '{}'::jsonb,
    plan jsonb NOT NULL DEFAULT '{}'::jsonb,
    error jsonb,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

CREATE TABLE session_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id text NOT NULL,
    role text NOT NULL,
    content text NOT NULL DEFAULT '',
    tool_call_id text,
    tool_calls jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tool_registry_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL UNIQUE,
    owner text NOT NULL,
    description text NOT NULL DEFAULT '',
    parameters jsonb NOT NULL DEFAULT '{}'::jsonb,
    capability text NOT NULL,
    risk_level text NOT NULL,
    requires_approval boolean NOT NULL DEFAULT false,
    timeout_ms integer,
    enabled boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE risk_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id text,
    session_id text,
    tool_call_id text NOT NULL,
    tool_name text NOT NULL,
    risk_level text NOT NULL,
    decision text NOT NULL,
    requires_approval boolean NOT NULL DEFAULT false,
    reason text,
    checked_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tool_executions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id text,
    session_id text,
    tool_call_id text NOT NULL UNIQUE,
    tool_name text NOT NULL,
    input jsonb NOT NULL DEFAULT '{}'::jsonb,
    execution_status text NOT NULL,
    result_success boolean,
    result_data jsonb,
    error jsonb,
    requested_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz
);

CREATE TABLE approval_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id text NOT NULL UNIQUE,
    parent_approval_id text REFERENCES approval_requests(approval_id) ON DELETE SET NULL,
    request_id text NOT NULL,
    session_id text NOT NULL,
    tool_call_id text NOT NULL,
    status text NOT NULL,
    risk_level text NOT NULL,
    summary text NOT NULL,
    details text,
    tool_call jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    resolved_at timestamptz
);

CREATE TABLE approval_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id text NOT NULL REFERENCES approval_requests(approval_id) ON DELETE CASCADE,
    request_id text NOT NULL,
    decision text NOT NULL,
    decided_by text,
    decided_at timestamptz NOT NULL,
    comment text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp timestamptz NOT NULL DEFAULT now(),
    request_id text,
    update_id bigint,
    channel text,
    chat_id bigint,
    session_id text,
    input text NOT NULL DEFAULT '',
    intent text NOT NULL DEFAULT '',
    system_op_type text NOT NULL DEFAULT '',
    confidence double precision,
    action_taken text NOT NULL DEFAULT '',
    output text NOT NULL DEFAULT '',
    hitl_required boolean NOT NULL DEFAULT false,
    error text
);

CREATE INDEX idx_agent_runs_session_started_at ON agent_runs(session_id, started_at);
CREATE INDEX idx_session_messages_session_created_at ON session_messages(session_id, created_at);
CREATE INDEX idx_risk_decisions_request_id ON risk_decisions(request_id);
CREATE INDEX idx_risk_decisions_session_id ON risk_decisions(session_id);
CREATE INDEX idx_tool_executions_request_id ON tool_executions(request_id);
CREATE INDEX idx_tool_executions_session_id ON tool_executions(session_id);
CREATE INDEX idx_approval_requests_session_status ON approval_requests(session_id, status);
CREATE INDEX idx_approval_requests_tool_call_id ON approval_requests(tool_call_id);
CREATE INDEX idx_approval_requests_parent_approval_id ON approval_requests(parent_approval_id);
CREATE INDEX idx_approval_decisions_approval_id ON approval_decisions(approval_id);
CREATE INDEX idx_audit_entries_request_id ON audit_entries(request_id);
CREATE INDEX idx_audit_entries_session_timestamp ON audit_entries(session_id, timestamp);
