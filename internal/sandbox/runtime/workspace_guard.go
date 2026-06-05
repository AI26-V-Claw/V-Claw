package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ─── WorkspaceGuard ───────────────────────────────────────────────────────────

// WorkspaceGuard enforces that every sandbox job is confined to a
// session-scoped subdirectory under a single trusted root on the host.
//
// Two-layer protection:
//
//  1. Host-side (this package): validates paths before Docker is involved.
//     Blocks path traversal, absolute escapes, and credential file patterns.
//
//  2. Container-side (Docker flags): --read-only rootfs + bind-mount only
//     /workspace. Even if the guard were bypassed, Docker prevents writes
//     outside the mount point.
//
// Usage:
//
//	guard := runtime.NewWorkspaceGuard("/var/vclaw/workspaces")
//	dir, cleanup, err := guard.PrepareSessionWorkspace("sess_abc")
//	defer cleanup()
type WorkspaceGuard struct {
	// root is the canonical absolute path of the trusted workspace base.
	// All session workspaces must be direct children of this directory.
	root string
}

// NewWorkspaceGuard creates a WorkspaceGuard rooted at baseDir.
// baseDir is resolved to its real absolute path (symlinks followed).
// An error is returned if baseDir does not exist or is not a directory.
func NewWorkspaceGuard(baseDir string) (*WorkspaceGuard, error) {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("workspace guard: cannot resolve base dir %q: %w", baseDir, err)
	}
	// Resolve symlinks so comparisons below work correctly.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Base dir may not exist yet; callers can create it and retry.
		return nil, fmt.Errorf("workspace guard: base dir %q does not exist: %w", abs, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Errorf("workspace guard: cannot stat base dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace guard: base dir %q is not a directory", real)
	}
	return &WorkspaceGuard{root: real}, nil
}

// Root returns the canonical workspace root path.
func (g *WorkspaceGuard) Root() string { return g.root }

// ─── Session workspace lifecycle ─────────────────────────────────────────────

// PrepareSessionWorkspace creates (if necessary) the workspace directory for
// the given session and returns its absolute host path along with a cleanup
// function. The cleanup function removes the workspace directory on call.
//
// The workspace path is: <root>/<sessionID>/workspace
//
// Example:
//
//	dir, cleanup, err := guard.PrepareSessionWorkspace("sess_abc")
//	// dir == "/var/vclaw/workspaces/sess_abc/workspace"
//	defer cleanup()
func (g *WorkspaceGuard) PrepareSessionWorkspace(sessionID string) (dir string, cleanup func(), err error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", nil, err
	}

	dir = filepath.Join(g.root, sessionID, "workspace")

	if mkErr := os.MkdirAll(dir, 0750); mkErr != nil {
		return "", nil, fmt.Errorf("workspace guard: failed to create session workspace %q: %w", dir, mkErr)
	}

	cleanup = func() {
		// Remove only the session subtree, not the root.
		_ = os.RemoveAll(filepath.Join(g.root, sessionID))
	}
	return dir, cleanup, nil
}

// ─── Validation ───────────────────────────────────────────────────────────────

// ValidateWorkspaceDir checks that hostDir is a safe, trusted workspace path
// that may be passed to Docker as a bind-mount source.
//
// Rules enforced:
//  1. Must be an absolute path.
//  2. After resolving the path, must be a subdirectory of g.root.
//  3. Must not contain null bytes or suspicious sequences.
//  4. Must refer to an existing directory.
func (g *WorkspaceGuard) ValidateWorkspaceDir(hostDir string) error {
	if hostDir == "" {
		return errors.New("workspace guard: workspace dir must not be empty")
	}
	if !filepath.IsAbs(hostDir) {
		return fmt.Errorf("workspace guard: workspace dir must be absolute, got %q", hostDir)
	}
	if strings.ContainsRune(hostDir, 0) {
		return errors.New("workspace guard: workspace dir contains null byte")
	}

	clean := filepath.Clean(hostDir)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return fmt.Errorf("workspace guard: cannot resolve workspace dir %q: %w", clean, err)
	}

	// Must be under the trusted root.
	if !isUnder(g.root, real) {
		return fmt.Errorf("workspace guard: workspace dir %q resolves outside trusted root %q", clean, g.root)
	}
	if samePath(g.root, real) {
		return fmt.Errorf("workspace guard: workspace dir must be a session subdirectory under trusted root %q", g.root)
	}

	// Directory must exist.
	info, err := os.Stat(real)
	if err != nil {
		return fmt.Errorf("workspace guard: workspace dir %q does not exist: %w", real, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace guard: workspace dir %q is not a directory", real)
	}
	return nil
}

// ValidateScriptPath checks that a script path provided in RunPythonRequest
// is safe to use inside /workspace inside the container.
//
// Rules enforced:
//  1. Must be relative (no leading /).
//  2. After cleaning, must not escape /workspace via `../`.
//  3. Must end with .py.
//  4. Must not match known credential file patterns.
func ValidateScriptPath(scriptPath string) error {
	if strings.TrimSpace(scriptPath) == "" {
		return errors.New("workspace guard: script path must not be empty")
	}
	if isAbsPath(scriptPath) {
		return fmt.Errorf("workspace guard: script path must be relative, got %q", scriptPath)
	}

	clean := filepath.ToSlash(filepath.Clean(scriptPath))

	// After cleaning, must not start with ..
	if strings.HasPrefix(clean, "..") {
		return fmt.Errorf("workspace guard: script path traversal detected: %q", scriptPath)
	}
	if strings.ContainsRune(clean, 0) {
		return errors.New("workspace guard: script path contains null byte")
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".py") {
		return fmt.Errorf("workspace guard: script path must end with .py, got %q", scriptPath)
	}
	if isCredentialPath(clean) {
		return fmt.Errorf("workspace guard: script path matches credential pattern: %q", scriptPath)
	}
	return nil
}

// ValidateShellCommand performs lightweight host-side inspection of a shell
// command before it is passed to the container. This is a best-effort layer;
// the Docker sandbox provides the primary enforcement.
//
// The function blocks commands that:
//   - Reference common credential file paths.
//   - Attempt to break out of /workspace by referencing paths above it.
//
// Note: deep shell command analysis (e.g. detecting `eval`, chained
// redirections, process substitution) is handled by the Policy Checker in a
// separate layer. This guard focuses only on path safety.
func ValidateShellCommand(command string) error {
	if strings.TrimSpace(command) == "" {
		return errors.New("workspace guard: command must not be empty")
	}

	lower := strings.ToLower(command)

	// Block references to common credential files.
	credentialPatterns := []string{
		".env", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
		".pem", ".p12", ".pfx", ".key",
		"credentials.json", "token.json", "secrets.json",
		"/etc/shadow", "/etc/passwd", "/proc/",
	}
	for _, pat := range credentialPatterns {
		if strings.Contains(lower, pat) {
			return fmt.Errorf("workspace guard: command references sensitive path %q", pat)
		}
	}

	// Block obvious absolute path escapes outside /workspace.
	// Note: Docker --read-only already prevents writes; this is an early warning.
	escapePrefixes := []string{
		"/root/", "/home/", "/etc/", "/var/", "/sys/", "/proc/",
		"/boot/", "/lib/", "/usr/", "/bin/", "/sbin/",
	}
	for _, prefix := range escapePrefixes {
		if strings.Contains(lower, prefix) {
			return fmt.Errorf("workspace guard: command references path outside /workspace: %q", prefix)
		}
	}

	return nil
}

// ─── Credential file detection ────────────────────────────────────────────────

// credentialFileNames lists basenames and patterns that indicate a file
// contains secrets. Used to block access even if the path appears otherwise
// valid.
var credentialFileNames = []string{
	".env",
	".env.local",
	".env.production",
	"id_rsa",
	"id_ed25519",
	"id_ecdsa",
	"id_dsa",
	"credentials.json",
	"token.json",
	"secrets.json",
	"service_account.json",
	".netrc",
	".pgpass",
	"kubeconfig",
}

var credentialExtensions = []string{
	".pem", ".p12", ".pfx", ".key", ".crt", ".cer", ".der",
}

// isCredentialPath returns true if the path (relative or absolute) looks like
// a credential or secret file based on name or extension.
func isCredentialPath(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	for _, name := range credentialFileNames {
		if base == name {
			return true
		}
	}
	for _, ext := range credentialExtensions {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}
	return false
}

// ─── Path helpers ─────────────────────────────────────────────────────────────

// isUnder returns true if child is equal to parent or is a descendant of parent.
// Both paths must already be absolute and cleaned.
func isUnder(parent, child string) bool {
	parent = strings.TrimRight(filepath.ToSlash(filepath.Clean(parent)), "/")
	child = strings.TrimRight(filepath.ToSlash(filepath.Clean(child)), "/")
	if parent == "" {
		parent = "/"
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

func samePath(left, right string) bool {
	return strings.TrimRight(filepath.ToSlash(filepath.Clean(left)), "/") ==
		strings.TrimRight(filepath.ToSlash(filepath.Clean(right)), "/")
}

func isAbsPath(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(filepath.ToSlash(p), "/")
}

// validateSessionID ensures a session ID is safe to use as a directory name.
func validateSessionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("workspace guard: session ID must not be empty")
	}
	if strings.ContainsAny(id, `/\:*?"<>|`) {
		return fmt.Errorf("workspace guard: session ID contains invalid characters: %q", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("workspace guard: session ID must not contain '..': %q", id)
	}
	return nil
}
