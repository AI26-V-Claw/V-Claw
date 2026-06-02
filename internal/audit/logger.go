package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// ─── Logger interface ─────────────────────────────────────────────────────────

// Logger is the interface for writing and querying audit events.
// All sandbox pipeline stages log through this interface; implementations
// may write to memory, a file, or a database.
type Logger interface {
	// Log writes an AuditEvent to the audit store.
	// It must be safe to call concurrently.
	Log(event AuditEvent) error

	// Query returns events matching the given Filter.
	// An empty Filter returns all events.
	Query(filter Filter) ([]AuditEvent, error)
}

// ─── Filter ───────────────────────────────────────────────────────────────────

// Filter restricts the events returned by Logger.Query.
// Zero-value fields are ignored (no restriction on that dimension).
type Filter struct {
	// RequestID filters to events for a specific tool invocation.
	RequestID string

	// SessionID filters to events for a specific session.
	SessionID string

	// UserID filters to events by user.
	UserID string

	// EventType filters to a specific event type.
	EventType EventType

	// Status filters to events with a specific lifecycle status.
	Status LifecycleStatus

	// RiskLevel filters to events with a specific risk level.
	RiskLevel string

	// Since returns only events at or after this time. Zero means no lower bound.
	Since time.Time

	// Until returns only events at or before this time. Zero means no upper bound.
	Until time.Time

	// Limit caps the number of events returned. Zero means no limit.
	Limit int
}

// ─── NopLogger ────────────────────────────────────────────────────────────────

// NopLogger silently discards all events. Useful in tests that don't care
// about audit output.
type NopLogger struct{}

func (n *NopLogger) Log(_ AuditEvent) error        { return nil }
func (n *NopLogger) Query(_ Filter) ([]AuditEvent, error) { return nil, nil }

// ─── MemoryLogger ─────────────────────────────────────────────────────────────

// MemoryLogger stores events in memory. Intended for unit tests and
// local development; it is not persistent across restarts.
//
// Usage:
//
//	logger := audit.NewMemoryLogger()
//	logger.Log(event)
//	events, _ := logger.Query(audit.Filter{SessionID: "sess_abc"})
type MemoryLogger struct {
	mu     sync.RWMutex
	events []AuditEvent
}

// NewMemoryLogger creates an empty MemoryLogger.
func NewMemoryLogger() *MemoryLogger {
	return &MemoryLogger{}
}

// Log appends event to the in-memory store.
func (m *MemoryLogger) Log(event AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

// Query returns all stored events matching filter.
func (m *MemoryLogger) Query(filter Filter) ([]AuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []AuditEvent
	for _, ev := range m.events {
		if !matchesFilter(ev, filter) {
			continue
		}
		results = append(results, ev)
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}
	return results, nil
}

// Count returns the total number of stored events, regardless of filter.
func (m *MemoryLogger) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.events)
}

// Clear removes all stored events. Useful between test cases.
func (m *MemoryLogger) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
}

// ─── FileLogger ───────────────────────────────────────────────────────────────

// FileLogger appends JSON lines (JSONL) to a file. Each line is one AuditEvent
// serialised as compact JSON followed by a newline.
//
// Suitable for MVP persistent logging without a database dependency.
// For production, events should be forwarded to a structured log store.
//
// Usage:
//
//	logger, err := audit.NewFileLogger("/var/vclaw/audit.jsonl")
//	defer logger.Close()
//	logger.Log(event)
type FileLogger struct {
	mu   sync.Mutex
	path string
	f    *os.File
	enc  *json.Encoder
}

// NewFileLogger opens (or creates) the file at path and returns a FileLogger.
// The file is opened in append mode so existing entries are preserved.
func NewFileLogger(path string) (*FileLogger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, fmt.Errorf("audit file logger: cannot open %q: %w", path, err)
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return &FileLogger{path: path, f: f, enc: enc}, nil
}

// Log serialises event as a JSON line and appends it to the file.
func (fl *FileLogger) Log(event AuditEvent) error {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if err := fl.enc.Encode(event); err != nil {
		return fmt.Errorf("audit file logger: encode failed: %w", err)
	}
	return nil
}

// Query reads the entire file and filters in memory.
// For large files, consider an indexed store; FileLogger is MVP only.
func (fl *FileLogger) Query(filter Filter) ([]AuditEvent, error) {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	f, err := os.Open(fl.path)
	if err != nil {
		return nil, fmt.Errorf("audit file logger: cannot read %q: %w", fl.path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var results []AuditEvent
	for dec.More() {
		var ev AuditEvent
		if err := dec.Decode(&ev); err != nil {
			continue // skip malformed lines
		}
		if !matchesFilter(ev, filter) {
			continue
		}
		results = append(results, ev)
		if filter.Limit > 0 && len(results) >= filter.Limit {
			break
		}
	}
	return results, nil
}

// Close flushes and closes the underlying file.
func (fl *FileLogger) Close() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	return fl.f.Close()
}

// ─── MultiLogger ──────────────────────────────────────────────────────────────

// MultiLogger fans out Log calls to multiple Logger implementations.
// Query is delegated to the first logger only (typically the primary store).
//
// Usage: combine MemoryLogger (for tests) and FileLogger (for persistence).
//
//	multi := audit.NewMultiLogger(fileLogger, memLogger)
type MultiLogger struct {
	loggers []Logger
}

// NewMultiLogger creates a MultiLogger backed by the given loggers.
// At least one logger must be provided.
func NewMultiLogger(loggers ...Logger) *MultiLogger {
	return &MultiLogger{loggers: loggers}
}

// Log forwards the event to every backing logger.
// Returns the first non-nil error encountered but continues logging to all.
func (m *MultiLogger) Log(event AuditEvent) error {
	var firstErr error
	for _, l := range m.loggers {
		if err := l.Log(event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Query delegates to the first logger in the list.
func (m *MultiLogger) Query(filter Filter) ([]AuditEvent, error) {
	if len(m.loggers) == 0 {
		return nil, nil
	}
	return m.loggers[0].Query(filter)
}

// ─── Filter matching ──────────────────────────────────────────────────────────

func matchesFilter(ev AuditEvent, f Filter) bool {
	if f.RequestID != "" && ev.RequestID != f.RequestID {
		return false
	}
	if f.SessionID != "" && ev.SessionID != f.SessionID {
		return false
	}
	if f.UserID != "" && ev.UserID != f.UserID {
		return false
	}
	if f.EventType != "" && ev.EventType != f.EventType {
		return false
	}
	if f.Status != "" && ev.Status != f.Status {
		return false
	}
	if f.RiskLevel != "" && ev.RiskLevel != f.RiskLevel {
		return false
	}
	if !f.Since.IsZero() && ev.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Timestamp.After(f.Until) {
		return false
	}
	return true
}
