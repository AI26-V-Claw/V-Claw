package longmem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Close() error { return nil }

func TestFlusherLLMPath(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{response: `## USER_FACTS
- Ten: Quang Ho

## NOTES_FACTS
- Dang lam sprint 2`}

	f := NewFlusher(dir, p, "")
	if err := f.FlushWithSource(context.Background(), FlushInput{
		Summary:         "session summary text",
		SessionID:       "sess_1",
		RequestID:       "req_1",
		ClassifierModel: "classifier-test",
	}); err != nil {
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
	sources := readFileOrEmpty(t, dir, memorySourcesFile)
	if !strings.Contains(sources, "session_compaction") || !strings.Contains(sources, "sess_1") {
		t.Errorf("memory_sources.json missing compaction provenance, got: %q", sources)
	}
}

func TestFlusherLLMFailFallback(t *testing.T) {
	dir := t.TempDir()
	p := &fakeProvider{err: errors.New("provider timeout")}

	f := NewFlusher(dir, p, "")
	summary := "timezone: Asia/Ho_Chi_Minh configured."
	if err := f.Flush(context.Background(), summary); err != nil {
		t.Fatalf("Flush error: %v", err)
	}

	notesMD := readFileOrEmpty(t, dir, "NOTES.md")
	if !strings.Contains(notesMD, "Ho_Chi_Minh") {
		t.Errorf("NOTES.md missing fallback fact, got: %q", notesMD)
	}
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
	p := &fakeProvider{response: "## USER_FACTS\n\n## NOTES_FACTS\n"}
	f := NewFlusher(dir, p, "")
	if err := f.Flush(context.Background(), "timezone: Asia/Ho_Chi_Minh configured."); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	notesMD := readFileOrEmpty(t, dir, "NOTES.md")
	if !strings.Contains(notesMD, "Ho_Chi_Minh") {
		t.Errorf("fallback did not extract name, NOTES.md: %q", notesMD)
	}
}

func TestRecordRepeatedHabitsPromotesAcrossTwoSessionsAfterFiveGlobalRepeats(t *testing.T) {
	dir := t.TempDir()
	f := NewFlusher(dir, &fakeProvider{}, "")
	base := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

	recordHabitMessages(t, f, []habitTestEvent{
		{sessionID: "sess_a", requestID: "req_1", at: base},
		{sessionID: "sess_a", requestID: "req_2", at: base.Add(time.Minute)},
		{sessionID: "sess_b", requestID: "req_3", at: base.Add(2 * time.Minute)},
		{sessionID: "sess_b", requestID: "req_4", at: base.Add(3 * time.Minute)},
		{sessionID: "sess_b", requestID: "req_5", at: base.Add(4 * time.Minute)},
	})

	userMD := readFileOrEmpty(t, dir, "USER.md")
	if !strings.Contains(userMD, "email") || !strings.Contains(userMD, "08:00") {
		t.Fatalf("USER.md missing repeated habit, got:\n%s", userMD)
	}
	if !strings.Contains(userMD, "## "+userCategories[1]) {
		t.Fatalf("habit should be under work preference section, got:\n%s", userMD)
	}
	sources := readFileOrEmpty(t, dir, memorySourcesFile)
	if !strings.Contains(sources, "repeated_habit") || !strings.Contains(sources, `"count": 5`) {
		t.Fatalf("memory_sources.json missing repeated habit provenance, got:\n%s", sources)
	}
	patterns := readFileOrEmpty(t, dir, habitPatternsFile)
	if !strings.Contains(patterns, `"promoted": true`) || !strings.Contains(patterns, "sess_a") || !strings.Contains(patterns, "sess_b") {
		t.Fatalf("habit_patterns.json missing promoted global pattern, got:\n%s", patterns)
	}
}

func TestRecordRepeatedHabitsDoesNotPromoteFiveRepeatsInOneShortSession(t *testing.T) {
	dir := t.TempDir()
	f := NewFlusher(dir, &fakeProvider{}, "")
	base := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

	recordHabitMessages(t, f, []habitTestEvent{
		{sessionID: "sess_a", requestID: "req_1", at: base},
		{sessionID: "sess_a", requestID: "req_2", at: base.Add(time.Minute)},
		{sessionID: "sess_a", requestID: "req_3", at: base.Add(2 * time.Minute)},
		{sessionID: "sess_a", requestID: "req_4", at: base.Add(3 * time.Minute)},
		{sessionID: "sess_a", requestID: "req_5", at: base.Add(4 * time.Minute)},
	})
	if _, err := os.Stat(filepath.Join(dir, "USER.md")); err == nil {
		t.Fatal("USER.md should not be created for one short-session burst")
	}
	patterns := readFileOrEmpty(t, dir, habitPatternsFile)
	if !strings.Contains(patterns, `"count": 5`) || strings.Contains(patterns, `"promoted": true`) {
		t.Fatalf("habit pattern should be counted but not promoted, got:\n%s", patterns)
	}
}

func TestRecordRepeatedHabitsPromotesSingleSessionAfterStabilityWindow(t *testing.T) {
	dir := t.TempDir()
	f := NewFlusher(dir, &fakeProvider{}, "")
	base := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

	recordHabitMessages(t, f, []habitTestEvent{
		{sessionID: "sess_a", requestID: "req_1", at: base},
		{sessionID: "sess_a", requestID: "req_2", at: base.Add(24 * time.Hour)},
		{sessionID: "sess_a", requestID: "req_3", at: base.Add(48 * time.Hour)},
		{sessionID: "sess_a", requestID: "req_4", at: base.Add(72 * time.Hour)},
		{sessionID: "sess_a", requestID: "req_5", at: base.Add(96 * time.Hour)},
	})

	userMD := readFileOrEmpty(t, dir, "USER.md")
	if !strings.Contains(userMD, "email") || !strings.Contains(userMD, "08:00") {
		t.Fatalf("USER.md missing stable single-session habit, got:\n%s", userMD)
	}
}

func TestRecordRepeatedHabitsDeduplicatesExistingFact(t *testing.T) {
	dir := t.TempDir()
	f := NewFlusher(dir, &fakeProvider{}, "")
	base := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

	recordHabitMessages(t, f, []habitTestEvent{
		{sessionID: "sess_a", requestID: "req_1", at: base},
		{sessionID: "sess_a", requestID: "req_2", at: base.Add(time.Minute)},
		{sessionID: "sess_b", requestID: "req_3", at: base.Add(2 * time.Minute)},
		{sessionID: "sess_b", requestID: "req_4", at: base.Add(3 * time.Minute)},
		{sessionID: "sess_b", requestID: "req_5", at: base.Add(4 * time.Minute)},
		{sessionID: "sess_b", requestID: "req_6", at: base.Add(5 * time.Minute)},
		{sessionID: "sess_c", requestID: "req_7", at: base.Add(6 * time.Minute)},
	})
	userMD := readFileOrEmpty(t, dir, "USER.md")
	if count := strings.Count(userMD, "08:00"); count != 1 {
		t.Fatalf("expected habit once, got %d:\n%s", count, userMD)
	}
	sources := readFileOrEmpty(t, dir, memorySourcesFile)
	if count := strings.Count(sources, "repeated_habit"); count != 1 {
		t.Fatalf("expected one repeated_habit source observation, got %d:\n%s", count, sources)
	}
	patterns := readFileOrEmpty(t, dir, habitPatternsFile)
	if !strings.Contains(patterns, `"promoted": true`) || !strings.Contains(patterns, `"count": 7`) {
		t.Fatalf("expected promoted pattern to keep counting without re-promoting, got:\n%s", patterns)
	}
}

type habitTestEvent struct {
	sessionID string
	requestID string
	at        time.Time
}

func recordHabitMessages(t *testing.T, f *Flusher, events []habitTestEvent) {
	t.Helper()
	messages := []string{
		"Xem mail luc 8h sang giup toi",
		"Check email luc 8h sang",
		"Kiem tra mail luc 8h sang nhe",
		"Xem email luc 8h sang",
		"Check mail luc 8h sang",
	}
	for i, event := range events {
		if err := f.RecordRepeatedHabits(context.Background(), HabitInput{
			Transcript: []providers.Message{{Role: providers.MessageRoleUser, Content: messages[i%len(messages)]}},
			SessionID:  event.sessionID,
			RequestID:  event.requestID,
			ObservedAt: event.at,
		}); err != nil {
			t.Fatalf("RecordRepeatedHabits event %d error: %v", i, err)
		}
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
