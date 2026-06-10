package slack

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"

	"vclaw/internal/contracts"
)

const slackApprovalStateTTL = 30 * time.Minute

var slackSensitiveTextPattern = regexp.MustCompile(`(?i)(authorization:|bearer\s+[a-z0-9._\-]+|xox[baprs]-|xapp-|sk-[a-z0-9]|stack trace|traceback|panic:|provider chat failed|client secret|refresh token|access token|api[_ -]?key)`)

type slackChannelState struct {
	mu        sync.Mutex
	approvals map[string]slackApprovalContext
}

type slackApprovalContext struct {
	ApprovalID   string
	SessionID    string
	ChannelID    string
	MessageTS    string
	PromptText   string
	ToolName     string
	RegisteredAt time.Time
}

func newSlackChannelState() *slackChannelState {
	return &slackChannelState{
		approvals: make(map[string]slackApprovalContext),
	}
}

func (s *slackChannelState) registerApproval(ctx slackApprovalContext) {
	if s == nil || strings.TrimSpace(ctx.ApprovalID) == "" || strings.TrimSpace(ctx.SessionID) == "" || strings.TrimSpace(ctx.ChannelID) == "" || strings.TrimSpace(ctx.MessageTS) == "" {
		return
	}
	if ctx.RegisteredAt.IsZero() {
		ctx.RegisteredAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for approvalID, existing := range s.approvals {
		if existing.ChannelID == ctx.ChannelID && existing.SessionID == ctx.SessionID && approvalID != ctx.ApprovalID {
			delete(s.approvals, approvalID)
		}
	}
	s.approvals[ctx.ApprovalID] = ctx
}

func (s *slackChannelState) lookupApproval(approvalID, sessionID, channelID, messageTS string) (slackApprovalContext, bool) {
	if s == nil {
		return slackApprovalContext{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, ok := s.approvals[strings.TrimSpace(approvalID)]
	if !ok {
		return slackApprovalContext{}, false
	}
	if isExpiredSlackState(ctx.RegisteredAt) {
		delete(s.approvals, ctx.ApprovalID)
		return slackApprovalContext{}, false
	}
	if ctx.SessionID != strings.TrimSpace(sessionID) || ctx.ChannelID != strings.TrimSpace(channelID) || ctx.MessageTS != strings.TrimSpace(messageTS) {
		return slackApprovalContext{}, false
	}
	return ctx, true
}

func (s *slackChannelState) approvalForSession(sessionID, channelID string) (slackApprovalContext, bool) {
	if s == nil {
		return slackApprovalContext{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for approvalID, ctx := range s.approvals {
		if ctx.SessionID != strings.TrimSpace(sessionID) || ctx.ChannelID != strings.TrimSpace(channelID) {
			continue
		}
		if isExpiredSlackState(ctx.RegisteredAt) {
			delete(s.approvals, approvalID)
			return slackApprovalContext{}, false
		}
		return ctx, true
	}
	return slackApprovalContext{}, false
}

func (s *slackChannelState) deleteApproval(approvalID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.approvals, strings.TrimSpace(approvalID))
}

func isExpiredSlackState(registeredAt time.Time) bool {
	if registeredAt.IsZero() {
		return false
	}
	return time.Since(registeredAt) > slackApprovalStateTTL
}

func slackTextFromResponse(response contracts.AgentResponse) string {
	if slackIsUserCancelledApproval(response) {
		return "Đã hủy theo yêu cầu của bạn."
	}

	switch response.Status {
	case contracts.AgentStatusFailed, contracts.AgentStatusBlocked, contracts.AgentStatusMaxIterationsReached:
		return slackGenericErrorText()
	case contracts.AgentStatusApprovalRequired:
		if response.ApprovalRequest != nil {
			return slackApprovalText(*response.ApprovalRequest)
		}
		return "Mình cần bạn xác nhận trước khi thực hiện hành động này."
	}

	text := strings.TrimSpace(response.Message)
	if text == "" && response.Output != nil {
		text = strings.TrimSpace(response.Output.Text)
	}
	text = sanitizeSlackResponseText(text)
	if text != "" {
		return text
	}

	switch response.Status {
	case contracts.AgentStatusCompleted:
		return "Đã hoàn tất."
	case contracts.AgentStatusNeedClarification:
		if response.Data != nil {
			if clarifyQuestion, ok := response.Data["clarifyQuestion"].(string); ok {
				if text := sanitizeSlackResponseText(clarifyQuestion); text != "" {
					return text
				}
			}
		}
		return "Bạn muốn mình làm gì cụ thể hơn?"
	default:
		return "Agent chưa có phản hồi."
	}
}

func slackIsUserCancelledApproval(response contracts.AgentResponse) bool {
	if response.Error != nil && strings.EqualFold(strings.TrimSpace(response.Error.Message), "approval rejected") {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(response.Message)), "đã hủy thao tác")
}

func slackApprovalText(approval contracts.ApprovalRequest) string {
	action := slackActionLabel(approval.ToolCall.ToolName)
	lines := []string{
		"Cần bạn xác nhận trước khi thực hiện.",
		"",
		"Hành động: " + action,
	}
	if summary := sanitizeSlackResponseText(approval.Summary); summary != "" && !strings.EqualFold(summary, "Mình cần bạn xác nhận trước khi thực hiện hành động này.") {
		lines = append(lines, summary)
	}
	if detail := slackApprovalDetailText(approval); detail != "" {
		lines = append(lines, "", detail)
	}
	lines = append(lines, "", "Bạn có thể xác nhận hoặc hủy. Nếu muốn thay đổi, cứ nhắn thêm cho mình.")
	return formatSlackUserText(lines...)
}

func slackRevisionPrompt(ctx slackApprovalContext) string {
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
	return formatSlackUserText(lines...)
}

func slackActionLabel(toolName string) string {
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

func sanitizeSlackResponseText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if looksLikeSlackMachinePayload(text) {
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
		case slackSensitiveTextPattern.MatchString(trimmed):
			continue
		default:
			filtered = append(filtered, trimmed)
		}
	}

	clean := formatSlackUserText(filtered...)
	if slackSensitiveTextPattern.MatchString(clean) {
		return ""
	}
	return clean
}

func slackApprovalDetailText(approval contracts.ApprovalRequest) string {
	input := approval.ToolCall.Input
	toolName := strings.TrimSpace(approval.ToolCall.ToolName)
	switch toolName {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		if detail := slackDraftApprovalDetailText(input); detail != "" {
			return detail
		}
	case "gmail.sendDraft":
		return "Bản nháp Gmail này sẽ được gửi ngay sau khi bạn xác nhận."
	}
	switch strings.TrimSpace(approval.ToolCall.ToolName) {
	case "sandbox.runPython":
		if code := stringMapValue(input, "code"); code != "" {
			return "Mã Python sẽ chạy:\n\n" + code
		}
		if scriptPath := stringMapValue(input, "script_path", "scriptPath"); scriptPath != "" {
			return "File Python sẽ chạy: " + scriptPath
		}
	case "sandbox.runShell":
		if command := stringMapValue(input, "command"); command != "" {
			return "Lệnh shell sẽ chạy:\n\n" + command
		}
	}
	return slackGenericApprovalDetailText(input)
}

func slackDraftApprovalDetailText(input map[string]any) string {
	lines := []string{}
	if recipients := stringSliceMapValue(input, "to"); len(recipients) > 0 {
		lines = append(lines, "Người nhận: "+strings.Join(recipients, ", "))
	}
	if cc := stringSliceMapValue(input, "cc"); len(cc) > 0 {
		lines = append(lines, "CC: "+strings.Join(cc, ", "))
	}
	if bcc := stringSliceMapValue(input, "bcc"); len(bcc) > 0 {
		lines = append(lines, "BCC: "+strings.Join(bcc, ", "))
	}
	if subject := stringMapValue(input, "subject"); subject != "" {
		lines = append(lines, "Tiêu đề: "+subject)
	}
	if body := firstNonEmptyStringMapValue(input, "textBody", "body", "content", "message", "text", "htmlBody"); body != "" {
		lines = append(lines, "", "Nội dung email:", "", body)
	}
	if attachments := attachmentNames(input, "attachments"); len(attachments) > 0 {
		lines = append(lines, "", "Tệp đính kèm: "+strings.Join(attachments, ", "))
	}
	return formatSlackUserText(lines...)
}

func slackGenericApprovalDetailText(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}

	lines := []string{}
	seen := map[string]struct{}{}

	appendLine := func(label string, value any) {
		text := slackApprovalValueText(value)
		if strings.TrimSpace(text) == "" {
			return
		}
		lines = append(lines, label+": "+text)
	}
	appendMultiline := func(label string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
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
			if !ok || slackShouldSkipApprovalField(key, value) {
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
		if _, ok := seen[key]; ok || slackShouldSkipApprovalField(key, value) {
			continue
		}
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		appendLine(humanizeApprovalKey(key), input[key])
	}

	return formatSlackUserText(lines...)
}

func looksLikeSlackMachinePayload(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func formatSlackUserText(lines ...string) string {
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

func slackApprovalBlocks(text, approvalID, sessionID string) []slack.Block {
	approve := slack.NewButtonBlockElement(
		slackApprovalApproveActionID,
		slackApprovalValue("approve", approvalID, sessionID),
		slack.NewTextBlockObject(slack.PlainTextType, "Xác nhận", false, false),
	).WithStyle(slack.StylePrimary)
	reject := slack.NewButtonBlockElement(
		slackApprovalRejectActionID,
		slackApprovalValue("reject", approvalID, sessionID),
		slack.NewTextBlockObject(slack.PlainTextType, "Hủy", false, false),
	).WithStyle(slack.StyleDanger)
	sectionText := slack.NewTextBlockObject(slack.MarkdownType, slackMrkdwn(text), false, false)
	return []slack.Block{
		slack.NewSectionBlock(sectionText, nil, nil),
		slack.NewActionBlock("vclaw_approval_actions", approve, reject),
	}
}

func slackApprovalValue(action, approvalID, sessionID string) string {
	data, err := json.Marshal(slackApprovalPayload{
		Action:     strings.TrimSpace(action),
		ApprovalID: strings.TrimSpace(approvalID),
		SessionID:  strings.TrimSpace(sessionID),
	})
	if err != nil {
		return "{}"
	}
	return string(data)
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

func slackShouldSkipApprovalField(key string, value any) bool {
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
	return strings.TrimSpace(slackApprovalValueText(value)) == ""
}

func slackApprovalValueText(value any) string {
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
