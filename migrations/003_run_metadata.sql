ALTER TABLE agent_runs
    ADD COLUMN IF NOT EXISTS cost_usd double precision NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS steps jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS short_label text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS category text NOT NULL DEFAULT 'chat',
    ADD COLUMN IF NOT EXISTS error_ref text NOT NULL DEFAULT '';
