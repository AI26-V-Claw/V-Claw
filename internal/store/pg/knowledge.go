package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/knowledge"
)

func (s *Store) UpsertNode(ctx context.Context, node knowledge.Node) (knowledge.Node, error) {
	if s == nil || s.db == nil {
		return knowledge.Node{}, errorsStoreClosed()
	}
	aliases, err := json.Marshal(node.Aliases)
	if err != nil {
		return knowledge.Node{}, err
	}
	metadata, err := json.Marshal(nonNilMap(node.Metadata))
	if err != nil {
		return knowledge.Node{}, err
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO knowledge_nodes (type, title, canonical_key, aliases, metadata, confidence, stale, deleted_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, false, NULL, now())
		ON CONFLICT (type, canonical_key) DO UPDATE
		SET title = CASE WHEN EXCLUDED.title <> '' THEN EXCLUDED.title ELSE knowledge_nodes.title END,
		    aliases = CASE WHEN EXCLUDED.aliases <> '[]'::jsonb THEN EXCLUDED.aliases ELSE knowledge_nodes.aliases END,
		    metadata = knowledge_nodes.metadata || EXCLUDED.metadata,
		    confidence = GREATEST(knowledge_nodes.confidence, EXCLUDED.confidence),
		    stale = false,
		    deleted_at = NULL,
		    updated_at = now()
		RETURNING node_id::text, type, title, canonical_key, aliases, metadata, confidence, stale, deleted_at, created_at, updated_at`,
		strings.TrimSpace(node.Type),
		dbText(node.Title),
		dbText(node.CanonicalKey),
		string(aliases),
		string(metadata),
		boundedConfidence(node.Confidence),
	)
	return scanKnowledgeNode(row)
}

func (s *Store) UpsertEdge(ctx context.Context, edge knowledge.Edge) (knowledge.Edge, error) {
	if s == nil || s.db == nil {
		return knowledge.Edge{}, errorsStoreClosed()
	}
	metadata, err := json.Marshal(nonNilMap(edge.Metadata))
	if err != nil {
		return knowledge.Edge{}, err
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO knowledge_edges (from_node_id, to_node_id, relation, source_key, metadata, confidence, stale, deleted_at, updated_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::jsonb, $6, false, NULL, now())
		ON CONFLICT (from_node_id, to_node_id, relation, source_key) DO UPDATE
		SET metadata = knowledge_edges.metadata || EXCLUDED.metadata,
		    confidence = GREATEST(knowledge_edges.confidence, EXCLUDED.confidence),
		    stale = false,
		    deleted_at = NULL,
		    updated_at = now()
		RETURNING edge_id::text, from_node_id::text, to_node_id::text, relation, source_key, metadata, confidence, stale, deleted_at, created_at, updated_at`,
		edge.FromNodeID,
		edge.ToNodeID,
		dbText(edge.Relation),
		dbText(edge.SourceKey),
		string(metadata),
		boundedConfidence(edge.Confidence),
	)
	return scanKnowledgeEdge(row)
}

func (s *Store) MarkNodeDeleted(ctx context.Context, ref knowledge.NodeRef, deletedAt time.Time) (string, error) {
	if s == nil || s.db == nil {
		return "", errorsStoreClosed()
	}
	if deletedAt.IsZero() {
		deletedAt = time.Now().UTC()
	}
	var nodeID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE knowledge_nodes
		SET stale = true, deleted_at = $3, updated_at = now()
		WHERE type = $1 AND canonical_key = $2
		RETURNING node_id::text`,
		dbText(ref.Type),
		dbText(ref.CanonicalKey),
		deletedAt,
	).Scan(&nodeID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return nodeID, err
}

func (s *Store) MarkEdgesDeletedForNode(ctx context.Context, nodeID string, deletedAt time.Time) error {
	if s == nil || s.db == nil {
		return errorsStoreClosed()
	}
	if strings.TrimSpace(nodeID) == "" {
		return nil
	}
	if deletedAt.IsZero() {
		deletedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE knowledge_edges
		SET stale = true, deleted_at = $2, updated_at = now()
		WHERE from_node_id = $1::uuid OR to_node_id = $1::uuid`,
		nodeID,
		deletedAt,
	)
	return err
}

func (s *Store) RecordObservation(ctx context.Context, observation knowledge.Observation) error {
	if s == nil || s.db == nil {
		return errorsStoreClosed()
	}
	sourceRef, err := json.Marshal(nonNilMap(observation.SourceRef))
	if err != nil {
		return err
	}
	observedAt := observation.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO knowledge_observations (
			node_id, edge_id, source_type, session_id, run_id, request_id, tool_call_id,
			tool_name, source_ref, summary, observed_at
		)
		VALUES (
			NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7,
			$8, $9::jsonb, $10, $11
		)`,
		strings.TrimSpace(observation.NodeID),
		strings.TrimSpace(observation.EdgeID),
		dbText(observation.SourceType),
		dbText(observation.SessionID),
		dbText(observation.RunID),
		dbText(observation.RequestID),
		dbText(observation.ToolCallID),
		dbText(observation.ToolName),
		string(sourceRef),
		dbText(observation.Summary),
		observedAt,
	)
	return err
}

func (s *Store) FindLinkedContext(ctx context.Context, query knowledge.Query) (knowledge.LinkedContext, error) {
	if s == nil || s.db == nil {
		return knowledge.LinkedContext{}, errorsStoreClosed()
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 15
	}
	if limit > 30 {
		limit = 30
	}
	terms := queryTerms(query.Text)
	if len(terms) == 0 {
		return knowledge.LinkedContext{}, nil
	}
	args := []any{limit}
	where := "deleted_at IS NULL AND stale = false"
	var parts []string
	for _, term := range terms {
		args = append(args, "%"+term+"%")
		idx := len(args)
		parts = append(parts, fmt.Sprintf("(lower(title) LIKE $%d OR lower(canonical_key) LIKE $%d OR lower(metadata::text) LIKE $%d)", idx, idx, idx))
	}
	where += " AND (" + strings.Join(parts, " OR ") + ")"
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id::text, type, title, canonical_key, metadata, confidence, updated_at
		FROM knowledge_nodes
		WHERE `+where+`
		ORDER BY confidence DESC, updated_at DESC
		LIMIT $1`, args...)
	if err != nil {
		return knowledge.LinkedContext{}, err
	}
	defer rows.Close()
	var items []knowledge.ContextItem
	var nodeIDs []string
	for rows.Next() {
		item, err := scanContextNode(rows)
		if err != nil {
			return knowledge.LinkedContext{}, err
		}
		items = append(items, item)
		nodeIDs = append(nodeIDs, item.NodeID)
	}
	if err := rows.Err(); err != nil {
		return knowledge.LinkedContext{}, err
	}
	if len(nodeIDs) == 0 {
		return knowledge.LinkedContext{Items: items}, nil
	}
	edgeItems, err := s.findEdgesForContextNodes(ctx, nodeIDs, limit-len(items))
	if err != nil {
		return knowledge.LinkedContext{}, err
	}
	items = append(items, edgeItems...)
	return knowledge.LinkedContext{Items: items}, nil
}

func (s *Store) findEdgesForContextNodes(ctx context.Context, nodeIDs []string, limit int) ([]knowledge.ContextItem, error) {
	if limit <= 0 {
		limit = 5
	}
	args := make([]any, 0, len(nodeIDs)+1)
	args = append(args, limit)
	var placeholders []string
	for _, id := range nodeIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d::uuid", len(args)))
	}
	query := `
		SELECT e.edge_id::text, e.relation, e.metadata, e.confidence, e.updated_at,
		       f.node_id::text, f.type, f.title, f.canonical_key,
		       t.type, t.title
		FROM knowledge_edges e
		JOIN knowledge_nodes f ON f.node_id = e.from_node_id
		JOIN knowledge_nodes t ON t.node_id = e.to_node_id
		WHERE e.deleted_at IS NULL AND e.stale = false
		  AND f.deleted_at IS NULL AND f.stale = false
		  AND t.deleted_at IS NULL AND t.stale = false
		  AND (e.from_node_id IN (` + strings.Join(placeholders, ",") + `)
		       OR e.to_node_id IN (` + strings.Join(placeholders, ",") + `))
		ORDER BY e.confidence DESC, e.updated_at DESC
		LIMIT $1`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []knowledge.ContextItem
	for rows.Next() {
		var item knowledge.ContextItem
		var metadata []byte
		var edgeID string
		if err := rows.Scan(&edgeID, &item.Relation, &metadata, &item.Confidence, &item.ObservedAt, &item.NodeID, &item.Type, &item.Title, &item.CanonicalKey, &item.LinkedType, &item.LinkedTitle); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metadata, &item.Metadata)
		items = append(items, item)
	}
	return items, rows.Err()
}

type scanRow interface {
	Scan(dest ...any) error
}

func scanKnowledgeNode(row scanRow) (knowledge.Node, error) {
	var node knowledge.Node
	var aliases []byte
	var metadata []byte
	err := row.Scan(&node.ID, &node.Type, &node.Title, &node.CanonicalKey, &aliases, &metadata, &node.Confidence, &node.Stale, &node.DeletedAt, &node.CreatedAt, &node.UpdatedAt)
	if err != nil {
		return knowledge.Node{}, err
	}
	_ = json.Unmarshal(aliases, &node.Aliases)
	_ = json.Unmarshal(metadata, &node.Metadata)
	return node, nil
}

func scanKnowledgeEdge(row scanRow) (knowledge.Edge, error) {
	var edge knowledge.Edge
	var metadata []byte
	err := row.Scan(&edge.ID, &edge.FromNodeID, &edge.ToNodeID, &edge.Relation, &edge.SourceKey, &metadata, &edge.Confidence, &edge.Stale, &edge.DeletedAt, &edge.CreatedAt, &edge.UpdatedAt)
	if err != nil {
		return knowledge.Edge{}, err
	}
	_ = json.Unmarshal(metadata, &edge.Metadata)
	return edge, nil
}

func scanContextNode(rows *sql.Rows) (knowledge.ContextItem, error) {
	var item knowledge.ContextItem
	var metadata []byte
	if err := rows.Scan(&item.NodeID, &item.Type, &item.Title, &item.CanonicalKey, &metadata, &item.Confidence, &item.ObservedAt); err != nil {
		return knowledge.ContextItem{}, err
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	return item, nil
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func dbText(value string) string {
	return strings.TrimSpace(strings.ToValidUTF8(value, ""))
}

func boundedConfidence(value float64) float64 {
	if value <= 0 {
		return 0.5
	}
	if value > 1 {
		return 1
	}
	return value
}

func queryTerms(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	raw := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == '.' || r == ':' || r == ';' || r == '?' || r == '!'
	})
	var terms []string
	for _, term := range raw {
		term = strings.TrimSpace(term)
		if len([]rune(term)) >= 3 {
			terms = append(terms, term)
		}
		if len(terms) >= 5 {
			break
		}
	}
	return terms
}

func errorsStoreClosed() error {
	return fmt.Errorf("postgres store is not initialized")
}
