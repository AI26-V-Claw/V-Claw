package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"vclaw/internal/tools"
)

type fileRuntimeStateSnapshot struct {
	Runs      map[string]RunState     `json:"runs"`
	Actions   map[string]ActionRecord `json:"actions"`
	ToolCalls []ToolCallRecord        `json:"toolCalls,omitempty"`
	Events    []RunEvent              `json:"events,omitempty"`
}

// FileRuntimeStateStore persists run and approval state so pending actions can
// be resumed after a process restart.
type FileRuntimeStateStore struct {
	mu     sync.Mutex
	path   string
	memory *InMemoryRuntimeStateStore
}

func NewFileRuntimeStateStore(dataDir string) (*FileRuntimeStateStore, error) {
	if dataDir == "" {
		dataDir = "./data"
	}
	store := &FileRuntimeStateStore{
		path:   filepath.Join(dataDir, "runtime-state.json"),
		memory: NewInMemoryRuntimeStateStore(),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileRuntimeStateStore) CreateRun(ctx context.Context, state RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.memory.CreateRun(ctx, state); err != nil {
		return err
	}
	return s.persist()
}

func (s *FileRuntimeStateStore) GetRun(ctx context.Context, runID string) (RunState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.GetRun(ctx, runID)
}

func (s *FileRuntimeStateStore) UpdateRun(ctx context.Context, state RunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.memory.UpdateRun(ctx, state); err != nil {
		return err
	}
	return s.persist()
}

func (s *FileRuntimeStateStore) FindOrCreateAction(ctx context.Context, record ActionRecord) (ActionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, created, err := s.memory.FindOrCreateAction(ctx, record)
	if err != nil {
		return ActionRecord{}, false, err
	}
	if err := s.persist(); err != nil {
		return ActionRecord{}, false, err
	}
	return action, created, nil
}

func (s *FileRuntimeStateStore) GetAction(ctx context.Context, actionID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.GetAction(ctx, actionID)
}

func (s *FileRuntimeStateStore) GetActionByApprovalID(ctx context.Context, approvalID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.GetActionByApprovalID(ctx, approvalID)
}

func (s *FileRuntimeStateStore) FindLatestPendingApproval(ctx context.Context, sessionID string) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memory.FindLatestPendingApproval(ctx, sessionID)
}

func (s *FileRuntimeStateStore) MarkActionApproved(ctx context.Context, actionID string) (ActionRecord, error) {
	return s.updateAction(func() (ActionRecord, error) {
		return s.memory.MarkActionApproved(ctx, actionID)
	})
}

func (s *FileRuntimeStateStore) MarkActionRejected(ctx context.Context, actionID string) (ActionRecord, error) {
	return s.updateAction(func() (ActionRecord, error) {
		return s.memory.MarkActionRejected(ctx, actionID)
	})
}

func (s *FileRuntimeStateStore) MarkActionExpired(ctx context.Context, actionID string) (ActionRecord, error) {
	return s.updateAction(func() (ActionRecord, error) {
		return s.memory.MarkActionExpired(ctx, actionID)
	})
}

func (s *FileRuntimeStateStore) TryMarkActionExecuting(ctx context.Context, actionID string) (ActionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, changed, err := s.memory.TryMarkActionExecuting(ctx, actionID)
	if err != nil {
		return ActionRecord{}, false, err
	}
	if changed {
		if err := s.persist(); err != nil {
			return ActionRecord{}, false, err
		}
	}
	return action, changed, nil
}

func (s *FileRuntimeStateStore) CompleteAction(ctx context.Context, actionID string, result tools.ToolResult) (ActionRecord, error) {
	return s.updateAction(func() (ActionRecord, error) {
		return s.memory.CompleteAction(ctx, actionID, result)
	})
}

func (s *FileRuntimeStateStore) FailAction(ctx context.Context, actionID string, result tools.ToolResult) (ActionRecord, error) {
	return s.updateAction(func() (ActionRecord, error) {
		return s.memory.FailAction(ctx, actionID, result)
	})
}

func (s *FileRuntimeStateStore) RecordToolCall(ctx context.Context, record ToolCallRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.memory.RecordToolCall(ctx, record); err != nil {
		return err
	}
	return s.persist()
}

func (s *FileRuntimeStateStore) AppendRunEvent(ctx context.Context, event RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.memory.AppendRunEvent(ctx, event); err != nil {
		return err
	}
	return s.persist()
}

func (s *FileRuntimeStateStore) updateAction(update func() (ActionRecord, error)) (ActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	action, err := update()
	if err != nil {
		return ActionRecord{}, err
	}
	if err := s.persist(); err != nil {
		return ActionRecord{}, err
	}
	return action, nil
}

func (s *FileRuntimeStateStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot fileRuntimeStateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	if snapshot.Runs != nil {
		s.memory.runs = snapshot.Runs
	}
	if snapshot.Actions != nil {
		s.memory.actions = snapshot.Actions
	}
	s.memory.toolCalls = snapshot.ToolCalls
	s.memory.events = snapshot.Events
	for actionID, action := range s.memory.actions {
		if action.ApprovalID != "" {
			s.memory.actionsByApprovalID[action.ApprovalID] = actionID
		}
		if action.IdempotencyKey != "" {
			s.memory.actionsByIdempotency[action.IdempotencyKey] = actionID
		}
	}
	return nil
}

func (s *FileRuntimeStateStore) persist() error {
	s.memory.mu.Lock()
	snapshot := fileRuntimeStateSnapshot{
		Runs:      make(map[string]RunState, len(s.memory.runs)),
		Actions:   make(map[string]ActionRecord, len(s.memory.actions)),
		ToolCalls: make([]ToolCallRecord, len(s.memory.toolCalls)),
		Events:    make([]RunEvent, len(s.memory.events)),
	}
	for key, state := range s.memory.runs {
		snapshot.Runs[key] = cloneRunState(state)
	}
	for key, action := range s.memory.actions {
		snapshot.Actions[key] = cloneActionRecord(action)
	}
	for i, record := range s.memory.toolCalls {
		snapshot.ToolCalls[i] = cloneToolCallRecord(record)
	}
	for i, event := range s.memory.events {
		snapshot.Events[i] = cloneRunEvent(event)
	}
	s.memory.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
