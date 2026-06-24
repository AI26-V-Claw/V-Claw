package knowledge

import (
	"context"
	"time"
)

const (
	NodeTypeUser        = "user"
	NodeTypePerson      = "person"
	NodeTypeProject     = "project"
	NodeTypeDocument    = "document"
	NodeTypeMeeting     = "meeting"
	NodeTypeEmail       = "email"
	NodeTypeChatSpace   = "chat_space"
	NodeTypeChatMessage = "chat_message"
	NodeTypeNote        = "note"

	RelationRelatedTo   = "related_to"
	RelationMentions    = "mentions"
	RelationDiscussedIn = "discussed_in"
	RelationAttended    = "attended"
	RelationOrganizedBy = "organized_by"
	RelationCreatedBy   = "created_by"
	RelationSentBy      = "sent_by"
	RelationSentTo      = "sent_to"
	RelationSharedWith  = "shared_with"
	RelationLocatedIn   = "located_in"
	RelationDerivedFrom = "derived_from"
)

type Node struct {
	ID           string
	Type         string
	Title        string
	CanonicalKey string
	Aliases      []string
	Metadata     map[string]any
	Confidence   float64
	Stale        bool
	DeletedAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type NodeRef struct {
	Type         string
	CanonicalKey string
}

type Edge struct {
	ID         string
	FromNodeID string
	ToNodeID   string
	Relation   string
	SourceKey  string
	Metadata   map[string]any
	Confidence float64
	Stale      bool
	DeletedAt  *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Observation struct {
	NodeID     string
	EdgeID     string
	SourceType string
	SessionID  string
	RunID      string
	RequestID  string
	ToolCallID string
	ToolName   string
	SourceRef  map[string]any
	Summary    string
	ObservedAt time.Time
}

type Query struct {
	Text      string
	SessionID string
	RunID     string
	RequestID string
	Limit     int
	Now       time.Time
}

type LinkedContext struct {
	Items []ContextItem
}

type ContextItem struct {
	NodeID       string
	Type         string
	Title        string
	CanonicalKey string
	Relation     string
	LinkedTitle  string
	LinkedType   string
	Metadata     map[string]any
	Confidence   float64
	ObservedAt   time.Time
}

type Repository interface {
	UpsertNode(ctx context.Context, node Node) (Node, error)
	UpsertEdge(ctx context.Context, edge Edge) (Edge, error)
	MarkNodeDeleted(ctx context.Context, ref NodeRef, deletedAt time.Time) (string, error)
	MarkEdgesDeletedForNode(ctx context.Context, nodeID string, deletedAt time.Time) error
	RecordObservation(ctx context.Context, observation Observation) error
	FindLinkedContext(ctx context.Context, query Query) (LinkedContext, error)
}

type Retriever interface {
	Retrieve(ctx context.Context, query Query) (LinkedContext, error)
}
