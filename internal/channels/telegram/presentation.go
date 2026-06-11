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
)

const telegramApprovalStateTTL = 30 * time.Minute

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
	case contracts.AgentStatusFailed, contracts.AgentStatusBlocked, contracts.AgentStatusMaxIterationsReached:
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
	if detail := telegramApprovalDetailText(approval); detail != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, detail)
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
	case "drive.moveFile":
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
	case "calendar.createEvent", "calendar.updateEvent":
		if detail := telegramCalendarApprovalDetailText(input); detail != "" {
			return detail
		}
	case "chat.sendMessage", "chat.updateMessage":
		if detail := telegramChatApprovalDetailText(input); detail != "" {
			return detail
		}
	case "gmail.sendDraft":
		return "Bản nháp Gmail này sẽ được gửi ngay sau khi bạn xác nhận."
	case "gmail.downloadAttachments":
		input = telegramDisplayDownloadAttachmentInput(input)
	}
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
	return telegramGenericApprovalDetailText(input)
}

func telegramDisplayDownloadAttachmentInput(input map[string]any) map[string]any {
	outputDir, ok := input["outputDir"]
	if !ok {
		return input
	}
	text := strings.TrimSpace(fmt.Sprint(outputDir))
	if text == "" || filepath.IsAbs(text) {
		return input
	}

	resolved, err := telegramDownloadOutputDir()
	if err != nil || strings.TrimSpace(resolved) == "" {
		return input
	}

	clone := make(map[string]any, len(input))
	for key, value := range input {
		clone[key] = value
	}
	clone["outputDir"] = telegramDisplayDownloadDir(filepath.Join(resolved, "attachment"))
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
		lines = append(lines, "", telegramPreBlock(body))
	}
	if attachments := attachmentNames(input, "attachments"); len(attachments) > 0 {
		lines = append(lines, "", telegramField("Tệp đính kèm", strings.Join(attachments, ", ")))
	}
	return formatTelegramUserText(lines...)
}

func telegramCalendarApprovalDetailText(input map[string]any) string {
	lines := []string{}

	if title := firstNonEmptyStringMapValue(input, "title", "name", "subject"); title != "" {
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

	if attendees := stringSliceMapValue(input, "attendees"); len(attendees) > 0 {
		lines = append(lines, telegramTextField("Người tham gia", strings.Join(attendees, ", ")))
	}
	if location := stringMapValue(input, "location"); location != "" {
		lines = append(lines, telegramTextField("Địa điểm", location))
	}
	if description := firstNonEmptyStringMapValue(input, "description"); description != "" {
		lines = append(lines, "", "Ghi chú:", "", telegramPreBlock(description))
	}

	return formatTelegramUserText(lines...)
}

func telegramChatApprovalDetailText(input map[string]any) string {
	lines := []string{}
	if body := firstNonEmptyStringMapValue(input, "text", "message", "content", "body"); body != "" {
		lines = append(lines, telegramPreBlock(body))
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

	for _, spec := range []struct {
		keys      []string
		label     string
		multiline bool
	}{
		{keys: []string{"title", "name", "subject"}, label: "Tiêu đề"},
		{keys: []string{"description", "textBody", "body", "content", "message", "text", "htmlBody"}, label: "Nội dung", multiline: true},
		{keys: []string{"to"}, label: "Người nhận"},
		{keys: []string{"cc"}, label: "CC"},
		{keys: []string{"bcc"}, label: "BCC"},
		{keys: []string{"location"}, label: "Địa điểm"},
		{keys: []string{"date", "startDate", "startTime"}, label: "Bắt đầu"},
		{keys: []string{"endDate", "endTime", "dueDate", "dueTime"}, label: "Kết thúc"},
		{keys: []string{"attachments"}, label: "Tệp đính kèm"},
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
				appendMultiline(spec.label, fmt.Sprint(value))
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
				if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
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
	case "draftid", "messageid", "threadid", "userid", "user_id", "approvalid", "toolcallid", "tool_call_id", "rendermode", "previewchars", "full", "pagetoken", "page_token", "source":
		return true
	}
	if strings.HasSuffix(key, "id") || strings.HasSuffix(key, "ids") {
		return true
	}
	return strings.TrimSpace(telegramApprovalValueText(value)) == ""
}

func telegramApprovalValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []string:
		return strings.Join(cleanStringSlice(typed), ", ")
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				items = append(items, text)
			}
		}
		return strings.Join(items, ", ")
	case bool:
		if typed {
			return "Có"
		}
		return "Không"
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
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
	case "revise", "/revise", "sửa", "sua", "chỉnh", "chinh", "sÃ¡Â»Â­a", "chÃ¡Â»â€°nh":
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
	case "approve", "/approve", "reject", "/reject", "revise", "/revise", "sửa", "sua", "chỉnh", "chinh", "sÃ¡Â»Â­a", "chÃ¡Â»â€°nh":
		return true
	default:
		return false
	}
}
