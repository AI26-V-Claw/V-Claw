package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vclaw/internal/contracts"
)

const maxOutboundTextRunes = 3500

type RuntimeMessenger struct {
	runtime *Runtime
}

func NewRuntimeMessenger(runtime *Runtime) *RuntimeMessenger {
	return &RuntimeMessenger{runtime: runtime}
}

func (m *RuntimeMessenger) HandleMessage(ctx context.Context, msg contracts.UserMessage) (contracts.AgentResponse, error) {
	if m == nil || m.runtime == nil {
		return contracts.AgentResponse{}, fmt.Errorf("runtime is required")
	}

	msg.Text = strings.TrimSpace(msg.Text)
	response, err := m.runtime.Run(ctx, msg)
	if err != nil {
		return contracts.AgentResponse{}, err
	}

	if text := renderAgentResponse(response); strings.TrimSpace(text) != "" {
		response.Message = text
	}
	return response, nil
}

func (m *RuntimeMessenger) FinalizeAudit(_ contracts.UserMessage, _ error) {}

func (m *RuntimeMessenger) RecordIgnored(_ contracts.UserMessage, _ string) {}

func renderAgentResponse(response contracts.AgentResponse) string {
	if response.ApprovalRequest != nil {
		return limitOutboundText(renderApprovalRequest(*response.ApprovalRequest))
	}
	if strings.TrimSpace(response.Message) != "" {
		return limitOutboundText(formatOutboundText(response.Message))
	}
	if response.Error != nil {
		return limitOutboundText(renderError(response.Error))
	}
	for _, result := range response.ToolResults {
		if result.Success {
			if data, ok := result.Data.(map[string]any); ok {
				if text, ok := data["contentForUser"].(string); ok && strings.TrimSpace(text) != "" {
					return limitOutboundText(renderToolFallback(result.ToolName, text))
				}
			}
		}
	}
	return string(response.Status)
}

func renderApprovalRequest(approval contracts.ApprovalRequest) string {
	var lines []string
	lines = append(lines, "Cần xác nhận trước khi thực hiện.")
	lines = append(lines, "")
	if strings.TrimSpace(approval.Summary) != "" {
		lines = append(lines, "Tóm tắt: "+strings.TrimSpace(approval.Summary))
	}
	if strings.TrimSpace(approval.Details) != "" {
		lines = append(lines, "Chi tiết: "+strings.TrimSpace(approval.Details))
	}
	lines = append(lines, "Tool: "+strings.TrimSpace(approval.ToolCall.ToolName))
	lines = append(lines, "Risk: "+string(approval.RiskLevel))

	body := formatOutboundText(strings.Join(lines, "\n"))
	if len(approval.ToolCall.Input) > 0 {
		if data, err := json.MarshalIndent(approval.ToolCall.Input, "", "  "); err == nil {
			return body + "\n\nInput:\n" + string(data)
		}
	}
	return body
}

func renderError(errorShape *contracts.ErrorShape) string {
	if errorShape == nil {
		return "Không thể hoàn tất yêu cầu."
	}
	var lines []string
	lines = append(lines, "Không thể hoàn tất yêu cầu.")
	if strings.TrimSpace(errorShape.Code) != "" {
		lines = append(lines, "Mã lỗi: "+strings.TrimSpace(errorShape.Code))
	}
	if strings.TrimSpace(errorShape.Message) != "" {
		lines = append(lines, "Chi tiết: "+strings.TrimSpace(errorShape.Message))
	}
	return formatOutboundText(strings.Join(lines, "\n"))
}

func renderToolFallback(toolName string, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	title := "Kết quả"
	if strings.TrimSpace(toolName) != "" {
		title = "Kết quả từ " + strings.TrimSpace(toolName)
	}
	return formatOutboundText(title + "\n\n" + content)
}

func formatOutboundText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	formatted := make([]string, 0, len(lines))
	previousBlank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "### ")
		line = strings.TrimPrefix(line, "## ")
		line = strings.TrimPrefix(line, "# ")
		line = stripInlineMarkdownMarkers(line)
		if line == "" {
			if len(formatted) > 0 && !previousBlank {
				formatted = append(formatted, "")
			}
			previousBlank = true
			continue
		}
		formatted = append(formatted, line)
		previousBlank = false
	}
	return strings.TrimSpace(strings.Join(formatted, "\n"))
}

func stripInlineMarkdownMarkers(text string) string {
	return strings.NewReplacer(
		"**", "",
		"__", "",
		"`", "",
	).Replace(text)
}

func limitOutboundText(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxOutboundTextRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxOutboundTextRunes])) + "\n\n...[đã rút gọn]"
}
