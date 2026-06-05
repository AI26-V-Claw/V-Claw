package sessions

import (
	"context"
	"sync"
	"time"

	"vclaw/internal/providers"
)

type Store interface {
	LoadTranscript(ctx context.Context, sessionID string) ([]providers.Message, error)
	AppendMessage(ctx context.Context, sessionID string, message providers.Message) error
	ClearSession(ctx context.Context, sessionID string) error
}

type MemoryStore interface {
	LoadMemory(ctx context.Context, sessionID string) (SessionMemory, error)
	SaveMemory(ctx context.Context, sessionID string, memory SessionMemory) error
}

type SessionMemory struct {
	Summary              string                `json:"summary,omitempty"`
	LastActionResults    []ActionResult        `json:"lastActionResults,omitempty"`
	PendingClarification *PendingClarification `json:"pendingClarification,omitempty"`
	UpdatedAt            time.Time             `json:"updatedAt,omitempty"`
}

type ActionResult struct {
	ToolName  string    `json:"toolName"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type PendingClarification struct {
	OriginalRequest string         `json:"originalRequest,omitempty"`
	Question        string         `json:"question,omitempty"`
	ToolName        string         `json:"toolName,omitempty"`
	MissingFields   []string       `json:"missingFields,omitempty"`
	PartialInput    map[string]any `json:"partialInput,omitempty"`
	CreatedAt       time.Time      `json:"createdAt,omitempty"`
}

type InMemoryStore struct {
	mu         sync.RWMutex
	transcript map[string][]providers.Message
	memory     map[string]SessionMemory
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		transcript: make(map[string][]providers.Message),
		memory:     make(map[string]SessionMemory),
	}
}

func (s *InMemoryStore) LoadTranscript(_ context.Context, sessionID string) ([]providers.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneMessages(s.transcript[sessionID]), nil
}

func (s *InMemoryStore) AppendMessage(_ context.Context, sessionID string, message providers.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.transcript[sessionID] = append(s.transcript[sessionID], cloneMessage(message))
	return nil
}

func (s *InMemoryStore) ClearSession(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.transcript, sessionID)
	delete(s.memory, sessionID)
	return nil
}

func (s *InMemoryStore) LoadMemory(_ context.Context, sessionID string) (SessionMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneMemory(s.memory[sessionID]), nil
}

func (s *InMemoryStore) SaveMemory(_ context.Context, sessionID string, memory SessionMemory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.memory[sessionID] = cloneMemory(memory)
	return nil
}

func cloneMessages(messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]providers.Message, len(messages))
	for i, message := range messages {
		cloned[i] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message providers.Message) providers.Message {
	cloned := message
	if len(message.ToolCalls) == 0 {
		return cloned
	}
	cloned.ToolCalls = make([]providers.ToolCall, len(message.ToolCalls))
	for i, toolCall := range message.ToolCalls {
		cloned.ToolCalls[i] = toolCall
		if toolCall.Arguments == nil {
			continue
		}
		cloned.ToolCalls[i].Arguments = make(map[string]any, len(toolCall.Arguments))
		for key, value := range toolCall.Arguments {
			cloned.ToolCalls[i].Arguments[key] = value
		}
	}
	return cloned
}

func cloneMemory(memory SessionMemory) SessionMemory {
	cloned := memory
	if len(memory.LastActionResults) > 0 {
		cloned.LastActionResults = append([]ActionResult(nil), memory.LastActionResults...)
	}
	if memory.PendingClarification != nil {
		cloned.PendingClarification = clonePendingClarification(memory.PendingClarification)
	}
	return cloned
}

func clonePendingClarification(pending *PendingClarification) *PendingClarification {
	if pending == nil {
		return nil
	}
	cloned := *pending
	if len(pending.MissingFields) > 0 {
		cloned.MissingFields = append([]string(nil), pending.MissingFields...)
	}
	if len(pending.PartialInput) > 0 {
		cloned.PartialInput = make(map[string]any, len(pending.PartialInput))
		for key, value := range pending.PartialInput {
			cloned.PartialInput[key] = value
		}
	}
	return &cloned
}
