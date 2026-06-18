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
	if result.UserFacts[0].Fact != "Tên: Quang Ho" {
		t.Errorf("unexpected UserFacts[0]: %q", result.UserFacts[0])
	}
	// Bullets without a "###" sub-heading fall back to the default category.
	if result.UserFacts[0].Category != defaultUserCategory() {
		t.Errorf("unexpected UserFacts[0].Category: %q", result.UserFacts[0].Category)
	}
	if result.NotesFacts[0] != "Đang làm sprint 2" {
		t.Errorf("unexpected NotesFacts[0]: %q", result.NotesFacts[0])
	}
}

func TestParseClassifyResponseCategorizedUserFacts(t *testing.T) {
	input := `## USER_FACTS
### Thông tin cơ bản
- Email: quang@vclaw.site
### Người quen thuộc
- Bao Le (baolnc@vclaw.site)

## NOTES_FACTS
`
	result := parseClassifyResponse(input)
	if len(result.UserFacts) != 2 {
		t.Fatalf("UserFacts = %#v, want 2 items", result.UserFacts)
	}
	if result.UserFacts[0].Category != "Thông tin cơ bản" || result.UserFacts[0].Fact != "Email: quang@vclaw.site" {
		t.Errorf("unexpected UserFacts[0]: %#v", result.UserFacts[0])
	}
	if result.UserFacts[1].Category != "Người quen thuộc" || result.UserFacts[1].Fact != "Bao Le (baolnc@vclaw.site)" {
		t.Errorf("unexpected UserFacts[1]: %#v", result.UserFacts[1])
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
	result := mergeUserFacts(existing, []CategorizedFact{
		{Category: "Thông tin cơ bản", Fact: "Tên: Quang Ho"},
		{Category: "Thông tin cơ bản", Fact: "Email: quang@vclaw.com"},
	})
	count := strings.Count(result, "Tên: Quang Ho")
	if count != 1 {
		t.Errorf("duplicate fact: 'Tên: Quang Ho' appears %d times", count)
	}
	if !strings.Contains(result, "Email: quang@vclaw.com") {
		t.Error("new fact not added")
	}
}

func TestMergeUserFactsDedupIgnoresTrailingPunctuationAndCase(t *testing.T) {
	existing := userMDSkeleton() + "- Tên người dùng: Quang\n"
	result := mergeUserFacts(existing, []CategorizedFact{
		// Differs only by trailing period and case — must not be re-added.
		{Category: "Thông tin cơ bản", Fact: "Tên người dùng: quang."},
	})
	if n := strings.Count(strings.ToLower(result), "tên người dùng: quang"); n != 1 {
		t.Fatalf("expected the fact once after semantic dedup, got %d:\n%s", n, result)
	}
}

func TestMergeUserFactsPlacesFactUnderCategoryHeading(t *testing.T) {
	result := mergeUserFacts("", []CategorizedFact{
		{Category: "Người quen thuộc", Fact: "Bao Le (baolnc@vclaw.site)"},
	})
	idxHeading := strings.Index(result, "## Người quen thuộc")
	idxFact := strings.Index(result, "Bao Le (baolnc@vclaw.site)")
	idxNext := strings.Index(result, "## Quy tắc làm việc")
	if idxHeading < 0 || idxFact < 0 || idxNext < 0 {
		t.Fatalf("missing heading or fact:\n%s", result)
	}
	// The fact must sit between its own heading and the following heading.
	if !(idxHeading < idxFact && idxFact < idxNext) {
		t.Fatalf("fact not placed under its category heading:\n%s", result)
	}
}

func TestMergeUserFactsCreatesSkeletonWhenEmpty(t *testing.T) {
	result := mergeUserFacts("", []CategorizedFact{{Category: "Thông tin cơ bản", Fact: "Tên: Quang"}})
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
