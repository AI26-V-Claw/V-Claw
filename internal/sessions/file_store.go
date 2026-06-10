package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"vclaw/internal/providers"
)

// FileStore persists session transcripts and memory as JSON files under baseDir.
// Layout: {baseDir}/{sessionID}/transcript.json and memory.json.
// Writes are atomic (write-to-temp + rename) to prevent file corruption on crash.
type FileStore struct {
	baseDir string
	mu      sync.Map // map[sanitized sessionID string] -> *sync.RWMutex
}

// NewFileStore creates a FileStore rooted at {baseDir}/sessions.
// The directory is created if it does not exist.
func NewFileStore(baseDir string) (*FileStore, error) {
	dir := filepath.Join(baseDir, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &FileStore{baseDir: dir}, nil
}

func (s *FileStore) sessionDir(sessionID string) string {
	return filepath.Join(s.baseDir, sanitizeSessionID(sessionID))
}

func (s *FileStore) transcriptPath(sessionID string) string {
	return filepath.Join(s.sessionDir(sessionID), "transcript.json")
}

func (s *FileStore) memoryPath(sessionID string) string {
	return filepath.Join(s.sessionDir(sessionID), "memory.json")
}

func (s *FileStore) sessionMu(sessionID string) *sync.RWMutex {
	v, _ := s.mu.LoadOrStore(sanitizeSessionID(sessionID), &sync.RWMutex{})
	return v.(*sync.RWMutex)
}

func (s *FileStore) LoadTranscript(_ context.Context, sessionID string) ([]providers.Message, error) {
	mu := s.sessionMu(sessionID)
	mu.RLock()
	defer mu.RUnlock()
	return s.readTranscript(sessionID)
}

func (s *FileStore) AppendMessage(_ context.Context, sessionID string, message providers.Message) error {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	messages, err := s.readTranscript(sessionID)
	if err != nil {
		return err
	}
	messages = append(messages, cloneMessage(message))
	return atomicWriteJSON(s.transcriptPath(sessionID), messages)
}

func (s *FileStore) SetTranscript(_ context.Context, sessionID string, messages []providers.Message) error {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return atomicWriteJSON(s.transcriptPath(sessionID), messages)
}

func (s *FileStore) ClearSession(_ context.Context, sessionID string) error {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return os.RemoveAll(s.sessionDir(sessionID))
}

func (s *FileStore) LoadMemory(_ context.Context, sessionID string) (SessionMemory, error) {
	mu := s.sessionMu(sessionID)
	mu.RLock()
	defer mu.RUnlock()
	data, err := os.ReadFile(s.memoryPath(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return SessionMemory{}, nil
	}
	if err != nil {
		return SessionMemory{}, err
	}
	var memory SessionMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return SessionMemory{}, err
	}
	return memory, nil
}

func (s *FileStore) SaveMemory(_ context.Context, sessionID string, memory SessionMemory) error {
	mu := s.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	return atomicWriteJSON(s.memoryPath(sessionID), memory)
}

func (s *FileStore) readTranscript(sessionID string) ([]providers.Message, error) {
	data, err := os.ReadFile(s.transcriptPath(sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var messages []providers.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// atomicWriteJSON writes v as JSON to path using temp-file-then-rename so a
// crash mid-write leaves the original file intact.
func atomicWriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sanitizeSessionID replaces characters unsafe for directory names with underscores,
// keeping ASCII alphanumerics, hyphens, and underscores.
func sanitizeSessionID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r <= 127 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if result == "" {
		return "_empty"
	}
	return result
}
