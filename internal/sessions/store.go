package sessions

import (
	"context"
	"sync"

	"vclaw/internal/providers"
)

type Store interface {
	LoadTranscript(ctx context.Context, sessionID string) ([]providers.Message, error)
	AppendMessage(ctx context.Context, sessionID string, message providers.Message) error
	ClearSession(ctx context.Context, sessionID string) error
}

type InMemoryStore struct {
	mu         sync.RWMutex
	transcript map[string][]providers.Message
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{transcript: make(map[string][]providers.Message)}
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
