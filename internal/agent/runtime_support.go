package agent

import (
	"context"
	"fmt"
	"html"
	"strings"

	"vclaw/internal/tools"
)

const maxToolContentForLLM = 4000

func executeToolSafely(ctx context.Context, tool tools.Tool, toolCall tools.ToolCall) (result tools.ToolResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = tools.ExecutionErrorResult(toolCall, fmt.Errorf("panic: %v", recovered))
		}
	}()

	return tool.Execute(ctx, toolCall)
}

func truncateToolContentForLLM(content string) string {
	if len(content) <= maxToolContentForLLM {
		return content
	}

	return content[:maxToolContentForLLM] + fmt.Sprintf("\n...[truncated %d bytes]", len(content)-maxToolContentForLLM)
}

func truncateStringBytes(content string, limit int) string {
	if limit <= 0 || len(content) <= limit {
		return content
	}
	return content[:limit] + fmt.Sprintf("\n...[truncated %d bytes]", len(content)-limit)
}

func extractPlannerJSONObject(text string) string {
	return extractJSONObject(text)
}

func extractJSONObject(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func xmlEscape(value string) string {
	return html.EscapeString(strings.TrimSpace(value))
}

func isOrdinalActionReference(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return containsAnyText(lower,
		"số 1", "so 1", "số 2", "so 2", "số 3", "so 3", "số 4", "so 4", "số 5", "so 5",
		"cái 1", "cai 1", "cái 2", "cai 2", "cái 3", "cai 3",
		"cái đầu tiên", "cai dau tien", "cái đầu", "cai dau",
		"cái thứ nhất", "cai thu nhat", "cái thứ hai", "cai thu hai", "cái thứ ba", "cai thu ba",
		"mục 1", "muc 1", "mục 2", "muc 2", "mục 3", "muc 3",
		"#1", "#2", "#3", "#4", "#5",
		"item 1", "item 2", "item 3", "option 1", "option 2",
	)
}
