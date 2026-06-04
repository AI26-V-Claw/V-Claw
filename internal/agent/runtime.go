package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	agentintent "vclaw/internal/agent/intent"
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
	Provider         providers.Provider
	Registry         *tools.ToolRegistry
	IntentClassifier IntentClassifier
	TaskPlanner      TaskPlanner
	TurnRouter       TurnRouter
	Policy           policies.ToolPolicy
	SessionStore     sessions.Store
	Logger           *slog.Logger
	MaxIterations    int
	ToolTimeout      time.Duration
	Model            string
	Now              func() time.Time
}

type IntentClassifier interface {
	Classify(ctx context.Context, userInput string) (*agentintent.ClassificationOutput, error)
}

type MemoryAwareIntentClassifier interface {
	ClassifyWithMemoryIsolation(ctx context.Context, userInput string, recentHistory []string) (*agentintent.ClassificationOutput, error)
}

type Runtime struct {
	provider         providers.Provider
	registry         *tools.ToolRegistry
	intentClassifier IntentClassifier
	taskPlanner      TaskPlanner
	turnRouter       TurnRouter
	policy           policies.ToolPolicy
	sessionStore     sessions.Store
	logger           *slog.Logger
	approvalMu       sync.Mutex
	pendingApprovals map[string]pendingApproval
	pendingBySession map[string]string
	clarifyMu        sync.Mutex
	pendingClarifies map[string]pendingClarification
	maxIterations    int
	toolTimeout      time.Duration
	model            string
	now              func() time.Time
}

type pendingApproval struct {
	message    contracts.UserMessage
	request    contracts.ApprovalRequest
	toolCall   providers.ToolCall
	definition tools.ToolDefinition
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
	turnRouter := config.TurnRouter
	if turnRouter == nil {
		turnRouter = NewLLMTurnRouter(config.Provider, config.Model)
	}
	return &Runtime{
		provider:         config.Provider,
		registry:         config.Registry,
		intentClassifier: config.IntentClassifier,
		taskPlanner:      config.TaskPlanner,
		turnRouter:       turnRouter,
		policy:           config.Policy,
		sessionStore:     sessionStore,
		logger:           logger,
		pendingApprovals: make(map[string]pendingApproval),
		pendingBySession: make(map[string]string),
		pendingClarifies: make(map[string]pendingClarification),
		maxIterations:    maxIterations,
		toolTimeout:      toolTimeout,
		model:            config.Model,
		now:              now,
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
	history := recentHistoryForPrompt(transcript, 8)

	userMessage := providers.Message{Role: providers.MessageRoleUser, Content: message.Text}
	transcript = append(transcript, userMessage)
	if err := r.sessionStore.AppendMessage(ctx, message.SessionID, userMessage); err != nil {
		base.Error = internalError("append user message: "+err.Error(), contracts.ErrorSourceSession)
		base.Message = base.Error.Message
		return base, nil
	}
	r.clearPendingClarification(message.SessionID)

	route, errShape := r.routeTurn(ctx, message, history)
	if errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	if route.Mode == TurnModeBlockedPromptInjection {
		blocked := "Tôi không thể hỗ trợ yêu cầu cố gắng thay đổi hoặc bỏ qua hướng dẫn hệ thống."
		if errShape := r.appendAssistantTranscript(ctx, message.SessionID, blocked); errShape != nil {
			base.Error = errShape
			base.Message = errShape.Message
			return base, nil
		}
		return contracts.AgentResponse{
			RequestID: message.RequestID,
			SessionID: message.SessionID,
			Status:    contracts.AgentStatusBlocked,
			Message:   blocked,
			Data:      r.traceData(nil, nil, route),
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorActionBlockedByPolicy,
				Message:   "prompt injection blocked",
				Source:    contracts.ErrorSourcePolicy,
				Retryable: false,
			},
		}, nil
	}

	toolResults := []contracts.ToolResult{}
	for iteration := 1; iteration <= r.maxIterations; iteration++ {
		r.logger.Debug("agent iteration started", "request_id", message.RequestID, "session_id", message.SessionID, "iteration", iteration)
		emitProgress(ctx, ProgressEvent{Stage: ProgressStageThinking, Message: "Agent is thinking"})
		providerMessages := r.withRuntimeSystemPrompt(transcript, nil, nil, route)
		providerResponse, err := r.provider.Chat(ctx, providers.ChatRequest{
			Model:      r.model,
			Messages:   providerMessages,
			Tools:      r.providerToolsForRoute(route),
			ToolChoice: toolChoiceForRoute(route),
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
				Data:        r.traceData(nil, nil, route),
				ToolResults: toolResults,
			}, nil
		}

		for index, providerToolCall := range assistantMessage.ToolCalls {
			if isClarifyToolCall(providerToolCall) {
				clarification := clarificationFromToolCall(providerToolCall)
				r.storePendingClarification(message.SessionID, clarification)
				if err := r.appendToolObservation(ctx, message.SessionID, transcript, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM("CLARIFICATION_REQUESTED: " + clarification.question),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservations(ctx, message.SessionID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because clarification is required first"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				return contracts.AgentResponse{
					RequestID: message.RequestID,
					SessionID: message.SessionID,
					Status:    contracts.AgentStatusNeedClarification,
					Message:   clarification.question,
					Data:      r.traceData(nil, nil, route),
				}, nil
			}

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
				r.storePendingApproval(pendingApproval{
					message:    message,
					request:    approval,
					toolCall:   providerToolCall,
					definition: definition,
				})
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
					Data:            r.traceData(nil, nil, route),
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
		Data:        r.traceData(nil, nil, route),
		ToolResults: toolResults,
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorMaxIterationsExceeded,
			Message:   "agent exceeded max iterations",
			Source:    contracts.ErrorSourceAgent,
			Retryable: false,
		},
	}, nil
}

func (r *Runtime) HasPendingApproval(sessionID string) bool {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	approvalID := r.pendingBySession[strings.TrimSpace(sessionID)]
	if approvalID == "" {
		return false
	}
	_, ok := r.pendingApprovals[approvalID]
	return ok
}

func (r *Runtime) ResolveApproval(ctx context.Context, sessionID string, decision contracts.ApprovalDecision) (contracts.AgentResponse, error) {
	pending, ok := r.takePendingApproval(sessionID, decision.ApprovalID)
	if !ok {
		return contracts.AgentResponse{
			RequestID: decision.RequestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không tìm thấy yêu cầu xác nhận đang chờ.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalNotFound,
				Message:   "pending approval not found",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	if pending.request.ExpiresAt.Before(r.now()) {
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Yêu cầu xác nhận đã hết hạn. Vui lòng gửi lại yêu cầu.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalExpired,
				Message:   "approval expired",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	switch decision.Decision {
	case contracts.ApprovalDecisionApproved:
		result := r.executeAllowedTool(ctx, pending.toolCall, pending.definition)
		contractResult := contractToolResult(result)
		response := contracts.AgentResponse{
			RequestID:   pending.message.RequestID,
			SessionID:   pending.message.SessionID,
			Status:      contracts.AgentStatusCompleted,
			Message:     approvalExecutionMessage(result),
			Data:        r.traceData(nil, nil),
			ToolResults: []contracts.ToolResult{contractResult},
		}
		if !result.Success {
			response.Status = contracts.AgentStatusFailed
			response.Error = toolErrorShape(result)
			response.Message = response.Error.Message
		}
		return response, nil
	case contracts.ApprovalDecisionRejected:
		comment := strings.TrimSpace(decision.Comment)
		if comment != "" {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusNeedClarification,
				Message:   "Đã hủy thao tác đang chờ. Bạn muốn chỉnh lại như thế nào?\n\nGhi chú của bạn: " + comment,
				Data:      r.traceData(nil, nil),
			}, nil
		}
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusBlocked,
			Message:   "Đã hủy thao tác. Tôi chưa thực hiện tool nào.",
			Data:      r.traceData(nil, nil),
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorActionBlockedByPolicy,
				Message:   "approval rejected",
				Source:    contracts.ErrorSourcePolicy,
				Retryable: false,
			},
		}, nil
	default:
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Quyết định xác nhận không hợp lệ.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorInvalidInput,
				Message:   "approval decision must be approved or rejected",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}
}

func (r *Runtime) withRuntimeSystemPrompt(transcript []providers.Message, classification *agentintent.ClassificationOutput, planResult *TaskPlanResult, route *TurnRoute) []providers.Message {
	messages := make([]providers.Message, 0, len(transcript)+3)
	messages = append(messages, providers.Message{
		Role:    providers.MessageRoleSystem,
		Content: runtimeSystemPrompt(r.now()),
	})
	if prompt := routeContextPrompt(route); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	if prompt := intentContextPrompt(classification); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	if prompt := planContextPrompt(planResult); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	messages = append(messages, cloneProviderMessages(transcript)...)
	return messages
}

func routeContextPrompt(route *TurnRoute) string {
	if route == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf(`Turn router result:
- tool_exposure_mode: %s
- reason: %s

This is not an intent label, not a tool choice, not a clarification decision, and not a risk decision.
If tools are available, decide naturally whether to answer directly, call a relevant tool, or call clarify when required information is missing.`, route.Mode, strings.TrimSpace(route.Reason)))
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
When tools are available and required details are missing, call clarify with one concise question instead of inventing values. In no-tool mode, ask normally if the conversation needs it.
Keep final answers concise and include the useful result, not internal implementation details.

Current date and time: %s.
When users ask about relative dates or ranges, convert them to concrete tool arguments before calling tools.
For calendar.listEvents:
- "today" / "hôm nay" means local start of today through local start of tomorrow.
- "this week" / "tuần này" means Monday 00:00 through next Monday 00:00 in the current local timezone.
- "next week" / "tuần sau" means next Monday 00:00 through the following Monday 00:00.
- For a date range, set timeMin to the beginning of the range and timeMax to the exclusive end of the range.
- Do not put date words like "today", "this week", "hôm nay", or "tuần này" into query. Use query only for event title, description, location, or attendee keywords.
For gmail.listEmails and gmail.listThreads:
- Use after and before as date-only YYYY-MM-DD values, not RFC3339 datetimes.
- "today" / "hôm nay" means after is today's local date and before is tomorrow's local date.
- Do not put date words like "today", "this week", "hôm nay", or "tuần này" into query. Use query only for sender, subject, body, or Gmail search terms.
Gmail date rules, restated in ASCII:
- gmail.listEmails and gmail.listThreads after/before must be date-only YYYY-MM-DD, never RFC3339 datetime strings.
- "today" / "hom nay" means after=today local date and before=tomorrow local date.
- Keep relative date words out of Gmail query; query is only for sender, subject, body, labels, or Gmail search terms.
For calendar.createEvent and calendar.updateEvent:
- Attendees must be valid email addresses.
- If the user provides a person name instead of an email address, call people.searchDirectory first and use the resolved Workspace email.
- Do not pass display names like "Bao" or "Tung" into attendees.
- If no matching email can be resolved, ask one concise clarification question for the attendee email.

Format final answers for chat channels:
- Start with one short summary line.
- For Gmail, Calendar, Chat, or People results, use compact bullets with the important fields only.
- Prefer 5 to 10 bullets unless the user asks for more.
- Do not dump raw JSON, raw tool outputs, internal tool names, or opaque IDs unless the user explicitly asks.
- Use plain text only. Do not use Markdown bold, italic, inline code, headings, or syntax markers like **, __, backticks, or #.
- Avoid Markdown tables because Telegram renders them poorly in plain text.
- If no relevant result is found, say that plainly and suggest the next useful query.`, now.Format(time.RFC3339)))
}

func (r *Runtime) classifyIntent(ctx context.Context, message contracts.UserMessage, recentHistory []string) (*agentintent.ClassificationOutput, *contracts.ErrorShape) {
	if r.intentClassifier == nil {
		return nil, nil
	}
	emitProgress(ctx, ProgressEvent{Stage: ProgressStageClassifying, Message: "Intent classification started"})
	var classification *agentintent.ClassificationOutput
	var err error
	if memoryAware, ok := r.intentClassifier.(MemoryAwareIntentClassifier); ok && len(recentHistory) > 0 {
		classification, err = memoryAware.ClassifyWithMemoryIsolation(ctx, message.Text, recentHistory)
	} else {
		classification, err = r.intentClassifier.Classify(ctx, message.Text)
	}
	if err != nil {
		retryable := providers.IsRetryableError(err)
		code := contracts.ErrorProviderError
		if retryable {
			code = contracts.ErrorProviderUnavailable
		}
		return nil, &contracts.ErrorShape{
			Code:      code,
			Message:   "intent classification failed: " + err.Error(),
			Source:    contracts.ErrorSourceProvider,
			Retryable: retryable,
		}
	}
	if classification == nil || classification.Intent == nil {
		return nil, internalError("intent classifier returned empty result", contracts.ErrorSourceAgent)
	}
	r.logger.Info("intent classified",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"intent_type", classification.Intent.Type,
		"confidence", classification.Intent.Confidence,
		"needs_clarification", classification.NeedsClarification,
	)
	emitProgress(ctx, ProgressEvent{Stage: ProgressStageClassified, Message: "Intent classification completed"})
	return classification, nil
}

func (r *Runtime) routeTurn(ctx context.Context, message contracts.UserMessage, recentHistory []string) (*TurnRoute, *contracts.ErrorShape) {
	if r.turnRouter == nil {
		route := TurnRoute{Mode: TurnModeToolEnabled, Reason: "router unavailable; exposing tools by default"}
		return &route, nil
	}
	emitProgress(ctx, ProgressEvent{Stage: ProgressStageClassifying, Message: "Turn routing started"})
	route, err := r.turnRouter.RouteTurn(ctx, TurnRouteInput{
		Message:       message.Text,
		RecentHistory: recentHistory,
		Now:           r.now(),
	})
	if err != nil {
		retryable := providers.IsRetryableError(err)
		code := contracts.ErrorProviderError
		if retryable {
			code = contracts.ErrorProviderUnavailable
		}
		return nil, &contracts.ErrorShape{
			Code:      code,
			Message:   "turn routing failed: " + err.Error(),
			Source:    contracts.ErrorSourceProvider,
			Retryable: retryable,
		}
	}
	if route.Mode == "" {
		route.Mode = TurnModeToolEnabled
	}
	if strings.TrimSpace(route.Reason) == "" {
		route.Reason = string(route.Mode)
	}
	r.logger.Info("turn routed",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"mode", route.Mode,
		"reason", route.Reason,
	)
	emitProgress(ctx, ProgressEvent{Stage: ProgressStageClassified, Message: "Turn routing completed"})
	return &route, nil
}

func (r *Runtime) providerToolsForRoute(route *TurnRoute) []providers.ToolDefinition {
	if route == nil || route.Mode != TurnModeToolEnabled {
		return nil
	}
	definitions := providers.ToolDefinitionsFromRegistry(r.registry.ListTools())
	definitions = append(definitions, clarifyToolDefinition())
	return definitions
}

func toolChoiceForRoute(route *TurnRoute) string {
	if route == nil || route.Mode != TurnModeToolEnabled {
		return "none"
	}
	return "auto"
}

func (r *Runtime) clarificationResponse(message contracts.UserMessage, classification *agentintent.ClassificationOutput) *contracts.AgentResponse {
	if classification == nil || !classification.NeedsClarification {
		return nil
	}
	clarification := strings.TrimSpace(classification.ClarificationMessage)
	if clarification == "" {
		clarification = "Bạn có thể nói rõ hơn bạn muốn tôi làm gì không?"
	}
	return &contracts.AgentResponse{
		RequestID: message.RequestID,
		SessionID: message.SessionID,
		Status:    contracts.AgentStatusNeedClarification,
		Message:   clarification,
		Data:      r.traceData(classification, nil),
	}
}

func intentContextPrompt(classification *agentintent.ClassificationOutput) string {
	if classification == nil || classification.Intent == nil {
		return ""
	}
	intentResult := classification.Intent
	toolNames := make([]string, 0, len(intentResult.ToolCalls))
	for _, toolCall := range intentResult.ToolCalls {
		if strings.TrimSpace(toolCall.Name) != "" {
			toolNames = append(toolNames, toolCall.Name)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`Intent classifier result for the current user message:
- intent_type: %s
- confidence: %.2f
- needs_confirm: %t
- missing_params: %s
- proposed_tools: %s
- reasoning_vi: %s

Use this only as routing/safety context. Do not expose it directly to the user. If the intent indicates a write, destructive, local file, or code execution action, keep following the tool approval policy.`,
		intentResult.Type,
		intentResult.Confidence,
		intentResult.NeedsConfirm,
		strings.Join(intentResult.MissingParams, ", "),
		strings.Join(toolNames, ", "),
		strings.TrimSpace(intentResult.Reasoning),
	))
}

func (r *Runtime) planTask(ctx context.Context, message contracts.UserMessage, classification *agentintent.ClassificationOutput, recentHistory []string) (*TaskPlanResult, *contracts.ErrorShape) {
	if r.taskPlanner == nil {
		return nil, nil
	}
	emitProgress(ctx, ProgressEvent{Stage: ProgressStagePlanning, Message: "Task planning started"})
	toolDefs := []tools.ToolDefinition{}
	if r.registry != nil {
		toolDefs = r.registry.ListTools()
	}
	planResult, err := r.taskPlanner.Plan(ctx, TaskPlanningInput{
		Message:        message,
		Classification: classification,
		Tools:          toolDefs,
		RecentHistory:  recentHistory,
		Now:            r.now(),
	})
	if err != nil {
		retryable := providers.IsRetryableError(err)
		code := contracts.ErrorProviderError
		if retryable {
			code = contracts.ErrorProviderUnavailable
		}
		return nil, &contracts.ErrorShape{
			Code:      code,
			Message:   "task planning failed: " + err.Error(),
			Source:    contracts.ErrorSourceProvider,
			Retryable: retryable,
		}
	}
	if planResult == nil {
		planResult = &TaskPlanResult{}
	}
	r.logger.Info("task planned",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"steps", len(planResult.Plan.Steps),
		"needs_clarification", planResult.NeedsClarification,
	)
	emitProgress(ctx, ProgressEvent{Stage: ProgressStagePlanned, Message: "Task planning completed"})
	return planResult, nil
}

func (r *Runtime) planningClarificationResponse(message contracts.UserMessage, classification *agentintent.ClassificationOutput, planResult *TaskPlanResult) *contracts.AgentResponse {
	if planResult == nil || !planResult.NeedsClarification {
		return nil
	}
	clarification := strings.TrimSpace(planResult.ClarificationMessage)
	if clarification == "" {
		clarification = "Bạn có thể bổ sung thêm thông tin để tôi lập kế hoạch chính xác hơn không?"
	}
	return &contracts.AgentResponse{
		RequestID: message.RequestID,
		SessionID: message.SessionID,
		Status:    contracts.AgentStatusNeedClarification,
		Message:   clarification,
		Data:      r.traceData(classification, planResult),
		Plan:      responsePlan(planResult),
	}
}

func planContextPrompt(planResult *TaskPlanResult) string {
	if planResult == nil || len(planResult.Plan.Steps) == 0 {
		return ""
	}
	lines := make([]string, 0, len(planResult.Plan.Steps))
	for _, step := range planResult.Plan.Steps {
		lines = append(lines, fmt.Sprintf("- %s: %s (%s)", step.ID, step.Description, step.Status))
	}
	return strings.TrimSpace(fmt.Sprintf(`Task planner result for the current user message:
%s

Use this as execution guidance only. The tool policy and approval boundary remain mandatory. Do not expose this plan unless the user asks for the internal plan.`, strings.Join(lines, "\n")))
}

func responsePlan(planResult *TaskPlanResult) *contracts.Plan {
	if planResult == nil || len(planResult.Plan.Steps) == 0 {
		return nil
	}
	plan := planResult.Plan
	return &plan
}

func (r *Runtime) traceData(classification *agentintent.ClassificationOutput, planResult *TaskPlanResult, routes ...*TurnRoute) map[string]any {
	data := map[string]any{
		"model": r.model,
	}
	if len(routes) > 0 && routes[0] != nil {
		data["turnRouter"] = map[string]any{
			"mode":   routes[0].Mode,
			"reason": routes[0].Reason,
		}
	}
	if classification != nil && classification.Intent != nil {
		data["intent"] = map[string]any{
			"type":                 classification.Intent.Type,
			"confidence":           classification.Intent.Confidence,
			"needsClarification":   classification.NeedsClarification,
			"clarificationMessage": classification.ClarificationMessage,
			"toolCalls":            classification.Intent.ToolCalls,
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

func (r *Runtime) storePendingClarification(sessionID string, clarification pendingClarification) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	r.clarifyMu.Lock()
	defer r.clarifyMu.Unlock()
	r.pendingClarifies[sessionID] = clarification
}

func (r *Runtime) clearPendingClarification(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	r.clarifyMu.Lock()
	defer r.clarifyMu.Unlock()
	delete(r.pendingClarifies, sessionID)
}

func (r *Runtime) appendAssistantTranscript(ctx context.Context, sessionID string, content string) *contracts.ErrorShape {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if err := r.sessionStore.AppendMessage(ctx, sessionID, providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: content,
	}); err != nil {
		return internalError("append assistant message: "+err.Error(), contracts.ErrorSourceSession)
	}
	return nil
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
		normalized["query"] = normalizeRelativeProviderQuery(query, userText, calendarQueryIntentTerms())
	}
	return normalized
}

func normalizeGmailListArgs(now time.Time, args map[string]any, userText string) map[string]any {
	start, end, ok := providerRelativeDateRange(now, userText)
	if !ok {
		return nil
	}

	normalized := cloneArguments(args)
	if normalized == nil {
		normalized = map[string]any{}
	}
	normalized["after"] = start.Format("2006-01-02")
	normalized["before"] = end.Format("2006-01-02")
	if query, ok := normalized["query"].(string); ok {
		normalized["query"] = normalizeRelativeProviderQuery(query, userText, gmailQueryIntentTerms())
	}
	return normalized
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
	case containsAnyText(text, "hom nay", "today"):
		start := startOfDay(now)
		return start, start.AddDate(0, 0, 1), true
	default:
		return time.Time{}, time.Time{}, false
	}
}

func normalizeRelativeProviderQuery(query string, userText string, intentTerms []string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	queryText := foldVietnameseSearchText(strings.ToLower(trimmed))
	userText = foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(userText)))
	if queryText == userText {
		return ""
	}
	if containsAnyText(queryText, relativeQueryTerms()...) && containsAnyText(queryText, intentTerms...) {
		return ""
	}
	return trimmed
}

func relativeQueryTerms() []string {
	return []string{
		"tuan nay", "tuan sau", "thang nay", "thang toi", "thang sau",
		"ngay mai", "hom nay", "today", "tomorrow", "this week", "next week",
		"this month", "next month",
	}
}

func calendarQueryIntentTerms() []string {
	return []string{"lich", "calendar", "su kien", "event"}
}

func gmailQueryIntentTerms() []string {
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

func (r *Runtime) approvalRequest(message contracts.UserMessage, toolCall providers.ToolCall, decision contracts.RiskDecision) contracts.ApprovalRequest {
	now := r.now()
	contractCall := contracts.ToolCall{
		ToolCallID: toolCall.ID,
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolName:   toolCall.Name,
		Input:      cloneArguments(toolCall.Arguments),
	}
	summary := approvalSummary(toolCall.Name, decision.RiskLevel)
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

func (r *Runtime) storePendingApproval(pending pendingApproval) {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	approvalID := strings.TrimSpace(pending.request.ApprovalID)
	sessionID := strings.TrimSpace(pending.message.SessionID)
	if approvalID == "" || sessionID == "" {
		return
	}
	if oldID := r.pendingBySession[sessionID]; oldID != "" {
		delete(r.pendingApprovals, oldID)
	}
	r.pendingApprovals[approvalID] = pending
	r.pendingBySession[sessionID] = approvalID
}

func (r *Runtime) takePendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		approvalID = r.pendingBySession[sessionID]
	}
	if approvalID == "" {
		return pendingApproval{}, false
	}
	pending, ok := r.pendingApprovals[approvalID]
	if !ok {
		return pendingApproval{}, false
	}
	delete(r.pendingApprovals, approvalID)
	if sessionID == "" {
		sessionID = pending.message.SessionID
	}
	if r.pendingBySession[sessionID] == approvalID {
		delete(r.pendingBySession, sessionID)
	}
	return pending, true
}

func approvalSummary(toolName string, riskLevel contracts.RiskLevel) string {
	switch toolName {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		return "Tôi cần bạn xác nhận trước khi tạo hoặc sửa Gmail draft."
	case "gmail.sendDraft":
		return "Tôi cần bạn xác nhận trước khi gửi email."
	case "calendar.createEvent":
		return "Tôi cần bạn xác nhận trước khi tạo sự kiện Calendar."
	case "calendar.updateEvent":
		return "Tôi cần bạn xác nhận trước khi sửa sự kiện Calendar."
	case "calendar.deleteEvent":
		return "Tôi cần bạn xác nhận trước khi xóa sự kiện Calendar."
	case "chat.sendMessage":
		return "Tôi cần bạn xác nhận trước khi gửi tin nhắn Google Chat."
	case "sandbox.runPython", "sandbox.runShell":
		return "Tôi cần bạn xác nhận trước khi chạy code hoặc lệnh trong sandbox."
	default:
		return fmt.Sprintf("Tôi cần bạn xác nhận trước khi chạy %s vì risk là %s.", toolName, riskLevel)
	}
}

func approvalExecutionMessage(result tools.ToolResult) string {
	if strings.TrimSpace(result.ContentForUser) != "" {
		return result.ContentForUser
	}
	if result.Success {
		return "Đã thực hiện thao tác sau khi bạn xác nhận."
	}
	if result.Error != nil && strings.TrimSpace(result.Error.Message) != "" {
		return result.Error.Message
	}
	return "Tool không hoàn tất."
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
