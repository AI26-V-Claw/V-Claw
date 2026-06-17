-- 003_governance_metadata.sql
--
-- Task N1 — adds the five governance metadata fields documented in
-- docs/03-contracts.md (GovernanceMetadata) so every run, tool call,
-- approval, risk decision, and audit entry carries provenance that the N4
-- monitoring/trace UI can read without joining across tables.
--
-- Fields added per table:
--   agent_runs          : model, prompt_version
--   tool_calls          : model, prompt_version, tool_schema_version,
--                         policy_decision_ref, source
--   approval_actions    : model, prompt_version, tool_schema_version,
--                         policy_decision_ref
--   approval_requests   : model, prompt_version
--   risk_decisions      : policy_decision_ref
--   audit_entries       : model, prompt_version, tool_schema_version,
--                         policy_decision_ref, source
--
-- All columns default to '' so rows produced before the runtime starts
-- populating governance round-trip cleanly. The runtime overwrites the
-- defaults whenever the data is available; '' marks "unknown" rather than
-- "not applicable".

ALTER TABLE agent_runs
    ADD COLUMN IF NOT EXISTS model text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_version text NOT NULL DEFAULT '';

ALTER TABLE tool_calls
    ADD COLUMN IF NOT EXISTS model text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tool_schema_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS policy_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT '';

ALTER TABLE approval_actions
    ADD COLUMN IF NOT EXISTS model text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tool_schema_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS policy_decision_ref text NOT NULL DEFAULT '';

ALTER TABLE approval_requests
    ADD COLUMN IF NOT EXISTS model text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_version text NOT NULL DEFAULT '';

ALTER TABLE risk_decisions
    ADD COLUMN IF NOT EXISTS policy_decision_ref text NOT NULL DEFAULT '';

ALTER TABLE audit_entries
    ADD COLUMN IF NOT EXISTS model text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tool_schema_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS policy_decision_ref text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT '';

-- Indexes for the queries N4 will run most often. Every index is partial on
-- "value present" so empty defaults from un-instrumented code paths don't
-- bloat the indexes.

CREATE INDEX IF NOT EXISTS idx_audit_entries_model
    ON audit_entries(model)
    WHERE model <> '';

CREATE INDEX IF NOT EXISTS idx_audit_entries_prompt_version
    ON audit_entries(prompt_version)
    WHERE prompt_version <> '';

CREATE INDEX IF NOT EXISTS idx_audit_entries_policy_decision_ref
    ON audit_entries(policy_decision_ref)
    WHERE policy_decision_ref <> '';

CREATE INDEX IF NOT EXISTS idx_tool_calls_model
    ON tool_calls(model)
    WHERE model <> '';

CREATE INDEX IF NOT EXISTS idx_tool_calls_policy_decision_ref
    ON tool_calls(policy_decision_ref)
    WHERE policy_decision_ref <> '';

CREATE INDEX IF NOT EXISTS idx_risk_decisions_policy_decision_ref
    ON risk_decisions(policy_decision_ref)
    WHERE policy_decision_ref <> '';
