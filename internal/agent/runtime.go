package agent

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"vclaw/internal/agent/reference"
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

var (
	emailAnswerPattern  = regexp.MustCompile(`(?i)\b[[:alnum:]._%+\-]+@[[:alnum:].\-]+\.[[:alpha:]]{2,}\b`)
	timeAnswerPattern   = regexp.MustCompile(`(?i)\b\d{1,2}(:\d{2})?\s*(am|pm)?\b`)
	viTimeAnswerPattern = regexp.MustCompile(`(?i)\b\d{1,2}\s*(h|g|gio|giờ)(\s*\d{1,2})?\b`)
)

type RuntimeConfig struct {
	Provider          providers.Provider
	Registry          *tools.ToolRegistry
	ReferenceResolver reference.Resolver
	Policy            policies.ToolPolicy
	SessionStore      sessions.Store
	StateStore        RuntimeStateStore
	Logger            *slog.Logger
	MaxIterations     int
	ToolTimeout       time.Duration
	Model             string
	Now               func() time.Time
}

type Runtime struct {
	provider          providers.Provider
	registry          *tools.ToolRegistry
	referenceResolver reference.Resolver
	policy            policies.ToolPolicy
	sessionStore      sessions.Store
	stateStore        RuntimeStateStore
	logger            *slog.Logger
	approvalMu        sync.Mutex
	pendingApprovals  map[string]pendingApproval
	pendingBySession  map[string]string
	maxIterations     int
	toolTimeout       time.Duration
	model             string
	now               func() time.Time
}

type pendingApproval struct {
	runID              string
	actionID           string
	message            contracts.UserMessage
	request            contracts.ApprovalRequest
	toolCall           providers.ToolCall
	definition         tools.ToolDefinition
	remainingToolCalls []providers.ToolCall // tool calls skipped after this one; processed after approval
}

type pendingClarificationResolution struct {
	IsAnswer       bool     `json:"is_answer"`
	IsNewRequest   bool     `json:"is_new_request"`
	UpdatedRequest string   `json:"updated_request"`
	ProvidedFields []string `json:"provided_fields"`
	StillMissing   []string `json:"still_missing"`
	Reason         string   `json:"reason"`
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
	stateStore := config.StateStore
	if stateStore == nil {
		stateStore = NewInMemoryRuntimeStateStore()
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	referenceResolver := config.ReferenceResolver
	if referenceResolver == nil {
		if config.Provider != nil {
			referenceResolver = newHeuristicFirstResolver(
				reference.NewHeuristicResolver(),
				reference.NewLLMResolver(config.Provider, config.Model),
			)
		} else {
			referenceResolver = reference.NewHeuristicResolver()
		}
	}
	return &Runtime{
		provider:          config.Provider,
		registry:          config.Registry,
		referenceResolver: referenceResolver,
		policy:            config.Policy,
		sessionStore:      sessionStore,
		stateStore:        stateStore,
		logger:            logger,
		pendingApprovals:  make(map[string]pendingApproval),
		pendingBySession:  make(map[string]string),
		maxIterations:     maxIterations,
		toolTimeout:       toolTimeout,
		model:             config.Model,
		now:               now,
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
	if r.stateStore == nil {
		base.Error = internalError("runtime state store is required", contracts.ErrorSourceAgent)
		base.Message = base.Error.Message
		return base, nil
	}
	runState, errShape := r.startRunState(ctx, message)
	if errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	failStartedRun := func(errShape *contracts.ErrorShape) (contracts.AgentResponse, error) {
		if finishErr := r.finishRunState(ctx, runState, RuntimeRunStatusFailed); finishErr != nil {
			errShape = finishErr
		}
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}

	transcript, err := r.sessionStore.LoadTranscript(ctx, message.SessionID)
	if err != nil {
		return failStartedRun(internalError("load session transcript: "+err.Error(), contracts.ErrorSourceSession))
	}
	if errShape := r.refreshSessionSummary(ctx, message.SessionID, transcript); errShape != nil {
		return failStartedRun(errShape)
	}
	sessionMemory, errShape := r.loadSessionMemory(ctx, message.SessionID)
	if errShape != nil {
		return failStartedRun(errShape)
	}
	history := recentHistoryForPrompt(transcript, 8)
	pendingClarification := clonePendingClarification(sessionMemory.PendingClarification)
	pendingClarificationResolution := pendingClarificationResolution{}
	pendingClarificationActive := false
	pendingMemoryChanged := false
	if isUsablePendingClarification(pendingClarification) {
		pendingClarificationResolution = r.resolvePendingClarification(ctx, *pendingClarification, message.Text, history)
		if pendingClarificationResolution.IsAnswer {
			pendingClarificationActive = true
			history = historyWithPendingClarification(*pendingClarification, history)
			sessionMemory.PendingClarification = nil
			pendingMemoryChanged = true
			if pendingRunID := strings.TrimSpace(pendingClarification.RunID); pendingRunID != "" && pendingRunID != runState.RunID {
				if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusCompleted); errShape != nil {
					return failStartedRun(errShape)
				}
				if errShape := r.resumeRunState(ctx, pendingRunID); errShape != nil {
					return failStartedRun(errShape)
				}
				resumedRun, err := r.stateStore.GetRun(ctx, pendingRunID)
				if err != nil {
					return failStartedRun(internalError("load resumed run state: "+err.Error(), contracts.ErrorSourceAgent))
				}
				runState = resumedRun
			}
		} else if pendingClarificationResolution.IsNewRequest || isPotentialWriteRequest(message.Text) || isLikelyReadRequest(message.Text) {
			sessionMemory.PendingClarification = nil
			pendingMemoryChanged = true
		}
	}
	if pendingMemoryChanged {
		if errShape := r.saveSessionMemory(ctx, message.SessionID, sessionMemory); errShape != nil {
			return failStartedRun(errShape)
		}
	}
	activeClarification := pendingClarificationActive || (hasActiveClarification(transcript) && isLikelyClarificationAnswer(message.Text))
	activeTranscript := []providers.Message(nil)
	if pendingClarificationActive {
		activeTranscript = activeClarificationTranscript(transcript, 8)
		if len(activeTranscript) == 0 {
			activeTranscript = pendingClarificationTranscript(*pendingClarification)
		}
	} else if activeClarification {
		history = activeClarificationHistoryForPrompt(transcript, 8)
		activeTranscript = activeClarificationTranscript(transcript, 8)
	}
	referenceResolution, errShape := r.resolveReference(ctx, message, history, sessionMemory, activeClarification)
	if errShape != nil {
		return failStartedRun(errShape)
	}
	hasReferenceCue := hasReferenceCueText(message.Text)
	resolvedReference := hasReferenceCue && isUsableReference(referenceResolution)
	resultFollowUp := !activeClarification &&
		((isLikelyResultFollowUpQuestion(message.Text) && (hasRecentActionResult(transcript) || hasRecentMemoryActionResult(sessionMemory))) ||
			(hasReferenceCue && isResultReferenceFollowUp(referenceResolution, message.Text)))
	contextualFollowUp := !activeClarification &&
		!resultFollowUp &&
		!resolvedReference &&
		isLikelyContextualFollowUpQuestion(message.Text, history, sessionMemory)
	standaloneReadRequest := !activeClarification &&
		!resultFollowUp &&
		!contextualFollowUp &&
		!resolvedReference &&
		shouldIsolateMemoryForStandaloneReadRequest(message.Text)
	isolatedNewWriteRequest := !resultFollowUp && !resolvedReference && shouldIsolateMemoryForNewRequest(message.Text, activeClarification)

	userMessage := providers.Message{Role: providers.MessageRoleUser, Content: messageTextWithAttachmentContext(message)}
	transcript = append(transcript, userMessage)
	if err := r.sessionStore.AppendMessage(ctx, message.SessionID, userMessage); err != nil {
		return failStartedRun(internalError("append user message: "+err.Error(), contracts.ErrorSourceSession))
	}
	if clarification := r.referenceClarificationResponse(message, referenceResolution); clarification != nil {
		if errShape := r.appendAssistantTranscript(ctx, message.SessionID, clarification.Message); errShape != nil {
			clarification.Error = errShape
			clarification.Message = errShape.Message
			clarification.Status = contracts.AgentStatusFailed
		}
		runState.Status = RuntimeRunStatusWaitingClarification
		if errShape := r.updateRunState(ctx, runState); errShape != nil {
			clarification.Error = errShape
			clarification.Message = errShape.Message
			clarification.Status = contracts.AgentStatusFailed
		}
		return *clarification, nil
	}

	understandingMessage := message
	if pendingClarificationActive {
		understandingMessage.Text = pendingClarificationResolution.UpdatedRequest
		if strings.TrimSpace(understandingMessage.Text) == "" {
			understandingMessage.Text = contextualPendingClarificationText(*pendingClarification, message.Text)
		}
	} else if activeClarification {
		understandingMessage.Text = contextualFollowUpText(history, message.Text)
	} else if resultFollowUp {
		understandingMessage.Text = contextualResultFollowUpText(history, message.Text)
	} else if contextualFollowUp {
		understandingMessage.Text = contextualConversationFollowUpText(history, sessionMemory, message.Text)
	} else if resolvedReference {
		understandingMessage.Text = contextualReferenceText(history, referenceResolution, message.Text)
	}
	understandingMessage.Text = textWithAttachmentContext(understandingMessage.Text, message.Metadata)

	providerTranscript := transcript
	providerMemory := sessionMemory
	providerReference := referenceResolution
	if isolatedNewWriteRequest || standaloneReadRequest {
		providerTranscript = []providers.Message{userMessage}
		providerMemory = sessions.SessionMemory{}
		providerReference = nil
	} else if activeClarification {
		providerUserMessage := userMessage
		providerUserMessage.Content = understandingMessage.Text
		providerTranscript = append(cloneProviderMessages(activeTranscript), providerUserMessage)
	} else if contextualFollowUp {
		providerTranscript = transcriptWithLastUserContent(transcript, understandingMessage.Text)
	}

	toolResults := []contracts.ToolResult{}
agentLoop:
	for iteration := 1; iteration <= r.maxIterations; iteration++ {
		runState.IterationCount = iteration
		if errShape := r.updateRunState(ctx, runState); errShape != nil {
			base.Error = errShape
			base.Message = errShape.Message
			return base, nil
		}
		r.logger.Debug("agent iteration started", "request_id", message.RequestID, "session_id", message.SessionID, "iteration", iteration)
		emitProgress(ctx, ProgressEvent{Stage: ProgressStageThinking, Message: "Agent is thinking"})
		providerMessages := r.withRuntimeSystemPrompt(providerTranscript, providerMemory, providerReference)
		providerResponse, err := r.provider.Chat(ctx, providers.ChatRequest{
			Model:      r.model,
			Messages:   providerMessages,
			Tools:      r.providerTools(),
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
			if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusFailed); errShape != nil {
				base.Error = errShape
			}
			base.Message = base.Error.Message
			return base, nil
		}

		assistantMessage := providerResponse.Message
		if assistantMessage.Role == "" {
			assistantMessage.Role = providers.MessageRoleAssistant
		}
		transcript = append(transcript, assistantMessage)
		providerTranscript = append(providerTranscript, assistantMessage)
		if err := r.sessionStore.AppendMessage(ctx, message.SessionID, assistantMessage); err != nil {
			return failStartedRun(internalError("append assistant message: "+err.Error(), contracts.ErrorSourceSession))
		}

		if len(assistantMessage.ToolCalls) == 0 {
			if shouldRetryTextualApprovalAsToolCall(assistantMessage.Content) {
				r.logger.Info("assistant requested approval without tool call; retrying for tool call",
					"request_id", message.RequestID,
					"session_id", message.SessionID,
					"iteration", iteration,
					"content_preview", logPreview(assistantMessage.Content, 180),
				)
				providerTranscript = append(providerTranscript, providers.Message{
					Role: providers.MessageRoleSystem,
					Content: strings.TrimSpace(`The previous assistant message asked the user to confirm an external write/destructive action in plain text, but did not call a tool.
Do not ask for approval in natural language.
If all required information is present, produce the matching tool call now.
The runtime will create the ApprovalRequest and channel buttons before execution.
If required information is missing, ask one concise clarification question instead of asking for confirmation.`),
				})
				continue
			}
			r.logger.Info("agent completed without tool calls",
				"request_id", message.RequestID,
				"session_id", message.SessionID,
				"iteration", iteration,
				"content_preview", logPreview(assistantMessage.Content, 180),
			)
			emitProgress(ctx, ProgressEvent{Stage: ProgressStageFinalizing, Message: "Agent is finalizing the response"})
			if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusCompleted); errShape != nil {
				base.Error = errShape
				base.Message = errShape.Message
				return base, nil
			}
			return contracts.AgentResponse{
				RequestID:   message.RequestID,
				SessionID:   message.SessionID,
				Status:      contracts.AgentStatusCompleted,
				Message:     assistantMessage.Content,
				Data:        r.traceData(referenceResolution),
				ToolResults: toolResults,
			}, nil
		}

		for index, providerToolCall := range assistantMessage.ToolCalls {
			evidenceText := providerTranscriptEvidenceText(providerTranscript)
			providerToolCall = sanitizeUnsupportedOptionalArguments(providerToolCall, evidenceText)
			if isClarifyToolCall(providerToolCall) {
				clarification := clarificationFromToolCall(providerToolCall)
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
				if errShape := r.appendAssistantTranscript(ctx, message.SessionID, clarification.question); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.storePendingClarification(ctx, message.SessionID, pendingClarificationFromToolCall(runState.RunID, message.Text, clarification.question, providerToolCall, stringSliceArg(providerToolCall.Arguments, "missing_fields"))); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				runState.Status = RuntimeRunStatusWaitingClarification
				runState.PendingClarificationID = providerToolCall.ID
				if errShape := r.updateRunState(ctx, runState); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				return contracts.AgentResponse{
					RequestID: message.RequestID,
					SessionID: message.SessionID,
					Status:    contracts.AgentStatusNeedClarification,
					Message:   clarification.question,
					Data:      r.traceData(referenceResolution),
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
			toolCallMissingFields := pendingMissingFieldsForToolCall(providerToolCall, definition, found, activeClarification, evidenceText)
			if clarification := r.toolCallClarificationResponse(message, providerToolCall, definition, found, activeClarification, evidenceText); clarification != nil {
				if shouldResolveChatSpaceBeforeClarification(providerToolCall) {
					toolMessage := providers.Message{
						Role:       providers.MessageRoleTool,
						ToolCallID: providerToolCall.ID,
						Content:    truncateToolContentForLLM(chatSpaceResolutionObservation(providerToolCall)),
					}
					transcript = append(transcript, toolMessage)
					providerTranscript = append(providerTranscript, toolMessage)
					if err := r.appendToolObservation(ctx, message.SessionID, transcript, toolMessage); err != nil {
						base.Error = err
						base.Message = err.Message
						return base, nil
					}
					for _, skipped := range skippedToolObservationMessages(assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because the current Google Chat target must be resolved first") {
						transcript = append(transcript, skipped)
						providerTranscript = append(providerTranscript, skipped)
						if err := r.appendToolObservation(ctx, message.SessionID, transcript, skipped); err != nil {
							base.Error = err
							base.Message = err.Message
							return base, nil
						}
					}
					continue agentLoop
				}
				if err := r.appendToolObservation(ctx, message.SessionID, transcript, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM("MISSING_REQUIRED_FIELD: " + clarification.Message),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservations(ctx, message.SessionID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because the current tool call needs clarification"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if errShape := r.appendAssistantTranscript(ctx, message.SessionID, clarification.Message); errShape != nil {
					clarification.Error = errShape
					clarification.Message = errShape.Message
					clarification.Status = contracts.AgentStatusFailed
					return *clarification, nil
				}
				if errShape := r.storePendingClarification(ctx, message.SessionID, pendingClarificationFromToolCall(runState.RunID, message.Text, clarification.Message, providerToolCall, toolCallMissingFields)); errShape != nil {
					clarification.Error = errShape
					clarification.Message = errShape.Message
					clarification.Status = contracts.AgentStatusFailed
				}
				runState.Status = RuntimeRunStatusWaitingClarification
				runState.PendingClarificationID = providerToolCall.ID
				if errShape := r.updateRunState(ctx, runState); errShape != nil {
					clarification.Error = errShape
					clarification.Message = errShape.Message
					clarification.Status = contracts.AgentStatusFailed
				}
				return *clarification, nil
			}
			switch decision.Decision {
			case contracts.RiskDecisionAllow:
				providerToolCall = normalizeProviderToolCall(r.now(), providerToolCall, message.Text)
				startedAt := time.Now()
				result := r.executeAllowedTool(ctx, providerToolCall, definition)
				if errShape := r.recordRuntimeToolCall(ctx, runState.RunID, providerToolCall, result, time.Since(startedAt)); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordActionResult(ctx, message.SessionID, result); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				contractResult := contractToolResult(result)
				toolResults = append(toolResults, contractResult)

				toolMessage := providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM(result.ContentForLLM),
				}
				transcript = append(transcript, toolMessage)
				providerTranscript = append(providerTranscript, toolMessage)
				if err := r.sessionStore.AppendMessage(ctx, message.SessionID, toolMessage); err != nil {
					base.Error = internalError("append tool message: "+err.Error(), contracts.ErrorSourceSession)
					base.Message = base.Error.Message
					return base, nil
				}
				if !result.Success {
					if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusFailed); errShape != nil {
						base.Error = errShape
						base.Message = errShape.Message
						return base, nil
					}
					base.ToolResults = toolResults
					base.Error = toolErrorShape(result)
					base.Message = base.Error.Message
					return base, nil
				}

			case contracts.RiskDecisionRequiresApproval:
				approval := r.approvalRequest(message, providerToolCall, decision)
				action, errShape := r.createApprovalAction(ctx, runState, message, providerToolCall, decision, approval)
				if errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if action.Status == ActionStatusCompleted && action.Result != nil {
					toolMessage := providers.Message{
						Role:       providers.MessageRoleTool,
						ToolCallID: providerToolCall.ID,
						Content:    truncateToolContentForLLM("ACTION_ALREADY_COMPLETED: " + action.Result.ContentForLLM),
					}
					transcript = append(transcript, toolMessage)
					providerTranscript = append(providerTranscript, toolMessage)
					if err := r.appendToolObservation(ctx, message.SessionID, transcript, toolMessage); err != nil {
						base.Error = err
						base.Message = err.Message
						return base, nil
					}
					continue agentLoop
				}
				approval.ApprovalID = action.ApprovalID
				approval.ToolCallID = action.ToolCallID
				approval.CreatedAt = action.CreatedAt
				approval.ExpiresAt = action.ApprovalExpiresAt
				approval.ToolCall.ToolCallID = action.ToolCallID
				approval.ToolCall.Input = cloneArguments(action.ArgsSnapshot)
				r.storePendingApproval(pendingApproval{
					runID:              runState.RunID,
					actionID:           action.ActionID,
					message:            message,
					request:            approval,
					toolCall:           providerToolCall,
					definition:         definition,
					remainingToolCalls: cloneProviderToolCalls(assistantMessage.ToolCalls[index+1:]),
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
				runState.Status = RuntimeRunStatusWaitingApproval
				runState.PendingActionID = action.ActionID
				if errShape := r.updateRunState(ctx, runState); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				return contracts.AgentResponse{
					RequestID:       message.RequestID,
					SessionID:       message.SessionID,
					Status:          contracts.AgentStatusApprovalRequired,
					Message:         approval.Summary,
					ApprovalID:      approval.ApprovalID,
					ApprovalRequest: &approval,
					Data:            r.traceData(referenceResolution),
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
				if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusBlocked); errShape != nil {
					base.Error = errShape
				}
				base.Message = base.Error.Message
				return base, nil
			}
		}
	}

	if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusMaxIterations); errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	return contracts.AgentResponse{
		RequestID:   message.RequestID,
		SessionID:   message.SessionID,
		Status:      contracts.AgentStatusFailed,
		Message:     "agent exceeded max iterations",
		Data:        r.traceData(referenceResolution),
		ToolResults: toolResults,
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorMaxIterationsExceeded,
			Message:   "agent exceeded max iterations",
			Source:    contracts.ErrorSourceAgent,
			Retryable: false,
		},
	}, nil
}
