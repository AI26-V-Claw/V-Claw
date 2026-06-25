package telegram

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vclaw/internal/channels/formatting"
	"vclaw/internal/contracts"
	"vclaw/internal/traceutil"
)

const telegramApprovalStateTTL = 30 * time.Minute
const telegramApprovalPreviewRunes = 1200

const (
	telegramCodeBlockOpen  = "\uE000"
	telegramCodeBlockClose = "\uE001"
	telegramPreBlockOpen   = "\uE002"
	telegramPreBlockClose  = "\uE003"
	telegramFieldOpen      = "\uE004"
	telegramFieldClose     = "\uE005"
	telegramFieldSeparator = "\uE006"
	telegramTextFieldOpen  = "\uE007"
	telegramTextFieldClose = "\uE008"
)

var telegramSensitiveTextPattern = regexp.MustCompile(`(?i)(authorization:|bearer\s+[a-z0-9._\-]+|xox[baprs]-|xapp-|sk-[a-z0-9]|telegram-token|stack trace|traceback|panic:|provider chat failed|client secret|refresh token|access token|api[_ -]?key)`)
var telegramCalendarEventURLPattern = regexp.MustCompile(`https?://(?:calendar\.google\.com|(?:www\.)?google\.com/calendar)/\S+`)

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

func (s *telegramChannelState) hasApproval(approvalID string) bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, ok := s.approvals[strings.TrimSpace(approvalID)]
	if !ok {
		return false
	}
	if isExpiredTelegramState(ctx.RegisteredAt) {
		delete(s.approvals, ctx.ApprovalID)
		return false
	}
	return true
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
	if telegramIsUserCancelledApproval(response) {
		return "Đã hủy theo yêu cầu của bạn."
	}
	if response.Error != nil && response.Error.Code == contracts.ErrorActionBlockedByPolicy {
		return "Hành động này không được phép thực hiện do chính sách bảo mật hiện tại."
	}
	if response.Error != nil && response.Error.Code == contracts.ErrorApprovalExpired {
		return "Yêu cầu xác nhận đã hết hạn. Vui lòng thử lại."
	}

	switch response.Status {
	case contracts.AgentStatusFailed, contracts.AgentStatusBlocked, contracts.AgentStatusIterationBudgetExhausted:
		return telegramGenericErrorText()
	case contracts.AgentStatusApprovalRequired:
		if response.ApprovalRequest != nil {
			return telegramApprovalText(*response.ApprovalRequest)
		}
		return "Mình cần bạn xác nhận trước khi thực hiện hành động này."
	}

	if text := telegramDownloadAttachmentsResultText(response.ToolResults); text != "" {
		return text
	}

	text := strings.TrimSpace(response.Message)
	if text == "" && response.Output != nil {
		text = strings.TrimSpace(response.Output.Text)
	}
	text = sanitizeTelegramResponseText(text)
	if text != "" {
		text = telegramCompactCalendarEventLinksV2(text)
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

func telegramTraceURL(response contracts.AgentResponse) string {
	if response.Status != contracts.AgentStatusFailed {
		return ""
	}
	if response.Data == nil {
		return ""
	}
	traceID, _ := response.Data["trace_id"].(string)
	return traceutil.BuildTraceURL(traceID)
}

func telegramDownloadAttachmentsResultText(results []contracts.ToolResult) string {
	for _, result := range results {
		if !result.Success || strings.TrimSpace(result.ToolName) != "gmail.downloadAttachments" {
			continue
		}
		data, ok := result.Data.(map[string]any)
		if !ok {
			continue
		}
		content, ok := data["contentForLLM"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}

		var payload struct {
			Files []struct {
				Filename string `json:"filename"`
				Path     string `json:"path"`
			} `json:"files"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil || len(payload.Files) == 0 {
			continue
		}

		names := make([]string, 0, len(payload.Files))
		for _, file := range payload.Files {
			if strings.TrimSpace(file.Filename) != "" {
				names = append(names, strings.TrimSpace(file.Filename))
			}
		}
		if len(names) == 0 {
			continue
		}

		dir := telegramDisplayDownloadDir(payload.Files[0].Path)
		if len(names) == 1 {
			return fmt.Sprintf("Đã tải xuống: %s\nThư mục: %s", names[0], dir)
		}
		return fmt.Sprintf("Đã tải xuống: %s\nThư mục: %s", strings.Join(names, ", "), dir)
	}
	return ""
}

func telegramCompactCalendarEventLinks(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, "](") {
			continue
		}
		lines[i] = telegramCalendarEventURLPattern.ReplaceAllStringFunc(line, func(rawURL string) string {
			return "[Mở sự kiện](" + rawURL + ")"
		})
	}
	return strings.Join(lines, "\n")
}

func telegramCompactCalendarEventLinksV2(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, "](") {
			continue
		}
		lines[i] = telegramCompactCalendarEventLinksInLine(line)
	}
	return strings.Join(lines, "\n")
}

func telegramCompactCalendarEventLinksInLine(line string) string {
	matches := telegramCalendarEventURLPattern.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}
	var builder strings.Builder
	last := 0
	for _, match := range matches {
		if len(match) != 2 {
			continue
		}
		start, end := match[0], match[1]
		rawURL := line[start:end]
		if !isTelegramCalendarEventURL(rawURL) {
			continue
		}
		builder.WriteString(line[last:start])
		builder.WriteString("[Mở sự kiện](")
		builder.WriteString(rawURL)
		builder.WriteString(")")
		last = end
	}
	if last == 0 {
		return line
	}
	builder.WriteString(line[last:])
	return builder.String()
}

func isTelegramCalendarEventURL(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	return strings.Contains(lower, "calendar.google.com/") || strings.Contains(lower, "google.com/calendar/")
}

func telegramDownloadOutputDir() (string, error) {
	homeDir := telegramHomeDir()

	downloadsDir := filepath.Join(homeDir, "Downloads")
	if info, err := os.Stat(downloadsDir); err == nil && info.IsDir() {
		return filepath.Join(downloadsDir, "Vclaw"), nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	return filepath.Join(homeDir, "Vclaw"), nil
}

func telegramDisplayDownloadDir(path string) string {
	dir := filepath.Dir(strings.TrimSpace(path))
	if dir == "." || dir == "" {
		return "~/Downloads/Vclaw/"
	}

	homeDir := telegramHomeDir()
	if strings.TrimSpace(homeDir) != "" {
		homeDir = filepath.Clean(homeDir)
		cleanDir := filepath.Clean(dir)
		if cleanDir == homeDir {
			dir = "~"
		} else if strings.HasPrefix(cleanDir, homeDir+string(filepath.Separator)) {
			dir = "~" + string(filepath.Separator) + strings.TrimPrefix(cleanDir, homeDir+string(filepath.Separator))
		} else {
			dir = cleanDir
		}
	}

	dir = filepath.ToSlash(dir)
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	return dir
}

func telegramHomeDir() string {
	if homeDir := strings.TrimSpace(os.Getenv("HOME")); homeDir != "" {
		return homeDir
	}
	if homeDir := strings.TrimSpace(os.Getenv("USERPROFILE")); homeDir != "" {
		return homeDir
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return homeDir
}

func telegramIsUserCancelledApproval(response contracts.AgentResponse) bool {
	if response.Error != nil && strings.EqualFold(strings.TrimSpace(response.Error.Message), "approval rejected") {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(response.Message)), "đã hủy thao tác")
}

func telegramApprovalText(approval contracts.ApprovalRequest) string {
	lines := []string{}
	if summary := sanitizeTelegramResponseText(approval.Summary); summary != "" && !strings.EqualFold(summary, "Mình cần bạn xác nhận trước khi thực hiện hành động này.") {
		lines = append(lines, summary)
	}
	if telegramShouldShowApprovalDetail(approval) {
		if detail := telegramApprovalDetailText(approval); detail != "" {
			lines = append(lines, "", detail)
		}
	}
	lines = append(lines, "", "Bạn có thể xác nhận hoặc hủy.")
	return formatTelegramUserText(lines...)
}

func telegramShouldShowApprovalDetail(approval contracts.ApprovalRequest) bool {
	switch approval.RiskLevel {
	case contracts.RiskLevelExternalWrite,
		contracts.RiskLevelLocalWrite,
		contracts.RiskLevelCodeExecution,
		contracts.RiskLevelDestructive:
		return true
	}

	switch strings.TrimSpace(approval.ToolCall.ToolName) {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft",
		"gmail.sendDraft", "gmail.deleteDraft", "gmail.downloadAttachments",
		"gmail.modifyMessage", "gmail.batchModifyMessages", "gmail.trashMessage", "gmail.untrashMessage",
		"calendar.createEvent", "calendar.updateEvent", "calendar.respondEvent", "calendar.deleteEvent",
		"chat.sendMessage", "chat.updateMessage", "chat.deleteMessage", "chat.createSpace", "chat.addMember", "chat.removeMember",
		"drive.saveFile", "drive.createFolder", "drive.createFile", "drive.uploadFile", "drive.updateFileMetadata",
		"drive.shareFile", "drive.revokePermission", "drive.moveFile", "drive.moveFiles", "drive.trashFile", "drive.untrashFile",
		"docs.createDocument", "docs.appendText", "docs.replaceText", "docs.insertText", "docs.deleteContent",
		"sheets.createSpreadsheet", "sheets.updateValues", "sheets.batchUpdateValues", "sheets.appendValues",
		"sheets.clearValues", "sheets.addSheet", "sheets.renameSheet", "sheets.deleteSheet", "sheets.duplicateSheet",
		"meet.createMeeting", "sandbox.runPython", "sandbox.runShell":
		return true
	default:
		return false
	}
}

func telegramRevisionPrompt(ctx telegramApprovalContext) string {
	return formatTelegramUserText(
		"Bạn muốn chỉnh phần nào trước khi mình thực hiện?",
		"",
		"Nhắn ngắn gọn phần bạn muốn đổi, rồi mình làm lại.",
	)
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
	case "calendar.respondEvent":
		return "Phản hồi lời mời Google Calendar"
	case "calendar.deleteEvent":
		return "Xóa sự kiện Google Calendar"
	case "drive.createFolder":
		return "Tạo thư mục Google Drive"
	case "drive.createFile":
		return "Tạo tệp Google Drive"
	case "drive.uploadFile":
		return "Upload tệp lên Google Drive"
	case "drive.updateFileMetadata":
		return "Cập nhật metadata Google Drive"
	case "drive.shareFile":
		return "Chia sẻ tệp Google Drive"
	case "drive.revokePermission":
		return "Thu hồi quyền Google Drive"
	case "drive.moveFile", "drive.moveFiles":
		return "Di chuyển tệp Google Drive"
	case "drive.trashFile":
		return "Chuyển tệp Google Drive vào thùng rác"
	case "drive.untrashFile":
		return "Khôi phục tệp Google Drive"
	case "docs.createDocument":
		return "Tạo Google Docs"
	case "docs.appendText", "docs.replaceText", "docs.insertText":
		return "Cập nhật nội dung Google Docs"
	case "docs.deleteContent":
		return "Xóa nội dung Google Docs"
	case "sheets.createSpreadsheet":
		return "Tạo Google Sheets"
	case "sheets.updateValues", "sheets.batchUpdateValues", "sheets.appendValues", "sheets.clearValues":
		return "Cập nhật dữ liệu Google Sheets"
	case "sheets.addSheet", "sheets.renameSheet", "sheets.duplicateSheet":
		return "Cập nhật tab Google Sheets"
	case "sheets.deleteSheet":
		return "Xóa tab Google Sheets"
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
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = formatting.NormalizeLineEndings(text)
	if looksLikeTelegramMachinePayload(strings.TrimSpace(text)) {
		return ""
	}

	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inFence {
			filtered = append(filtered, line)
			if formatting.IsFencedCodeBlockClose(line) {
				inFence = false
			}
			continue
		}
		if _, ok := formatting.ParseFencedCodeBlockOpen(line); ok {
			filtered = append(filtered, line)
			inFence = true
			continue
		}
		switch {
		case trimmed == "":
			filtered = append(filtered, "")
		case telegramSensitiveTextPattern.MatchString(trimmed):
			continue
		default:
			filtered = append(filtered, line)
		}
	}

	clean := strings.Join(filtered, "\n")
	if strings.TrimSpace(clean) == "" {
		return ""
	}
	if telegramSensitiveTextPattern.MatchString(clean) {
		return ""
	}
	return clean
}

func telegramApprovalDetailText(approval contracts.ApprovalRequest) string {
	input := approval.ToolCall.Input
	toolName := strings.TrimSpace(approval.ToolCall.ToolName)
	switch toolName {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		if detail := telegramDraftApprovalDetailText(input); detail != "" {
			return detail
		}
	case "gmail.sendDraft", "gmail.deleteDraft", "gmail.downloadAttachments",
		"gmail.modifyMessage", "gmail.batchModifyMessages", "gmail.trashMessage", "gmail.untrashMessage":
		if detail := telegramGmailApprovalDetailText(toolName, input); detail != "" {
			return detail
		}
	case "calendar.createEvent", "calendar.updateEvent", "calendar.respondEvent", "calendar.deleteEvent":
		if detail := telegramCalendarApprovalDetailText(input); detail != "" {
			return detail
		}
	case "chat.sendMessage", "chat.updateMessage", "chat.deleteMessage", "chat.createSpace", "chat.addMember", "chat.removeMember":
		if detail := telegramChatApprovalDetailText(toolName, input); detail != "" {
			return detail
		}
	case "sandbox.runPython":
		if scriptPath := stringMapValue(input, "script_path", "scriptPath"); scriptPath != "" {
			return telegramTextField("File Python", filepath.Base(scriptPath)) + "\n" +
				telegramTextField("Môi trường", "Sandbox")
		}
		return telegramTextField("Môi trường", "Python sandbox") + "\n" +
			"Code được ẩn khỏi tin nhắn xác nhận."
	case "sandbox.runShell":
		if command := stringMapValue(input, "command"); command != "" {
			return "Lệnh shell sẽ chạy:\n\n" + telegramCodeBlock("bash", telegramApprovalPreview(command))
		}
	case "meet.createMeeting":
		if mode := stringMapValue(input, "mode"); mode != "" {
			return telegramTextField("Chế độ", mode)
		}
	}
	return telegramGenericApprovalDetailText(input)
}

func telegramDisplayDownloadAttachmentInput(input map[string]any) map[string]any {
	outputDirText := ""
	if v, ok := input["outputDir"]; ok {
		outputDirText = strings.TrimSpace(fmt.Sprint(v))
	}

	var displayDir string
	if outputDirText == "" || !filepath.IsAbs(outputDirText) {
		// Absent or relative — the workspace guard defaults to sandbox workspace root.
		displayDir = "workspace sandbox (mặc định)"
	} else {
		displayDir = telegramDisplayDownloadDir(outputDirText)
	}

	clone := make(map[string]any, len(input)+1)
	for key, value := range input {
		clone[key] = value
	}
	clone["outputDir"] = displayDir
	return clone
}

func telegramDraftApprovalDetailText(input map[string]any) string {
	lines := []string{}
	if recipients := stringSliceMapValue(input, "to"); len(recipients) > 0 {
		lines = append(lines, telegramField("Người nhận", strings.Join(recipients, ", ")))
	}
	if cc := stringSliceMapValue(input, "cc"); len(cc) > 0 {
		lines = append(lines, telegramField("CC", strings.Join(cc, ", ")))
	}
	if bcc := stringSliceMapValue(input, "bcc"); len(bcc) > 0 {
		lines = append(lines, telegramField("BCC", strings.Join(bcc, ", ")))
	}
	if subject := stringMapValue(input, "subject"); subject != "" {
		lines = append(lines, telegramField("Tiêu đề", subject))
	}
	if body := firstNonEmptyStringMapValue(input, "textBody", "body", "content", "message", "text", "htmlBody"); body != "" {
		lines = append(lines, "", telegramPreBlock(telegramApprovalPreview(body)))
	}
	if attachments := attachmentNames(input, "attachments"); len(attachments) > 0 {
		lines = append(lines, "", telegramField("Tệp đính kèm", strings.Join(attachments, ", ")))
	}
	return formatTelegramUserText(lines...)
}

func telegramGmailApprovalDetailText(toolName string, input map[string]any) string {
	lines := []string{}
	if name := firstNonEmptyStringMapValue(input, "resourceName", "subject", "title"); name != "" {
		lines = append(lines, telegramTextField("Email", name))
	}

	switch toolName {
	case "gmail.sendDraft":
		lines = append(lines, telegramTextField("Hành động", "Gửi bản nháp đã chọn"))
	case "gmail.deleteDraft":
		lines = append(lines, telegramTextField("Hành động", "Xóa bản nháp đã chọn"))
	case "gmail.downloadAttachments":
		input = telegramDisplayDownloadAttachmentInput(input)
		if filenames := stringSliceMapValue(input, "filenames"); len(filenames) > 0 {
			lines = append(lines, telegramTextField("Tệp đính kèm", strings.Join(filenames, ", ")))
		} else {
			lines = append(lines, telegramTextField("Tệp đính kèm", "Tất cả tệp trong email đã chọn"))
		}
		if outputDir := stringMapValue(input, "outputDir"); outputDir != "" {
			lines = append(lines, telegramTextField("Lưu vào", outputDir))
		}
	case "gmail.modifyMessage", "gmail.batchModifyMessages":
		if action := stringMapValue(input, "action"); action != "" {
			lines = append(lines, telegramTextField("Thay đổi", action))
		}
		if labels := stringSliceMapValue(input, "labelIds"); len(labels) > 0 {
			lines = append(lines, telegramTextField("Nhãn", strings.Join(labels, ", ")))
		}
	case "gmail.trashMessage":
		lines = append(lines, telegramTextField("Hành động", "Chuyển email vào thùng rác"))
	case "gmail.untrashMessage":
		lines = append(lines, telegramTextField("Hành động", "Khôi phục email khỏi thùng rác"))
	}
	return formatTelegramUserText(lines...)
}

func telegramCalendarApprovalDetailText(input map[string]any) string {
	lines := []string{}

	if title := firstNonEmptyStringMapValue(input, "eventTitle", "resourceName", "title", "name", "subject"); title != "" {
		lines = append(lines, telegramTextField("Tiêu đề", title))
	}

	startRaw := firstNonEmptyStringMapValue(input, "start", "startTime", "startDate", "date")
	endRaw := firstNonEmptyStringMapValue(input, "end", "endTime", "endDate", "dueDate", "dueTime")
	if start := telegramFormatApprovalDateTime(startRaw); start != "" {
		lines = append(lines, telegramTextField("Bắt đầu", start))
	}
	if end := telegramFormatApprovalDateTime(endRaw); end != "" {
		lines = append(lines, telegramTextField("Kết thúc", end))
	}
	if duration := approvalDurationText(startRaw, endRaw); duration != "" {
		lines = append(lines, telegramTextField("Thời lượng", duration))
	}

	if email := stringMapValue(input, "email"); email != "" {
		lines = append(lines, telegramTextField("Email", email))
	}
	if responseStatus := stringMapValue(input, "responseStatus"); responseStatus != "" {
		lines = append(lines, telegramTextField("Trạng thái tham dự", responseStatus))
	}
	if value, ok := input["attendees"]; ok {
		attendees := telegramApprovalValueText(value)
		if strings.Contains(attendees, "\n") {
			lines = append(lines, "Người tham gia:", attendees)
		} else if attendees != "" {
			lines = append(lines, telegramTextField("Người tham gia", attendees))
		}
	}
	if location := stringMapValue(input, "location"); location != "" {
		lines = append(lines, telegramTextField("Địa điểm", location))
	}
	if createConference, ok := input["createConference"].(bool); ok && createConference {
		lines = append(lines, telegramTextField("Google Meet", "Sẽ tạo link cho sự kiện"))
	}
	if description := firstNonEmptyStringMapValue(input, "description"); description != "" {
		lines = append(lines, "", "Ghi chú:", "", telegramPreBlock(telegramApprovalPreview(description)))
	}

	return formatTelegramUserText(lines...)
}

func telegramChatApprovalDetailText(toolName string, input map[string]any) string {
	lines := []string{}
	if recipient := stringMapValue(input, "recipientEmail"); recipient != "" {
		lines = append(lines, telegramTextField("Người nhận", recipient))
	} else if conversation := firstNonEmptyStringMapValue(input, "conversationName", "displayName"); conversation != "" {
		lines = append(lines, telegramTextField("Cuộc trò chuyện", conversation))
	}
	if members := stringSliceMapValue(input, "memberUsers", "users"); len(members) > 0 {
		lines = append(lines, telegramTextField("Thành viên", strings.Join(members, ", ")))
	}
	if spaceType := stringMapValue(input, "spaceType"); spaceType != "" {
		lines = append(lines, telegramTextField("Loại", spaceType))
	}
	if resource := firstNonEmptyStringMapValue(input, "resourceName", "messagePreview"); resource != "" {
		label := "Tin nhắn"
		if toolName == "chat.removeMember" {
			label = "Thành viên"
		}
		lines = append(lines, telegramTextField(label, telegramApprovalPreview(resource)))
	}
	if body := firstNonEmptyStringMapValue(input, "text", "message", "content", "body"); body != "" {
		lines = append(lines, "", telegramPreBlock(telegramApprovalPreview(body)))
	}
	if attachments := attachmentNames(input, "attachments"); len(attachments) > 0 {
		lines = append(lines, "", telegramTextField("Tệp đính kèm", strings.Join(attachments, ", ")))
	}
	return formatTelegramUserText(lines...)
}

func telegramGenericApprovalDetailText(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}

	lines := []string{}
	seen := map[string]struct{}{}

	appendLine := func(label string, value any) {
		text := telegramApprovalValueText(value)
		if strings.TrimSpace(text) == "" {
			return
		}
		lines = append(lines, label+": "+text)
	}
	appendMultiline := func(label string, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		lines = append(lines, label+":", "", value)
	}

	if localPath := stringMapValue(input, "localPath"); localPath != "" {
		lines = append(lines, "Tệp local: "+filepath.Base(localPath))
		seen["localPath"] = struct{}{}
	}

	for _, spec := range []struct {
		keys      []string
		label     string
		multiline bool
	}{
		{keys: []string{"resourceName"}, label: "Tài nguyên"},
		{keys: []string{"resourceNames"}, label: "Tài nguyên"},
		{keys: []string{"sourceFiles"}, label: "Nguồn di chuyển"},
		{keys: []string{"targetFolder"}, label: "Thư mục đích"},
		{keys: []string{"title", "name", "subject"}, label: "Tiêu đề"},
		{keys: []string{"description", "textBody", "body", "content", "message", "text", "htmlBody"}, label: "Nội dung", multiline: true},
		{keys: []string{"to", "recipientEmail", "emailAddress"}, label: "Người nhận"},
		{keys: []string{"cc"}, label: "CC"},
		{keys: []string{"bcc"}, label: "BCC"},
		{keys: []string{"users", "memberUsers"}, label: "Thành viên"},
		{keys: []string{"location"}, label: "Địa điểm"},
		{keys: []string{"date", "startDate", "startTime"}, label: "Bắt đầu"},
		{keys: []string{"endDate", "endTime", "dueDate", "dueTime"}, label: "Kết thúc"},
		{keys: []string{"attachments", "filenames"}, label: "Tệp đính kèm"},
		{keys: []string{"filename"}, label: "Tên tệp"},
		{keys: []string{"outputDir"}, label: "Lưu vào"},
		{keys: []string{"role"}, label: "Quyền"},
		{keys: []string{"type"}, label: "Đối tượng chia sẻ"},
		{keys: []string{"action"}, label: "Thay đổi"},
		{keys: []string{"range"}, label: "Vùng dữ liệu"},
		{keys: []string{"ranges"}, label: "Các vùng dữ liệu"},
		{keys: []string{"values"}, label: "Giá trị"},
		{keys: []string{"sheetTitles"}, label: "Tên các sheet"},
		{keys: []string{"newTitle"}, label: "Tên mới"},
		{keys: []string{"oldText"}, label: "Nội dung cần thay", multiline: true},
		{keys: []string{"newText"}, label: "Nội dung thay thế", multiline: true},
		{keys: []string{"query"}, label: "Truy vấn"},
		{keys: []string{"command"}, label: "Lệnh", multiline: true},
		{keys: []string{"path", "filePath", "scriptPath", "script_path"}, label: "Đường dẫn"},
	} {
		for _, key := range spec.keys {
			value, ok := input[key]
			if !ok || telegramShouldSkipApprovalField(key, value) {
				continue
			}
			seen[key] = struct{}{}
			if spec.multiline {
				appendMultiline(spec.label, telegramApprovalPreview(fmt.Sprint(value)))
			} else {
				appendLine(spec.label, value)
			}
			break
		}
	}

	extraKeys := make([]string, 0, len(input))
	for key, value := range input {
		if _, ok := seen[key]; ok || telegramShouldSkipApprovalField(key, value) {
			continue
		}
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		appendLine(humanizeApprovalKey(key), input[key])
	}

	return formatTelegramUserText(lines...)
}

func telegramApprovalPreview(text string) string {
	text = formatting.NormalizeLineEndings(strings.TrimSpace(text))
	runes := []rune(text)
	if len(runes) <= telegramApprovalPreviewRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:telegramApprovalPreviewRunes])) + "\n...[đã rút gọn]"
}

func telegramCodeBlock(language string, code string) string {
	return telegramCodeBlockOpen + strings.TrimSpace(language) + "\n" + code + telegramCodeBlockClose
}

func telegramPreBlock(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return telegramPreBlockOpen + formatting.NormalizeLineEndings(text) + telegramPreBlockClose
}

func telegramField(label string, value string) string {
	return telegramFieldOpen + strings.TrimSpace(label) + ":" + telegramFieldSeparator + strings.TrimSpace(value) + telegramFieldClose
}

func telegramTextField(label string, value string) string {
	return telegramTextFieldOpen + strings.TrimSpace(label) + ":" + telegramFieldSeparator + strings.TrimSpace(value) + telegramTextFieldClose
}

func telegramFormatApprovalDateTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Format("02/01/2006, 15:04") + " (" + utcOffsetText(parsed) + ")"
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("02/01/2006")
	}
	return value
}

func approvalDurationText(start string, end string) string {
	startTime, err := time.Parse(time.RFC3339, strings.TrimSpace(start))
	if err != nil {
		return ""
	}
	endTime, err := time.Parse(time.RFC3339, strings.TrimSpace(end))
	if err != nil || !endTime.After(startTime) {
		return ""
	}

	totalMinutes := int(endTime.Sub(startTime).Minutes())
	hours := totalMinutes / 60
	minutes := totalMinutes % 60

	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d giờ %d phút", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%d giờ", hours)
	default:
		return fmt.Sprintf("%d phút", minutes)
	}
}

func utcOffsetText(value time.Time) string {
	_, offsetSeconds := value.Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
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
			if strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func stringSliceMapValue(input map[string]any, keys ...string) []string {
	for _, key := range keys {
		if input == nil {
			continue
		}
		value, ok := input[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []string:
			if cleaned := cleanStringSlice(typed); len(cleaned) > 0 {
				return cleaned
			}
		case []any:
			items := make([]string, 0, len(typed))
			for _, item := range typed {
				if text := strings.TrimSpace(telegramApprovalValueText(item)); text != "" {
					items = append(items, text)
				}
			}
			if cleaned := cleanStringSlice(items); len(cleaned) > 0 {
				return cleaned
			}
		case string:
			if cleaned := cleanStringSlice(strings.Split(typed, ",")); len(cleaned) > 0 {
				return cleaned
			}
		}
	}
	return nil
}

func firstNonEmptyStringMapValue(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringMapValue(input, key); value != "" {
			return value
		}
	}
	return ""
}

func attachmentNames(input map[string]any, keys ...string) []string {
	paths := stringSliceMapValue(input, keys...)
	if len(paths) == 0 {
		return nil
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		name := strings.TrimSpace(filepath.Base(path))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = strings.TrimSpace(path)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func telegramShouldSkipApprovalField(key string, value any) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return true
	}
	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "authorization") || strings.Contains(key, "apikey") || strings.Contains(key, "api_key") {
		return true
	}
	switch key {
	case "draftid", "messageid", "threadid", "userid", "user_id", "approvalid", "toolcallid", "tool_call_id", "rendermode", "previewchars", "full", "pagetoken", "page_token", "source", "space":
		return true
	}
	if key == "name" {
		if text, ok := value.(string); ok && looksLikeTelegramOpaqueResourceName(text) {
			return true
		}
	}
	if strings.HasSuffix(key, "id") || strings.HasSuffix(key, "ids") {
		return true
	}
	return strings.TrimSpace(telegramApprovalValueText(value)) == ""
}

func looksLikeTelegramOpaqueResourceName(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "spaces/") ||
		strings.HasPrefix(value, "users/") ||
		strings.HasPrefix(value, "people/") ||
		strings.HasPrefix(value, "messages/")
}

func telegramApprovalValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.Join(cleanStringSlice(typed), ", ")
	case []any:
		items := make([]string, 0, len(typed))
		structured := false
		for _, item := range typed {
			if _, ok := item.(map[string]any); ok {
				structured = true
			}
			if text := strings.TrimSpace(telegramApprovalValueText(item)); text != "" {
				items = append(items, text)
			}
		}
		if structured {
			for i, item := range items {
				items[i] = "- " + item
			}
			return strings.Join(items, "\n")
		}
		return strings.Join(items, ", ")
	case map[string]any:
		return telegramApprovalMapText(typed)
	case bool:
		if typed {
			return "Có"
		}
		return "Không"
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func telegramApprovalMapText(value map[string]any) string {
	if len(value) == 0 {
		return ""
	}
	labels := []struct {
		key   string
		label string
	}{
		{key: "email", label: "email"},
		{key: "displayName", label: "tên"},
		{key: "responseStatus", label: "trạng thái"},
		{key: "title", label: "tiêu đề"},
	}
	seen := map[string]bool{}
	parts := make([]string, 0, len(value))
	for _, item := range labels {
		if text := strings.TrimSpace(telegramApprovalValueText(value[item.key])); text != "" {
			parts = append(parts, item.label+": "+text)
			seen[item.key] = true
		}
	}
	extraKeys := make([]string, 0, len(value))
	for key := range value {
		if !seen[key] && !telegramShouldSkipApprovalField(key, value[key]) {
			extraKeys = append(extraKeys, key)
		}
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		if text := strings.TrimSpace(telegramApprovalValueText(value[key])); text != "" {
			parts = append(parts, humanizeApprovalKey(key)+": "+text)
		}
	}
	return strings.Join(parts, ", ")
}

func humanizeApprovalKey(key string) string {
	replacer := strings.NewReplacer("_", " ", "-", " ")
	key = replacer.Replace(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	var builder strings.Builder
	runes := []rune(key)
	for i, r := range runes {
		if i > 0 && r >= 'A' && r <= 'Z' && runes[i-1] != ' ' {
			builder.WriteRune(' ')
		}
		builder.WriteRune(r)
	}
	text := strings.TrimSpace(builder.String())
	if text == "" {
		return ""
	}
	return strings.ToUpper(text[:1]) + text[1:]
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
	inFence := false
	for _, line := range lines {
		if inFence {
			out = append(out, line)
			if formatting.IsFencedCodeBlockClose(line) {
				inFence = false
			}
			previousBlank = false
			continue
		}
		if _, ok := formatting.ParseFencedCodeBlockOpen(line); ok {
			out = append(out, line)
			inFence = true
			previousBlank = false
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(out) > 0 && !previousBlank {
				out = append(out, "")
			}
			previousBlank = true
			continue
		}
		out = append(out, line)
		previousBlank = false
	}
	return strings.Join(out, "\n")
}

func telegramApprovalTextAction(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	switch strings.ToLower(trimmed) {
	case "approve", "/approve":
		return "approve"
	case "reject", "/reject":
		return "reject"
	}

	first := strings.ToLower(strings.TrimSpace(strings.Fields(trimmed)[0]))
	switch first {
	case "approve", "/approve":
		return "approve"
	case "reject", "/reject":
		return "reject"
	case "revise", "/revise", "sửa", "sua", "chỉnh", "chinh":
		return "revise"
	default:
		return ""
	}
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
