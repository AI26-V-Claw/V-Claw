package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateToolContentForLLMPreservesUTF8(t *testing.T) {
	content := strings.Repeat("a", maxToolContentForLLM-1) + "ắtail"

	got := truncateToolContentForLLM(content)

	if !utf8.ValidString(got) {
		t.Fatalf("truncated content is not valid UTF-8: %q", got)
	}
	if !strings.Contains(got, "...[truncated") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestTruncateStringBytesPreservesUTF8WhenLimitSplitsRune(t *testing.T) {
	got := truncateStringBytes("ắbc", 1)

	if !utf8.ValidString(got) {
		t.Fatalf("truncated content is not valid UTF-8: %q", got)
	}
	if strings.Contains(got, "\ufffd") {
		t.Fatalf("truncated content contains replacement rune: %q", got)
	}
	if !strings.Contains(got, "...[truncated") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}
