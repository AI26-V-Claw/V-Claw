package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

func (r *Runtime) appendToolObservation(ctx context.Context, sessionID string, _ []providers.Message, message providers.Message) *contracts.ErrorShape {
	if strings.TrimSpace(message.ToolCallID) == "" {
		return internalError("append tool message: missing tool call id", contracts.ErrorSourceSession)
	}
	if err := r.sessionStore.AppendMessage(ctx, sessionID, message); err != nil {
		return internalError("append tool message: "+err.Error(), contracts.ErrorSourceSession)
	}
	return nil
}

func (r *Runtime) appendSkippedToolObservations(ctx context.Context, sessionID string, toolCalls []providers.ToolCall, content string) *contracts.ErrorShape {
	for _, message := range skippedToolObservationMessages(toolCalls, content) {
		if err := r.appendToolObservation(ctx, sessionID, nil, message); err != nil {
			return err
		}
	}
	return nil
}

func skippedToolObservationMessages(toolCalls []providers.ToolCall, content string) []providers.Message {
	if len(toolCalls) == 0 {
		return nil
	}
	messages := make([]providers.Message, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		messages = append(messages, providers.Message{
			Role:       providers.MessageRoleTool,
			ToolCallID: toolCall.ID,
			Content:    truncateToolContentForLLM(content),
		})
	}
	return messages
}

func validateUserMessage(message contracts.UserMessage) *contracts.ErrorShape {
	switch {
	case strings.TrimSpace(message.RequestID) == "":
		return missingField("requestId")
	case strings.TrimSpace(message.SessionID) == "":
		return missingField("sessionId")
	case strings.TrimSpace(message.Channel) == "":
		return missingField("channel")
	case strings.TrimSpace(message.Text) == "":
		return missingField("text")
	case message.Timestamp.IsZero():
		return missingField("timestamp")
	default:
		return nil
	}
}

func missingField(field string) *contracts.ErrorShape {
	return &contracts.ErrorShape{
		Code:      contracts.ErrorMissingRequiredField,
		Message:   "missing required field: " + field,
		Source:    contracts.ErrorSourceAgent,
		Retryable: false,
	}
}

func (r *Runtime) executeAllowedTool(ctx context.Context, toolCall providers.ToolCall, definition tools.ToolDefinition) tools.ToolResult {
	tool, ok := r.registry.GetTool(toolCall.Name)
	if !ok {
		return tools.ToolNotFoundResult(providerToolCallToToolCall(toolCall))
	}
	r.logger.Info("tool execution started",
		"tool_call_id", toolCall.ID,
		"tool_name", toolCall.Name,
		"arguments", logToolArguments(toolCall.Name, toolCall.Arguments),
	)
	emitProgress(ctx, ProgressEvent{
		Stage:      ProgressStageToolStarted,
		ToolName:   toolCall.Name,
		ToolCallID: toolCall.ID,
		Message:    "Tool started",
	})

	timeout := definition.Timeout
	if timeout <= 0 {
		timeout = r.toolTimeout
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultCh := make(chan tools.ToolResult, 1)
	go func() {
		resultCh <- executeToolSafely(toolCtx, tool, providerToolCallToToolCall(toolCall))
	}()

	select {
	case result := <-resultCh:
		stage := ProgressStageToolCompleted
		if !result.Success {
			stage = ProgressStageToolFailed
		}
		r.logger.Info("tool execution completed",
			"tool_call_id", toolCall.ID,
			"tool_name", toolCall.Name,
			"success", result.Success,
			"error_code", toolErrorCode(result),
			"content_preview", logPreview(result.ContentForLLM, 260),
		)
		emitProgress(ctx, ProgressEvent{
			Stage:      stage,
			ToolName:   toolCall.Name,
			ToolCallID: toolCall.ID,
			Message:    "Tool finished",
		})
		return result
	case <-toolCtx.Done():
		emitProgress(ctx, ProgressEvent{
			Stage:      ProgressStageToolFailed,
			ToolName:   toolCall.Name,
			ToolCallID: toolCall.ID,
			Message:    toolCtx.Err().Error(),
		})
		return tools.ToolResult{
			ToolCallID:     toolCall.ID,
			ToolName:       toolCall.Name,
			Success:        false,
			ContentForLLM:  "Tool execution error for " + toolCall.Name + ": " + toolCtx.Err().Error(),
			ContentForUser: "Tool lỗi khi chạy: " + toolCall.Name,
			Error: &tools.ToolError{
				Code:    tools.ErrorTimeout,
				Message: toolCtx.Err().Error(),
			},
		}
	}
}

func logToolArguments(toolName string, args map[string]any) any {
	if args == nil {
		return map[string]any{}
	}
	if toolName == "calendar.listEvents" {
		return map[string]any{
			"timeMin": stringLogArg(args, "timeMin"),
			"timeMax": stringLogArg(args, "timeMax"),
			"query":   stringLogArg(args, "query"),
		}
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	return map[string]any{"keys": keys}
}

func stringLogArg(args map[string]any, key string) string {
	value, ok := args[key].(string)
	if !ok {
		return ""
	}
	return value
}

func toolErrorCode(result tools.ToolResult) string {
	if result.Error == nil {
		return ""
	}
	return result.Error.Code
}

func logPreview(text string, limit int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "..."
	}
	return trimmed
}

func normalizeProviderToolCall(now time.Time, toolCall providers.ToolCall, userText string) providers.ToolCall {
	var normalizedArgs map[string]any
	switch toolCall.Name {
	case "calendar.listEvents":
		normalizedArgs = normalizeCalendarListEventsArgs(now, toolCall.Arguments, userText)
	case "gmail.listEmails", "gmail.listThreads":
		normalizedArgs = normalizeGmailListArgs(now, toolCall.Arguments, userText)
	default:
		return toolCall
	}
	if normalizedArgs == nil {
		return toolCall
	}
	toolCall.Arguments = normalizedArgs
	return toolCall
}

func normalizeCalendarListEventsArgs(now time.Time, args map[string]any, userText string) map[string]any {
	start, end, ok := providerRelativeDateRange(now, userText)
	if !ok {
		return nil
	}

	normalized := cloneArguments(args)
	if normalized == nil {
		normalized = map[string]any{}
	}
	normalized["timeMin"] = start.Format(time.RFC3339)
	normalized["timeMax"] = end.Format(time.RFC3339)
	if query, ok := normalized["query"].(string); ok {
		normalized["query"] = normalizeRelativeProviderQuery(query, userText, calendarQueryPurposeTerms())
	}
	return normalized
}

func normalizeGmailListArgs(now time.Time, args map[string]any, userText string) map[string]any {
	sentQuery, sentLabelIDs, hasSentRecipient := sentMailSearchQuery(userText)
	disjointDateQuery, hasDisjointDateQuery := gmailDisjointDateQuery(now, userText)
	start, end, ok := providerRelativeDateRange(now, userText)
	if !ok && !hasSentRecipient && !hasDisjointDateQuery {
		return nil
	}

	normalized := cloneArguments(args)
	if normalized == nil {
		normalized = map[string]any{}
	}
	baseQuery := ""
	if query, ok := normalized["query"].(string); ok {
		baseQuery = normalizeRelativeProviderQuery(query, userText, gmailQueryPurposeTerms())
	}
	if hasSentRecipient {
		baseQuery = sentQuery
		normalized["labelIds"] = sentLabelIDs
	}
	if hasDisjointDateQuery {
		normalized["query"] = combineGmailQueries(baseQuery, disjointDateQuery)
		delete(normalized, "after")
		delete(normalized, "before")
		return normalized
	}
	if ok {
		normalized["after"] = start.Format("2006-01-02")
		normalized["before"] = end.Format("2006-01-02")
	}
	if hasSentRecipient {
		normalized["query"] = baseQuery
		return normalized
	}
	if _, ok := normalized["query"].(string); ok {
		normalized["query"] = baseQuery
	}
	return normalized
}

func gmailDisjointDateQuery(now time.Time, userText string) (string, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	text := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(userText)))
	if text == "" {
		return "", false
	}
	hasToday := containsAnyText(text, "hom nay", "today")
	hasDayBeforeYesterday := containsAnyText(text, "hom kia", "day before yesterday", "two days ago")
	if !hasToday || !hasDayBeforeYesterday {
		return "", false
	}

	today := startOfDay(now)
	dayBeforeYesterday := today.AddDate(0, 0, -2)
	return fmt.Sprintf("((after:%s before:%s) OR (after:%s before:%s))",
		today.Format("2006/01/02"),
		today.AddDate(0, 0, 1).Format("2006/01/02"),
		dayBeforeYesterday.Format("2006/01/02"),
		dayBeforeYesterday.AddDate(0, 0, 1).Format("2006/01/02"),
	), true
}

func combineGmailQueries(base string, dateQuery string) string {
	base = strings.TrimSpace(base)
	dateQuery = strings.TrimSpace(dateQuery)
	if base == "" {
		return dateQuery
	}
	if dateQuery == "" {
		return base
	}
	return base + " " + dateQuery
}

func sentMailSearchQuery(userText string) (string, []string, bool) {
	trimmed := strings.TrimSpace(userText)
	if trimmed == "" {
		return "", nil, false
	}
	lower := foldVietnameseSearchText(strings.ToLower(trimmed))
	hasSentCue := containsAnyText(lower,
		"toi da gui", "minh da gui",
		"mail da gui", "email da gui",
		"da gui den", "da gui toi", "da gui cho",
		"sent to", "sent mail", "sent email",
	)
	if !hasSentCue {
		return "", nil, false
	}
	email := emailAnswerPattern.FindString(trimmed)
	if email == "" {
		return "", nil, false
	}
	return "in:sent to:" + strings.ToLower(email), []string{"SENT"}, true
}

func providerRelativeDateRange(now time.Time, userText string) (time.Time, time.Time, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	text := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(userText)))
	if text == "" {
		return time.Time{}, time.Time{}, false
	}

	switch {
	case containsAnyText(text, "tuan sau", "next week"):
		start := startOfWeekMonday(now).AddDate(0, 0, 7)
		return start, start.AddDate(0, 0, 7), true
	case containsAnyText(text, "tuan nay", "this week", "trong tuan"):
		start := startOfWeekMonday(now)
		return start, start.AddDate(0, 0, 7), true
	case containsAnyText(text, "thang toi", "thang sau", "next month"):
		thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		start := thisMonth.AddDate(0, 1, 0)
		return start, start.AddDate(0, 1, 0), true
	case containsAnyText(text, "thang nay", "this month"):
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return start, start.AddDate(0, 1, 0), true
	case containsAnyText(text, "ngay mai", "tomorrow"):
		start := startOfDay(now).AddDate(0, 0, 1)
		return start, start.AddDate(0, 0, 1), true
	case containsAnyText(text, "hom kia", "day before yesterday", "two days ago"):
		start := startOfDay(now).AddDate(0, 0, -2)
		return start, start.AddDate(0, 0, 1), true
	case containsAnyText(text, "hom qua", "yesterday"):
		start := startOfDay(now).AddDate(0, 0, -1)
		return start, start.AddDate(0, 0, 1), true
	case containsAnyText(text, "hom nay", "today"):
		start := startOfDay(now)
		return start, start.AddDate(0, 0, 1), true
	default:
		return time.Time{}, time.Time{}, false
	}
}

func normalizeRelativeProviderQuery(query string, userText string, purposeTerms []string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	queryText := foldVietnameseSearchText(strings.ToLower(trimmed))
	userText = foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(userText)))
	if queryText == userText {
		return ""
	}
	if containsAnyText(queryText, relativeQueryTerms()...) && containsAnyText(queryText, purposeTerms...) {
		return ""
	}
	return trimmed
}

func relativeQueryTerms() []string {
	return []string{
		"tuan nay", "tuan sau", "thang nay", "thang toi", "thang sau",
		"hom kia", "hom qua", "ngay mai", "hom nay", "day before yesterday", "two days ago", "yesterday", "today", "tomorrow", "this week", "next week",
		"this month", "next month",
	}
}

func calendarQueryPurposeTerms() []string {
	return []string{"lich", "calendar", "su kien", "event"}
}

func gmailQueryPurposeTerms() []string {
	return []string{"email", "mail", "gmail", "thu", "hop thu"}
}

func foldVietnameseSearchText(text string) string {
	replacer := strings.NewReplacer(
		"\u00e0", "a", "\u00e1", "a", "\u1ea1", "a", "\u1ea3", "a", "\u00e3", "a",
		"\u00e2", "a", "\u1ea7", "a", "\u1ea5", "a", "\u1ead", "a", "\u1ea9", "a", "\u1eab", "a",
		"\u0103", "a", "\u1eb1", "a", "\u1eaf", "a", "\u1eb7", "a", "\u1eb3", "a", "\u1eb5", "a",
		"\u00e8", "e", "\u00e9", "e", "\u1eb9", "e", "\u1ebb", "e", "\u1ebd", "e",
		"\u00ea", "e", "\u1ec1", "e", "\u1ebf", "e", "\u1ec7", "e", "\u1ec3", "e", "\u1ec5", "e",
		"\u00ec", "i", "\u00ed", "i", "\u1ecb", "i", "\u1ec9", "i", "\u0129", "i",
		"\u00f2", "o", "\u00f3", "o", "\u1ecd", "o", "\u1ecf", "o", "\u00f5", "o",
		"\u00f4", "o", "\u1ed3", "o", "\u1ed1", "o", "\u1ed9", "o", "\u1ed5", "o", "\u1ed7", "o",
		"\u01a1", "o", "\u1edd", "o", "\u1edb", "o", "\u1ee3", "o", "\u1edf", "o", "\u1ee1", "o",
		"\u00f9", "u", "\u00fa", "u", "\u1ee5", "u", "\u1ee7", "u", "\u0169", "u",
		"\u01b0", "u", "\u1eeb", "u", "\u1ee9", "u", "\u1ef1", "u", "\u1eed", "u", "\u1eef", "u",
		"\u1ef3", "y", "\u00fd", "y", "\u1ef5", "y", "\u1ef7", "y", "\u1ef9", "y",
		"\u0111", "d",
	)
	return replacer.Replace(text)
}

func relativeDateRange(now time.Time, userText string) (time.Time, time.Time, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return time.Time{}, time.Time{}, false
	}

	switch {
	case containsAnyText(lower, "tuần sau", "tuan sau", "next week"):
		start := startOfWeekMonday(now).AddDate(0, 0, 7)
		return start, start.AddDate(0, 0, 7), true
	case containsAnyText(lower, "tuần này", "tuan nay", "this week", "trong tuần", "trong tuan"):
		start := startOfWeekMonday(now)
		return start, start.AddDate(0, 0, 7), true
	case containsAnyText(lower, "tháng tới", "thang toi", "tháng sau", "thang sau", "next month"):
		thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		start := thisMonth.AddDate(0, 1, 0)
		return start, start.AddDate(0, 1, 0), true
	case containsAnyText(lower, "tháng này", "thang nay", "this month"):
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return start, start.AddDate(0, 1, 0), true
	case containsAnyText(lower, "ngày mai", "ngay mai", "tomorrow"):
		start := startOfDay(now).AddDate(0, 0, 1)
		return start, start.AddDate(0, 0, 1), true
	case containsAnyText(lower, "hôm qua", "hom qua", "yesterday"):
		start := startOfDay(now).AddDate(0, 0, -1)
		return start, start.AddDate(0, 0, 1), true
	case containsAnyText(lower, "hôm nay", "hom nay", "today"):
		start := startOfDay(now)
		return start, start.AddDate(0, 0, 1), true
	default:
		return time.Time{}, time.Time{}, false
	}
}

func normalizeCalendarListEventsArgsLegacy(now time.Time, args map[string]any, userText string) map[string]any {
	if now.IsZero() {
		now = time.Now()
	}
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return nil
	}

	var start, end time.Time
	switch {
	case containsAnyText(lower, "tuần sau", "tuan sau", "next week"):
		thisWeek := startOfWeekMonday(now)
		start = thisWeek.AddDate(0, 0, 7)
		end = start.AddDate(0, 0, 7)
	case containsAnyText(lower, "tuần này", "tuan nay", "this week", "trong tuần", "trong tuan"):
		start = startOfWeekMonday(now)
		end = start.AddDate(0, 0, 7)
	case containsAnyText(lower, "tháng tới", "thang toi", "tháng sau", "thang sau", "next month"):
		thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		start = thisMonth.AddDate(0, 1, 0)
		end = start.AddDate(0, 1, 0)
	case containsAnyText(lower, "tháng này", "thang nay", "this month"):
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end = start.AddDate(0, 1, 0)
	case containsAnyText(lower, "ngày mai", "ngay mai", "tomorrow"):
		start = startOfDay(now).AddDate(0, 0, 1)
		end = start.AddDate(0, 0, 1)
	case containsAnyText(lower, "hôm qua", "hom qua", "yesterday"):
		start = startOfDay(now).AddDate(0, 0, -1)
		end = start.AddDate(0, 0, 1)
	case containsAnyText(lower, "hôm nay", "hom nay", "today"):
		start = startOfDay(now)
		end = start.AddDate(0, 0, 1)
	default:
		return nil
	}

	normalized := cloneArguments(args)
	if normalized == nil {
		normalized = map[string]any{}
	}
	normalized["timeMin"] = start.Format(time.RFC3339)
	normalized["timeMax"] = end.Format(time.RFC3339)
	if query, ok := normalized["query"].(string); ok {
		normalized["query"] = normalizeRelativeCalendarQuery(query, userText)
	}
	return normalized
}

func normalizeRelativeCalendarQuery(query string, userText string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	lowerQuery := strings.ToLower(trimmed)
	lowerText := strings.ToLower(strings.TrimSpace(userText))
	if lowerQuery == lowerText {
		return ""
	}
	if containsAnyText(lowerQuery, "tuần này", "tuan nay", "tuần sau", "tuan sau", "tháng này", "thang nay", "tháng tới", "thang toi", "hôm nay", "hom nay", "today", "this week", "next week", "this month", "next month") &&
		containsAnyText(lowerQuery, "lịch", "lich", "calendar", "sự kiện", "su kien", "event") {
		return ""
	}
	return trimmed
}

func normalizeRelativeGmailQuery(query string, userText string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	lowerQuery := strings.ToLower(trimmed)
	lowerText := strings.ToLower(strings.TrimSpace(userText))
	if lowerQuery == lowerText {
		return ""
	}
	if containsAnyText(lowerQuery, "tuần này", "tuan nay", "tuần sau", "tuan sau", "tháng này", "thang nay", "tháng tới", "thang toi", "hôm nay", "hom nay", "today", "this week", "next week", "this month", "next month") &&
		containsAnyText(lowerQuery, "email", "mail", "gmail", "thư", "thu", "hộp thư", "hop thu") {
		return ""
	}
	return trimmed
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func startOfWeekMonday(t time.Time) time.Time {
	dayStart := startOfDay(t)
	weekday := int(dayStart.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return dayStart.AddDate(0, 0, -(weekday - 1))
}

func containsAnyText(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
