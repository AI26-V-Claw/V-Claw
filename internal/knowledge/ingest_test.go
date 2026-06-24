package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"
	"unicode/utf8"

	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

type fakeRepo struct {
	nodes           map[string]Node
	edges           []Edge
	observations    []Observation
	deletedRefs     []NodeRef
	deletedEdgeNode string
	findErr         error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{nodes: map[string]Node{}}
}

func (r *fakeRepo) UpsertNode(_ context.Context, node Node) (Node, error) {
	if node.ID == "" {
		node.ID = "node_" + node.CanonicalKey
	}
	r.nodes[node.Type+"|"+node.CanonicalKey] = node
	return node, nil
}

func (r *fakeRepo) UpsertEdge(_ context.Context, edge Edge) (Edge, error) {
	if edge.ID == "" {
		edge.ID = "edge_" + edge.SourceKey
	}
	r.edges = append(r.edges, edge)
	return edge, nil
}

func (r *fakeRepo) MarkNodeDeleted(_ context.Context, ref NodeRef, _ time.Time) (string, error) {
	r.deletedRefs = append(r.deletedRefs, ref)
	return "node_" + ref.CanonicalKey, nil
}

func (r *fakeRepo) MarkEdgesDeletedForNode(_ context.Context, nodeID string, _ time.Time) error {
	r.deletedEdgeNode = nodeID
	return nil
}

func (r *fakeRepo) RecordObservation(_ context.Context, observation Observation) error {
	r.observations = append(r.observations, observation)
	return nil
}

func (r *fakeRepo) FindLinkedContext(context.Context, Query) (LinkedContext, error) {
	if r.findErr != nil {
		return LinkedContext{}, r.findErr
	}
	return LinkedContext{Items: []ContextItem{{Type: NodeTypeMeeting, Title: "N1 Long-term Test", Confidence: 0.9}}}, nil
}

func TestIngestCalendarCreatesMeetingPeopleEdgesAndObservations(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, "", nil)

	service.IngestToolResult(context.Background(), ingestInput{
		SessionID:  "sess_1",
		RunID:      "run_1",
		RequestID:  "req_1",
		ToolCallID: "call_1",
		ToolName:   "calendar.listEvents",
		Result: tools.ToolResult{
			Success:       true,
			ContentForLLM: `[{"id":"event_1","title":"Design review","start":"2026-06-23T09:00:00+07:00","end":"2026-06-23T10:00:00+07:00","attendees":[{"email":"bao@example.com","displayName":"Bao","responseStatus":"accepted"}],"organizer":{"email":"quang@example.com","displayName":"Quang","self":true}}]`,
		},
		ObservedAt: time.Date(2026, 6, 23, 2, 0, 0, 0, time.UTC),
	})

	if _, ok := repo.nodes[NodeTypeMeeting+"|calendar:event:event_1"]; !ok {
		t.Fatal("expected meeting node")
	}
	if _, ok := repo.nodes[NodeTypeUser+"|person:quang@example.com"]; !ok {
		t.Fatal("expected organizer self user node")
	}
	if _, ok := repo.nodes[NodeTypePerson+"|person:bao@example.com"]; !ok {
		t.Fatal("expected attendee person node")
	}
	if !hasRelation(repo.edges, RelationOrganizedBy) || !hasRelation(repo.edges, RelationAttended) {
		t.Fatalf("expected organizer and attendee edges, got %#v", repo.edges)
	}
	if len(repo.observations) == 0 {
		t.Fatal("expected provenance observations")
	}
}

func TestIngestCalendarDeleteSoftDeletesNodeAndEdges(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, "", nil)

	service.IngestToolResult(context.Background(), ingestInput{
		ToolName: "calendar.deleteEvent",
		Input:    map[string]any{"eventId": "event_1"},
		Result:   tools.ToolResult{Success: true},
	})

	if len(repo.deletedRefs) != 1 {
		t.Fatalf("expected deleted node ref, got %#v", repo.deletedRefs)
	}
	if repo.deletedRefs[0].CanonicalKey != "calendar:event:event_1" {
		t.Fatalf("unexpected deleted ref: %#v", repo.deletedRefs[0])
	}
	if repo.deletedEdgeNode == "" {
		t.Fatal("expected related edges to be soft-deleted")
	}
}

func TestKnowledgeHookIgnoresFailedToolResult(t *testing.T) {
	repo := newFakeRepo()
	hook := Hook{Service: NewService(repo, "", nil)}

	if err := hook.AfterTool(context.Background(), toolhooks.PostToolInput{
		ToolName: "calendar.listEvents",
		Result:   tools.ToolResult{Success: false},
	}); err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
	if len(repo.nodes) != 0 || len(repo.edges) != 0 || len(repo.observations) != 0 {
		t.Fatalf("failed result should not ingest graph data: nodes=%#v edges=%#v observations=%#v", repo.nodes, repo.edges, repo.observations)
	}
}

func TestRetrieveFailsSoftWhenMemorySyncFailsButRepoFindWorks(t *testing.T) {
	repo := newFakeRepo()
	service := NewService(repo, t.TempDir(), nil)

	ctx, err := service.Retrieve(context.Background(), Query{Text: "Design review"})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(ctx.Items) != 1 || ctx.Items[0].Title != "N1 Long-term Test" {
		t.Fatalf("unexpected context: %#v", ctx)
	}
}

func TestRetrieveReturnsRepoErrorForRuntimeToLogAndContinue(t *testing.T) {
	repo := newFakeRepo()
	repo.findErr = errors.New("db unavailable")
	service := NewService(repo, "", nil)

	_, err := service.Retrieve(context.Background(), Query{Text: "Design review"})
	if err == nil {
		t.Fatal("expected retrieval error")
	}
}

func TestStableKeyAndSummaryTruncationPreserveUTF8(t *testing.T) {
	fact := "Lịch sự kiện đã lưu: N1 Long-term Test từ 12:00 đến 13:00, ngày 22 tháng 06 năm 2026, ghi chú tiếng Việt rất dài"
	key := stableKey(fact)
	if !utf8.ValidString(key) {
		t.Fatalf("stableKey produced invalid UTF-8: %q", key)
	}
	if len([]rune(key)) > 96 {
		t.Fatalf("stableKey should cap by runes, got %d", len([]rune(key)))
	}

	summary := truncate(fact, 37)
	if !utf8.ValidString(summary) {
		t.Fatalf("truncate produced invalid UTF-8: %q", summary)
	}
	if len([]rune(summary)) > 37 {
		t.Fatalf("truncate should cap by runes, got %d", len([]rune(summary)))
	}
}

func hasRelation(edges []Edge, relation string) bool {
	for _, edge := range edges {
		if edge.Relation == relation {
			return true
		}
	}
	return false
}
