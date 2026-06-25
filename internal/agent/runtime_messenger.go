package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
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

func (m *RuntimeMessenger) ResetSession(ctx context.Context, sessionID string) error {
	if m == nil || m.runtime == nil {
		return fmt.Errorf("runtime is required")
	}
	return m.runtime.sessionStore.ClearSession(ctx, sessionID)
}

// CancelSession cancels the active run for the given sessionID, if any.
// Returns true if a run was found and cancelled.
func (m *RuntimeMessenger) CancelSession(sessionID string) bool {
	if m == nil || m.runtime == nil {
		return false
	}
	return m.runtime.CancelSession(sessionID)
}

func (m *RuntimeMessenger) HandleMessage(ctx context.Context, msg contracts.UserMessage) (response contracts.AgentResponse, err error) {
	if m == nil || m.runtime == nil {
		return contracts.AgentResponse{}, fmt.Errorf("runtime is required")
	}
	ctx, finishTrace := m.runtime.startRequestTelemetry(ctx, msg)
	defer func() { finishTrace(response, err) }()

	msg.Text = strings.TrimSpace(msg.Text)
	if command, ok := parseApprovalCommand(msg.Text, m.runtime.HasPendingApproval(ctx, msg.SessionID)); ok {
		if m.runtime != nil && m.runtime.logger != nil {
			m.runtime.logger.Info("approval decision received and parsed",
				"request_id", msg.RequestID,
				"session_id", msg.SessionID,
				"approval_id", strings.TrimSpace(command.approvalID),
				"decision", command.decision,
				"comment", strings.TrimSpace(command.comment),
			)
		}
		response, err = m.runtime.ResolveApproval(ctx, msg.SessionID, contracts.ApprovalDecision{
			ApprovalID: command.approvalID,
			RequestID:  msg.RequestID,
			Decision:   command.decision,
			DecidedBy:  "owner",
			Channel:    msg.Channel,
			DecidedAt:  time.Now().UTC(),
			Comment:    command.comment,
		})
		if err != nil {
			m.runtime.recordRequestObservation(contracts.AgentResponse{}, err)
			return contracts.AgentResponse{}, err
		}
		if output := renderUserOutput(response); output != nil {
			response.Output = output
		}
		if text := renderAgentResponse(response); strings.TrimSpace(text) != "" {
			response.Message = text
		}
		m.scheduleGmailBounceFollowUps(ctx, response)
		m.runtime.recordRequestObservation(response, nil)
		return response, nil
	}

	response, err = m.runtime.Run(ctx, msg)
	if err != nil {
		m.runtime.recordRequestObservation(contracts.AgentResponse{}, err)
		return contracts.AgentResponse{}, err
	}

	if output := renderUserOutput(response); output != nil {
		response.Output = output
	}
	if text := renderAgentResponse(response); strings.TrimSpace(text) != "" {
		response.Message = text
	}
	m.scheduleGmailBounceFollowUps(ctx, response)
	m.runtime.recordRequestObservation(response, nil)
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
		meta := map[string]any{
			"approvalId": response.ApprovalRequest.ApprovalID,
			"expiresAt":  response.ApprovalRequest.ExpiresAt.Format(time.RFC3339),
		}
		if strings.TrimSpace(response.ApprovalRequest.ParentApprovalID) != "" {
			meta["parentApprovalId"] = response.ApprovalRequest.ParentApprovalID
		}
		return &contracts.UserOutput{
			Kind: contracts.UserOutputKindApproval,
			Text: limitOutboundText(renderApprovalRequest(*response.ApprovalRequest)),
			Meta: meta,
		}
	}

	if strings.TrimSpace(response.Message) != "" {
		kind := contracts.UserOutputKindMessage
		switch response.Status {
		case contracts.AgentStatusCompleted:
			kind = contracts.UserOutputKindSuccess
		case contracts.AgentStatusNeedClarification:
			kind = contracts.UserOutputKindClarify
		case contracts.AgentStatusFailed, contracts.AgentStatusBlocked, contracts.AgentStatusIterationBudgetExhausted:
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
			if artifactRef := preferredArtifactRef(result); artifactRef != nil {
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
					if artifactRef := preferredArtifactRef(result); artifactRef != nil {
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

// preferredArtifactRef returns the artifact reference the tool attached to its
// result. Tools set this directly at the tool layer (see each office tool's
// *ArtifactRef helper), so the messenger never reverse-engineers it from result
// text. Returns nil when the tool produced no referenceable artifact.
func preferredArtifactRef(result contracts.ToolResult) *contracts.ArtifactRef {
	return result.ArtifactRef
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
		lines = append(lines, strings.TrimSpace(approval.Summary))
	}

	if fields := visibleApprovalFields(approval.ToolCall.Input); len(fields) > 0 {
		lines = append(lines, "")
		for _, kv := range fields {
			lines = append(lines, approvalFieldLabel(kv[0])+": "+kv[1])
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Trả lời một trong các lệnh:")
	lines = append(lines, "- approve")
	lines = append(lines, "- reject")
	lines = append(lines, "- revise <nội dung muốn chỉnh>")

	return formatOutboundText(strings.Join(lines, "\n"))
}

// approvalSkipFields are opaque internal IDs that are not meaningful to the user.
var approvalSkipFields = map[string]bool{
	"draftId": true, "eventId": true, "messageId": true,
	"threadId": true, "threadKey": true, "threadName": true,
	"space": true, "calendarId": true, "messageName": true,
	"replyToMessageId": true, "messageReplyOption": true, "requestId": true,
	"pageToken": true, "previewChars": true, "full": true, "localPath": true,
}

// approvalFieldPriority controls display order; lower = shown first.
var approvalFieldPriority = map[string]int{
	"subject": 1, "title": 1,
	"name": 1, "fileName": 1, "newTitle": 1,
	"sourceFiles": 1, "targetFolder": 2,
	"to": 2, "attendees": 2, "emailAddress": 2,
	"textBody": 3, "htmlBody": 3, "text": 3, "body": 3, "content": 3, "oldText": 3, "newText": 4, "values": 4, "ranges": 4,
	"start": 4, "end": 5,
	"cc": 6, "bcc": 7,
	"range": 6, "role": 7, "type": 8,
	"description": 8, "location": 9,
	"attachments": 10,
	"fileId":      20, "fileIds": 20, "documentId": 20, "spreadsheetId": 20, "targetParentId": 21, "permissionId": 21, "sheetId": 21, "sourceSheetId": 21,
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// visibleApprovalFields returns [key, formattedValue] pairs in display order.
func visibleApprovalFields(input map[string]any) [][2]string {
	type entry struct {
		key      string
		priority int
	}
	var entries []entry
	for k := range input {
		if approvalSkipFields[k] {
			continue
		}
		pri, ok := approvalFieldPriority[k]
		if !ok {
			pri = 99
		}
		entries = append(entries, entry{k, pri})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].priority != entries[j].priority {
			return entries[i].priority < entries[j].priority
		}
		return entries[i].key < entries[j].key
	})

	var result [][2]string
	for _, e := range entries {
		v := formatApprovalValue(e.key, input[e.key])
		if v == "" {
			continue
		}
		result = append(result, [2]string{e.key, v})
	}
	return result
}

func approvalFieldLabel(key string) string {
	switch key {
	case "subject", "title":
		return "Tiêu đề"
	case "name", "fileName":
		return "Tên"
	case "newTitle":
		return "Tiêu đề mới"
	case "to":
		return "Người nhận"
	case "emailAddress":
		return "Email được chia sẻ"
	case "cc":
		return "CC"
	case "bcc":
		return "BCC"
	case "textBody", "body", "content":
		return "Nội dung"
	case "htmlBody":
		return "Nội dung"
	case "text":
		return "Tin nhắn"
	case "oldText":
		return "Nội dung cần thay"
	case "newText":
		return "Nội dung thay thế"
	case "range":
		return "Vùng dữ liệu"
	case "ranges":
		return "Các vùng dữ liệu"
	case "values":
		return "Giá trị"
	case "role":
		return "Quyền"
	case "type":
		return "Loại chia sẻ"
	case "fileId":
		return "Drive file ID"
	case "fileIds":
		return "Drive file IDs"
	case "sourceFiles":
		return "Nguồn di chuyển"
	case "targetFolder":
		return "Thư mục đích"
	case "documentId":
		return "Document ID"
	case "spreadsheetId":
		return "Spreadsheet ID"
	case "targetParentId":
		return "Folder đích ID"
	case "permissionId":
		return "Permission ID"
	case "sheetId":
		return "Sheet ID"
	case "sourceSheetId":
		return "Sheet nguồn ID"
	case "start":
		return "Bắt đầu"
	case "end":
		return "Kết thúc"
	case "attendees":
		return "Người tham gia"
	case "description":
		return "Mô tả"
	case "location":
		return "Địa điểm"
	case "attachments":
		return "Đính kèm"
	default:
		return key
	}
}

const maxApprovalFieldRunes = 300

func formatApprovalValue(key string, v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		s := strings.TrimSpace(val)
		if key == "htmlBody" {
			s = strings.TrimSpace(htmlTagRe.ReplaceAllString(s, " "))
			s = strings.Join(strings.Fields(s), " ")
		}
		if runes := []rune(s); len(runes) > maxApprovalFieldRunes {
			s = string(runes[:maxApprovalFieldRunes]) + "..."
		}
		return s
	case []any:
		var parts []string
		for _, item := range val {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			} else if item != nil {
				parts = append(parts, fmt.Sprintf("%v", item))
			}
		}
		return truncateApprovalText(strings.Join(parts, ", "))
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return truncateApprovalText(fmt.Sprintf("%v", v))
		}
		return truncateApprovalText(string(data))
	}
}

func truncateApprovalText(s string) string {
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > maxApprovalFieldRunes {
		return string(runes[:maxApprovalFieldRunes]) + "..."
	}
	return s
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
	if strings.TrimSpace(toolName) == "gmail.listEmails" && !looksLikeMachinePayload(content) {
		return formatOutboundText(content)
	}
	title := "Kết quả"
	if strings.TrimSpace(toolName) != "" {
		title = "Kết quả từ " + strings.TrimSpace(toolName)
	}
	return formatOutboundText(title + "\n\n" + content)
}

func formatOutboundText(text string) string {
	// text = strings.ReplaceAll(text, "\r\n", "\n")
	// text = strings.ReplaceAll(text, "\r", "\n")

	// lines := strings.Split(text, "\n")
	// formatted := make([]string, 0, len(lines))
	// previousBlank := false
	// for _, line := range lines {
	// 	line = strings.TrimSpace(line)
	// 	line = strings.TrimPrefix(line, "### ")
	// 	line = strings.TrimPrefix(line, "## ")
	// 	line = strings.TrimPrefix(line, "# ")
	// 	line = stripInlineMarkdownMarkers(line)
	// 	if line == "" {
	// 		if len(formatted) > 0 && !previousBlank {
	// 			formatted = append(formatted, "")
	// 		}
	// 		previousBlank = true
	// 		continue
	// 	}
	// 	formatted = append(formatted, line)
	// 	previousBlank = false
	// }
	// return strings.TrimSpace(strings.Join(formatted, "\n"))
	return text
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
			decision: contracts.ApprovalDecisionRevised,
			comment:  strings.TrimSpace(strings.TrimPrefix(trimmed, parts[0])),
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
