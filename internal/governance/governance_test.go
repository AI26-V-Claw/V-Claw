package governance

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPromptVersionIsStable(t *testing.T) {
	a := PromptVersion("system prompt body", "soul content")
	b := PromptVersion("system prompt body", "soul content")
	if a != b {
		t.Fatalf("expected stable hash, got %q vs %q", a, b)
	}
	if len(a) != versionHashLen {
		t.Fatalf("expected hash length %d, got %d (%q)", versionHashLen, len(a), a)
	}
}

func TestPromptVersionDiffersWhenAnyPartChanges(t *testing.T) {
	base := PromptVersion("system", "soul")
	if v := PromptVersion("system", "soul "); v == base {
		t.Fatalf("trailing whitespace must change the hash, got identical %q", v)
	}
	if v := PromptVersion("system!", "soul"); v == base {
		t.Fatalf("change in first part must change the hash, got identical %q", v)
	}
	if v := PromptVersion("system", "soul updated"); v == base {
		t.Fatalf("change in second part must change the hash, got identical %q", v)
	}
}

func TestPromptVersionLengthPrefixingPreventsConcatCollision(t *testing.T) {
	// Without length prefixing, ("ab", "c") and ("a", "bc") would collide.
	v1 := PromptVersion("ab", "c")
	v2 := PromptVersion("a", "bc")
	if v1 == v2 {
		t.Fatalf("naive concatenation collision: both produced %q", v1)
	}
}

func TestPromptVersionSkipsEmptyParts(t *testing.T) {
	a := PromptVersion("system", "")
	b := PromptVersion("system")
	if a != b {
		t.Fatalf("empty trailing part should be ignored, got %q vs %q", a, b)
	}
}

func TestToolSchemaVersionIsOrderInsensitive(t *testing.T) {
	// Same schema written with keys in different orders must hash to the same
	// version — otherwise edits that only re-serialise drift the version.
	first := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to":      map[string]any{"type": "string"},
			"subject": map[string]any{"type": "string"},
		},
		"required": []any{"to", "subject"},
	}
	second := map[string]any{
		"properties": map[string]any{
			"subject": map[string]any{"type": "string"},
			"to":      map[string]any{"type": "string"},
		},
		"required": []any{"to", "subject"},
		"type":     "object",
	}
	if ToolSchemaVersion(first) != ToolSchemaVersion(second) {
		t.Fatalf("logically identical schemas must produce the same version")
	}
}

func TestToolSchemaVersionDiffersWhenSchemaChanges(t *testing.T) {
	base := ToolSchemaVersion(map[string]any{"type": "object", "required": []any{"a"}})
	changed := ToolSchemaVersion(map[string]any{"type": "object", "required": []any{"a", "b"}})
	if base == changed {
		t.Fatalf("schema change must shift version, both got %q", base)
	}
}

func TestToolSchemaVersionEmpty(t *testing.T) {
	if v := ToolSchemaVersion(nil); v != "" {
		t.Fatalf("nil schema should produce empty version, got %q", v)
	}
	// Marshallable but empty object is still a valid schema and gets a hash.
	if v := ToolSchemaVersion(map[string]any{}); v == "" {
		t.Fatalf("empty object schema should still produce a hash")
	}
}

func TestToolSchemaVersionAcceptsRoundTripJSON(t *testing.T) {
	raw := `{"type":"object","properties":{"x":{"type":"number"}}}`
	var typed map[string]any
	if err := json.Unmarshal([]byte(raw), &typed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v := ToolSchemaVersion(typed)
	if len(v) != versionHashLen {
		t.Fatalf("expected hash length %d, got %d (%q)", versionHashLen, len(v), v)
	}
}

func TestPolicyRefFormat(t *testing.T) {
	ts := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	got := PolicyRef("run_abc", "tc_001", ts)
	want := "policy:run_abc:tc_001:" // unix-suffix checked separately
	if !strings.HasPrefix(got, want) {
		t.Fatalf("expected prefix %q, got %q", want, got)
	}
	// Suffix must be the unix-second representation, not RFC3339.
	wantUnix := ts.Unix() // 2026-06-15T12:00:00Z → 1781524800
	if !strings.HasSuffix(got, "1781524800") || !strings.HasSuffix(got, fmt.Sprintf("%d", wantUnix)) {
		t.Fatalf("expected unix-seconds suffix %d, got %q", wantUnix, got)
	}
}

func TestPolicyRefIsTimezoneIndependent(t *testing.T) {
	utc := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	saigon := utc.In(time.FixedZone("ICT", 7*60*60)) // same instant, different zone
	if PolicyRef("r", "t", utc) != PolicyRef("r", "t", saigon) {
		t.Fatalf("PolicyRef must use UTC seconds and be zone-independent")
	}
}

func TestPolicyRefRejectsBlankInputs(t *testing.T) {
	ts := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	if v := PolicyRef("", "tc", ts); v != "" {
		t.Fatalf("blank runID must produce empty ref, got %q", v)
	}
	if v := PolicyRef("run", "", ts); v != "" {
		t.Fatalf("blank toolCallID must produce empty ref, got %q", v)
	}
	if v := PolicyRef("run", "tc", time.Time{}); v != "" {
		t.Fatalf("zero time must produce empty ref, got %q", v)
	}
	if v := PolicyRef("  ", "tc", ts); v != "" {
		t.Fatalf("whitespace-only runID must produce empty ref, got %q", v)
	}
}

func TestSourceConstants(t *testing.T) {
	// Smoke-check the prefixes so accidental rename breaks tests rather than
	// audit dashboards.
	if SourceToolPrefix != "tool:" {
		t.Fatalf("SourceToolPrefix changed: %q", SourceToolPrefix)
	}
	if SourceConnectorPrefix != "connector:" {
		t.Fatalf("SourceConnectorPrefix changed: %q", SourceConnectorPrefix)
	}
	if SourceUserChannel != "channel" {
		t.Fatalf("SourceUserChannel changed: %q", SourceUserChannel)
	}
}
