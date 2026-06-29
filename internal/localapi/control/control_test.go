package control

import (
	"context"
	"testing"
)

type fakeCanceller struct {
	sessionID string
	active    bool
}

func (f *fakeCanceller) CancelSession(sessionID string) bool {
	f.sessionID = sessionID
	return f.active
}

func TestCancelRoutesToRunningControlServer(t *testing.T) {
	t.Setenv("VCLAW_CONTROL_IPC_ENABLED", "true")
	dataDir := t.TempDir()
	canceller := &fakeCanceller{active: true}
	server, err := Start(context.Background(), dataDir, canceller)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer server.Close(context.Background())

	response, err := Cancel(context.Background(), dataDir, "dev")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if response.Status != "cancelled" || response.SessionID != "dev" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if canceller.sessionID != "dev" {
		t.Fatalf("CancelSession session = %q, want dev", canceller.sessionID)
	}
}

func TestCancelReportsNoActiveRun(t *testing.T) {
	t.Setenv("VCLAW_CONTROL_IPC_ENABLED", "true")
	dataDir := t.TempDir()
	server, err := Start(context.Background(), dataDir, &fakeCanceller{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer server.Close(context.Background())

	response, err := Cancel(context.Background(), dataDir, "dev")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if response.Status != "no_active_run" {
		t.Fatalf("status = %q, want no_active_run", response.Status)
	}
}

func TestCancelFailsWhenControlServerNotRunning(t *testing.T) {
	_, err := Cancel(context.Background(), t.TempDir(), "dev")
	if err == nil {
		t.Fatal("expected error when control manifest is missing")
	}
}

func TestCloseDoesNotRemoveAnotherProcessManifest(t *testing.T) {
	t.Setenv("VCLAW_CONTROL_IPC_ENABLED", "true")
	dataDir := t.TempDir()
	inactive := &fakeCanceller{}
	active := &fakeCanceller{active: true}
	first, err := Start(context.Background(), dataDir, inactive)
	if err != nil {
		t.Fatalf("Start(first) error = %v", err)
	}
	second, err := Start(context.Background(), dataDir, active)
	if err != nil {
		t.Fatalf("Start(second) error = %v", err)
	}
	defer second.Close(context.Background())

	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	response, err := Cancel(context.Background(), dataDir, "dev")
	if err != nil {
		t.Fatalf("Cancel() after first close error = %v", err)
	}
	if response.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", response.Status)
	}
	if active.sessionID != "dev" {
		t.Fatalf("active CancelSession session = %q, want dev", active.sessionID)
	}
}
