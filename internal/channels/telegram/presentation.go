package telegram

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"vclaw/internal/contracts"
)

const telegramApprovalStateTTL = 30 * time.Minute

const (
	telegramCodeBlockOpen  = "\uE000"
	telegramCodeBlockClose = "\uE001"
)

var telegramSensitiveTextPattern = regexp.MustCompile(`(?i)(authorization:|bearer\s+[a-z0-9._\-]+|xox[baprs]-|xapp-|sk-[a-z0-9]|telegram-token|stack trace|traceback|panic:|provider chat failed|client secret|refresh token|access token|api[_ -]?key)`)

type telegramChannelState struct {
	mu        sync.Mutex
	approvals map[string]telegramApprovalContext
	revisions map[int64]telegramRevisionContext
}

type telegramApprovalContext struct {
	ApprovalID   string
	SessionID    string
	ChatID       int64
	MessageID    int
	PromptText   string
	ToolName     string
	RegisteredAt time.Time
}

type telegramRevisionContext struct {
	ApprovalID   string
	SessionID    string
	ChatID       int64
	MessageID    int
	PromptText   string
	ToolName     string
	RegisteredAt time.Time
}

func newTelegramChannelState() *telegramChannelState {
	return &telegramChannelState{
		approvals: make(map[string]telegramApprovalContext),
		revisions: make(map[int64]telegramRevisionContext),
	}
}

func (s *telegramChannelState) registerApproval(ctx telegramApprovalContext) {
	if s == nil || strings.TrimSpace(ctx.ApprovalID) == "" || strings.TrimSpace(ctx.SessionID) == "" || ctx.ChatID == 0 {
		return
	}
	if ctx.RegisteredAt.IsZero() {
		ctx.RegisteredAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for approvalID, existing := range s.approvals {
		if existing.ChatID == ctx.ChatID && approvalID != ctx.ApprovalID {
			delete(s.approvals, approvalID)
		}
	}
	delete(s.revisions, ctx.ChatID)
	s.approvals[ctx.ApprovalID] = ctx
}

func (s *telegramChannelState) lookupApproval(approvalID string, chatID int64, messageID int) (telegramApprovalContext, bool) {
	if s == nil {
		return telegramApprovalContext{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, ok := s.approvals[strings.TrimSpace(approvalID)]
	if !ok {
		return telegramApprovalContext{}, false
	}
	if isExpiredTelegramState(ctx.RegisteredAt) {
		delete(s.approvals, ctx.ApprovalID)
		return telegramApprovalContext{}, false
	}
	if ctx.ChatID != chatID {
		return telegramApprovalContext{}, false
	}
	if ctx.MessageID != 0 && messageID != 0 && ctx.MessageID != messageID {
		return telegramApprovalContext{}, false
	}
	return ctx, true
}

func (s *telegramChannelState) approvalForChat(chatID int64) (telegramApprovalContext, bool) {
	if s == nil || chatID == 0 {
		return telegramApprovalContext{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for approvalID, ctx := range s.approvals {
		if ctx.ChatID != chatID {
			continue
		}
		if isExpiredTelegramState(ctx.RegisteredAt) {
			delete(s.approvals, approvalID)
			return telegramApprovalContext{}, false
		}
		return ctx, true
	}
	return telegramApprovalContext{}, false
}

func (s *telegramChannelState) deleteApproval(approvalID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.approvals, strings.TrimSpace(approvalID))
}

func (s *telegramChannelState) rememberRevision(ctx telegramApprovalContext) {
	if s == nil || ctx.ChatID == 0 || strings.TrimSpace(ctx.SessionID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.approvals, ctx.ApprovalID)
	s.revisions[ctx.ChatID] = telegramRevisionContext{
		ApprovalID:   ctx.ApprovalID,
		SessionID:    ctx.SessionID,
		ChatID:       ctx.ChatID,
		MessageID:    ctx.MessageID,
		PromptText:   ctx.PromptText,
		ToolName:     ctx.ToolName,
		RegisteredAt: time.Now().UTC(),
	}
}

func (s *telegramChannelState) consumeRevision(chatID int64) (telegramRevisionContext, bool) {
	if s == nil {
		return telegramRevisionContext{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, ok := s.revisions[chatID]
	if !ok {
		return telegramRevisionContext{}, false
	}
	delete(s.revisions, chatID)
	if isExpiredTelegramState(ctx.RegisteredAt) {
		return telegramRevisionContext{}, false
	}
	return ctx, true
}

func isExpiredTelegramState(registeredAt time.Time) bool {
	if registeredAt.IsZero() {
		return false
	}
	return time.Since(registeredAt) > telegramApprovalStateTTL
}

func telegramTextFromResponse(response contracts.AgentResponse) string {
	switch response.Status {
	case contracts.AgentStatusFailed, contracts.AgentStatusBlocked, contracts.AgentStatusMaxIterationsReached:
		return telegramGenericErrorText()
	case contracts.AgentStatusApprovalRequired:
		if response.ApprovalRequest != nil {
			return telegramApprovalText(*response.ApprovalRequest)
		}
		return "Mình cần bạn xác nhận trước khi thực hiện hành động này."
	}

	text := strings.TrimSpace(response.Message)
	if text == "" && response.Output != nil {
		text = strings.TrimSpace(response.Output.Text)
	}
	text = sanitizeTelegramResponseText(text)
	if text != "" {
		return text
	}

	switch response.Status {
	case contracts.AgentStatusCompleted:
		return "Đã hoàn tất."
	case contracts.AgentStatusNeedClarification:
		if response.Data != nil {
			if clarifyQuestion, ok := response.Data["clarifyQuestion"].(string); ok {
				if text := sanitizeTelegramResponseText(clarifyQuestion); text != "" {
					return text
				}
			}
		}
		return "Bạn muốn mình làm gì cụ thể hơn?"
	default:
		return "Agent chưa có phản hồi."
	}
}

func telegramApprovalText(approval contracts.ApprovalRequest) string {
	action := telegramActionLabel(approval.ToolCall.ToolName)
	lines := []string{
		"Cần bạn xác nhận trước khi thực hiện.",
		"",
		"Hành động: " + action,
	}
	if summary := sanitizeTelegramResponseText(approval.Summary); summary != "" && !strings.EqualFold(summary, "Mình cần bạn xác nhận trước khi thực hiện hành động này.") {
		lines = append(lines, summary)
	}
	if detail := telegramApprovalDetailText(approval); detail != "" {
		lines = append(lines, "", detail)
	}
	lines = append(lines, "", "Bạn có thể xác nhận hoặc hủy. Nếu muốn thay đổi, cứ nhắn thêm cho mình.")
	return formatTelegramUserText(lines...)
}

func telegramRevisionPrompt(ctx telegramApprovalContext) string {
	lines := []string{
		"Bạn muốn chỉnh phần nào trước khi mình thực hiện?",
	}
	if strings.TrimSpace(ctx.PromptText) != "" {
		lines = append(lines, "", "Nội dung đang chờ xác nhận:", "", ctx.PromptText)
	}
	lines = append(lines, "")
	switch strings.TrimSpace(ctx.ToolName) {
	case "sandbox.runPython":
		lines = append(lines, "Ví dụ: đổi đoạn code, đổi file script, hoặc nói rõ bạn muốn code làm gì.")
	case "sandbox.runShell":
		lines = append(lines, "Ví dụ: đổi câu lệnh, đổi thư mục chạy, hoặc nói rõ kết quả bạn muốn.")
	default:
		lines = append(lines, "Ví dụ: đổi người nhận, đổi nội dung, đổi thời gian, hoặc nói rõ phần bạn muốn sửa.")
	}
	return formatTelegramUserText(lines...)
}

func telegramActionLabel(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "gmail.createDraft":
		return "Tạo bản nháp Gmail"
	case "gmail.updateDraft":
		return "Cập nhật bản nháp Gmail"
	case "gmail.replyDraft":
		return "Soạn thư trả lời Gmail"
	case "gmail.forwardDraft":
		return "Soạn thư chuyển tiếp Gmail"
	case "gmail.sendDraft":
		return "Gửi email"
	case "gmail.deleteDraft":
		return "Xóa bản nháp Gmail"
	case "gmail.modifyMessage", "gmail.batchModifyMessages":
		return "Cập nhật trạng thái email"
	case "gmail.trashMessage":
		return "Chuyển email vào thùng rác"
	case "gmail.untrashMessage":
		return "Khôi phục email khỏi thùng rác"
	case "gmail.downloadAttachments":
		return "Tải tệp đính kèm Gmail"
	case "calendar.createEvent":
		return "Tạo sự kiện Google Calendar"
	case "calendar.updateEvent":
		return "Cập nhật sự kiện Google Calendar"
	case "calendar.deleteEvent":
		return "Xóa sự kiện Google Calendar"
	case "chat.sendMessage":
		return "Gửi tin nhắn Google Chat"
	case "chat.updateMessage":
		return "Cập nhật tin nhắn Google Chat"
	case "chat.deleteMessage":
		return "Xóa tin nhắn Google Chat"
	case "chat.createSpace":
		return "Tạo Google Chat space"
	case "chat.addMember":
		return "Thêm thành viên vào Google Chat"
	case "chat.removeMember":
		return "Xóa thành viên khỏi Google Chat"
	case "sandbox.runPython":
		return "Chạy mã Python trong sandbox"
	case "sandbox.runShell":
		return "Chạy lệnh shell trong sandbox"
	default:
		if strings.TrimSpace(toolName) == "" {
			return "Thực hiện hành động này"
		}
		return fmt.Sprintf("Chạy %s", strings.TrimSpace(toolName))
	}
}

func sanitizeTelegramResponseText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if looksLikeTelegramMachinePayload(text) {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	skipJSONBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		if skipJSONBlock {
			if trimmed == "" {
				skipJSONBlock = false
			}
			continue
		}
		switch {
		case trimmed == "":
			filtered = append(filtered, "")
		case strings.HasPrefix(lower, "approval id:"),
			strings.HasPrefix(lower, "tool:"),
			strings.HasPrefix(lower, "risk:"):
			continue
		case strings.HasPrefix(lower, "input:"):
			skipJSONBlock = true
			continue
		case telegramSensitiveTextPattern.MatchString(trimmed):
			continue
		default:
			filtered = append(filtered, trimmed)
		}
	}

	clean := formatTelegramUserText(filtered...)
	if telegramSensitiveTextPattern.MatchString(clean) {
		return ""
	}
	return clean
}

func telegramApprovalDetailText(approval contracts.ApprovalRequest) string {
	input := approval.ToolCall.Input
	switch strings.TrimSpace(approval.ToolCall.ToolName) {
	case "sandbox.runPython":
		if code := stringMapValue(input, "code"); code != "" {
			return "Mã Python sẽ chạy:\n\n" + telegramCodeBlock("python", code)
		}
		if scriptPath := stringMapValue(input, "script_path", "scriptPath"); scriptPath != "" {
			return "File Python sẽ chạy: " + scriptPath
		}
	case "sandbox.runShell":
		if command := stringMapValue(input, "command"); command != "" {
			return "Lệnh shell sẽ chạy:\n\n" + telegramCodeBlock("bash", command)
		}
	}
	return ""
}

func telegramCodeBlock(language string, code string) string {
	return telegramCodeBlockOpen + strings.TrimSpace(language) + "\n" + strings.TrimSpace(code) + telegramCodeBlockClose
}

func stringMapValue(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if input == nil {
			continue
		}
		value, ok := input[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			text = strings.TrimSpace(text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func looksLikeTelegramMachinePayload(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func formatTelegramUserText(lines ...string) string {
	out := make([]string, 0, len(lines))
	previousBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) > 0 && !previousBlank {
				out = append(out, "")
			}
			previousBlank = true
			continue
		}
		out = append(out, trimmed)
		previousBlank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func looksLikeTelegramApprovalCommand(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	switch strings.ToLower(trimmed) {
	case "approve", "/approve", "reject", "/reject":
		return true
	}

	first := strings.ToLower(strings.TrimSpace(strings.Fields(trimmed)[0]))
	switch first {
	case "approve", "/approve", "reject", "/reject", "revise", "/revise", "sửa", "sua", "chỉnh", "chinh":
		return true
	default:
		return false
	}
}
