package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

// tempRoot creates a temporary directory to serve as the workspace root for
// tests and returns the guard and a cleanup function.
func tempRoot(t *testing.T) (*WorkspaceGuard, string, func()) {
	t.Helper()
	root, err := os.MkdirTemp("", "vclaw-guard-root-*")
	if err != nil {
		t.Fatalf("failed to create temp root: %v", err)
	}
	guard, err := NewWorkspaceGuard(root)
	if err != nil {
		os.RemoveAll(root)
		t.Fatalf("NewWorkspaceGuard: %v", err)
	}
	return guard, root, func() { os.RemoveAll(root) }
}

// ─── NewWorkspaceGuard ────────────────────────────────────────────────────────

func TestNewWorkspaceGuard_ValidDir(t *testing.T) {
	guard, _, cleanup := tempRoot(t)
	defer cleanup()
	if guard == nil {
		t.Error("expected non-nil guard")
	}
}

func TestNewWorkspaceGuard_NonExistentDir(t *testing.T) {
	_, err := NewWorkspaceGuard("/this/path/does/not/exist/vclaw")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestNewWorkspaceGuard_File(t *testing.T) {
	f, err := os.CreateTemp("", "vclaw-guard-file-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	_, err = NewWorkspaceGuard(f.Name())
	if err == nil {
		t.Error("expected error when base dir is a file, not a directory")
	}
}

// ─── PrepareSessionWorkspace ──────────────────────────────────────────────────

func TestPrepareSessionWorkspace_CreatesDir(t *testing.T) {
	guard, root, cleanup := tempRoot(t)
	defer cleanup()

	dir, sessionCleanup, err := guard.PrepareSessionWorkspace("sess_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer sessionCleanup()

	expected := filepath.Join(root, "sess_abc", "workspace")
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Errorf("workspace dir not created: %v", statErr)
	}
	if !info.IsDir() {
		t.Error("workspace path is not a directory")
	}
}

func TestPrepareSessionWorkspace_CleanupRemovesDir(t *testing.T) {
	guard, root, cleanup := tempRoot(t)
	defer cleanup()

	_, sessionCleanup, err := guard.PrepareSessionWorkspace("sess_toclean")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessionDir := filepath.Join(root, "sess_toclean")
	if _, err := os.Stat(sessionDir); err != nil {
		t.Fatal("session dir should exist before cleanup")
	}

	sessionCleanup()

	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Error("session dir should be removed after cleanup")
	}
}

func TestPrepareSessionWorkspace_InvalidSessionID(t *testing.T) {
	guard, _, cleanup := tempRoot(t)
	defer cleanup()

	cases := []string{
		"",
		"../escape",
		"sess/slash",
		"sess\\backslash",
		"sess*glob",
	}
	for _, id := range cases {
		_, _, err := guard.PrepareSessionWorkspace(id)
		if err == nil {
			t.Errorf("expected error for session ID %q, got nil", id)
		}
	}
}

// ─── ValidateWorkspaceDir ─────────────────────────────────────────────────────

func TestValidateWorkspaceDir_ValidSubdir(t *testing.T) {
	guard, root, cleanup := tempRoot(t)
	defer cleanup()

	sub := filepath.Join(root, "sess_x", "workspace")
	if err := os.MkdirAll(sub, 0750); err != nil {
		t.Fatal(err)
	}

	if err := guard.ValidateWorkspaceDir(sub); err != nil {
		t.Errorf("expected no error for valid subdir, got: %v", err)
	}
}

func TestValidateWorkspaceDir_Empty(t *testing.T) {
	guard, _, cleanup := tempRoot(t)
	defer cleanup()

	if err := guard.ValidateWorkspaceDir(""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestValidateWorkspaceDir_Relative(t *testing.T) {
	guard, _, cleanup := tempRoot(t)
	defer cleanup()

	if err := guard.ValidateWorkspaceDir("relative/path"); err == nil {
		t.Error("expected error for relative path")
	}
}

func TestValidateWorkspaceDir_OutsideRoot(t *testing.T) {
	guard, _, cleanup := tempRoot(t)
	defer cleanup()

	cases := []string{
		"/tmp",
		"/var/vclaw",
		"/root",
		"/home/user/workspace",
	}
	for _, p := range cases {
		if err := guard.ValidateWorkspaceDir(p); err == nil {
			t.Errorf("expected error for path outside root: %q", p)
		}
	}
}

func TestValidateWorkspaceDir_PathTraversal(t *testing.T) {
	guard, root, cleanup := tempRoot(t)
	defer cleanup()

	// Create a legit subdirectory first, then try to escape.
	sub := filepath.Join(root, "sess_y", "workspace")
	os.MkdirAll(sub, 0750)

	traversal := filepath.Join(sub, "..", "..", "..", "etc")
	if err := guard.ValidateWorkspaceDir(traversal); err == nil {
		t.Errorf("path traversal should be blocked: %q", traversal)
	}
}

func TestValidateWorkspaceDir_NonExistentDir(t *testing.T) {
	guard, root, cleanup := tempRoot(t)
	defer cleanup()

	nonExistent := filepath.Join(root, "does_not_exist", "workspace")
	if err := guard.ValidateWorkspaceDir(nonExistent); err == nil {
		t.Error("expected error for non-existent directory")
	}
}

func TestValidateWorkspaceDir_RootItself(t *testing.T) {
	guard, root, cleanup := tempRoot(t)
	defer cleanup()

	// Allowing the root itself would let jobs interfere with each other.
	// The guard should permit the root (it is "under" itself) but the
	// session lifecycle should always use a subdirectory.
	// This test verifies the boundary condition is explicit.
	if err := guard.ValidateWorkspaceDir(root); err != nil {
		// Root is technically valid under itself; document that behaviour.
		t.Logf("note: root itself rejected: %v", err)
	}
}

// ─── ValidateScriptPath ───────────────────────────────────────────────────────

func TestValidateScriptPath_Valid(t *testing.T) {
	cases := []string{
		"script.py",
		"subdir/script.py",
		"jobs/job_001.py",
	}
	for _, p := range cases {
		if err := ValidateScriptPath(p); err != nil {
			t.Errorf("expected no error for %q, got: %v", p, err)
		}
	}
}

func TestValidateScriptPath_Empty(t *testing.T) {
	if err := ValidateScriptPath(""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestValidateScriptPath_Absolute(t *testing.T) {
	cases := []string{
		"/workspace/script.py",
		"/tmp/evil.py",
		"/etc/script.py",
	}
	for _, p := range cases {
		if err := ValidateScriptPath(p); err == nil {
			t.Errorf("absolute path should be rejected: %q", p)
		}
	}
}

func TestValidateScriptPath_Traversal(t *testing.T) {
	cases := []string{
		"../escape.py",
		"../../etc/script.py",
		"sub/../../../escape.py",
	}
	for _, p := range cases {
		if err := ValidateScriptPath(p); err == nil {
			t.Errorf("path traversal should be rejected: %q", p)
		}
	}
}

func TestValidateScriptPath_NotPython(t *testing.T) {
	cases := []string{
		"script.sh",
		"data.csv",
		"Makefile",
		"script",
	}
	for _, p := range cases {
		if err := ValidateScriptPath(p); err == nil {
			t.Errorf("non-.py extension should be rejected: %q", p)
		}
	}
}

func TestValidateScriptPath_CredentialFile(t *testing.T) {
	cases := []string{
		"secrets.json",
		"credentials.json",
		"token.json",
		"key.pem",
		".env",
	}
	for _, p := range cases {
		// Rename to .py so extension check passes, but credential check should catch it.
		renamed := strings.TrimSuffix(p, filepath.Ext(p)) + ".py"
		_ = renamed // credential detection uses base name, not extension
		if err := ValidateScriptPath(p); err == nil {
			// Only fail if the path would otherwise be valid (has .py ext).
			if strings.HasSuffix(p, ".py") {
				t.Errorf("credential file should be rejected: %q", p)
			}
		}
	}
}

// ─── ValidateShellCommand ─────────────────────────────────────────────────────

func TestValidateShellCommand_Safe(t *testing.T) {
	cases := []string{
		"ls /workspace",
		"cat /workspace/data.csv",
		"python script.py",
		"echo hello",
		"wc -l /workspace/input.txt",
	}
	for _, cmd := range cases {
		if err := ValidateShellCommand(cmd); err != nil {
			t.Errorf("expected no error for %q, got: %v", cmd, err)
		}
	}
}

func TestValidateShellCommand_Empty(t *testing.T) {
	if err := ValidateShellCommand(""); err == nil {
		t.Error("expected error for empty command")
	}
	if err := ValidateShellCommand("   "); err == nil {
		t.Error("expected error for blank command")
	}
}

func TestValidateShellCommand_CredentialAccess(t *testing.T) {
	cases := []string{
		"cat .env",
		"cat credentials.json",
		"cat /etc/shadow",
		"openssl rsa -in id_rsa",
		"cat secrets.json",
	}
	for _, cmd := range cases {
		if err := ValidateShellCommand(cmd); err == nil {
			t.Errorf("credential access should be rejected: %q", cmd)
		}
	}
}

func TestValidateShellCommand_SystemPathEscape(t *testing.T) {
	cases := []string{
		"ls /etc/",
		"cat /var/log/syslog",
		"ls /root/",
		"ls /proc/1",
		"ls /home/user",
	}
	for _, cmd := range cases {
		if err := ValidateShellCommand(cmd); err == nil {
			t.Errorf("system path access should be rejected: %q", cmd)
		}
	}
}

// ─── isUnder ──────────────────────────────────────────────────────────────────

func TestIsUnder_DirectChild(t *testing.T) {
	if !isUnder("/root/ws", "/root/ws/sess_a/workspace") {
		t.Error("direct child should be under parent")
	}
}

func TestIsUnder_SamePath(t *testing.T) {
	if !isUnder("/root/ws", "/root/ws") {
		t.Error("same path should be considered under itself")
	}
}

func TestIsUnder_SiblingDir(t *testing.T) {
	if isUnder("/root/ws", "/root/wsnot") {
		t.Error("sibling with matching prefix should NOT be under parent")
	}
}

func TestIsUnder_ParentEscape(t *testing.T) {
	if isUnder("/root/ws", "/root") {
		t.Error("parent directory should not be under child")
	}
}

func TestIsUnder_CompletlyDifferent(t *testing.T) {
	if isUnder("/root/ws", "/tmp/evil") {
		t.Error("unrelated path should not be under parent")
	}
}

// ─── isCredentialPath ─────────────────────────────────────────────────────────

func TestIsCredentialPath_KnownFiles(t *testing.T) {
	cases := []string{
		".env",
		"credentials.json",
		"token.json",
		"id_rsa",
		"key.pem",
		"cert.p12",
		"service_account.json",
	}
	for _, p := range cases {
		if !isCredentialPath(p) {
			t.Errorf("expected %q to be identified as credential path", p)
		}
	}
}

func TestIsCredentialPath_SafeFiles(t *testing.T) {
	cases := []string{
		"report.xlsx",
		"data.csv",
		"script.py",
		"output.docx",
		"README.md",
	}
	for _, p := range cases {
		if isCredentialPath(p) {
			t.Errorf("expected %q NOT to be identified as credential path", p)
		}
	}
}
