package longmem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vclaw/internal/providers"
)

// fakeProvider is a minimal providers.Provider for testing.
type fakeProvider struct {
	response string
	err      error
}

func (f *fakeProvider) Generate(_ context.Context, _ *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &providers.GenerateResponse{Text: f.response}, nil
}

func (f *fakeProvider) Chat(_ context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (f *fakeProvider) Name() string  { return "fake" }
func (f *fakeProvider) Close() error  { return nil }

func TestFlusherLLMPath(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{response: `## USER_FACTS
- Tên: Quang Ho

## NOTES_FACTS
- Đang làm sprint 2`}

	f := NewFlusher(dir, p, "")
	if err := f.Flush(context.Background(), "session summary text"); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	userMD := readFileOrEmpty(t, dir, "USER.md")
	if !strings.Contains(userMD, "Quang Ho") {
		t.Errorf("USER.md missing fact, got: %q", userMD)
	}
	notesMD := readFileOrEmpty(t, dir, "NOTES.md")
	if !strings.Contains(notesMD, "sprint 2") {
		t.Errorf("NOTES.md missing fact, got: %q", notesMD)
	}
}

func TestFlusherLLMFailFallback(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{err: errors.New("provider timeout")}

	// Put something in the summary that regex can extract.
	f := NewFlusher(dir, p, "")
	summary := "timezone: Asia/Ho_Chi_Minh configured."
	if err := f.Flush(context.Background(), summary); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	notesMD := readFileOrEmpty(t, dir, "NOTES.md")
	if !strings.Contains(notesMD, "Ho_Chi_Minh") {
		t.Errorf("NOTES.md missing fallback fact, got: %q", notesMD)
	}
	// USER.md should not be created by fallback.
	if _, err := os.Stat(filepath.Join(dir, "USER.md")); err == nil {
		t.Error("USER.md should not be created by regex fallback")
	}
}

func TestFlusherEmptySummaryNoWrite(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{response: ""}
	f := NewFlusher(dir, p, "")
	if err := f.Flush(context.Background(), "   "); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files written for empty summary, got: %v", entries)
	}
}

func TestFlusherLLMEmptyResultFallback(t *testing.T) {
	dir := t.TempDir()
	// LLM returns valid format but no facts.
	p := &fakeProvider{response: "## USER_FACTS\n\n## NOTES_FACTS\n"}
	f := NewFlusher(dir, p, "")
	// Summary with something regex can catch.
	if err := f.Flush(context.Background(), "tên: Minh Le đã tham gia."); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	notesMD := readFileOrEmpty(t, dir, "NOTES.md")
	if !strings.Contains(notesMD, "Minh Le") {
		t.Errorf("fallback did not extract name, NOTES.md: %q", notesMD)
	}
}

// readFileOrEmpty reads a file and returns its content; returns "" if not found.
func readFileOrEmpty(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(data)
}
