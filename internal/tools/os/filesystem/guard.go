// Package filesystem provides file system tools (list, read, write, info)
// for the V-Claw agent. All file access is guarded by PathGuard to prevent
// path traversal and restrict operations to allowed workspace directories.
package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathGuard restricts file operations to a set of allowed root directories.
// If no roots are configured, all paths are allowed (useful for testing).
type PathGuard struct {
	allowedRoots []string
}

// NewPathGuard creates a guard that only allows access within the given root directories.
func NewPathGuard(roots []string) PathGuard {
	cleaned := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if real, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
			abs = real
		}
		cleaned = append(cleaned, abs)
	}
	return PathGuard{allowedRoots: cleaned}
}

// Resolve converts a user-supplied path to an absolute path and verifies
// it falls within the allowed roots. Relative paths are resolved against
// the first allowed root. Returns an error if the path is outside allowed directories.
func (g PathGuard) Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}

	// If path is relative and we have roots, resolve against first root
	if !filepath.IsAbs(path) && len(g.allowedRoots) > 0 {
		path = filepath.Join(g.allowedRoots[0], path)
	}

	// Resolve to absolute, clean path
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	abs = filepath.Clean(abs)

	// Resolve symlinks
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If file doesn't exist yet (for write), check parent
		parent := filepath.Dir(abs)
		realParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			real = abs // fall back to cleaned path
		} else {
			real = filepath.Join(realParent, filepath.Base(abs))
		}
	}

	// If no roots configured, allow everything (for testing)
	if len(g.allowedRoots) == 0 {
		return real, nil
	}

	// Check against allowed roots
	for _, root := range g.allowedRoots {
		rel, relErr := filepath.Rel(root, real)
		if relErr == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return real, nil
		}
	}

	return "", fmt.Errorf("path %q is outside allowed directories", path)
}

// AllowedRoots returns a copy of the configured allowed root directories.
func (g PathGuard) AllowedRoots() []string {
	out := make([]string, len(g.allowedRoots))
	copy(out, g.allowedRoots)
	return out
}

// HasRoots returns true if at least one allowed root is configured.
func (g PathGuard) HasRoots() bool {
	return len(g.allowedRoots) > 0
}

// IsAllowed checks whether the given path is within allowed roots without resolving it.
func (g PathGuard) IsAllowed(path string) bool {
	_, err := g.Resolve(path)
	return err == nil
}

// EnsureDir creates the directory at the resolved path if it doesn't exist.
func (g PathGuard) EnsureDir(path string) (string, error) {
	resolved, err := g.Resolve(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(resolved, 0750); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	return resolved, nil
}
