package knowledge

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	repo      Repository
	memoryDir string
	logger    *slog.Logger
}

func NewService(repo Repository, memoryDir string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, memoryDir: strings.TrimSpace(memoryDir), logger: logger}
}

func (s *Service) Retrieve(ctx context.Context, query Query) (LinkedContext, error) {
	if s == nil || s.repo == nil {
		return LinkedContext{}, nil
	}
	if query.Limit <= 0 {
		query.Limit = 15
	}
	if query.Now.IsZero() {
		query.Now = time.Now().UTC()
	}
	if err := s.SyncLongTermMemory(ctx, query); err != nil {
		s.logger.Warn("knowledge long-term memory sync failed", "error", err)
	}
	return s.repo.FindLinkedContext(ctx, query)
}

func (s *Service) upsertObservation(ctx context.Context, nodeID string, edgeID string, input ingestInput, sourceType string, sourceRef map[string]any, summary string) {
	if s == nil || s.repo == nil {
		return
	}
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if err := s.repo.RecordObservation(ctx, Observation{
		NodeID:     nodeID,
		EdgeID:     edgeID,
		SourceType: sourceType,
		SessionID:  input.SessionID,
		RunID:      input.RunID,
		RequestID:  input.RequestID,
		ToolCallID: input.ToolCallID,
		ToolName:   input.ToolName,
		SourceRef:  sourceRef,
		Summary:    truncate(summary, 300),
		ObservedAt: observedAt,
	}); err != nil {
		s.logger.Warn("knowledge observation failed", "tool", input.ToolName, "error", err)
	}
}

func (s *Service) upsertNode(ctx context.Context, node Node) (Node, bool) {
	if s == nil || s.repo == nil {
		return Node{}, false
	}
	if strings.TrimSpace(node.Type) == "" || strings.TrimSpace(node.CanonicalKey) == "" {
		return Node{}, false
	}
	if node.Confidence <= 0 {
		node.Confidence = 0.6
	}
	updated, err := s.repo.UpsertNode(ctx, node)
	if err != nil {
		s.logger.Warn("knowledge node upsert failed", "type", node.Type, "canonical_key", node.CanonicalKey, "error", err)
		return Node{}, false
	}
	return updated, true
}

func (s *Service) upsertEdge(ctx context.Context, edge Edge) (Edge, bool) {
	if s == nil || s.repo == nil {
		return Edge{}, false
	}
	if strings.TrimSpace(edge.FromNodeID) == "" || strings.TrimSpace(edge.ToNodeID) == "" || strings.TrimSpace(edge.Relation) == "" {
		return Edge{}, false
	}
	if edge.Confidence <= 0 {
		edge.Confidence = 0.6
	}
	updated, err := s.repo.UpsertEdge(ctx, edge)
	if err != nil {
		s.logger.Warn("knowledge edge upsert failed", "relation", edge.Relation, "error", err)
		return Edge{}, false
	}
	return updated, true
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:max]))
}
