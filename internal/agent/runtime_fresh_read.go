package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
)

type calendarListEventForAnswer struct {
	Title     string `json:"title"`
	Start     string `json:"start"`
	End       string `json:"end"`
	EventLink string `json:"eventLink"`
}

func freshWorkspaceReadAnswerFromToolResults(userText string, results []contracts.ToolResult) (string, bool) {
	if !isSimpleCalendarReadRequest(userText) {
		return "", false
	}
	var calendarResult *contracts.ToolResult
	successfulReadCount := 0
	for i := range results {
		result := results[i]
		if !result.Success || !isWorkspaceReadResultForFreshAnswer(result.ToolName) {
			continue
		}
		successfulReadCount++
		if result.ToolName == "calendar.listEvents" {
			calendarResult = &results[i]
		}
	}
	if successfulReadCount != 1 || calendarResult == nil {
		return "", false
	}
	return calendarListEventsAnswer(*calendarResult)
}

func missingRequestedWorkspaceReadDomains(userText string, results []contracts.ToolResult) []string {
	requested := requestedWorkspaceReadDomains(userText)
	if len(requested) < 2 {
		return nil
	}
	seen := map[string]bool{}
	for _, result := range results {
		if !result.Success {
			continue
		}
		if domain := workspaceReadResultDomain(result.ToolName); domain != "" {
			seen[domain] = true
		}
	}
	missing := make([]string, 0, len(requested))
	for _, domain := range requested {
		if !seen[domain] {
			missing = append(missing, domain)
		}
	}
	return missing
}

func requestedWorkspaceReadDomains(userText string) []string {
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return nil
	}
	var domains []string
	if containsAnyText(lower, "email", "mail", "gmail", "hộp thư", "hop thu") {
		domains = append(domains, "gmail")
	}
	if containsAnyText(lower, "lịch", "lich", "calendar", "sự kiện", "su kien") {
		domains = append(domains, "calendar")
	}
	if containsAnyText(lower, "drive", "google drive", "tài liệu drive", "tai lieu drive") {
		domains = append(domains, "drive")
	}
	if containsAnyText(lower, "chat", "google chat", "tin nhắn", "tin nhan") {
		domains = append(domains, "chat")
	}
	if containsAnyText(lower, "web", "internet", "tìm kiếm", "tim kiem", "tra cứu", "tra cuu") {
		domains = append(domains, "web")
	}
	return domains
}

func isSimpleCalendarReadRequest(userText string) bool {
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" || !containsAnyText(lower, "lịch", "lich", "calendar", "sự kiện", "su kien") {
		return false
	}
	if len(requestedWorkspaceReadDomains(lower)) != 1 {
		return false
	}
	return !containsAnyText(lower,
		"briefing", "brief", "tổng hợp", "tong hop", "tóm tắt", "tom tat",
		"song song", "nhánh", "nhanh", "branch", "branches",
		"gmail", "email", "mail", "drive", "chat", "web",
	)
}

func isWorkspaceReadResultForFreshAnswer(toolName string) bool {
	return workspaceReadResultDomain(toolName) != ""
}

func workspaceReadResultDomain(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	switch {
	case strings.HasPrefix(toolName, "gmail."):
		return "gmail"
	case strings.HasPrefix(toolName, "calendar."):
		return "calendar"
	case strings.HasPrefix(toolName, "drive."):
		return "drive"
	case strings.HasPrefix(toolName, "chat."):
		return "chat"
	case strings.HasPrefix(toolName, "web."):
		return "web"
	default:
		return ""
	}
}

func calendarListEventsAnswer(result contracts.ToolResult) (string, bool) {
	content := toolResultContentForLLM(result)
	if strings.TrimSpace(content) == "" || strings.HasPrefix(strings.TrimSpace(content), "Kh") {
		return "Mình đã kiểm tra lịch theo dữ liệu mới nhất.\n\nKhông tìm thấy sự kiện nào trong khoảng thời gian đã chọn.", true
	}
	var events []calendarListEventForAnswer
	if err := json.Unmarshal([]byte(content), &events); err != nil {
		return "", false
	}
	if len(events) == 0 {
		return "Mình đã kiểm tra lịch theo dữ liệu mới nhất.\n\nKhông tìm thấy sự kiện nào trong khoảng thời gian đã chọn.", true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Mình đã kiểm tra lịch theo dữ liệu mới nhất.\n\nCó %d sự kiện:\n", len(events))
	for _, event := range events {
		title := strings.TrimSpace(event.Title)
		if title == "" {
			title = "Không có tiêu đề"
		}
		startDate, startClock := formatCalendarEventDateTime(event.Start)
		_, endClock := formatCalendarEventDateTime(event.End)
		if startDate == "" {
			startDate = strings.TrimSpace(event.Start)
		}
		timeText := strings.TrimSpace(startClock)
		if endClock != "" {
			if timeText != "" {
				timeText += "–" + endClock
			} else {
				timeText = strings.TrimSpace(event.End)
			}
		}
		if timeText == "" {
			timeText = "không rõ giờ"
		}
		fmt.Fprintf(&b, "\n- %s %s: %s", startDate, timeText, title)
		if link := strings.TrimSpace(event.EventLink); link != "" {
			fmt.Fprintf(&b, "\n  Link: %s", link)
		}
	}
	return strings.TrimSpace(b.String()), true
}

func toolResultContentForLLM(result contracts.ToolResult) string {
	data, _ := result.Data.(map[string]any)
	if data == nil {
		return ""
	}
	content, _ := data["contentForLLM"].(string)
	return strings.TrimSpace(content)
}

func formatCalendarEventDateTime(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value, ""
	}
	return parsed.Format("02/01/2006"), parsed.Format("15:04")
}
