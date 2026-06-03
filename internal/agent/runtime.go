package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

const (
	DefaultMaxIterations = 8
	DefaultToolTimeout   = 30 * time.Second
	approvalTTL          = 10 * time.Minute
)

type RuntimeConfig struct {
	Provider      providers.Provider
	Registry      *tools.ToolRegistry
	Policy        policies.ToolPolicy
	SessionStore  sessions.Store
	Logger        *slog.Logger
	MaxIterations int
	ToolTimeout   time.Duration
	Model         string
	Now           func() time.Time
}

type Runtime struct {
	provider      providers.Provider
	registry      *tools.ToolRegistry
	policy        policies.ToolPolicy
	sessionStore  sessions.Store
	logger        *slog.Logger
	maxIterations int
	toolTimeout   time.Duration
	model         string
	now           func() time.Time
}

func NewRuntime(config RuntimeConfig) *Runtime {
	maxIterations := config.MaxIterations
	if maxIterations <= 0 {
		maxIterations = DefaultMaxIterations
	}
	toolTimeout := config.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}
	sessionStore := config.SessionStore
	if sessionStore == nil {
		sessionStore = sessions.NewInMemoryStore()
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Runtime{
		provider:      config.Provider,
		registry:      config.Registry,
		policy:        config.Policy,
		sessionStore:  sessionStore,
		logger:        logger,
		maxIterations: maxIterations,
		toolTimeout:   toolTimeout,
		model:         config.Model,
		now:           now,
	}
}

func (r *Runtime) Run(ctx context.Context, message contracts.UserMessage) (contracts.AgentResponse, error) {
	emitProgress(ctx, ProgressEvent{Stage: ProgressStageStarted, Message: "Agent run started"})
	base := contracts.AgentResponse{
		RequestID: message.RequestID,
		SessionID: message.SessionID,
		Status:    contracts.AgentStatusFailed,
	}
	if errShape := validateUserMessage(message); errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	if r.provider == nil {
		base.Error = internalError("provider is required", contracts.ErrorSourceAgent)
		base.Message = base.Error.Message
		return base, nil
	}
	if r.registry == nil {
		base.Error = internalError("tool registry is required", contracts.ErrorSourceAgent)
		base.Message = base.Error.Message
		return base, nil
	}
	if r.sessionStore == nil {
		base.Error = internalError("session store is required", contracts.ErrorSourceSession)
		base.Message = base.Error.Message
		return base, nil
	}

	transcript, err := r.sessionStore.LoadTranscript(ctx, message.SessionID)
	if err != nil {
		base.Error = internalError("load session transcript: "+err.Error(), contracts.ErrorSourceSession)
		base.Message = base.Error.Message
		return base, nil
	}

	userMessage := providers.Message{Role: providers.MessageRoleUser, Content: message.Text}
	transcript = append(transcript, userMessage)
	if err := r.sessionStore.AppendMessage(ctx, message.SessionID, userMessage); err != nil {
		base.Error = internalError("append user message: "+err.Error(), contracts.ErrorSourceSession)
		base.Message = base.Error.Message
		return base, nil
	}

	toolResults := []contracts.ToolResult{}
	for iteration := 1; iteration <= r.maxIterations; iteration++ {
		r.logger.Debug("agent iteration started", "request_id", message.RequestID, "session_id", message.SessionID, "iteration", iteration)
		emitProgress(ctx, ProgressEvent{Stage: ProgressStageThinking, Message: "Agent is thinking"})
		providerMessages := r.withRuntimeSystemPrompt(transcript)
		providerResponse, err := r.provider.Chat(ctx, providers.ChatRequest{
			Model:      r.model,
			Messages:   providerMessages,
			Tools:      providers.ToolDefinitionsFromRegistry(r.registry.ListTools()),
			ToolChoice: "auto",
		})
		if err != nil {
			code := contracts.ErrorProviderError
			retryable := providers.IsRetryableError(err)
			if retryable {
				code = contracts.ErrorProviderUnavailable
			}
			base.Error = &contracts.ErrorShape{
				Code:      code,
				Message:   "provider chat failed: " + err.Error(),
				Source:    contracts.ErrorSourceProvider,
				Retryable: retryable,
			}
			base.Message = base.Error.Message
			return base, nil
		}

		assistantMessage := providerResponse.Message
		if assistantMessage.Role == "" {
			assistantMessage.Role = providers.MessageRoleAssistant
		}
		transcript = append(transcript, assistantMessage)
		if err := r.sessionStore.AppendMessage(ctx, message.SessionID, assistantMessage); err != nil {
			base.Error = internalError("append assistant message: "+err.Error(), contracts.ErrorSourceSession)
			base.Message = base.Error.Message
			return base, nil
		}

		if len(assistantMessage.ToolCalls) == 0 {
			r.logger.Info("agent completed without tool calls",
				"request_id", message.RequestID,
				"session_id", message.SessionID,
				"iteration", iteration,
				"content_preview", logPreview(assistantMessage.Content, 180),
			)
			emitProgress(ctx, ProgressEvent{Stage: ProgressStageFinalizing, Message: "Agent is finalizing the response"})
			return contracts.AgentResponse{
				RequestID:   message.RequestID,
				SessionID:   message.SessionID,
				Status:      contracts.AgentStatusCompleted,
				Message:     assistantMessage.Content,
				Data:        r.traceData(),
				ToolResults: toolResults,
			}, nil
		}

		for index, providerToolCall := range assistantMessage.ToolCalls {
			definition, found := r.registry.GetDefinition(providerToolCall.Name)
			if !found {
				definition.Name = providerToolCall.Name
			}

			decision := r.policy.DecideToolCall(providerToolCall.ID, definition, found, r.now())
			r.logger.Info("agent tool call proposed",
				"request_id", message.RequestID,
				"session_id", message.SessionID,
				"iteration", iteration,
				"tool_call_id", providerToolCall.ID,
				"tool_name", providerToolCall.Name,
				"decision", decision.Decision,
				"risk_level", decision.RiskLevel,
				"arguments", logToolArguments(providerToolCall.Name, providerToolCall.Arguments),
			)
			switch decision.Decision {
			case contracts.RiskDecisionAllow:
				providerToolCall = normalizeProviderToolCall(r.now(), providerToolCall, message.Text)
				result := r.executeAllowedTool(ctx, providerToolCall, definition)
				contractResult := contractToolResult(result)
				toolResults = append(toolResults, contractResult)

				toolMessage := providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM(result.ContentForLLM),
				}
				transcript = append(transcript, toolMessage)
				if err := r.sessionStore.AppendMessage(ctx, message.SessionID, toolMessage); err != nil {
					base.Error = internalError("append tool message: "+err.Error(), contracts.ErrorSourceSession)
					base.Message = base.Error.Message
					return base, nil
				}
				if !result.Success {
					base.ToolResults = toolResults
					base.Error = toolErrorShape(result)
					base.Message = base.Error.Message
					return base, nil
				}

			case contracts.RiskDecisionRequiresApproval:
				approval := r.approvalRequest(message, providerToolCall, decision)
				if err := r.appendToolObservation(ctx, message.SessionID, transcript, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM("ACTION_REQUIRES_APPROVAL: " + approval.Summary),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservations(ctx, message.SessionID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because another tool call requires approval"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				return contracts.AgentResponse{
					RequestID:       message.RequestID,
					SessionID:       message.SessionID,
					Status:          contracts.AgentStatusApprovalRequired,
					Message:         approval.Summary,
					ApprovalID:      approval.ApprovalID,
					ApprovalRequest: &approval,
					Data:            r.traceData(),
					ToolResults:     toolResults,
					Error: &contracts.ErrorShape{
						Code:      contracts.ErrorActionRequiresApproval,
						Message:   approval.Summary,
						Source:    contracts.ErrorSourcePolicy,
						Retryable: false,
					},
				}, nil

			default:
				reason := decision.Reason
				if strings.TrimSpace(reason) == "" {
					reason = "tool blocked by policy"
				}
				if err := r.appendToolObservation(ctx, message.SessionID, transcript, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM(policyErrorCode(found) + ": " + reason),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservations(ctx, message.SessionID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because another tool call was blocked"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				base.ToolResults = toolResults
				base.Error = &contracts.ErrorShape{
					Code:      policyErrorCode(found),
					Message:   reason,
					Source:    contracts.ErrorSourcePolicy,
					Retryable: false,
				}
				base.Message = base.Error.Message
				return base, nil
			}
		}
	}

	return contracts.AgentResponse{
		RequestID:   message.RequestID,
		SessionID:   message.SessionID,
		Status:      contracts.AgentStatusFailed,
		Message:     "agent exceeded max iterations",
		Data:        r.traceData(),
		ToolResults: toolResults,
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorMaxIterationsExceeded,
			Message:   "agent exceeded max iterations",
			Source:    contracts.ErrorSourceAgent,
			Retryable: false,
		},
	}, nil
}

func (r *Runtime) withRuntimeSystemPrompt(transcript []providers.Message) []providers.Message {
	messages := make([]providers.Message, 0, len(transcript)+1)
	messages = append(messages, providers.Message{
		Role:    providers.MessageRoleSystem,
		Content: runtimeSystemPrompt(r.now()),
	})
	messages = append(messages, cloneProviderMessages(transcript)...)
	return messages
}

func runtimeSystemPrompt(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return strings.TrimSpace(fmt.Sprintf(`You are V-Claw, an agent connected to real tools through a strict contract.
Reply in the user's language.
Use available tools when the user asks for information that a tool can retrieve or compute.
Never claim that an external action was completed unless a tool result confirms it.
For write, destructive, local file, or code execution actions, propose the action through the matching tool call; the runtime will stop for human approval before execution.
For missing required details, ask one concise clarification question instead of inventing values.
Keep final answers concise and include the useful result, not internal implementation details.

Current date and time: %s.
When users ask about relative dates or ranges, convert them to concrete ISO-8601 tool arguments before calling tools.
For calendar.listEvents:
- "today" / "hôm nay" means local start of today through local start of tomorrow.
- "this week" / "tuần này" means Monday 00:00 through next Monday 00:00 in the current local timezone.
- "next week" / "tuần sau" means next Monday 00:00 through the following Monday 00:00.
- For a date range, set timeMin to the beginning of the range and timeMax to the exclusive end of the range.
- Do not put date words like "today", "this week", "hôm nay", or "tuần này" into query. Use query only for event title, description, location, or attendee keywords.

Format final answers for chat channels:
- Start with one short summary line.
- For Gmail, Calendar, Chat, or People results, use compact bullets with the important fields only.
- Prefer 5 to 10 bullets unless the user asks for more.
- Do not dump raw JSON, raw tool outputs, internal tool names, or opaque IDs unless the user explicitly asks.
- Use plain text only. Do not use Markdown bold, italic, inline code, headings, or syntax markers like **, __, backticks, or #.
- Avoid Markdown tables because Telegram renders them poorly in plain text.
- If no relevant result is found, say that plainly and suggest the next useful query.`, now.Format(time.RFC3339)))
}

func (r *Runtime) traceData() map[string]any {
	data := map[string]any{
		"model": r.model,
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
	for _, toolCall := range toolCalls {
		if err := r.appendToolObservation(ctx, sessionID, nil, providers.Message{
			Role:       providers.MessageRoleTool,
			ToolCallID: toolCall.ID,
			Content:    truncateToolContentForLLM(content),
		}); err != nil {
			return err
		}
	}
	return nil
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
	if toolCall.Name != "calendar.listEvents" {
		return toolCall
	}
	normalizedArgs := normalizeCalendarListEventsArgs(now, toolCall.Arguments, userText)
	if normalizedArgs == nil {
		return toolCall
	}
	toolCall.Arguments = normalizedArgs
	return toolCall
}

func normalizeCalendarListEventsArgs(now time.Time, args map[string]any, userText string) map[string]any {
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

func (r *Runtime) approvalRequest(message contracts.UserMessage, toolCall providers.ToolCall, decision contracts.RiskDecision) contracts.ApprovalRequest {
	now := r.now()
	contractCall := contracts.ToolCall{
		ToolCallID: toolCall.ID,
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolName:   toolCall.Name,
		Input:      cloneArguments(toolCall.Arguments),
	}
	summary := fmt.Sprintf("Approval required before running %s.", toolCall.Name)
	return contracts.ApprovalRequest{
		ApprovalID: "appr_" + safeID(toolCall.ID),
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolCallID: toolCall.ID,
		Status:     contracts.ApprovalStatusPending,
		RiskLevel:  decision.RiskLevel,
		Summary:    summary,
		Details:    decision.Reason,
		ToolCall:   contractCall,
		CreatedAt:  now,
		ExpiresAt:  now.Add(approvalTTL),
	}
}

func providerToolCallToToolCall(call providers.ToolCall) tools.ToolCall {
	return tools.ToolCall{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: cloneArguments(call.Arguments),
	}
}

func contractToolResult(result tools.ToolResult) contracts.ToolResult {
	contractResult := contracts.ToolResult{
		ToolCallID: result.ToolCallID,
		ToolName:   result.ToolName,
		Success:    result.Success,
		Data: map[string]any{
			"contentForUser": result.ContentForUser,
			"contentForLLM":  result.ContentForLLM,
		},
	}
	if result.Error != nil {
		contractResult.Error = toolErrorShape(result)
	}
	return contractResult
}

func toolErrorShape(result tools.ToolResult) *contracts.ErrorShape {
	if result.Error == nil {
		return internalError("tool failed without error shape", contracts.ErrorSourceTool)
	}
	code := result.Error.Code
	switch code {
	case tools.ErrorToolNotFound:
		code = contracts.ErrorToolNotFound
	case tools.ErrorInvalidArgument:
		code = contracts.ErrorToolInputInvalid
	case tools.ErrorBlockedByPolicy:
		code = contracts.ErrorActionBlockedByPolicy
	case tools.ErrorTimeout:
		code = contracts.ErrorProviderTimeout
	case tools.ErrorExecutionFailed:
		code = contracts.ErrorInternal
	}
	return &contracts.ErrorShape{
		Code:      code,
		Message:   result.Error.Message,
		Source:    contracts.ErrorSourceTool,
		Retryable: false,
	}
}

func internalError(message string, source contracts.ErrorSource) *contracts.ErrorShape {
	return &contracts.ErrorShape{
		Code:      contracts.ErrorInternal,
		Message:   message,
		Source:    source,
		Retryable: false,
	}
}

func policyErrorCode(found bool) string {
	if !found {
		return contracts.ErrorToolNotFound
	}
	return contracts.ErrorActionBlockedByPolicy
}

func safeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "toolcall"
	}
	return strings.NewReplacer(" ", "_", "/", "_", "\\", "_").Replace(id)
}

func cloneProviderMessages(messages []providers.Message) []providers.Message {
	cloned := make([]providers.Message, len(messages))
	for i, message := range messages {
		cloned[i] = message
		cloned[i].ToolCalls = cloneProviderToolCalls(message.ToolCalls)
	}
	return cloned
}

func cloneProviderToolCalls(toolCalls []providers.ToolCall) []providers.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	cloned := make([]providers.ToolCall, len(toolCalls))
	for i, toolCall := range toolCalls {
		cloned[i] = toolCall
		cloned[i].Arguments = cloneArguments(toolCall.Arguments)
	}
	return cloned
}

func cloneArguments(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	cloned := make(map[string]any, len(args))
	for key, value := range args {
		cloned[key] = value
	}
	return cloned
}
