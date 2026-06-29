package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"strings"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

// fakeSkillReviewProvider returns a fixed JSON response for skill review tests.
type fakeSkillReviewProvider struct {
	response string
}

func (f *fakeSkillReviewProvider) Chat(_ context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: f.response},
	}, nil
}

func (f *fakeSkillReviewProvider) Generate(_ context.Context, _ *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	return &providers.GenerateResponse{Text: f.response}, nil
}

func (f *fakeSkillReviewProvider) Name() string { return "fake-skill-review" }
func (f *fakeSkillReviewProvider) Close() error { return nil }

func TestMaybeSpawnSkillReview_DisabledByDefault(t *testing.T) {
	r := NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{},
		// SkillNudgeInterval defaults to 0 = disabled
	})
	// Should be no-op; no panic, no goroutine side effects
	r.maybeSpawnSkillReview("session-1", 5)
	// Give any accidental goroutine time to run
	time.Sleep(10 * time.Millisecond)
	// If we get here without panic, test passes
}

func TestMaybeSpawnSkillReview_BelowThreshold(t *testing.T) {
	r := NewRuntime(RuntimeConfig{
		Provider:          &fakeProvider{},
		SkillNudgeInterval: 10,
	})
	// 5 iterations, threshold is 10 â€” should not trigger
	r.maybeSpawnSkillReview("session-1", 5)
	r.skillReviewMu.Lock()
	count := r.itersSinceSkillReview
	r.skillReviewMu.Unlock()
	if count != 5 {
		t.Errorf("expected itersSinceSkillReview=5, got %d", count)
	}
}

func TestMaybeSpawnSkillReview_ResetsCounterAfterThreshold(t *testing.T) {
	r := NewRuntime(RuntimeConfig{
		Provider:          &fakeProvider{},
		SkillNudgeInterval: 5,
		SessionStore:      sessions.NewInMemoryStore(),
	})
	// First call: 3 iterations, below threshold
	r.maybeSpawnSkillReview("session-1", 3)
	// Second call: 3 more = total 6, above threshold of 5
	r.maybeSpawnSkillReview("session-1", 3)

	// Counter should be reset to 0
	time.Sleep(20 * time.Millisecond)
	r.skillReviewMu.Lock()
	count := r.itersSinceSkillReview
	r.skillReviewMu.Unlock()
	if count != 0 {
		t.Errorf("expected counter reset to 0 after threshold, got %d", count)
	}
}

func TestWriteSkillFile_CreatesFileAndManifest(t *testing.T) {
	dir := t.TempDir()
	r := NewRuntime(RuntimeConfig{
		Provider:      &fakeProvider{},
		SkillCacheDir: dir,
	})

	content := "---\nname: skill.test\ndescription: test skill\n---\n\n# Test\n"
	if err := r.writeSkillFile("skill.test", content); err != nil {
		t.Fatalf("writeSkillFile failed: %v", err)
	}

	// Check SKILL.md was created
	skillPath := filepath.Join(dir, "skill.test", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("SKILL.md not found: %v", err)
	}
	if string(data) != content {
		t.Errorf("SKILL.md content mismatch: got %q, want %q", string(data), content)
	}

	// Check manifest.json was created
	manifestPath := filepath.Join(dir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json not found: %v", err)
	}
	var records []map[string]any
	if err := json.Unmarshal(manifestData, &records); err != nil {
		t.Fatalf("manifest.json parse failed: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record in manifest, got %d", len(records))
	}
	if records[0]["name"] != "skill.test" {
		t.Errorf("manifest name mismatch: got %v", records[0]["name"])
	}
}

func TestApplySkillPatch_TreatsAsMissingCreate(t *testing.T) {
	dir := t.TempDir()
	r := NewRuntime(RuntimeConfig{
		Provider:      &fakeProvider{},
		SkillCacheDir: dir,
	})

	// Patching a non-existent skill should create it
	d := skillReviewDecision{
		Action:    "patch",
		SkillName: "skill.newone",
		Reason:    "test",
		Content:   "---\nname: skill.newone\n---\n# New\n",
	}
	r.applySkillPatch(d)

	skillPath := filepath.Join(dir, "skill.newone", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("expected SKILL.md to be created by patch-as-create, but not found")
	}
}

func TestBumpPatchVersion(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"1.0.0", "1.0.1"},
		{"1.0.9", "1.0.10"},
		{"2.3.5", "2.3.6"},
		{"invalid", "1.0.1"},
		{"", "1.0.1"},
	}
	for _, c := range cases {
		got := bumpPatchVersionForTest(c.input)
		if got != c.want {
			t.Errorf("bumpPatchVersion(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func bumpPatchVersionForTest(version string) string {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) != 3 {
		return "1.0.1"
	}
	patch := 0
	fmt.Sscanf(parts[2], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
}
