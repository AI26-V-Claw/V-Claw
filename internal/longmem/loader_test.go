package longmem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderNoFiles(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir)
	if got := l.Load(); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestLoaderOnlyUserMD(t *testing.T) {
	dir := t.TempDir()
	userContent := "# Thông tin người dùng\n- Tên: Quang Ho\n"
	writeFile(t, dir, "USER.md", userContent)

	got := NewLoader(dir).Load()
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(got, safetyLabel[:30]) {
		t.Error("safety label missing")
	}
	if !strings.Contains(got, "Quang Ho") {
		t.Error("user content missing")
	}
}

func TestLoaderOnlyNotesMD(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "NOTES.md", "# Ghi chú gần đây\n- Đang làm sprint 2\n")

	got := NewLoader(dir).Load()
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(got, "sprint 2") {
		t.Error("notes content missing")
	}
}

func TestLoaderNotesTrimmedWhenExceedsBudget(t *testing.T) {
	dir := t.TempDir()
	// Build a NOTES.md that exceeds 1500 tokens (~4500 runes).
	var sb strings.Builder
	sb.WriteString("# Ghi chú gần đây\n")
	for i := 0; i < 600; i++ {
		sb.WriteString("- fact number one two three four five six seven eight nine ten\n")
	}
	writeFile(t, dir, "NOTES.md", sb.String())

	got := NewLoader(dir).Load()
	// The returned string should be within a reasonable size (safety label + trimmed notes).
	// Heading must survive.
	if !strings.Contains(got, "# Ghi chú gần đây") {
		t.Error("notes heading was removed during trim")
	}
	// Total content should not be enormous.
	if len(got) > 20_000 {
		t.Errorf("result too large after trim: %d bytes", len(got))
	}
}

func TestLoaderSafetyLabelAlwaysFirst(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USER.md", "# Thông tin người dùng\n- email: a@b.com\n")
	writeFile(t, dir, "NOTES.md", "# Ghi chú gần đây\n- project X\n")

	got := NewLoader(dir).Load()
	if !strings.HasPrefix(got, "## Bộ nhớ dài hạn") {
		t.Errorf("safety label not first, got prefix: %q", got[:min(60, len(got))])
	}
}

func TestLoaderSkipsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USER.md", "# Thông tin người dùng\n- tên: Quang\n")
	// Write NOTES.md then make it unreadable.
	notesPath := filepath.Join(dir, "NOTES.md")
	writeFile(t, dir, "NOTES.md", "# Ghi chú\n- project\n")
	_ = os.Chmod(notesPath, 0000)
	defer os.Chmod(notesPath, 0644) //nolint — best effort restore

	got := NewLoader(dir).Load()
	// USER.md should still load; NOTES.md silently skipped.
	if !strings.Contains(got, "Quang") {
		t.Error("USER.md content missing when NOTES.md is unreadable")
	}
}

// helpers

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
