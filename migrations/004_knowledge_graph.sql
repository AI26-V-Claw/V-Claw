CREATE TABLE IF NOT EXISTS knowledge_nodes (
    node_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type text NOT NULL,
    title text NOT NULL DEFAULT '',
    canonical_key text NOT NULL,
    aliases jsonb NOT NULL DEFAULT '[]'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    confidence double precision NOT NULL DEFAULT 0.5,
    stale boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT knowledge_nodes_confidence_range CHECK (confidence >= 0 AND confidence <= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_nodes_type_canonical
    ON knowledge_nodes(type, canonical_key);
CREATE INDEX IF NOT EXISTS idx_knowledge_nodes_active_updated
    ON knowledge_nodes(type, stale, deleted_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_knowledge_nodes_title_lower
    ON knowledge_nodes(lower(title));

CREATE TABLE IF NOT EXISTS knowledge_edges (
    edge_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    from_node_id uuid NOT NULL REFERENCES knowledge_nodes(node_id) ON DELETE CASCADE,
    to_node_id uuid NOT NULL REFERENCES knowledge_nodes(node_id) ON DELETE CASCADE,
    relation text NOT NULL,
    source_key text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    confidence double precision NOT NULL DEFAULT 0.5,
    stale boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT knowledge_edges_confidence_range CHECK (confidence >= 0 AND confidence <= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_edges_dedup
    ON knowledge_edges(from_node_id, to_node_id, relation, source_key);
CREATE INDEX IF NOT EXISTS idx_knowledge_edges_from_active
    ON knowledge_edges(from_node_id, stale, deleted_at);
CREATE INDEX IF NOT EXISTS idx_knowledge_edges_to_active
    ON knowledge_edges(to_node_id, stale, deleted_at);
CREATE INDEX IF NOT EXISTS idx_knowledge_edges_relation
    ON knowledge_edges(relation);

CREATE TABLE IF NOT EXISTS knowledge_observations (
    observation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id uuid REFERENCES knowledge_nodes(node_id) ON DELETE SET NULL,
    edge_id uuid REFERENCES knowledge_edges(edge_id) ON DELETE SET NULL,
    source_type text NOT NULL,
    session_id text NOT NULL DEFAULT '',
    run_id text NOT NULL DEFAULT '',
    request_id text NOT NULL DEFAULT '',
    tool_call_id text NOT NULL DEFAULT '',
    tool_name text NOT NULL DEFAULT '',
    source_ref jsonb NOT NULL DEFAULT '{}'::jsonb,
    summary text NOT NULL DEFAULT '',
    observed_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_knowledge_observations_node_time
    ON knowledge_observations(node_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_knowledge_observations_edge_time
    ON knowledge_observations(edge_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_knowledge_observations_request
    ON knowledge_observations(request_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_observations_tool
    ON knowledge_observations(tool_name, observed_at DESC);
