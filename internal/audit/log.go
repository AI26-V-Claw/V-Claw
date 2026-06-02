package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Channel   string
	Intent    string
	RiskLevel string
	Message   string
}

type InMemoryLog struct {
	mu     sync.Mutex
	events []Event
}

func NewInMemory() *InMemoryLog {
	return &InMemoryLog{}
}

func (l *InMemoryLog) Record(event Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

type Entry struct {
	Timestamp    string  `json:"timestamp"`
	RequestID    string  `json:"request_id,omitempty"`
	UpdateID     int64   `json:"update_id"`
	Channel      string  `json:"channel,omitempty"`
	ChatID       int64   `json:"chat_id"`
	SessionID    string  `json:"session_id,omitempty"`
	Input        string  `json:"input"`
	Intent       string  `json:"intent"`
	SystemOpType string  `json:"system_op_type"`
	Confidence   float64 `json:"confidence"`
	ActionTaken  string  `json:"action_taken"`
	Output       string  `json:"output"`
	HitlRequired bool    `json:"hitl_required"`
	Error        string  `json:"error,omitempty"`
}

type Logger struct {
	path string
	mu   sync.Mutex
}

func NewLogger(path string) *Logger {
	return &Logger{path: path}
}

func (l *Logger) Record(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	return json.NewEncoder(file).Encode(entry)
}
