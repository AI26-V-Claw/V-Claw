package longmem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFiles(t *testing.T) {
	dir := t.TempDir()
	writeFileContent(t, filepath.Join(dir, "USER.md"), "# Thông tin người dùng\n\n## Thông tin cơ bản\n- Email: test@example.com\n")
	writeFileContent(t, filepath.Join(dir, "NOTES.md"), "# Ghi chú gần đây\n- Dự án V-Claw đang trong sprint 2\n")

	userMD, notesMD, err := ReadFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(userMD, "test@example.com") {
		t.Errorf("userMD missing email, got: %s", userMD)
	}
	if !strings.Contains(notesMD, "V-Claw") {
		t.Errorf("notesMD missing content, got: %s", notesMD)
	}
}

func TestReadFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	userMD, notesMD, err := ReadFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if userMD != "" {
		t.Errorf("expected empty userMD, got: %s", userMD)
	}
	if notesMD != "" {
		t.Errorf("expected empty notesMD, got: %s", notesMD)
	}
}

func TestAddUserFact(t *testing.T) {
	dir := t.TempDir()
	if err := AddUserFact(dir, "Thông tin cơ bản", "Số điện thoại: 0901234567"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "USER.md"))
	if !strings.Contains(content, "0901234567") {
		t.Errorf("expected fact to be added, got:\n%s", content)
	}
	if !strings.Contains(content, "## Thông tin cơ bản") {
		t.Errorf("expected category heading, got:\n%s", content)
	}
}

func TestAddUserFact_Dedup(t *testing.T) {
	dir := t.TempDir()
	// Add the same fact twice.
	if err := AddUserFact(dir, "Thông tin cơ bản", "Email: dup@example.com"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := AddUserFact(dir, "Thông tin cơ bản", "Email: dup@example.com"); err != nil {
		t.Fatalf("second add: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "USER.md"))
	count := strings.Count(content, "dup@example.com")
	if count != 1 {
		t.Errorf("expected 1 occurrence, got %d in:\n%s", count, content)
	}
}

func TestAddUserFact_InvalidCategory(t *testing.T) {
	dir := t.TempDir()
	err := AddUserFact(dir, "Category không tồn tại", "some fact")
	if err == nil {
		t.Error("expected error for invalid category")
	}
}

func TestAddUserFact_EmptyFact(t *testing.T) {
	dir := t.TempDir()
	err := AddUserFact(dir, "Thông tin cơ bản", "")
	if err == nil {
		t.Error("expected error for empty fact")
	}
}

func TestRemoveUserFact(t *testing.T) {
	dir := t.TempDir()
	writeFileContent(t, filepath.Join(dir, "USER.md"), userMDSkeleton()+"\n## Thông tin cơ bản\n- Email: old@example.com\n- Phone: 090123\n")

	removed, err := RemoveUserFact(dir, "old@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}
	content := readFile(t, filepath.Join(dir, "USER.md"))
	if strings.Contains(content, "old@example.com") {
		t.Errorf("expected fact removed, got:\n%s", content)
	}
	// Other facts should remain.
	if !strings.Contains(content, "090123") {
		t.Errorf("expected other facts to remain, got:\n%s", content)
	}
}

func TestRemoveUserFact_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeFileContent(t, filepath.Join(dir, "USER.md"), userMDSkeleton()+"\n## Thông tin cơ bản\n- Email: keep@example.com\n")

	removed, err := RemoveUserFact(dir, "nonexistent-pattern")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed {
		t.Error("expected removed=false")
	}
}

func TestAddNotesFact(t *testing.T) {
	dir := t.TempDir()
	if err := AddNotesFact(dir, "Đang làm dự án V-Claw"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "NOTES.md"))
	if !strings.Contains(content, "Đang làm dự án V-Claw") {
		t.Errorf("expected fact in NOTES.md, got:\n%s", content)
	}
}

func TestAddNotesFact_Dedup(t *testing.T) {
	dir := t.TempDir()
	if err := AddNotesFact(dir, "Fact trùng lặp"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := AddNotesFact(dir, "Fact trùng lặp"); err != nil {
		t.Fatalf("second add: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "NOTES.md"))
	count := strings.Count(content, "Fact trùng lặp")
	if count != 1 {
		t.Errorf("expected 1 occurrence, got %d in:\n%s", count, content)
	}
}

func TestRemoveNotesFact(t *testing.T) {
	dir := t.TempDir()
	writeFileContent(t, filepath.Join(dir, "NOTES.md"), "# Ghi chú gần đây\n- Fact cũ\n- Fact mới\n")

	removed, err := RemoveNotesFact(dir, "Fact cũ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Error("expected removed=true")
	}
	content := readFile(t, filepath.Join(dir, "NOTES.md"))
	if strings.Contains(content, "Fact cũ") {
		t.Errorf("expected fact removed, got:\n%s", content)
	}
	if !strings.Contains(content, "Fact mới") {
		t.Errorf("expected other fact to remain, got:\n%s", content)
	}
}

func TestRemoveNotesFact_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	_, err := RemoveNotesFact(dir, "")
	if err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestResetAll(t *testing.T) {
	dir := t.TempDir()
	// Write some content first.
	writeFileContent(t, filepath.Join(dir, "USER.md"), "# old content\n- some fact\n")
	writeFileContent(t, filepath.Join(dir, "NOTES.md"), "# old notes\n- some note\n")

	if err := ResetAll(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	userMD := readFile(t, filepath.Join(dir, "USER.md"))
	if !strings.Contains(userMD, "Thông tin cơ bản") {
		t.Errorf("expected skeleton USER.md, got:\n%s", userMD)
	}
	if strings.Contains(userMD, "some fact") {
		t.Errorf("old content should be gone, got:\n%s", userMD)
	}

	notesMD := readFile(t, filepath.Join(dir, "NOTES.md"))
	if !strings.Contains(notesMD, "Ghi chú gần đây") {
		t.Errorf("expected skeleton NOTES.md, got:\n%s", notesMD)
	}
}

// --- helpers ---

// readFile reads a file and returns its content as string.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile %s: %v", path, err)
	}
	return string(data)
}

// ─── Read-error handling (P2 regression) ────────────────────────────────────
// A read error on the existing memory file must NOT be treated as "empty" —
// otherwise the next add would overwrite the file and drop existing content.
// We simulate a non-IsNotExist read error by placing a directory where the
// memory file is expected; os.ReadFile then fails with a real error.

func TestAddUserFact_ReadErrorDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	// Make USER.md a directory so os.ReadFile returns a non-IsNotExist error.
	if err := os.Mkdir(filepath.Join(dir, "USER.md"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := AddUserFact(dir, "Thông tin cơ bản", "Email: test@example.com")
	if err == nil {
		t.Fatal("expected error when existing USER.md is unreadable, got nil")
	}
}

func TestAddNotesFact_ReadErrorDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "NOTES.md"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err := AddNotesFact(dir, "Ghi chú mới")
	if err == nil {
		t.Fatal("expected error when existing NOTES.md is unreadable, got nil")
	}
}

func TestRemoveUserFact_ReadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "USER.md"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := RemoveUserFact(dir, "test"); err == nil {
		t.Fatal("expected error when existing USER.md is unreadable, got nil")
	}
}

func TestRemoveNotesFact_ReadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "NOTES.md"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := RemoveNotesFact(dir, "test"); err == nil {
		t.Fatal("expected error when existing NOTES.md is unreadable, got nil")
	}
}

func writeFileContent(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFileContent %s: %v", path, err)
	}
}

