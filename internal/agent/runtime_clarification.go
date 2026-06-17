package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

func (r *Runtime) toolCallClarificationResponse(message contracts.UserMessage, toolCall providers.ToolCall, definition tools.ToolDefinition, found bool, activeClarification bool, evidenceText string) *contracts.AgentResponse {
	if !found {
		return nil
	}
	missing := missingRequiredArguments(definition.Parameters, toolCall.Arguments)
	if len(missing) > 0 {
		return &contracts.AgentResponse{
			RequestID: message.RequestID,
			SessionID: message.SessionID,
			Status:    contracts.AgentStatusNeedClarification,
			Message:   missingToolArgumentQuestion(toolCall.Name, missing),
			Data:      r.traceData(nil),
		}
	}
	if malformed := malformedToolArguments(toolCall); len(malformed) > 0 {
		return &contracts.AgentResponse{
			RequestID: message.RequestID,
			SessionID: message.SessionID,
			Status:    contracts.AgentStatusNeedClarification,
			Message:   missingToolArgumentQuestion(toolCall.Name, malformed),
			Data:      r.traceData(nil),
		}
	}
	if activeClarification {
		return nil
	}
	if needs := missingCurrentRequestEvidence(evidenceText, toolCall); len(needs) > 0 {
		return &contracts.AgentResponse{
			RequestID: message.RequestID,
			SessionID: message.SessionID,
			Status:    contracts.AgentStatusNeedClarification,
			Message:   missingToolArgumentQuestion(toolCall.Name, needs),
			Data:      r.traceData(nil),
		}
	}
	return nil
}

func shouldResolveChatSpaceBeforeClarification(toolCall providers.ToolCall) bool {
	if len(malformedToolArguments(toolCall)) == 0 {
		return false
	}
	switch toolCall.Name {
	case "chat.sendMessage", "chat.listMessages", "chat.listMembers", "chat.addMember":
		value, ok := toolCall.Arguments["space"]
		return ok && !isEmptyArgument(value)
	default:
		return false
	}
}

func hasDriveMoveResolutionIntent(requestText string) bool {
	lower := strings.ToLower(strings.TrimSpace(requestText))
	if lower == "" {
		return false
	}
	return containsAnyText(lower,
		"di chuyển", "di chuyen",
		"chuyển", "chuyen",
		"move",
		"vào folder", "vao folder",
		"vào thư mục", "vao thu muc",
		"sang folder", "sang thư mục", "sang thu muc",
	)
}

func shouldResolveDriveMoveBeforeClarification(toolCall providers.ToolCall, requestText string, missing []string) bool {
	if !isDriveMoveTool(toolCall.Name) {
		return false
	}
	if !containsString(missing, "fileId") && !containsString(missing, "fileIds") && !containsString(missing, "targetParentId") {
		return false
	}
	return hasDriveMoveResolutionIntent(requestText)
}

// shouldRedirectClarifyToDriveMove returns true when the LLM called clarify on a request
// that has clear drive-move intent and the user already supplied file/folder names as text.
// In that case the clarification should be intercepted and replaced with a drive.listFiles
// resolution loop instead of surfacing a confirmation question to the user.
//
// evidenceText is the accumulated transcript text for the current turn. If it already
// contains the NEEDS_DRIVE_MOVE_RESOLUTION marker the redirect was already injected once,
// so we allow subsequent clarify calls to surface normally and avoid an infinite loop.
func shouldRedirectClarifyToDriveMove(requestText, evidenceText string) bool {
	if strings.Contains(evidenceText, "NEEDS_DRIVE_MOVE_RESOLUTION") {
		return false
	}
	return hasDriveMoveResolutionIntent(requestText)
}

func driveMoveResolutionObservation(missing []string) string {
	return fmt.Sprintf(`NEEDS_DRIVE_MOVE_RESOLUTION: The current request is a Google Drive move request, but %s is not resolved to a Drive ID yet.
Do not ask the user for fileId, fileIds, or targetParentId when they gave file/folder names.
First call safe read tools to resolve names:
- Call drive.listFiles to find each source file by its title/name. If the user says "docs" or "Google Docs", prefer the Google Docs MIME type.
- Call drive.listFiles to find the destination folder by its title/name, with folder MIME type application/vnd.google-apps.folder.
After resolving exactly one source file and one destination folder, retry drive.moveFile with fileId and targetParentId.
After resolving multiple source files and one destination folder, call drive.moveFiles with fileIds and targetParentId.
If read-tool resolution returns no match or multiple plausible matches, then ask one concise clarification question.`, strings.Join(missing, ", "))
}

func isDriveMoveTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "drive.moveFile", "drive.moveFiles":
		return true
	default:
		return false
	}
}

func chatSpaceResolutionObservation(toolCall providers.ToolCall) string {
	target := strings.TrimSpace(fmt.Sprint(toolCall.Arguments["space"]))
	if target == "" {
		target = "(empty)"
	}
	return fmt.Sprintf(`NEEDS_SPACE_RESOLUTION: The space argument %q is a display name, group name, person name, or other natural-language target, not a Google Chat resource name.
Do not ask the user for spaces/AAAA yet.
First call safe read tools to resolve it:
- If it looks like a group or space name, call chat.listSpaces and match the requested name against display names and space metadata.
- If it looks like a person name or email, call people.searchDirectory and then chat.findSpacesByMembers.
After resolving exactly one target, retry %s with the matched spaces/... resource name.
If read-tool resolution returns no match or multiple plausible matches, then ask one concise clarification question.`, target, toolCall.Name)
}

func pendingMissingFieldsForToolCall(toolCall providers.ToolCall, definition tools.ToolDefinition, found bool, activeClarification bool, evidenceText string) []string {
	if !found {
		return nil
	}
	if missing := missingRequiredArguments(definition.Parameters, toolCall.Arguments); len(missing) > 0 {
		return missing
	}
	if malformed := malformedToolArguments(toolCall); len(malformed) > 0 {
		return malformed
	}
	if activeClarification {
		return nil
	}
	return missingCurrentRequestEvidence(evidenceText, toolCall)
}

func sanitizeUnsupportedOptionalArguments(toolCall providers.ToolCall, evidenceText string) providers.ToolCall {
	if toolCall.Name != "calendar.createEvent" {
		return toolCall
	}
	attendees, ok := toolCall.Arguments["attendees"]
	if !ok || isEmptyArgument(attendees) {
		return toolCall
	}
	if hasAttendeeEvidence(evidenceText, attendees) {
		return toolCall
	}
	args := cloneArguments(toolCall.Arguments)
	delete(args, "attendees")
	toolCall.Arguments = args
	return toolCall
}

func shouldRerouteDriveMetadataMove(toolCall providers.ToolCall, requestText string) bool {
	if strings.TrimSpace(toolCall.Name) != "drive.updateFileMetadata" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(requestText))
	if lower == "" {
		return false
	}
	return containsAnyText(lower,
		"di chuyển", "di chuyen",
		"chuyển", "chuyen",
		"move",
		"vào folder", "vao folder",
		"vào thư mục", "vao thu muc",
		"sang folder", "sang thư mục", "sang thu muc",
	)
}

func hasAttendeeEvidence(evidenceText string, attendees any) bool {
	lower := strings.ToLower(evidenceText)
	for _, email := range attendeeStrings(attendees) {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		if strings.Contains(lower, email) {
			return true
		}
		local := strings.Split(email, "@")[0]
		if local != "" && strings.Contains(lower, local) {
			return true
		}
	}
	return false
}

func attendeeStrings(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func missingRequiredArguments(schema tools.ToolSchema, args map[string]any) []string {
	required := requiredFieldsFromToolSchema(schema)
	missing := make([]string, 0, len(required))
	for _, field := range required {
		if isEmptyArgument(args[field]) {
			missing = append(missing, field)
		}
	}
	return missing
}

func malformedToolArguments(toolCall providers.ToolCall) []string {
	switch toolCall.Name {
	case "chat.sendMessage", "chat.listMessages", "chat.listMembers", "chat.addMember":
		if value, ok := toolCall.Arguments["space"]; ok && !isEmptyArgument(value) && !containsSpaceResourceName(value) {
			return []string{"space"}
		}
	default:
		return nil
	}
	return nil
}

func containsSpaceResourceName(value any) bool {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return false
	}
	start := strings.Index(text, "spaces/")
	if start < 0 {
		return false
	}
	resource := text[start+len("spaces/"):]
	end := len(resource)
	for index, r := range resource {
		if r == '|' || r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			end = index
			break
		}
	}
	return strings.TrimSpace(resource[:end]) != ""
}

func requiredFieldsFromToolSchema(schema tools.ToolSchema) []string {
	value, ok := schema["required"]
	if !ok {
		return nil
	}
	switch fields := value.(type) {
	case []string:
		return append([]string(nil), fields...)
	case []any:
		required := make([]string, 0, len(fields))
		for _, field := range fields {
			name := strings.TrimSpace(fmt.Sprint(field))
			if name != "" {
				required = append(required, name)
			}
		}
		return required
	default:
		return nil
	}
}

func isEmptyArgument(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	default:
		return false
	}
}

func missingCurrentRequestEvidence(userText string, toolCall providers.ToolCall) []string {
	switch toolCall.Name {
	case "calendar.createEvent":
		return missingCalendarCreateEventEvidence(userText, toolCall.Arguments)
	default:
		return nil
	}
}

func missingCalendarCreateEventEvidence(userText string, args map[string]any) []string {
	lower := strings.ToLower(strings.TrimSpace(userText))
	missing := []string{}
	title := stringArgument(args, "title")
	if !hasCalendarTitleEvidence(lower, title) {
		missing = append(missing, "title")
	}
	if !hasCalendarStartEvidence(lower) {
		missing = append(missing, "start")
	}
	if !hasCalendarEndEvidence(lower) {
		missing = append(missing, "end")
	}
	return missing
}

func hasCalendarTitleEvidence(lowerText string, title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	if title != "" && strings.Contains(lowerText, title) {
		return true
	}
	return containsAnyText(lowerText,
		"tiêu đề", "tieu de", "chủ đề", "chu de", "nội dung", "noi dung",
		"về ", "ve ", "họp về", "hop ve", "meeting about",
	)
}

func hasCalendarStartEvidence(lowerText string) bool {
	return hasTimeExpression(lowerText) ||
		containsAnyText(lowerText,
			"hôm nay", "hom nay", "ngày mai", "ngay mai",
			"tuần này", "tuan nay", "tuần sau", "tuan sau",
			"tháng này", "thang nay", "tháng tới", "thang toi", "tháng sau", "thang sau",
			"today", "tomorrow", "this week", "next week", "this month", "next month",
		)
}

func hasCalendarEndEvidence(lowerText string) bool {
	if containsAnyText(lowerText,
		"đến", "den", "tới", "toi", "kết thúc", "ket thuc",
		"thời lượng", "thoi luong", "trong vòng", "trong vong",
		"tiếng", "tieng", "giờ", "gio", "phút", "phut",
		"hour", "hours", "minute", "minutes",
	) {
		return true
	}
	return countTimeExpressions(lowerText) >= 2 ||
		(strings.Contains(lowerText, "-") && hasTimeExpression(lowerText))
}

func hasTimeExpression(text string) bool {
	return timeAnswerPattern.MatchString(text) || viTimeAnswerPattern.MatchString(text)
}

func countTimeExpressions(text string) int {
	return len(timeAnswerPattern.FindAllString(text, -1)) + len(viTimeAnswerPattern.FindAllString(text, -1))
}

func stringArgument(args map[string]any, key string) string {
	value, ok := args[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func missingToolArgumentQuestion(toolName string, missing []string) string {
	if strings.HasPrefix(toolName, "chat.") && containsString(missing, "space") {
		return "Bạn muốn thao tác với Google Chat space nào? Hãy gửi resource name dạng spaces/AAAA, hoặc nói rõ tên nhóm/người trong chat để tôi tìm space trước."
	}
	if toolName == "calendar.createEvent" {
		if containsString(missing, "title") && containsString(missing, "start") {
			return "Bạn muốn tạo lịch với tiêu đề gì, vào ngày giờ nào, và kết thúc lúc mấy giờ?"
		}
		if containsString(missing, "start") {
			return "Bạn muốn tạo lịch vào ngày và giờ nào?"
		}
		if containsString(missing, "end") {
			return "Bạn có thể cung cấp giờ kết thúc hoặc thời lượng của cuộc họp không?"
		}
		if containsString(missing, "title") {
			return "Bạn muốn đặt tiêu đề cuộc họp là gì?"
		}
	}
	return "Bạn có thể bổ sung thông tin còn thiếu cho " + toolName + ": " + strings.Join(missing, ", ") + "?"
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (r *Runtime) resolvePendingClarification(ctx context.Context, pending sessions.PendingClarification, userAnswer string, recentHistory []string) pendingClarificationResolution {
	fallback := fallbackPendingClarificationResolution(pending, userAnswer)
	if r == nil || r.provider == nil {
		return fallback
	}
	req := &providers.GenerateRequest{
		SystemPrompt:   pendingClarificationResolverSystemPrompt(),
		UserPrompt:     pendingClarificationResolverUserPrompt(pending, userAnswer, recentHistory),
		Temperature:    0,
		MaxTokens:      1024,
		ResponseFormat: "json",
		Model:          r.model,
	}
	resp, err := r.provider.Generate(ctx, req)
	if err != nil {
		r.logger.Warn("pending clarification resolver failed; using heuristic fallback", "error", err)
		return fallback
	}
	var resolved pendingClarificationResolution
	if err := json.Unmarshal([]byte(extractJSONObject(resp.Text)), &resolved); err != nil {
		r.logger.Warn("pending clarification resolver returned invalid JSON; using heuristic fallback", "error", err, "response_preview", logPreview(resp.Text, 200))
		return fallback
	}
	resolved.Reason = strings.TrimSpace(resolved.Reason)
	if resolved.IsAnswer {
		resolved.UpdatedRequest = contextualPendingClarificationText(pending, userAnswer)
	}
	if !resolved.IsAnswer && fallback.IsAnswer {
		return fallback
	}
	return resolved
}

func pendingClarificationResolverSystemPrompt() string {
	return strings.TrimSpace(`<pending_clarification_resolver>
  <mission>Decide whether the latest user message answers an active clarification question in the same session.</mission>
  <rules>
    <rule>Return JSON only.</rule>
    <rule>If the user answer fills or modifies the missing fields for the pending request, set is_answer=true.</rule>
    <rule>If it is a clearly new task unrelated to the pending request, set is_new_request=true and is_answer=false.</rule>
    <rule>Do not execute tools and do not grant approval.</rule>
    <rule>For write/destructive actions, this resolver only merges context; HITL approval is still required later.</rule>
    <rule>updated_request should be a complete natural-language request that combines the original request and the answer.</rule>
  </rules>
  <response_schema>
    {
      "is_answer": true,
      "is_new_request": false,
      "updated_request": "string",
      "provided_fields": ["string"],
      "still_missing": ["string"],
      "reason": "short Vietnamese explanation"
    }
  </response_schema>
</pending_clarification_resolver>`)
}

func pendingClarificationResolverUserPrompt(pending sessions.PendingClarification, userAnswer string, recentHistory []string) string {
	partialInput := "{}"
	if len(pending.PartialInput) > 0 {
		if data, err := json.Marshal(pending.PartialInput); err == nil {
			partialInput = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`<pending_clarification_request>
  <original_request>%s</original_request>
  <assistant_question>%s</assistant_question>
  <target_tool>%s</target_tool>
  <missing_fields>%s</missing_fields>
  <partial_input>%s</partial_input>
  <recent_history>%s</recent_history>
  <current_user_message>%s</current_user_message>
</pending_clarification_request>`,
		xmlEscape(pending.OriginalRequest),
		xmlEscape(pending.Question),
		xmlEscape(pending.ToolName),
		xmlEscape(strings.Join(pending.MissingFields, ", ")),
		xmlEscape(partialInput),
		xmlEscape(strings.Join(recentHistory, "\n")),
		xmlEscape(userAnswer),
	))
}

func fallbackPendingClarificationResolution(pending sessions.PendingClarification, userAnswer string) pendingClarificationResolution {
	trimmed := strings.TrimSpace(userAnswer)
	if trimmed == "" {
		return pendingClarificationResolution{}
	}
	if isLikelyClarificationAnswer(trimmed) {
		return pendingClarificationResolution{
			IsAnswer:       true,
			UpdatedRequest: contextualPendingClarificationText(pending, trimmed),
			Reason:         "Heuristic matched a direct clarification answer.",
		}
	}
	if isPotentialWriteRequest(trimmed) || isLikelyReadRequest(trimmed) {
		return pendingClarificationResolution{
			IsNewRequest: true,
			Reason:       "Heuristic matched a new request.",
		}
	}
	return pendingClarificationResolution{}
}

func contextualPendingClarificationText(pending sessions.PendingClarification, userAnswer string) string {
	partialInput := "{}"
	if len(pending.PartialInput) > 0 {
		if data, err := json.Marshal(pending.PartialInput); err == nil {
			partialInput = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`The current user message answers a pending clarification in the same session.
Use the original request, assistant question, already-provided partial input, and current answer to continue the original task.
Do not treat this as a standalone request.
Do not execute write/destructive tools without the normal approval boundary.

original_request:
%s

assistant_question:
%s

target_tool:
%s

already_provided_input:
%s

missing_fields:
%s

current_user_answer:
%s`, pending.OriginalRequest, pending.Question, pending.ToolName, partialInput, strings.Join(pending.MissingFields, ", "), strings.TrimSpace(userAnswer)))
}

func historyWithPendingClarification(pending sessions.PendingClarification, history []string) []string {
	enriched := make([]string, 0, len(history)+2)
	if strings.TrimSpace(pending.OriginalRequest) != "" {
		enriched = append(enriched, "pending_original_request: "+truncateToolContentForLLM(pending.OriginalRequest))
	}
	if strings.TrimSpace(pending.Question) != "" {
		enriched = append(enriched, "pending_assistant_question: "+truncateToolContentForLLM(pending.Question))
	}
	enriched = append(enriched, history...)
	return enriched
}

func pendingClarificationTranscript(pending sessions.PendingClarification) []providers.Message {
	messages := []providers.Message{}
	if strings.TrimSpace(pending.OriginalRequest) != "" {
		messages = append(messages, providers.Message{Role: providers.MessageRoleUser, Content: pending.OriginalRequest})
	}
	if strings.TrimSpace(pending.Question) != "" {
		messages = append(messages, providers.Message{Role: providers.MessageRoleAssistant, Content: pending.Question})
	}
	return messages
}

// isUsablePendingClarification returns true when pending is non-nil, has content,
// and has not exceeded approvalTTL. Stale clarifications are treated
// as expired so the next user message starts fresh instead of being misread as an
// answer to a long-forgotten question.
// Backward compat: a zero CreatedAt (old memory.json files) skips the TTL check.
func isUsablePendingClarification(pending *sessions.PendingClarification, now time.Time) bool {
	if pending == nil {
		return false
	}
	hasContent := strings.TrimSpace(pending.OriginalRequest) != "" ||
		strings.TrimSpace(pending.Question) != ""
	if !hasContent {
		return false
	}
	if !pending.CreatedAt.IsZero() && now.Sub(pending.CreatedAt) >= approvalTTL {
		return false
	}
	return true
}

func clonePendingClarification(pending *sessions.PendingClarification) *sessions.PendingClarification {
	if pending == nil {
		return nil
	}
	cloned := *pending
	if len(pending.MissingFields) > 0 {
		cloned.MissingFields = append([]string(nil), pending.MissingFields...)
	}
	if len(pending.PartialInput) > 0 {
		cloned.PartialInput = make(map[string]any, len(pending.PartialInput))
		for key, value := range pending.PartialInput {
			cloned.PartialInput[key] = value
		}
	}
	return &cloned
}

func (r *Runtime) storePendingClarification(ctx context.Context, sessionID string, pending *sessions.PendingClarification) *contracts.ErrorShape {
	if pending == nil {
		return nil
	}
	if strings.TrimSpace(pending.OriginalRequest) == "" && strings.TrimSpace(pending.Question) == "" {
		return nil
	}
	memory, errShape := r.loadSessionMemory(ctx, sessionID)
	if errShape != nil {
		return errShape
	}
	if pending.CreatedAt.IsZero() {
		pending.CreatedAt = r.now()
	}
	memory.PendingClarification = clonePendingClarification(pending)
	return r.saveSessionMemory(ctx, sessionID, memory)
}

func pendingClarificationFromToolCall(runID string, originalRequest string, question string, toolCall providers.ToolCall, missing []string) *sessions.PendingClarification {
	return &sessions.PendingClarification{
		RunID:           strings.TrimSpace(runID),
		OriginalRequest: strings.TrimSpace(originalRequest),
		Question:        strings.TrimSpace(question),
		ToolName:        strings.TrimSpace(toolCall.Name),
		MissingFields:   append([]string(nil), missing...),
		PartialInput:    cloneAnyMap(toolCall.Arguments),
	}
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
