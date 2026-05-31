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
	messages []Message
	maxSize  int
}

func NewStore() *Store {
	return &Store{maxSize: 20}
}

func (s *Store) Append(role Role, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, Message{
		Role:      role,
		Text:      strings.TrimSpace(text),
		Timestamp: time.Now().UTC(),
	})
	if len(s.messages) > s.maxSize {
		s.messages = append([]Message(nil), s.messages[len(s.messages)-s.maxSize:]...)
	}
}

func (s *Store) GetHistory() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	history := make([]Message, len(s.messages))
	copy(history, s.messages)
	return history
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = nil
}
