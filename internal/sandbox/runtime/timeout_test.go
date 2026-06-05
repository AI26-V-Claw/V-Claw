package runtime

import (
	"testing"
	"time"
)

// ─── EffectivePythonTimeout ───────────────────────────────────────────────────

func TestEffectivePythonTimeout_UseDefault(t *testing.T) {
	req := &RunPythonRequest{Timeout: 0}
	got := EffectivePythonTimeout(req)
	if got != DefaultPythonTimeout {
		t.Errorf("expected default %v, got %v", DefaultPythonTimeout, got)
	}
}

func TestEffectivePythonTimeout_UseCustom(t *testing.T) {
	want := 5 * time.Second
	req := &RunPythonRequest{Timeout: want}
	got := EffectivePythonTimeout(req)
	if got != want {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestEffectivePythonTimeout_NegativeUsesDefault(t *testing.T) {
	req := &RunPythonRequest{Timeout: -1 * time.Second}
	got := EffectivePythonTimeout(req)
	if got != DefaultPythonTimeout {
		t.Errorf("negative timeout should fall back to default: got %v", got)
	}
}

// ─── EffectiveShellTimeout ────────────────────────────────────────────────────

func TestEffectiveShellTimeout_UseDefault(t *testing.T) {
	req := &RunShellRequest{Timeout: 0}
	got := EffectiveShellTimeout(req)
	if got != DefaultShellTimeout {
		t.Errorf("expected default %v, got %v", DefaultShellTimeout, got)
	}
}

func TestEffectiveShellTimeout_UseCustom(t *testing.T) {
	want := 3 * time.Second
	req := &RunShellRequest{Timeout: want}
	got := EffectiveShellTimeout(req)
	if got != want {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// ─── TruncateOutput ───────────────────────────────────────────────────────────

func TestTruncateOutput_ShortString(t *testing.T) {
	s := "hello"
	out, truncated := TruncateOutput(s)
	if out != s {
		t.Errorf("expected %q, got %q", s, out)
	}
	if truncated {
		t.Error("short string should not be truncated")
	}
}

func TestTruncateOutput_ExactLimit(t *testing.T) {
	s := string(make([]byte, MaxOutputBytes))
	out, truncated := TruncateOutput(s)
	if len(out) != MaxOutputBytes {
		t.Errorf("expected %d bytes, got %d", MaxOutputBytes, len(out))
	}
	if truncated {
		t.Error("string at exact limit should not be truncated")
	}
}

func TestTruncateOutput_OverLimit(t *testing.T) {
	s := string(make([]byte, MaxOutputBytes+100))
	out, truncated := TruncateOutput(s)
	if len(out) != MaxOutputBytes {
		t.Errorf("expected %d bytes after truncation, got %d", MaxOutputBytes, len(out))
	}
	if !truncated {
		t.Error("string over limit should be truncated")
	}
}

func TestTruncateOutput_Empty(t *testing.T) {
	out, truncated := TruncateOutput("")
	if out != "" {
		t.Errorf("expected empty string, got %q", out)
	}
	if truncated {
		t.Error("empty string should not be truncated")
	}
}

// ─── ValidateRunPythonRequest ─────────────────────────────────────────────────

func TestValidateRunPythonRequest_Valid(t *testing.T) {
	req := &RunPythonRequest{
		RequestID:    "req_1",
		SessionID:    "sess_1",
		WorkspaceDir: "/workspace",
		Code:         "print('hello')",
	}
	if err := ValidateRunPythonRequest(req); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateRunPythonRequest_NilRequest(t *testing.T) {
	if err := ValidateRunPythonRequest(nil); err == nil {
		t.Error("nil request should return error")
	}
}

func TestValidateRunPythonRequest_MissingRequestID(t *testing.T) {
	req := &RunPythonRequest{SessionID: "sess_1", WorkspaceDir: "/w", Code: "x"}
	if err := ValidateRunPythonRequest(req); err == nil {
		t.Error("missing request_id should return error")
	}
}

func TestValidateRunPythonRequest_MissingSessionID(t *testing.T) {
	req := &RunPythonRequest{RequestID: "req_1", WorkspaceDir: "/w", Code: "x"}
	if err := ValidateRunPythonRequest(req); err == nil {
		t.Error("missing session_id should return error")
	}
}

func TestValidateRunPythonRequest_MissingWorkspaceDir(t *testing.T) {
	req := &RunPythonRequest{RequestID: "req_1", SessionID: "sess_1", Code: "x"}
	if err := ValidateRunPythonRequest(req); err == nil {
		t.Error("missing workspace_dir should return error")
	}
}

func TestValidateRunPythonRequest_BothCodeAndScript(t *testing.T) {
	req := &RunPythonRequest{
		RequestID:    "req_1",
		SessionID:    "sess_1",
		WorkspaceDir: "/w",
		Code:         "print(1)",
		ScriptPath:   "script.py",
	}
	if err := ValidateRunPythonRequest(req); err == nil {
		t.Error("both code and script_path should return error")
	}
}

func TestValidateRunPythonRequest_NeitherCodeNorScript(t *testing.T) {
	req := &RunPythonRequest{
		RequestID:    "req_1",
		SessionID:    "sess_1",
		WorkspaceDir: "/w",
	}
	if err := ValidateRunPythonRequest(req); err == nil {
		t.Error("neither code nor script_path should return error")
	}
}

// ─── ValidateRunShellRequest ──────────────────────────────────────────────────

func TestValidateRunShellRequest_Valid(t *testing.T) {
	req := &RunShellRequest{
		RequestID:    "req_1",
		SessionID:    "sess_1",
		WorkspaceDir: "/workspace",
		Command:      "ls /workspace",
	}
	if err := ValidateRunShellRequest(req); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateRunShellRequest_NilRequest(t *testing.T) {
	if err := ValidateRunShellRequest(nil); err == nil {
		t.Error("nil request should return error")
	}
}

func TestValidateRunShellRequest_MissingCommand(t *testing.T) {
	req := &RunShellRequest{
		RequestID:    "req_1",
		SessionID:    "sess_1",
		WorkspaceDir: "/w",
		Command:      "   ",
	}
	if err := ValidateRunShellRequest(req); err == nil {
		t.Error("blank command should return error")
	}
}

// ─── newJobID ─────────────────────────────────────────────────────────────────

func TestNewJobID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := newJobID()
		if ids[id] {
			t.Fatalf("duplicate job ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestNewJobID_NotEmpty(t *testing.T) {
	id := newJobID()
	if id == "" {
		t.Error("job ID must not be empty")
	}
}

// ─── DockerRunner config defaults ─────────────────────────────────────────────

func TestNewDockerRunner_Defaults(t *testing.T) {
	r := NewDockerRunner(DockerRunnerConfig{})
	if r.cfg.Image != "vclaw-sandbox:latest" {
		t.Errorf("expected default image, got %q", r.cfg.Image)
	}
	if r.cfg.MaxOutputBytes != MaxOutputBytes {
		t.Errorf("expected default MaxOutputBytes %d, got %d", MaxOutputBytes, r.cfg.MaxOutputBytes)
	}
	if r.cfg.StopTimeoutSec != 3 {
		t.Errorf("expected default StopTimeoutSec 3, got %d", r.cfg.StopTimeoutSec)
	}
}

func TestNewDockerRunner_CustomValues(t *testing.T) {
	r := NewDockerRunner(DockerRunnerConfig{
		Image:          "my-image:v2",
		MaxOutputBytes: 4096,
		StopTimeoutSec: 10,
	})
	if r.cfg.Image != "my-image:v2" {
		t.Errorf("expected custom image, got %q", r.cfg.Image)
	}
	if r.cfg.MaxOutputBytes != 4096 {
		t.Errorf("expected 4096, got %d", r.cfg.MaxOutputBytes)
	}
	if r.cfg.StopTimeoutSec != 10 {
		t.Errorf("expected 10, got %d", r.cfg.StopTimeoutSec)
	}
}
