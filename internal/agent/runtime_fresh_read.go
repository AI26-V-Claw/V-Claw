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

func freshWorkspaceReadAnswerFromToolResults(results []contracts.ToolResult) (string, bool) {
	for i := len(results) - 1; i >= 0; i-- {
		result := results[i]
		if !result.Success {
			continue
		}
		switch result.ToolName {
		case "calendar.listEvents":
			return calendarListEventsAnswer(result)
		default:
			continue
		}
	}
	return "", false
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
