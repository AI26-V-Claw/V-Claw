package longmem

import (
	"strings"
	"testing"
)

func TestParseClassifyResponseBothSections(t *testing.T) {
	input := `## USER_FACTS
- Tên: Quang Ho
- Email: quang@vclaw.com

## NOTES_FACTS
- Đang làm sprint 2
- Debug memory system`

	result := parseClassifyResponse(input)
	if len(result.UserFacts) != 2 {
		t.Fatalf("UserFacts = %v, want 2 items", result.UserFacts)
	}
	if len(result.NotesFacts) != 2 {
		t.Fatalf("NotesFacts = %v, want 2 items", result.NotesFacts)
	}
	if result.UserFacts[0] != "Tên: Quang Ho" {
		t.Errorf("unexpected UserFacts[0]: %q", result.UserFacts[0])
	}
	if result.NotesFacts[0] != "Đang làm sprint 2" {
		t.Errorf("unexpected NotesFacts[0]: %q", result.NotesFacts[0])
	}
}

func TestParseClassifyResponseMalformed(t *testing.T) {
	result := parseClassifyResponse("some random text without headers")
	if len(result.UserFacts) != 0 || len(result.NotesFacts) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestParseClassifyResponseEmptySections(t *testing.T) {
	input := `## USER_FACTS

## NOTES_FACTS
`
	result := parseClassifyResponse(input)
	if len(result.UserFacts) != 0 {
		t.Errorf("UserFacts should be empty, got %v", result.UserFacts)
	}
	if len(result.NotesFacts) != 0 {
		t.Errorf("NotesFacts should be empty, got %v", result.NotesFacts)
	}
}

func TestMergeUserFactsDeduplication(t *testing.T) {
	existing := userMDSkeleton() + "- Tên: Quang Ho\n"
	result := mergeUserFacts(existing, []string{"Tên: Quang Ho", "Email: quang@vclaw.com"})
	count := strings.Count(result, "Tên: Quang Ho")
	if count != 1 {
		t.Errorf("duplicate fact: 'Tên: Quang Ho' appears %d times", count)
	}
	if !strings.Contains(result, "Email: quang@vclaw.com") {
		t.Error("new fact not added")
	}
}

func TestMergeUserFactsCreatesSkeletonWhenEmpty(t *testing.T) {
	result := mergeUserFacts("", []string{"Tên: Quang"})
	if !strings.Contains(result, "# Thông tin người dùng") {
		t.Error("skeleton heading missing")
	}
	if !strings.Contains(result, "Tên: Quang") {
		t.Error("fact not added to skeleton")
	}
}

func TestAppendNotesFactsTrimWhenExceedsBudget(t *testing.T) {
	// Build existing NOTES.md already near the limit.
	var sb strings.Builder
	sb.WriteString(notesMDSkeleton())
	for i := 0; i < 550; i++ {
		sb.WriteString("- old fact number one two three four five six seven eight\n")
	}
	existing := sb.String()

	// Append a few new facts that push it over 1500 tokens.
	result := appendNotesFacts(existing, []string{"new fact A", "new fact B"})

	// Heading must be preserved.
	if !strings.Contains(result, "# Ghi chú gần đây") {
		t.Error("heading removed after trim")
	}
	// New facts should be present.
	if !strings.Contains(result, "new fact A") {
		t.Error("new fact A missing")
	}
}

func TestTrimNotesContentPreservesHeading(t *testing.T) {
	// Build content with only a heading and many bullet lines.
	var sb strings.Builder
	sb.WriteString("# Ghi chú gần đây\n")
	for i := 0; i < 600; i++ {
		sb.WriteString("- fact one two three four five six seven eight nine ten\n")
	}
	result := trimNotesContent(sb.String(), 100)
	if !strings.Contains(result, "# Ghi chú gần đây") {
		t.Error("heading was removed by trim")
	}
}

func TestTrimNotesContentStopsAtBudget(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("# Ghi chú gần đây\n")
	// Each line ~10 tokens. 200 lines ≈ 2000 tokens > 1500 budget.
	for i := 0; i < 200; i++ {
		sb.WriteString("- fact one two three four five six seven eight nine ten\n")
	}
	content := sb.String()
	result := trimNotesContent(content, notesMaxTokens)

	from := strings.Count(content, "\n")
	to := strings.Count(result, "\n")
	if to >= from {
		t.Errorf("trim did not remove any lines: before=%d lines, after=%d lines", from, to)
	}
}
