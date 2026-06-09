package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	if command, ok := parseApprovalCommand(msg.Text, m.runtime.HasPendingApproval(msg.SessionID)); ok {
		var response contracts.AgentResponse
		var err error
		if command.revise {
			response, err = m.runtime.ReviseApproval(ctx, msg.SessionID, msg.RequestID, command.approvalID, command.comment)
		} else {
			response, err = m.runtime.ResolveApproval(ctx, msg.SessionID, contracts.ApprovalDecision{
				ApprovalID: command.approvalID,
				RequestID:  msg.RequestID,
				Decision:   command.decision,
				DecidedBy:  "owner",
				DecidedAt:  time.Now().UTC(),
				Comment:    command.comment,
			})
		}
		if err != nil {
			return contracts.AgentResponse{}, err
		}
		if output := renderUserOutput(response); output != nil {
			response.Output = output
		}
		if text := renderAgentResponse(response); strings.TrimSpace(text) != "" {
			response.Message = text
		}
		m.scheduleGmailBounceFollowUps(ctx, response)
		return response, nil
	}

	response, err := m.runtime.Run(ctx, msg)
	if err != nil {
		return contracts.AgentResponse{}, err
	}

	if output := renderUserOutput(response); output != nil {
		response.Output = output
	}
	if text := renderAgentResponse(response); strings.TrimSpace(text) != "" {
		response.Message = text
	}
	m.scheduleGmailBounceFollowUps(ctx, response)
	return response, nil
}

func (m *RuntimeMessenger) FinalizeAudit(_ contracts.UserMessage, _ error) {}

func (m *RuntimeMessenger) RecordIgnored(_ contracts.UserMessage, _ string) {}

func renderAgentResponse(response contracts.AgentResponse) string {
	if response.ApprovalRequest != nil {
		return limitOutboundText(renderApprovalRequest(*response.ApprovalRequest))
	}
	if strings.TrimSpace(response.Message) != "" {
		return limitOutboundText(renderAssistantMessage(response.Message, response.ToolResults))
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

func renderUserOutput(response contracts.AgentResponse) *contracts.UserOutput {
	if response.ApprovalRequest != nil {
		return &contracts.UserOutput{
			Kind: contracts.UserOutputKindApproval,
			Text: limitOutboundText(renderApprovalRequest(*response.ApprovalRequest)),
			Meta: map[string]any{
				"approvalId": response.ApprovalRequest.ApprovalID,
				"expiresAt":  response.ApprovalRequest.ExpiresAt.Format(time.RFC3339),
			},
		}
	}

	if strings.TrimSpace(response.Message) != "" {
		kind := contracts.UserOutputKindMessage
		switch response.Status {
		case contracts.AgentStatusCompleted:
			kind = contracts.UserOutputKindSuccess
		case contracts.AgentStatusNeedClarification:
			kind = contracts.UserOutputKindClarify
		case contracts.AgentStatusFailed, contracts.AgentStatusBlocked, contracts.AgentStatusMaxIterationsReached:
			kind = contracts.UserOutputKindError
		}
		return &contracts.UserOutput{
			Kind: kind,
			Text: limitOutboundText(renderAssistantMessage(response.Message, response.ToolResults)),
		}
	}

	if response.Error != nil {
		kind := contracts.UserOutputKindError
		if response.Error.Code == "APPROVAL_EXPIRED" {
			kind = contracts.UserOutputKindExpired
		}
		return &contracts.UserOutput{
			Kind: kind,
			Text: limitOutboundText(renderError(response.Error)),
		}
	}

	for _, result := range response.ToolResults {
		if result.Success {
			if artifactRef := buildArtifactRef(result.ToolName, result.Data); artifactRef != nil {
				if text := extractUserText(result.Data); strings.TrimSpace(text) != "" {
					return &contracts.UserOutput{
						Kind:        contracts.UserOutputKindSuccess,
						Text:        limitOutboundText(renderToolFallback(result.ToolName, text)),
						ArtifactRef: artifactRef,
					}
				}
			}
			if data, ok := result.Data.(map[string]any); ok {
				if text, ok := data["contentForUser"].(string); ok && strings.TrimSpace(text) != "" {
					output := &contracts.UserOutput{
						Kind: contracts.UserOutputKindSuccess,
						Text: limitOutboundText(renderToolFallback(result.ToolName, text)),
					}
					if artifactRef := buildArtifactRef(result.ToolName, result.Data); artifactRef != nil {
						output.ArtifactRef = artifactRef
					}
					return output
				}
			}
		}
	}

	return &contracts.UserOutput{
		Kind: contracts.UserOutputKindMessage,
		Text: string(response.Status),
	}
}

func buildArtifactRef(toolName string, data any) *contracts.ArtifactRef {
	payload, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	content, ok := payload["contentForUser"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return nil
	}
	value, ok := extractJSONValue(content)
	if !ok {
		return nil
	}

	switch strings.TrimSpace(toolName) {
	case "chat.sendMessage":
		if message, ok := nestedMap(value, "Message"); ok {
			id := firstStringValue(message, "Name", "name")
			if id == "" {
				return nil
			}
			return &contracts.ArtifactRef{
				Kind:  "chat.message",
				Label: "Google Chat message",
				ID:    id,
			}
		}
	case "gmail.sendDraft":
		if message, ok := nestedMap(value, "Message"); ok {
			id := firstStringValue(message, "ID", "Id", "id")
			if id == "" {
				return nil
			}
			return &contracts.ArtifactRef{
				Kind:  "gmail.message",
				Label: "Gmail message",
				ID:    id,
				URI:   "https://mail.google.com/mail/u/0/#sent/" + id,
			}
		}
	case "calendar.createEvent":
		if event, ok := nestedMap(value, "Event"); ok {
			id := firstStringValue(event, "ID", "Id", "id")
			if id == "" {
				return nil
			}
			ref := &contracts.ArtifactRef{
				Kind:  "calendar.event",
				Label: "Google Calendar event",
				ID:    id,
				URI:   "https://calendar.google.com/calendar/r/eventedit/" + id,
			}
			if meetLink := firstStringValue(event, "MeetLink", "meetLink"); meetLink != "" {
				ref.Meta = map[string]any{"meetLink": meetLink}
			}
			return ref
		}
	}
	return nil
}

func extractUserText(data any) string {
	payload, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	content, ok := payload["contentForUser"].(string)
	if !ok {
		return ""
	}
	return content
}

func extractJSONValue(text string) (any, bool) {
	_, jsonText := splitMachinePayload(text)
	if strings.TrimSpace(jsonText) == "" {
		return nil, false
	}
	var value any
	if err := json.Unmarshal([]byte(jsonText), &value); err != nil {
		return nil, false
	}
	return value, true
}

func nestedMap(value any, key string) (map[string]any, bool) {
	payload, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	nested, ok := payload[key].(map[string]any)
	return nested, ok
}

func stringValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func firstStringValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(payload, key); value != "" {
			return value
		}
	}
	return ""
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
	lines = append(lines, "Approval ID: "+strings.TrimSpace(approval.ApprovalID))
	lines = append(lines, "")
	lines = append(lines, "Trả lời một trong các lệnh:")
	lines = append(lines, "- approve")
	lines = append(lines, "- reject")
	lines = append(lines, "- revise <nội dung muốn chỉnh>")

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
	if rendered := renderMachinePayload(toolName, content); rendered != "" {
		return rendered
	}
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

type approvalCommand struct {
	decision   contracts.ApprovalDecisionStatus
	approvalID string
	comment    string
	revise     bool
}

func parseApprovalCommand(text string, hasPending bool) (approvalCommand, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return approvalCommand{}, false
	}
	lower := strings.ToLower(trimmed)
	parts := strings.Fields(trimmed)
	first := ""
	if len(parts) > 0 {
		first = strings.ToLower(parts[0])
	}

	switch first {
	case "/approve", "approve", "approved":
		return approvalCommand{decision: contracts.ApprovalDecisionApproved, approvalID: secondField(parts)}, true
	case "/reject", "reject", "rejected":
		return approvalCommand{decision: contracts.ApprovalDecisionRejected, approvalID: secondField(parts)}, true
	case "/revise", "revise", "sửa", "sua", "chỉnh", "chinh":
		if !hasPending {
			return approvalCommand{}, false
		}
		return approvalCommand{
			revise:  true,
			comment: strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0])),
		}, true
	}

	if hasPending {
		switch lower {
		case "ok", "yes", "duyệt", "dong-y", "đồng-ý", "đồng ý", "dong y", "xác nhận", "xac nhan":
			return approvalCommand{decision: contracts.ApprovalDecisionApproved}, true
		case "no", "cancel", "hủy", "huy", "từ-chối", "tu-choi", "từ chối", "tu choi", "không", "khong", "hủy bỏ", "huy bo":
			return approvalCommand{decision: contracts.ApprovalDecisionRejected}, true
		}
	}
	return approvalCommand{}, false
}

func secondField(parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
