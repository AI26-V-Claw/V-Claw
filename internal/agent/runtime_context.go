package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"vclaw/internal/agent/reference"
	"vclaw/internal/contracts"
	"vclaw/internal/longmem"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

// redactSensitiveForPrompt drops lines that look like a credential, token, or
// secret before they enter the assembled context. It reuses the same detector
// the long-term memory write boundary uses (longmem.ValidateMemoryContent) so
// the assembly layer never surfaces a secret that slipped into a summary, tool
// result, long-term memory file, or resolved reference context. Non-secret
// content is returned unchanged.
func redactSensitiveForPrompt(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if longmem.ValidateMemoryContent(content) == nil {
		return content
	}
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if longmem.ValidateMemoryContent(line) != nil {
			kept = append(kept, "[redacted: sensitive content removed]")
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// referenceSourcesPrompt assembles an explicit "reference sources" block listing
// where the most recent answers came from: the tools that produced recent action
// results and any resolved file references (local path or Drive ID). This lets the
// agent cite provenance without re-deriving it, and is kept separate from the
// continuity-focused session memory block. It emits labels/locations only — never
// raw result content — so it cannot leak sensitive payloads.
func referenceSourcesPrompt(memory sessions.SessionMemory) string {
	lines := make([]string, 0, len(memory.LastActionResults)+len(memory.FileRefs))

	seenTools := make(map[string]bool)
	for i := len(memory.LastActionResults) - 1; i >= 0; i-- {
		name := strings.TrimSpace(memory.LastActionResults[i].ToolName)
		if name == "" || seenTools[name] {
			continue
		}
		seenTools[name] = true
		lines = append(lines, fmt.Sprintf("- tool result from %s", name))
	}

	for name, ref := range memory.FileRefs {
		switch ref.Source {
		case "local":
			lines = append(lines, fmt.Sprintf("- file %q: local, path=%s", name, ref.Path))
		case "drive":
			lines = append(lines, fmt.Sprintf("- file %q: Google Drive, driveId=%s", name, ref.DriveID))
		}
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace("Reference sources for the most recent results (provenance only — cite these when relevant, do not treat as approval):\n" + strings.Join(lines, "\n"))
}

func sessionMemoryPrompt(memory sessions.SessionMemory) string {
	parts := []string{}
	if summary := redactSensitiveForPrompt(memory.Summary); summary != "" {
		parts = append(parts, "Conversation summary:\n"+summary)
	}
	if len(memory.LastActionResults) > 0 {
		lines := make([]string, 0, len(memory.LastActionResults))
		for _, result := range memory.LastActionResults {
			content := redactSensitiveForPrompt(result.Content)
			if content == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(result.ToolName), truncateToolContentForLLM(content)))
		}
		if len(lines) > 0 {
			parts = append(parts, "Recent action results:\n"+strings.Join(lines, "\n"))
		}
	}
	if len(memory.FileRefs) > 0 {
		lines := make([]string, 0, len(memory.FileRefs))
		for name, ref := range memory.FileRefs {
			switch ref.Source {
			case "local":
				lines = append(lines, fmt.Sprintf("- %s: local file, path=%s", name, ref.Path))
			case "drive":
				lines = append(lines, fmt.Sprintf("- %s: Google Drive file, driveId=%s", name, ref.DriveID))
			}
		}
		if len(lines) > 0 {
			parts = append(parts, "Resolved file references (use these to attach files without re-resolving):\n"+strings.Join(lines, "\n"))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(`Session memory for understanding context only.
Use this memory to answer follow-up questions and maintain conversational continuity.
Do not use memory alone to fill required parameters for a new write, destructive, local file, or code execution action.
If the current user message does not explicitly provide required write parameters, ask a concise clarification question.
HARD RULE — chat.sendMessage recipients: For DM to a specific person, always use the recipientEmail parameter with their email address — do NOT use chat.findSpacesByMembers or chat.createSpace separately; the tool handles find-or-create internally. For group chats, resolve the space name with chat.listSpaces and pass the spaces/... resource name in the space parameter. A spaces/... value from history must NEVER be reused as the destination for a different person.
FILE REFS RULE — when "Resolved file references" lists a file by name: use the recorded source and path/driveId directly. Do NOT call filesystem.fileInfo or drive.listFiles again for that file unless the user says the file has changed.

` + strings.Join(parts, "\n\n"))
}

func historyWithSessionMemory(memory sessions.SessionMemory, history []string) []string {
	enriched := make([]string, 0, len(history)+2)
	if strings.TrimSpace(memory.Summary) != "" {
		enriched = append(enriched, "memory_summary: "+truncateToolContentForLLM(memory.Summary))
	}
	if len(memory.LastActionResults) > 0 {
		result := memory.LastActionResults[len(memory.LastActionResults)-1]
		if strings.TrimSpace(result.Content) != "" {
			enriched = append(enriched, "last_action_result: "+truncateToolContentForLLM(result.ToolName+" "+result.Content))
		}
	}
	enriched = append(enriched, history...)
	return enriched
}

func (r *Runtime) traceData(parts ...any) map[string]any {
	var planResult *TaskPlanResult
	var resolution *reference.Resolution
	for _, part := range parts {
		switch typed := part.(type) {
		case *TaskPlanResult:
			planResult = typed
		case *reference.Resolution:
			resolution = typed
		}
	}
	data := map[string]any{
		"model": r.model,
	}
	if resolution != nil {
		data["reference"] = map[string]any{
			"hasReference":       resolution.HasReference,
			"referenceType":      resolution.ReferenceType,
			"referenceId":        resolution.ReferenceID,
			"source":             resolution.Source,
			"confidence":         resolution.Confidence,
			"needsClarification": resolution.NeedsClarification,
		}
	}
	if planResult != nil && len(planResult.Plan.Steps) > 0 {
		data["plan"] = planResult.Plan
	}
	if r.registry != nil {
		definitions := r.registry.ListTools()
		toolNames := make([]string, 0, len(definitions))
		for _, definition := range definitions {
			if definition.Enabled {
				toolNames = append(toolNames, definition.Name)
			}
		}
		data["toolsExposed"] = toolNames
	}
	return data
}

func (r *Runtime) appendTranscriptMessage(ctx context.Context, state RunState, message providers.Message) *contracts.ErrorShape {
	return r.appendTranscriptMessageForRun(ctx, state.SessionID, state.RunID, state.RequestID, message)
}

func (r *Runtime) appendTranscriptMessageForRun(ctx context.Context, sessionID string, runID string, requestID string, message providers.Message) *contracts.ErrorShape {
	if runAppender, ok := r.sessionStore.(sessions.RunMessageAppender); ok {
		if err := runAppender.AppendMessageForRun(ctx, sessionID, runID, requestID, message); err != nil {
			return internalError("append message: "+err.Error(), contracts.ErrorSourceSession)
		}
		return nil
	}
	if err := r.sessionStore.AppendMessage(ctx, sessionID, message); err != nil {
		return internalError("append message: "+err.Error(), contracts.ErrorSourceSession)
	}
	return nil
}

func (r *Runtime) appendAssistantTranscript(ctx context.Context, sessionID string, content string) *contracts.ErrorShape {
	return r.appendAssistantTranscriptForRun(ctx, sessionID, "", "", content)
}

func (r *Runtime) appendAssistantTranscriptForRun(ctx context.Context, sessionID string, runID string, requestID string, content string) *contracts.ErrorShape {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if err := r.appendTranscriptMessageForRun(ctx, sessionID, runID, requestID, providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: content,
	}); err != nil {
		err.Message = strings.Replace(err.Message, "append message:", "append assistant message:", 1)
		return err
	}
	return nil
}

func (r *Runtime) loadSessionMemory(ctx context.Context, sessionID string) (sessions.SessionMemory, *contracts.ErrorShape) {
	store, ok := r.sessionStore.(sessions.MemoryStore)
	if !ok {
		return sessions.SessionMemory{}, nil
	}
	memory, err := store.LoadMemory(ctx, sessionID)
	if err != nil {
		return sessions.SessionMemory{}, internalError("load session memory: "+err.Error(), contracts.ErrorSourceSession)
	}
	return memory, nil
}

func (r *Runtime) saveSessionMemory(ctx context.Context, sessionID string, memory sessions.SessionMemory) *contracts.ErrorShape {
	store, ok := r.sessionStore.(sessions.MemoryStore)
	if !ok {
		return nil
	}
	memory.UpdatedAt = r.now()
	if err := store.SaveMemory(ctx, sessionID, memory); err != nil {
		return internalError("save session memory: "+err.Error(), contracts.ErrorSourceSession)
	}
	return nil
}

func (r *Runtime) refreshSessionSummary(ctx context.Context, sessionID string, transcript []providers.Message) *contracts.ErrorShape {
	store, ok := r.sessionStore.(sessions.MemoryStore)
	if !ok {
		return nil
	}
	summary := buildExtractiveSessionSummary(transcript, 12, 8)
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	memory, err := store.LoadMemory(ctx, sessionID)
	if err != nil {
		return internalError("load session memory: "+err.Error(), contracts.ErrorSourceSession)
	}
	// LLM summary (written by compactor) takes priority over extractive fallback.
	// maybeCompactAsync runs as a deferred goroutine at the end of each turn, so
	// by the time the next turn calls refreshSessionSummary the compactor may have
	// already written a richer summary. Skip the heuristic to avoid overwriting it.
	if strings.TrimSpace(memory.Summary) != "" {
		return nil
	}
	memory.Summary = summary
	return r.saveSessionMemory(ctx, sessionID, memory)
}

func (r *Runtime) recordActionResult(ctx context.Context, sessionID string, result tools.ToolResult) *contracts.ErrorShape {
	if !result.Success {
		return nil
	}
	content := strings.TrimSpace(result.ContentForLLM)
	if content == "" {
		content = strings.TrimSpace(result.ContentForUser)
	}
	if content == "" {
		return nil
	}
	memory, errShape := r.loadSessionMemory(ctx, sessionID)
	if errShape != nil {
		return errShape
	}
	memory.PendingClarification = nil
	memory.LastActionResults = append(memory.LastActionResults, sessions.ActionResult{
		ToolName:  result.ToolName,
		Content:   truncateToolContentForLLM(content),
		CreatedAt: r.now(),
	})
	if len(memory.LastActionResults) > 10 {
		memory.LastActionResults = memory.LastActionResults[len(memory.LastActionResults)-10:]
	}
	if ref := extractFileRef(result); ref != nil {
		if memory.FileRefs == nil {
			memory.FileRefs = make(map[string]sessions.FileRef)
		}
		memory.FileRefs[ref.name] = sessions.FileRef{
			Source:  ref.source,
			Path:    ref.path,
			DriveID: ref.driveID,
		}
	}
	return r.saveSessionMemory(ctx, sessionID, memory)
}

type resolvedFileRef struct {
	name    string
	source  string
	path    string
	driveID string
}

// extractFileRef pulls a file reference from a tool result's ArtifactRef.
// Returns nil when the result does not represent a single resolvable file.
func extractFileRef(result tools.ToolResult) *resolvedFileRef {
	art := result.ArtifactRef
	if art == nil || strings.TrimSpace(art.Label) == "" {
		return nil
	}
	switch art.Kind {
	case "file":
		// Filesystem tool — local file with absolute host path.
		if strings.TrimSpace(art.URI) == "" {
			return nil
		}
		return &resolvedFileRef{
			name:   art.Label,
			source: "local",
			path:   art.URI,
		}
	case "google.drive.file":
		// Drive tool — file stored on Google Drive.
		if strings.TrimSpace(art.ID) == "" {
			return nil
		}
		return &resolvedFileRef{
			name:    art.Label,
			source:  "drive",
			driveID: art.ID,
		}
	}
	return nil
}

func buildExtractiveSessionSummary(transcript []providers.Message, recentWindow int, maxLines int) string {
	if recentWindow <= 0 {
		recentWindow = 12
	}
	if maxLines <= 0 {
		maxLines = 8
	}
	if len(transcript) <= recentWindow {
		return ""
	}
	older := transcript[:len(transcript)-recentWindow]
	lines := []string{}
	for _, message := range older {
		if len(lines) >= maxLines {
			break
		}
		if !isHistoryMessage(message) {
			continue
		}
		content := strings.Join(strings.Fields(strings.TrimSpace(message.Content)), " ")
		if content == "" {
			continue
		}
		role := "assistant"
		if message.Role == providers.MessageRoleUser {
			role = "user"
		}
		lines = append(lines, role+": "+truncateToolContentForLLM(content))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func recentHistoryForPrompt(transcript []providers.Message, maxMessages int) []string {
	if maxMessages <= 0 {
		maxMessages = 8
	}
	history := make([]string, 0, maxMessages)
	for i := len(transcript) - 1; i >= 0 && len(history) < maxMessages; i-- {
		message := transcript[i]
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		role := ""
		switch message.Role {
		case providers.MessageRoleUser:
			role = "user"
		case providers.MessageRoleAssistant:
			if len(message.ToolCalls) > 0 {
				continue
			}
			role = "assistant"
		default:
			continue
		}
		history = append(history, role+": "+truncateToolContentForLLM(content))
	}
	for left, right := 0, len(history)-1; left < right; left, right = left+1, right-1 {
		history[left], history[right] = history[right], history[left]
	}
	return history
}

func activeClarificationHistoryForPrompt(transcript []providers.Message, maxMessages int) []string {
	thread := activeClarificationTranscript(transcript, maxMessages)
	return providerMessagesToHistory(thread, maxMessages)
}

func activeClarificationTranscript(transcript []providers.Message, maxMessages int) []providers.Message {
	if maxMessages <= 0 {
		maxMessages = 8
	}
	collected := make([]providers.Message, 0, maxMessages)
	for i := len(transcript) - 1; i >= 0 && len(collected) < maxMessages; i-- {
		message := transcript[i]
		if !isHistoryMessage(message) {
			continue
		}
		collected = append(collected, message)
		if message.Role == providers.MessageRoleUser && (isPotentialWriteRequest(message.Content) || isLikelyReadRequest(message.Content)) {
			break
		}
	}
	for left, right := 0, len(collected)-1; left < right; left, right = left+1, right-1 {
		collected[left], collected[right] = collected[right], collected[left]
	}
	return cloneProviderMessages(collected)
}

func providerMessagesToHistory(messages []providers.Message, maxMessages int) []string {
	if maxMessages <= 0 {
		maxMessages = 8
	}
	history := make([]string, 0, maxMessages)
	for _, message := range messages {
		if len(history) >= maxMessages {
			break
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		switch message.Role {
		case providers.MessageRoleUser:
			history = append(history, "user: "+truncateToolContentForLLM(content))
		case providers.MessageRoleAssistant:
			if len(message.ToolCalls) == 0 {
				history = append(history, "assistant: "+truncateToolContentForLLM(content))
			}
		}
	}
	return history
}

func isHistoryMessage(message providers.Message) bool {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return false
	}
	if message.Role == providers.MessageRoleTool || len(message.ToolCalls) > 0 {
		return false
	}
	return message.Role == providers.MessageRoleUser || message.Role == providers.MessageRoleAssistant
}

func providerTranscriptEvidenceText(messages []providers.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role == providers.MessageRoleAssistant && len(message.ToolCalls) > 0 {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n")
}

func hasActiveClarification(transcript []providers.Message) bool {
	for i := len(transcript) - 1; i >= 0; i-- {
		message := transcript[i]
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		if message.Role == providers.MessageRoleTool || len(message.ToolCalls) > 0 {
			continue
		}
		if message.Role != providers.MessageRoleAssistant {
			return false
		}
		lower := strings.ToLower(content)
		return strings.Contains(content, "?") ||
			strings.Contains(lower, "có thể") ||
			strings.Contains(lower, "co the") ||
			strings.Contains(lower, "bổ sung") ||
			strings.Contains(lower, "bo sung") ||
			strings.Contains(lower, "cung cấp") ||
			strings.Contains(lower, "cung cap") ||
			strings.Contains(lower, "nói rõ") ||
			strings.Contains(lower, "noi ro")
	}
	return false
}

func isLikelyClarificationAnswer(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if isPotentialWriteRequest(trimmed) || isLikelyReadRequest(trimmed) {
		return false
	}
	if containsAnyText(lower,
		"không", "khong", "no", "có", "co", "yes", "ok", "okay",
		"không có", "khong co", "không cần", "khong can",
		"thêm", "them", "đổi", "doi", "sửa thành", "sua thanh",
		"tiêu đề", "tieu de", "nội dung", "noi dung",
		"địa điểm", "dia diem", "người tham gia", "nguoi tham gia",
		"thời gian", "thoi gian", "giờ", "gio", "tiếng", "tieng", "phút", "phut",
		"ngày mai", "ngay mai", "hôm nay", "hom nay",
	) {
		return true
	}
	if emailAnswerPattern.MatchString(trimmed) {
		return true
	}
	return hasTimeExpression(trimmed)
}

func shouldIsolateMemoryForNewRequest(text string, activeClarification bool) bool {
	if activeClarification {
		return false
	}
	return isPotentialWriteRequest(text)
}

func shouldIsolateMemoryForStandaloneReadRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" || isPotentialWriteRequest(lower) {
		return false
	}
	hasDomain := containsAnyText(lower,
		"calendar", "lịch", "lich",
		"gmail", "email", "mail",
		"google chat", "chat", "space", "nhóm", "nhom",
	)
	if !hasDomain {
		return false
	}
	hasConcreteScope := containsAnyText(lower,
		"hôm nay", "hom nay", "hôm qua", "hom qua", "ngày mai", "ngay mai",
		"tuần này", "tuan nay", "tuần trước", "tuan truoc", "tuần sau", "tuan sau",
		"tháng này", "thang nay", "tháng trước", "thang truoc", "tháng sau", "thang sau",
		"gần đây", "gan day", "recent", "latest",
	)
	if !hasConcreteScope {
		return false
	}
	return isLikelyReadRequest(lower) ||
		strings.Contains(lower, "?") ||
		(containsAnyText(lower, "có", "co") && containsAnyText(lower, "không", "khong")) ||
		containsAnyText(lower, "gì", "gi", "nào", "nao")
}

func isPotentialWriteRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if containsAnyText(lower, "email", "mail") && containsAnyText(lower, "viết", "viet", "soạn", "soan", "gửi", "gui", "send", "draft") {
		return true
	}
	if containsAnyText(lower, "chat", "nhóm chat", "nhom chat", "google chat", "space") &&
		containsAnyText(lower, "gửi", "gui", "nhắn", "nhan", "thông báo", "thong bao", "send", "reply", "file") {
		return true
	}
	// "gửi cho X" / "nhắn cho X" without explicit "tin nhắn" keyword — still a write request.
	if containsAnyText(lower, "gửi cho", "gui cho", "nhắn cho", "nhan cho", "nhắn tin cho", "nhan tin cho") {
		return true
	}
	return containsAnyText(lower,
		"tạo lịch", "tao lich", "tạo sự kiện", "tao su kien", "đặt lịch", "dat lich",
		"lên lịch", "len lich", "schedule", "create event", "create meeting",
		"gửi email", "gui email", "soạn email", "soan email", "viết email", "viet email", "gửi mail", "gui mail", "viết mail", "viet mail",
		"gửi tin nhắn", "gui tin nhan", "send message", "nhắn tin", "nhan tin", "gửi file", "gui file",
		"gửi vào nhóm", "gui vao nhom", "gửi vào trong nhóm", "gui vao trong nhom",
		"xóa", "xoa", "delete", "remove",
		"cập nhật", "cap nhat", "update", "sửa lịch", "sua lich",
		"chạy lệnh", "chay lenh", "run command", "run python",
		"tạo file", "tao file", "sửa file", "sua file",
	)
}

func isLikelyReadRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return containsAnyText(lower,
		"liệt kê", "liet ke", "xem", "đọc", "doc", "kiểm tra", "kiem tra",
		"tìm", "tim", "search", "list", "show", "read",
	)
}

func messageTextWithAttachmentContext(message contracts.UserMessage) string {
	return textWithAttachmentContext(message.Text, message.Metadata)
}

func textWithAttachmentContext(text string, metadata map[string]any) string {
	text = strings.TrimSpace(text)
	paths := attachmentPathsFromMetadata(metadata)
	if len(paths) == 0 {
		return text
	}
	lines := []string{
		"Current user attachments are available as local files.",
		"If the user says \"file này\", \"ảnh này\", or asks to send/upload the attached file, use these paths in tool inputs that accept attachments.",
		"Attachment paths:",
	}
	for _, path := range paths {
		lines = append(lines, "- "+path)
	}
	context := strings.Join(lines, "\n")
	if text == "" {
		return context
	}
	return text + "\n\n" + context
}

func attachmentPathsFromMetadata(metadata map[string]any) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["attachmentPaths"]
	if !ok {
		return nil
	}
	out := []string{}
	switch value := raw.(type) {
	case []string:
		out = append(out, value...)
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if ok {
				out = append(out, text)
			}
		}
	case string:
		out = append(out, value)
	}
	cleaned := make([]string, 0, len(out))
	for _, path := range out {
		path = strings.TrimSpace(path)
		if path != "" {
			cleaned = append(cleaned, path)
		}
	}
	return cleaned
}

func contextualFollowUpText(recentHistory []string, currentText string) string {
	currentText = strings.TrimSpace(currentText)
	if len(recentHistory) == 0 {
		return currentText
	}
	return strings.TrimSpace(fmt.Sprintf(`The current user message is a direct follow-up answer in the same session.
Use recent_history to combine the original request, the assistant clarification question, and this answer.
Do not treat this as a standalone request.

recent_history:
%s

current_user_answer:
%s`, strings.Join(recentHistory, "\n"), currentText))
}

func contextualResultFollowUpText(recentHistory []string, currentText string) string {
	currentText = strings.TrimSpace(currentText)
	if len(recentHistory) == 0 {
		return currentText
	}
	return strings.TrimSpace(fmt.Sprintf(`The current user message is a follow-up question about the recent tool result or assistant result in the same session.
Use recent_history to answer the question about the already completed action.
Do not treat this as a new write request.
Do not execute another write/destructive tool unless the current user message explicitly asks for a new action.

recent_history:
%s

current_user_question:
%s`, strings.Join(recentHistory, "\n"), currentText))
}

func contextualConversationFollowUpText(recentHistory []string, memory sessions.SessionMemory, currentText string) string {
	currentText = strings.TrimSpace(currentText)
	contextParts := []string{}
	if len(recentHistory) > 0 {
		contextParts = append(contextParts, "recent_history:\n"+strings.Join(recentHistory, "\n"))
	}
	if strings.TrimSpace(memory.Summary) != "" {
		contextParts = append(contextParts, "memory_summary:\n"+strings.TrimSpace(memory.Summary))
	}
	if len(memory.LastActionResults) > 0 {
		result := memory.LastActionResults[len(memory.LastActionResults)-1]
		if strings.TrimSpace(result.Content) != "" {
			contextParts = append(contextParts, "last_action_result:\n"+strings.TrimSpace(result.ToolName+" "+result.Content))
		}
	}
	if len(contextParts) == 0 {
		return currentText
	}
	return strings.TrimSpace(fmt.Sprintf(`The current user message is a contextual follow-up in the same conversation.
Use the conversation context below to infer what the follow-up refers to.
For read-only follow-ups like "hôm qua thì sao" after a Calendar question, answer by using the same domain and changing only the requested time/topic.
For meta questions like "tôi vừa nhắn gì", answer from recent_history.
Do not execute write/destructive tools unless the current user message explicitly asks for a new write/destructive action.

%s

current_user_question:
%s`, strings.Join(contextParts, "\n\n"), currentText))
}

func contextualReferenceText(recentHistory []string, resolution *reference.Resolution, currentText string) string {
	currentText = strings.TrimSpace(currentText)
	if !isUsableReference(resolution) {
		return currentText
	}
	referenceJSON := "{}"
	if resolution.ResolvedContext != nil {
		if data, err := json.MarshalIndent(resolution.ResolvedContext, "", "  "); err == nil {
			referenceJSON = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`The current user message contains a resolved reference to earlier context.
Use the reference_context to understand what the user is referring to.
Do not treat this as permission to execute a write/destructive action.

reference_type: %s
reference_source: %s
reference_context:
%s

recent_history:
%s

current_user_message:
%s`, resolution.ReferenceType, resolution.Source, referenceJSON, strings.Join(recentHistory, "\n"), currentText))
}

func historyWithReferenceResolution(resolution *reference.Resolution, history []string) []string {
	if !isUsableReference(resolution) {
		return history
	}
	context := ""
	if resolution.ResolvedContext != nil {
		if data, err := json.Marshal(resolution.ResolvedContext); err == nil {
			context = string(data)
		}
	}
	line := strings.TrimSpace(fmt.Sprintf("resolved_reference: type=%s source=%s confidence=%.2f context=%s", resolution.ReferenceType, resolution.Source, resolution.Confidence, context))
	if line == "" {
		return history
	}
	enriched := make([]string, 0, len(history)+1)
	enriched = append(enriched, line)
	enriched = append(enriched, history...)
	return enriched
}

func isUsableReference(resolution *reference.Resolution) bool {
	return resolution != nil &&
		resolution.HasReference &&
		!resolution.NeedsClarification &&
		resolution.ReferenceType != reference.TypeNone &&
		resolution.Confidence >= 0.6
}

func isRevisionMessage(message contracts.UserMessage) bool {
	if message.Metadata == nil {
		return false
	}
	_, hasApprovalID := message.Metadata["approvalId"]
	_, hasRevisionComment := message.Metadata["revisionComment"]
	_, hasContinuationOf := message.Metadata["continuationOf"]
	return hasApprovalID || hasRevisionComment || hasContinuationOf
}

func hasReferenceCueText(text string) bool {
	lower := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(text)))
	if lower == "" {
		return false
	}
	if hasDraftReferenceCueText(lower) {
		return true
	}
	return containsAnyText(lower,
		"lich nay", "lich vua roi",
		"su kien nay", "event nay", "cuoc hop tren", "cuoc hop o tren", "cuoc hop vua liet ke", "cuoc hop vua roi", "meeting above", "meeting vua roi",
		"email nay", "mail nay", "email vua roi", "mail vua roi",
		"ban nhap nay", "ban nhap do", "ban nhap vua roi", "ban nhap vua tao", "draft nay", "draft vua roi", "draft vua tao",
		"chat nay", "space nay", "nhom chat nay",
		"tin nhan nay", "message nay", "tin nhan vua roi",
		"noi dung minh vua noi", "noi dung vua noi",
		"chu de do", "chu de nay",
		"note lai", "ghi chu lai", "tom tat",
		"vua tao",
	)
}

func hasDraftReferenceCueText(lower string) bool {
	if lower == "" || !containsAnyText(lower, "draft", "ban nhap") {
		return false
	}
	return containsAnyText(lower,
		"nay", "do", "vua roi", "vua tao", "da tao", "ban tao", "ban da tao", "ban vua tao",
		"gui", "send", "email", "mail",
	)
}

func isResultReferenceFollowUp(resolution *reference.Resolution, text string) bool {
	if !isUsableReference(resolution) {
		return false
	}
	if isPotentialWriteRequest(text) && !containsAnyText(strings.ToLower(strings.TrimSpace(text)), "có", "co", "không", "khong", "?") {
		return false
	}
	switch resolution.ReferenceType {
	case reference.TypeCalendarEvent, reference.TypeGmailEmail, reference.TypeChatSpace, reference.TypeChatMessage, reference.TypeConversationTopic:
		return true
	default:
		return false
	}
}

func isLikelyResultFollowUpQuestion(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	hasReference := containsAnyText(lower,
		"lịch này", "lich nay", "sự kiện này", "su kien nay", "event này", "event nay",
		"cái này", "cai nay", "này có", "nay co", "vừa tạo", "vua tao",
		"có gửi mail", "co gui mail", "có gửi email", "co gui email",
		"mail thông báo", "mail thong bao", "email thông báo", "email thong bao",
		"người tham gia", "nguoi tham gia", "attendee", "attendees",
		"nó có", "no co",
	)
	if !hasReference {
		return false
	}
	if isPotentialWriteRequest(lower) && !containsAnyText(lower, "có", "co", "không", "khong", "?") {
		return false
	}
	return true
}

// hasClearVietnameseContextCue returns true when the text contains explicit
// back-reference signals that indicate the user is referring to something from
// prior conversation — even when the message also contains write-action keywords.
// Used as an early check before isPotentialWriteRequest to avoid misclassifying
// context-referencing write requests as fresh/isolated.
func hasClearVietnameseContextCue(lower string) bool {
	return containsAnyText(lower,
		// Time-based back-references
		"nãy giờ", "nay gio",
		"hồi nãy", "hoi nay",
		"lúc nãy", "luc nay",
		"vừa nói", "vua noi",
		"vừa trao đổi", "vua trao doi",
		"đã trao đổi", "da trao doi",
		"những gì đã", "nhung gi da",
		"nội dung vừa", "noi dung vua",
		// Demonstrative pronoun back-references
		"người đó", "nguoi do",
		"người kia", "nguoi kia",
		"cái đó", "cai do",
		"cái kia", "cai kia",
		"cái hồi nãy", "cai hoi nay",
		"vụ đó", "vu do",
		"vụ kia", "vu kia",
		"mail kia", "email kia",
		"tin nhắn đó", "tin nhan do",
		// Continuation cues
		"làm tiếp", "lam tiep",
		"ý tôi là", "y toi la",
		"ý mình là", "y minh la",
		"tiếp tục từ", "tiep tuc tu",
		"cuộc trò chuyện", "cuoc tro chuyen",
	)
}

func isLikelyContextualFollowUpQuestion(text string, history []string, memory sessions.SessionMemory) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	hasContext := len(history) > 0 ||
		strings.TrimSpace(memory.Summary) != "" ||
		len(memory.LastActionResults) > 0
	// Clear Vietnamese back-reference signals override the write-request guard:
	// a message like "tổng hợp những gì đã trao đổi nãy giờ vào file Docs" is
	// contextual even though it contains write-action keywords.
	if hasClearVietnameseContextCue(lower) {
		return hasContext
	}
	if isPotentialWriteRequest(lower) {
		return false
	}
	if !hasContext {
		return false
	}
	if containsAnyText(lower,
		"tôi vừa nhắn", "toi vua nhan",
		"tôi vừa hỏi", "toi vua hoi",
		"tôi vừa nói", "toi vua noi",
		"mình vừa nhắn", "minh vua nhan",
		"mình vừa hỏi", "minh vua hoi",
		"mình vừa nói", "minh vua noi",
		"tin nhắn trước", "tin nhan truoc",
		"câu trước", "cau truoc",
		"vừa nhắn gì", "vua nhan gi",
		"vừa hỏi gì", "vua hoi gi",
	) {
		return true
	}
	if containsAnyText(lower, "thì sao", "thi sao", "còn", "con") &&
		containsAnyText(lower,
			"hôm qua", "hom qua", "hôm nay", "hom nay", "ngày mai", "ngay mai",
			"tuần này", "tuan nay", "tuần trước", "tuan truoc", "tuần sau", "tuan sau",
			"tháng này", "thang nay", "tháng trước", "thang truoc", "tháng sau", "thang sau",
			"calendar", "lịch", "lich", "email", "mail", "chat",
		) {
		return true
	}
	if strings.HasSuffix(lower, "?") &&
		containsAnyText(lower,
			"hôm qua", "hom qua", "hôm nay", "hom nay", "ngày mai", "ngay mai",
			"tuần này", "tuan nay", "tuần trước", "tuan truoc", "tuần sau", "tuan sau",
			"tháng này", "thang nay", "tháng trước", "thang truoc", "tháng sau", "thang sau",
		) {
		return true
	}
	return false
}

func hasRecentActionResult(transcript []providers.Message) bool {
	for i := len(transcript) - 1; i >= 0; i-- {
		message := transcript[i]
		content := strings.ToLower(strings.TrimSpace(message.Content))
		if content == "" {
			continue
		}
		if message.Role != providers.MessageRoleAssistant {
			continue
		}
		if containsAnyText(content,
			"event created", "event updated", "event deleted",
			"đã thực hiện", "da thuc hien",
			"đã tạo", "da tao",
			"created", "updated", "deleted",
		) {
			return true
		}
	}
	return false
}

func hasRecentMemoryActionResult(memory sessions.SessionMemory) bool {
	for i := len(memory.LastActionResults) - 1; i >= 0; i-- {
		content := strings.ToLower(strings.TrimSpace(memory.LastActionResults[i].Content))
		if content == "" {
			continue
		}
		return true
	}
	return false
}
