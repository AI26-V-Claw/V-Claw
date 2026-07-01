package agent

import (
	"strings"
	"testing"

	"vclaw/internal/contracts"
)

func TestFreshWorkspaceReadAnswerOnlyHandlesSimpleCalendarRequest(t *testing.T) {
	result := calendarListToolResult()

	answer, ok := freshWorkspaceReadAnswerFromToolResults("Kiểm tra lịch hôm nay giúp tôi", []contracts.ToolResult{result})
	if !ok {
		t.Fatal("expected simple calendar read to use calendar fallback")
	}
	if !strings.Contains(answer, "Có 1 sự kiện") || !strings.Contains(answer, "Demo Day") {
		t.Fatalf("unexpected calendar fallback answer:\n%s", answer)
	}
}

func TestFreshWorkspaceReadAnswerDoesNotCollapseMultiSourceBriefingToCalendar(t *testing.T) {
	text := "Hãy chuẩn bị briefing buổi sáng cho tôi bằng cách chạy song song 3 nhánh: kiểm tra email ngày hôm nay, kiểm tra lịch hôm nay, và tìm tài liệu Drive liên quan đến project V-Claw. Sau đó tổng hợp thành một bản briefing ngắn gọn cho tôi"

	if answer, ok := freshWorkspaceReadAnswerFromToolResults(text, []contracts.ToolResult{calendarListToolResult()}); ok {
		t.Fatalf("multi-source briefing must not use calendar-only fallback:\n%s", answer)
	}

	missing := missingRequestedWorkspaceReadDomains(text, []contracts.ToolResult{calendarListToolResult()})
	if strings.Join(missing, ",") != "gmail,drive" {
		t.Fatalf("unexpected missing read domains: %#v", missing)
	}
}

func TestMissingRequestedWorkspaceReadDomainsCompleteWhenAllBranchesRead(t *testing.T) {
	text := "kiểm tra email hôm nay, lịch hôm nay và tài liệu Drive rồi tổng hợp briefing"
	results := []contracts.ToolResult{
		{ToolName: "gmail.listEmails", Success: true},
		calendarListToolResult(),
		{ToolName: "drive.listFiles", Success: true},
	}

	if missing := missingRequestedWorkspaceReadDomains(text, results); len(missing) != 0 {
		t.Fatalf("expected all requested read domains to be complete, missing: %#v", missing)
	}
}

func calendarListToolResult() contracts.ToolResult {
	return contracts.ToolResult{
		ToolName: "calendar.listEvents",
		Success:  true,
		Data: map[string]any{
			"contentForLLM": `[{"title":"Demo Day","start":"2026-06-30T10:00:00+07:00","end":"2026-06-30T11:00:00+07:00","eventLink":"https://calendar.google.com/event?eid=demo"}]`,
		},
	}
}
