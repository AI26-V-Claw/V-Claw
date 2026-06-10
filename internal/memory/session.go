package memory

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// MessageRole represents the role of a message in conversation
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
)

// Message represents a single message in conversation history
type Message struct {
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	Timestamp time.Time   `json:"timestamp"`
}

// SessionMemory manages short-term conversation history with memory isolation
type SessionMemory struct {
	mu       sync.RWMutex
	messages []Message
	maxTurns int // Maximum number of turns to retain
}

// NewSessionMemory creates a new session memory with a maximum number of turns
func NewSessionMemory(maxTurns int) *SessionMemory {
	if maxTurns <= 0 {
		maxTurns = 20 // Default
	}
	return &SessionMemory{
		messages: make([]Message, 0),
		maxTurns: maxTurns,
	}
}

// AddMessage adds a new message to the session history
func (sm *SessionMemory) AddMessage(role MessageRole, content string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.messages = append(sm.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})

	// Trim to maxTurns
	if len(sm.messages) > sm.maxTurns {
		sm.messages = sm.messages[len(sm.messages)-sm.maxTurns:]
	}
}

// GetFullHistory returns all messages in session
func (sm *SessionMemory) GetFullHistory() []Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]Message, len(sm.messages))
	copy(result, sm.messages)
	return result
}

// GetRecentHistory returns the last N messages
func (sm *SessionMemory) GetRecentHistory(n int) []Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if n <= 0 || len(sm.messages) == 0 {
		return []Message{}
	}

	start := 0
	if len(sm.messages) > n {
		start = len(sm.messages) - n
	}

	result := make([]Message, len(sm.messages)-start)
	copy(result, sm.messages[start:])
	return result
}

// GetFilteredHistoryForDangerousAction returns a very narrow context for
// DANGEROUS_ACTION intents. This implements the memory isolation requirement:
// only the last few messages are provided, and a hard reminder is prepended
// instructing the AI to ONLY use parameters from the latest user message.
//
// Parameters:
//   - maxRecentTurns: maximum number of recent messages to include (recommended: 3)
//
// Returns:
//   - filtered history as []string for prompt injection
//   - isolation warning text to prepend to the prompt
func (sm *SessionMemory) GetFilteredHistoryForDangerousAction(maxRecentTurns int) ([]string, string) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if maxRecentTurns <= 0 {
		maxRecentTurns = 3
	}

	// Only take the last few messages
	start := 0
	if len(sm.messages) > maxRecentTurns {
		start = len(sm.messages) - maxRecentTurns
	}

	history := make([]string, 0, len(sm.messages)-start)
	for i := start; i < len(sm.messages); i++ {
		msg := sm.messages[i]
		history = append(history, fmt.Sprintf("[%s]: %s", msg.Role, msg.Content))
	}

	// Hard isolation warning
	isolationWarning := strings.Join([]string{
		"⚠️ CẢNH BÁO CÁCH LY BỘ NHỚ (MEMORY ISOLATION WARNING) ⚠️",
		"",
		"Bạn đang xử lý một lệnh DANGEROUS_ACTION.",
		"Quy tắc bắt buộc:",
		"1. CHỈ sử dụng các tham số được cung cấp TRỰC TIẾP trong câu thoại CUỐI CÙNG của người dùng.",
		"2. KHÔNG được tự ý sao chép tham số từ hội thoại cũ hơn trừ khi người dùng chỉ thị rõ ràng.",
		"3. Nếu thiếu tham số bắt buộc → trả về needs_clarification = true.",
		"4. Khi không chắc chắn → Hỏi lại, ĐỪNG đoán mò.",
	}, "\n")

	return history, isolationWarning
}

// GetHistoryForReadInfo returns broader context for safe READ_INFO intents.
// Safe actions can leverage more conversation history.
func (sm *SessionMemory) GetHistoryForReadInfo(maxTurns int) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if maxTurns <= 0 {
		maxTurns = 10
	}

	start := 0
	if len(sm.messages) > maxTurns {
		start = len(sm.messages) - maxTurns
	}

	history := make([]string, 0, len(sm.messages)-start)
	for i := start; i < len(sm.messages); i++ {
		msg := sm.messages[i]
		history = append(history, fmt.Sprintf("[%s]: %s", msg.Role, msg.Content))
	}

	return history
}

// GetLastUserMessage returns the last message from the user
func (sm *SessionMemory) GetLastUserMessage() (Message, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for i := len(sm.messages) - 1; i >= 0; i-- {
		if sm.messages[i].Role == RoleUser {
			return sm.messages[i], true
		}
	}

	return Message{}, false
}

// Clear removes all messages from the session
func (sm *SessionMemory) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.messages = make([]Message, 0)
}

// Len returns the number of messages in the session
func (sm *SessionMemory) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.messages)
}

// Store interface for compatibility with other implementations
type Role string

const (
	RoleUserCompat      Role = "user"
	RoleAssistantCompat Role = "assistant"
)

type StoreMessage struct {
	Role      Role      `json:"role"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type Store struct {
	mu       sync.Mutex
	sessions map[string][]StoreMessage
	maxSize  int
}

func NewStore() *Store {
	return &Store{
		sessions: map[string][]StoreMessage{},
		maxSize:  20,
	}
}

func (s *Store) Append(sessionID string, role Role, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizedSessionID(sessionID)
	s.sessions[key] = append(s.sessions[key], StoreMessage{
		Role:      role,
		Text:      strings.TrimSpace(text),
		Timestamp: time.Now().UTC(),
	})
	if len(s.sessions[key]) > s.maxSize {
		s.sessions[key] = append([]StoreMessage(nil), s.sessions[key][len(s.sessions[key])-s.maxSize:]...)
	}
}

func (s *Store) GetHistory(sessionID string) []StoreMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := normalizedSessionID(sessionID)
	history := make([]StoreMessage, len(s.sessions[key]))
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
