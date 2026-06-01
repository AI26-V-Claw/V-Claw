package memory

import (
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role      Role      `json:"role"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type Store struct {
	mu       sync.Mutex
	sessions map[string][]Message
	maxSize  int
}

func NewStore() *Store {
	return &Store{
		sessions: map[string][]Message{},
		maxSize:  20,
	}
}

func (s *Store) Append(sessionID string, role Role, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizedSessionID(sessionID)
	s.sessions[key] = append(s.sessions[key], Message{
		Role:      role,
		Text:      strings.TrimSpace(text),
		Timestamp: time.Now().UTC(),
	})
	if len(s.sessions[key]) > s.maxSize {
		s.sessions[key] = append([]Message(nil), s.sessions[key][len(s.sessions[key])-s.maxSize:]...)
	}
}

func (s *Store) GetHistory(sessionID string) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizedSessionID(sessionID)
	history := make([]Message, len(s.sessions[key]))
	copy(history, s.sessions[key])
	return history
}

func (s *Store) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, normalizedSessionID(sessionID))
}

func normalizedSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "default"
	}
	return sessionID
}
