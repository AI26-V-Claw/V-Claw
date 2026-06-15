package agent

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	agentintent "vclaw/internal/agent/intent"
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
	viTimeAnswerPattern = regexp.MustCompile(`(?i)\b\d{1,2}\s*(h|g|gio|gi?)(\s*\d{1,2})?\b`)
)

type RuntimeConfig struct {
	Provider                   providers.Provider
	Registry                   *tools.ToolRegistry
	Observer                   RuntimeObserver
	ReferenceResolver          reference.Resolver
	Policy                     policies.ToolPolicy
	SessionStore               sessions.Store
	StateStore                 RuntimeStateStore
	TurnRouter                 TurnRouter
	Logger                     *slog.Logger
	MaxIterations              int
	ToolTimeout                time.Duration
	ParallelExecutionEnabled   bool
	ParallelMaxWorkers         int
	ParallelToolTimeoutDefault time.Duration
	Model                      string
	Now                        func() time.Time
	Compactor                  *sessions.Compactor
	ContextWindow              int
	MemoryClassifierModel      string
}

type Runtime struct {
	provider                   providers.Provider
	registry                   *tools.ToolRegistry
	observer                   RuntimeObserver
	referenceResolver          reference.Resolver
	policy                     policies.ToolPolicy
	sessionStore               sessions.Store
	stateStore                 RuntimeStateStore
	turnRouter                 TurnRouter
	logger                     *slog.Logger
	approvalMu                 sync.Mutex
	pendingApprovals           map[string]pendingApproval
	pendingBySession           map[string]string
	maxIterations              int
	toolTimeout                time.Duration
	parallelExecutionEnabled   bool
	parallelMaxWorkers         int
	parallelToolTimeoutDefault time.Duration
	model                      string
	now                        func() time.Time
	compactor                  *sessions.Compactor
	contextWindow              int
	memoryClassifierModel      string
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

type TurnMode string

const (
	TurnModeNoTool      TurnMode = "no_tool"
	TurnModeToolEnabled TurnMode = "tool_enabled"
)

type TurnRouteInput struct {
	Message       string
	RecentHistory []string
	Now           time.Time
}

type TurnRoute struct {
	Mode   TurnMode
	Reason string
}

type TurnRouter interface {
	RouteTurn(ctx context.Context, input TurnRouteInput) (TurnRoute, error)
}

type TaskPlanResult struct {
	Plan contracts.Plan
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
	parallelMaxWorkers := config.ParallelMaxWorkers
	if parallelMaxWorkers < 1 {
		parallelMaxWorkers = 1
	}
	if parallelMaxWorkers > 8 {
		parallelMaxWorkers = 8
	}
	parallelToolTimeoutDefault := config.ParallelToolTimeoutDefault
	if parallelToolTimeoutDefault <= 0 {
		parallelToolTimeoutDefault = toolTimeout
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
	contextWindow := config.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 128_000
	}
	return &Runtime{
		provider:                   config.Provider,
		registry:                   config.Registry,
		observer:                   config.Observer,
		referenceResolver:          referenceResolver,
		policy:                     config.Policy,
		sessionStore:               sessionStore,
		stateStore:                 stateStore,
		turnRouter:                 config.TurnRouter,
		logger:                     logger,
		pendingApprovals:           make(map[string]pendingApproval),
		pendingBySession:           make(map[string]string),
		maxIterations:              maxIterations,
		toolTimeout:                toolTimeout,
		parallelExecutionEnabled:   config.ParallelExecutionEnabled,
		parallelMaxWorkers:         parallelMaxWorkers,
		parallelToolTimeoutDefault: parallelToolTimeoutDefault,
		model:                      config.Model,
		now:                        now,
		compactor:                  config.Compactor,
		contextWindow:              contextWindow,
		memoryClassifierModel:      memoryClassifierModel(config),
	}
}

func memoryClassifierModel(config RuntimeConfig) string {
	if model := strings.TrimSpace(config.MemoryClassifierModel); model != "" {
		return model
	}
	return strings.TrimSpace(config.Model)
}

func (r *Runtime) Run(ctx context.Context, message contracts.UserMessage) (contracts.AgentResponse, error) {
	if r.compactor != nil {
		sessionID := message.SessionID
		defer func() { go r.maybeCompactAsync(sessionID) }()
	}
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
	r.clearExpiredApprovalsForSession(ctx, message.SessionID)
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
	isRevision := isRevisionMessage(message)
	var turnMeta turnAnalysis
	if !isRevision {
		clarificationQuestion := ""
		if !pendingClarificationActive && hasActiveClarification(transcript) {
			clarificationQuestion = lastAssistantText(transcript)
		}
		turnMeta = r.analyzeTurn(ctx, turnAnalysisInput{
			Text:                        message.Text,
			History:                     history,
			Memory:                      sessionMemory,
			ActiveClarificationQuestion: clarificationQuestion,
			HasRecentResults:            hasRecentActionResult(transcript) || hasRecentMemoryActionResult(sessionMemory),
			CheckMemoryMode:             isPotentialWriteRequest(message.Text),
		})
	}
	activeClarification := pendingClarificationActive || (hasActiveClarification(transcript) && turnMeta.IsClarificationAnswer)
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
		((turnMeta.IsResultFollowUp && (hasRecentActionResult(transcript) || hasRecentMemoryActionResult(sessionMemory))) ||
			(hasReferenceCue && isResultReferenceFollowUp(referenceResolution, message.Text)))
	contextualFollowUp := !activeClarification &&
		!resultFollowUp &&
		!resolvedReference &&
		turnMeta.IsContextualFollowUp
	standaloneReadRequest := !isRevision &&
		!activeClarification &&
		!resultFollowUp &&
		!contextualFollowUp &&
		!resolvedReference &&
		shouldIsolateMemoryForStandaloneReadRequest(message.Text)
	isolatedNewWriteRequest := !isRevision &&
		!resultFollowUp &&
		!resolvedReference &&
		turnMeta.MemoryMode == memoryModeFresh &&
		shouldIsolateMemoryForNewRequest(message.Text, activeClarification)

	userMessage := providers.Message{Role: providers.MessageRoleUser, Content: messageTextWithAttachmentContext(message)}
	if _, isContinuation := message.Metadata["continuationOf"]; isContinuation {
		completedTool, _ := message.Metadata["completedTool"].(string)
		if completedTool != "" {
			userMessage.Content = fmt.Sprintf("[Ti?p t?c sau khi %s du?c xác nh?n và th?c thi]", completedTool)
		} else {
			userMessage.Content = "[Ti?p t?c sau khi hành d?ng du?c xác nh?n và th?c thi]"
		}
	}
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
		providerMessages := r.withRuntimeSystemPrompt(providerTranscript, providerMemory, providerReference, nil)
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

		evidenceText := providerTranscriptEvidenceText(providerTranscript)
		if batch, ok := r.prepareParallelBatch(
			assistantMessage.ToolCalls,
			r.parallelExecutionEnabled,
			message.Text,
			evidenceText,
			activeClarification,
		); ok {
			batchResults := executeParallelBatch(ctx, batch, ParallelConfig{
				MaxWorkers:         r.parallelMaxWorkers,
				ToolTimeoutDefault: r.parallelToolTimeoutDefault,
				OnStart: func(index int) {
					item := batch[index]
					r.logger.Info("parallel tool execution started",
						"tool_call_id", item.call.ID,
						"tool_name", item.call.Name,
						"arguments", logToolArguments(item.call.Name, item.call.Arguments),
					)
					emitProgress(ctx, ProgressEvent{
						Stage:      ProgressStageToolStarted,
						ToolName:   item.call.Name,
						ToolCallID: item.call.ID,
						Message:    "Tool started",
					})
				},
			})
			for i, outcome := range batchResults {
				call := batch[i].call
				result := outcome.result
				stage := ProgressStageToolCompleted
				if !result.Success {
					stage = ProgressStageToolFailed
				}
				r.logger.Info("parallel tool execution completed",
					"tool_call_id", call.ID,
					"tool_name", call.Name,
					"success", result.Success,
					"error_code", toolErrorCode(result),
					"duration", outcome.duration,
					"content_preview", logPreview(result.ContentForLLM, 260),
				)
				emitProgress(ctx, ProgressEvent{
					Stage:      stage,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Message:    "Tool finished",
				})
				if errShape := r.recordRuntimeToolCall(ctx, runState.RunID, call, result, outcome.duration); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordActionResult(ctx, message.SessionID, result); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				toolResults = append(toolResults, contractToolResult(result))
				toolMessage := providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: call.ID,
					Content:    truncateToolContentForLLM(result.ContentForLLM),
				}
				transcript = append(transcript, toolMessage)
				providerTranscript = append(providerTranscript, toolMessage)
				if err := r.sessionStore.AppendMessage(ctx, message.SessionID, toolMessage); err != nil {
					base.Error = internalError("append tool message: "+err.Error(), contracts.ErrorSourceSession)
					base.Message = base.Error.Message
					return base, nil
				}
			}
			if isBatchSystemError(batchResults) {
				if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusFailed); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				base.ToolResults = toolResults
				base.Error = toolErrorShape(batchResults[0].result)
				base.Message = base.Error.Message
				return base, nil
			}
			continue agentLoop
		}

		for index, providerToolCall := range assistantMessage.ToolCalls {
			evidenceText := providerTranscriptEvidenceText(providerTranscript)
			// currentRequestText contains only the current user message for evidence checks
			// that must verify the user explicitly stated information in *this* request.
			// Using the full evidenceText causes false positives when historical tool results
			// contain times, titles, or emails that the user never mentioned in the current turn.
			currentRequestText := message.Text
			providerToolCall = sanitizeUnsupportedOptionalArguments(providerToolCall, evidenceText)
			providerToolCall = applyChannelToolDefaults(message, providerToolCall)
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
			toolCallMissingFields := pendingMissingFieldsForToolCall(providerToolCall, definition, found, activeClarification, currentRequestText)
			if clarification := r.toolCallClarificationResponse(message, providerToolCall, definition, found, activeClarification, currentRequestText); clarification != nil {
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
				if resp := r.handleContextError(ctx, runState, toolResults); resp != nil {
					resp.RequestID = message.RequestID
					resp.SessionID = message.SessionID
					return *resp, nil
				}
				contractResult := contractToolResult(result)
				toolResults = append(toolResults, contractResult)

				toolMessage := providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM(r.toolContentForProvider(providerToolCall.Name, result.ContentForLLM)),
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
				r.recordApprovalObservation(ActionStatusPendingApproval)
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
				r.logger.Info("agent loop exited after proposing approval-required tool",
					"request_id", message.RequestID,
					"session_id", message.SessionID,
					"tool_call_id", providerToolCall.ID,
					"tool_name", providerToolCall.Name,
					"approval_id", approval.ApprovalID,
					"risk_level", approval.RiskLevel,
					"waiting_for_approval", true,
				)
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
				base.Status = contracts.AgentStatusBlocked
				base.Message = "Hành d?ng này không du?c phép th?c hi?n do chính sách b?o m?t hi?n t?i."
				base.Error = &contracts.ErrorShape{
					Code:      policyErrorCode(found),
					Message:   base.Message,
					Source:    contracts.ErrorSourcePolicy,
					Retryable: false,
				}
				if errShape := r.finishRunState(ctx, runState, RuntimeRunStatusBlocked); errShape != nil {
					base.Error = errShape
				}
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

func (r *Runtime) legacyHasPendingApproval(_ context.Context, sessionID string) bool {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	approvalID := r.pendingBySession[strings.TrimSpace(sessionID)]
	if approvalID == "" {
		return false
	}
	_, ok := r.pendingApprovals[approvalID]
	return ok
}

func (r *Runtime) legacyResolveApproval(ctx context.Context, sessionID string, decision contracts.ApprovalDecision) (contracts.AgentResponse, error) {
	switch decision.Decision {
	case contracts.ApprovalDecisionRevised:
		return r.ReviseApproval(ctx, sessionID, decision.RequestID, decision.ApprovalID, decision.Comment)
	}

	pending, ok := r.takePendingApproval(sessionID, decision.ApprovalID)
	if !ok {
		r.logger.Info("approval request lookup failed or was already resolved",
			"request_id", decision.RequestID,
			"session_id", sessionID,
			"approval_id", strings.TrimSpace(decision.ApprovalID),
		)
		return contracts.AgentResponse{
			RequestID: decision.RequestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không tìm th?y yêu c?u xác nh?n dang ch?.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalNotFound,
				Message:   "pending approval not found",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	if pending.request.ExpiresAt.Before(r.now()) {
		r.logger.Info("approval decision received for expired request",
			"request_id", decision.RequestID,
			"session_id", sessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusExpired,
		)
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Yêu c?u xác nh?n dã h?t h?n. Vui lòng g?i l?i yêu c?u.",
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
		r.logger.Info("approval request status updated",
			"request_id", pending.message.RequestID,
			"session_id", pending.message.SessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusApproved,
		)
		r.logger.Info("runtime resuming pending tool after approval",
			"request_id", pending.message.RequestID,
			"session_id", pending.message.SessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
		)
		result := r.executeAllowedTool(ctx, pending.toolCall, pending.definition)
		if errShape := r.recordActionResult(ctx, pending.message.SessionID, result); errShape != nil {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusFailed,
				Message:   errShape.Message,
				Error:     errShape,
			}, nil
		}
		contractResult := contractToolResult(result)
		response := contracts.AgentResponse{
			RequestID:   pending.message.RequestID,
			SessionID:   pending.message.SessionID,
			Status:      contracts.AgentStatusCompleted,
			Data:        r.traceData(nil, nil, nil),
			ToolResults: []contracts.ToolResult{contractResult},
		}
		response.Message = approvalExecutionMessage(result, contractResult)
		if !result.Success {
			response.Status = contracts.AgentStatusFailed
			response.Error = toolErrorShape(result)
			response.Message = response.Error.Message
		}
		if errShape := r.appendAssistantTranscript(ctx, pending.message.SessionID, response.Message); errShape != nil {
			response.Status = contracts.AgentStatusFailed
			response.Error = errShape
			response.Message = errShape.Message
		}
		// After a successful approval, always run a continuation pass so that
		// remaining tasks from the original multi-step request are not lost.
		// If remainingToolCalls is non-empty (same-batch siblings), the continuation
		// replays them explicitly. If empty, the continuation gives the LLM a chance
		// to detect and execute any tasks from the original request not yet done.
		if result.Success {
			continuation := buildApprovalContinuationMessage(pending, result, r.now())
			r.logger.Info("runtime received resume signal for continuation after approval",
				"request_id", pending.message.RequestID,
				"session_id", pending.message.SessionID,
				"approval_id", pending.request.ApprovalID,
				"tool_call_id", pending.request.ToolCallID,
			)
			if continuationResp, err := r.Run(ctx, continuation); err == nil {
				return continuationResp, nil
			}
		}
		return response, nil
	case contracts.ApprovalDecisionRejected:
		r.logger.Info("approval request status updated",
			"request_id", pending.message.RequestID,
			"session_id", pending.message.SessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusRejected,
		)
		comment := strings.TrimSpace(decision.Comment)
		if comment != "" {
			return contracts.AgentResponse{
				RequestID: pending.message.RequestID,
				SessionID: pending.message.SessionID,
				Status:    contracts.AgentStatusNeedClarification,
				Message:   "Ðã h?y thao tác dang ch?. B?n mu?n ch?nh l?i nhu th? nào?\n\nGhi chú c?a b?n: " + comment,
				Data:      r.traceData(nil, nil, nil),
			}, nil
		}
		return contracts.AgentResponse{
			RequestID: pending.message.RequestID,
			SessionID: pending.message.SessionID,
			Status:    contracts.AgentStatusBlocked,
			Message:   "Ðã h?y thao tác. Tôi chua th?c hi?n tool nào.",
			Data:      r.traceData(nil, nil, nil),
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
			Message:   "Quy?t d?nh xác nh?n không h?p l?.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorInvalidInput,
				Message:   "approval decision must be approved, rejected, or revised",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}
}

func (r *Runtime) legacyReviseApproval(ctx context.Context, sessionID string, requestID string, approvalID string, comment string) (contracts.AgentResponse, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = "Tôi mu?n ch?nh l?i yêu c?u dang ch? xác nh?n."
	}
	pending, ok := r.takePendingApproval(sessionID, approvalID)
	if !ok {
		r.logger.Info("approval request lookup failed",
			"request_id", requestID,
			"session_id", sessionID,
			"approval_id", strings.TrimSpace(approvalID),
		)
		return contracts.AgentResponse{
			RequestID: requestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Không tìm th?y yêu c?u xác nh?n dang ch?.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalNotFound,
				Message:   "pending approval not found",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}
	if pending.request.ExpiresAt.Before(r.now()) {
		r.logger.Info("approval decision received for expired request",
			"request_id", requestID,
			"session_id", sessionID,
			"approval_id", pending.request.ApprovalID,
			"tool_call_id", pending.request.ToolCallID,
			"old_status", pending.request.Status,
			"new_status", contracts.ApprovalStatusExpired,
		)
		return contracts.AgentResponse{
			RequestID: requestID,
			SessionID: sessionID,
			Status:    contracts.AgentStatusFailed,
			Message:   "Yêu c?u xác nh?n dã h?t h?n. Vui lòng g?i l?i yêu c?u.",
			Error: &contracts.ErrorShape{
				Code:      contracts.ErrorApprovalExpired,
				Message:   "approval expired",
				Source:    contracts.ErrorSourceAgent,
				Retryable: false,
			},
		}, nil
	}

	revisionText := buildRevisionRequest(pending, comment)
	revisionMessage := pending.message
	revisionMessage.RequestID = strings.TrimSpace(requestID)
	if revisionMessage.RequestID == "" {
		revisionMessage.RequestID = pending.message.RequestID
	}
	revisionMessage.Text = revisionText
	revisionMessage.Timestamp = r.now()
	if revisionMessage.Metadata == nil {
		revisionMessage.Metadata = map[string]any{}
	}
	revisionMessage.Metadata["approvalId"] = pending.request.ApprovalID
	revisionMessage.Metadata["parentApprovalId"] = pending.request.ApprovalID
	revisionMessage.Metadata["revisionComment"] = comment
	r.logger.Info("approval request status updated",
		"request_id", pending.message.RequestID,
		"session_id", pending.message.SessionID,
		"approval_id", pending.request.ApprovalID,
		"tool_call_id", pending.request.ToolCallID,
		"old_status", pending.request.Status,
		"new_status", contracts.ApprovalStatusRevised,
	)

	return r.Run(ctx, revisionMessage)
}

func (r *Runtime) withRuntimeSystemPrompt(transcript []providers.Message, memory sessions.SessionMemory, resolution *reference.Resolution, route *TurnRoute) []providers.Message {
	messages := make([]providers.Message, 0, len(transcript)+4)
	messages = append(messages, providers.Message{
		Role:    providers.MessageRoleSystem,
		Content: runtimeSystemPrompt(r.now()),
	})
	if prompt := sessionMemoryPrompt(memory); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	if prompt := referenceContextPrompt(resolution); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	if prompt := routeContextPrompt(route); prompt != "" {
		messages = append(messages, providers.Message{
			Role:    providers.MessageRoleSystem,
			Content: prompt,
		})
	}
	messages = append(messages, sanitizeProviderTranscriptForToolProtocol(transcript)...)
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
If the user writes in Vietnamese, always answer in Vietnamese even when tool results, system context, revision prompts, or memory snippets are in English.
Use available tools when the user asks for information that a tool can retrieve or compute.
Do not answer explicit Google Workspace read requests from conversation memory alone. If the current user asks for Gmail, Calendar, Chat, or People data for a concrete date/range/query, call the matching read tool.
Never claim that an external action was completed unless a tool result confirms it.
For write, destructive, local file, or code execution actions, propose the action through the matching tool call; the runtime will stop for human approval before execution.
When the user asks for multiple actions in one request (multi-step task), generate ALL required tool calls in a single response — do not wait for intermediate results before producing the next tool call, unless the next call strictly depends on an output (such as an ID) that cannot be known until the first call completes. The runtime processes approvals sequentially and resumes remaining tool calls automatically; generating them all upfront preserves the full multi-step plan.
When tools are available and required details are missing, call clarify with one concise question instead of inventing values. In no-tool mode, ask normally if the conversation needs it.
Keep final answers concise and include the useful result, not internal implementation details.

Current date and time: %s.
When users ask about relative dates or ranges, convert them to concrete tool arguments before calling tools.
For calendar.listEvents:
- "today" / "hôm nay" means local start of today through local start of tomorrow.
- "this week" / "tu?n này" means Monday 00:00 through next Monday 00:00 in the current local timezone.
- "next week" / "tu?n sau" means next Monday 00:00 through the following Monday 00:00.
- For a date range, set timeMin to the beginning of the range and timeMax to the exclusive end of the range.
- Do not put date words like "today", "this week", "hôm nay", or "tu?n này" into query. Use query only for event title, description, location, or attendee keywords.
For gmail.listEmails and gmail.listThreads:
- Use after and before as date-only YYYY-MM-DD values, not RFC3339 datetimes.
- "today" / "hôm nay" means after is today's local date and before is tomorrow's local date.
- Do not put date words like "today", "this week", "hôm nay", or "tu?n này" into query. Use query only for sender, subject, body, or Gmail search terms.
- gmail.listEmails returns message summaries only. It does not include attachment metadata.
- If you need to check whether an email has attachments or to get attachmentId values, call gmail.getEmail on the messageId first.
Gmail date rules, restated in ASCII:
- gmail.listEmails and gmail.listThreads after/before must be date-only YYYY-MM-DD, never RFC3339 datetime strings.
- "today" / "hom nay" means after=today local date and before=tomorrow local date.
- Keep relative date words out of Gmail query; query is only for sender, subject, body, labels, or Gmail search terms.
- Sent mail rule: "mail/email toi da gui toi/cho <email>" means query "in:sent to:<email>" with labelIds ["SENT"].
For sending email (g?i email / send email):
- Sending an email is a two-step process: first call gmail.createDraft to compose the draft, then call gmail.sendDraft with the draftId returned by createDraft to actually deliver it.
- gmail.createDraft alone does NOT send the email — the draft sits unsent until gmail.sendDraft is called.
- When the user asks to send (not draft) an email, you MUST plan to call both tools. Because sendDraft depends on the draftId from createDraft, generate createDraft first; after it is approved and the draftId is returned, call sendDraft in the continuation.
- Do not consider the email task complete after createDraft succeeds — it is only complete after sendDraft succeeds.
For calendar.createEvent and calendar.updateEvent:
- Attendees must be valid email addresses.
- If the user provides a person name instead of an email address, call people.searchDirectory first and use the resolved Workspace email.
- Do not pass display names like "Bao" or "Tung" into attendees.
- If no matching email can be resolved, ask one concise clarification question for the attendee email.
For Google Chat tools:
- chat.sendMessage, chat.listMessages, chat.listMembers, and chat.addMember require space to be a Google Chat resource name like spaces/AAAA.
- If the user gives a group name, person name, or email instead of spaces/AAAA, do not put that raw name into space.
- Resolve the target first with people.searchDirectory plus chat.findSpacesByMembers when the user names people, or chat.listSpaces when the user names a space/group.
- For requests like "g?i tin nh?n vào nhóm chat VClaw" or "g?i file này vào nhóm VClaw", call chat.listSpaces first, match the requested group/display name from the returned spaces, then call chat.sendMessage with the matched spaces/... resource.
- Do not ask the user to provide spaces/AAAA until chat.listSpaces or member resolution has already failed or returned ambiguous matches.
- If the target space is still ambiguous after read-tool resolution, ask one concise clarification question before calling a write tool.
For channel attachments:
- If the user message contains "Attachment paths:", those are local files sent through the current channel.
- If the user says "file này", "file tôi dã g?i", "?nh này", or asks to attach/send/upload the current file, use those paths in tool arguments that accept attachments.
- For Gmail drafts, use attachment paths in gmail.createDraft/gmail.updateDraft/gmail.replyDraft/gmail.forwardDraft attachments.
- For Google Chat messages, use attachment paths in chat.sendMessage attachments.
- Do not call gmail.downloadAttachments unless the user explicitly wants to download an attachment from an existing Gmail message.

Format final answers for chat channels:
- Start with one short summary line.
- For Gmail, Calendar, Chat, or People results, use compact bullets with the important fields only.
- Prefer 5 to 10 bullets unless the user asks for more.
- For Gmail list results, if the user asks to list every email, include every message in Messages and do not group by sender unless the user asks for unique senders.
- For Gmail list results, group relative-date answers by LocalDate. Date is the original email header and may use a different timezone.
- Do not dump raw JSON, raw tool outputs, internal tool names, or opaque IDs unless the user explicitly asks.
- Use plain text only. Do not use Markdown bold, italic, inline code, headings, or syntax markers like **, __, backticks, or #.
- Avoid Markdown tables because Telegram renders them poorly in plain text.
- If no relevant result is found, say that plainly and suggest the next useful query.`, now.Format(time.RFC3339)))
}

func (r *Runtime) resolveReference(ctx context.Context, message contracts.UserMessage, recentHistory []string, memory sessions.SessionMemory, activeClarification bool) (*reference.Resolution, *contracts.ErrorShape) {
	if r.referenceResolver == nil || activeClarification {
		return nil, nil
	}
	// Revision messages are structured internal requests built by buildRevisionRequest;
	// they contain tool names and keywords that would falsely trigger reference resolution.
	if isRevisionMessage(message) {
		return nil, nil
	}
	if !hasReferenceCueText(message.Text) {
		return nil, nil
	}
	resolution, err := r.referenceResolver.Resolve(ctx, reference.Input{
		CurrentMessage: message.Text,
		RecentHistory:  recentHistory,
		Memory:         memory,
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
			Message:   "reference resolution failed: " + err.Error(),
			Source:    contracts.ErrorSourceProvider,
			Retryable: retryable,
		}
	}
	if resolution == nil {
		return nil, nil
	}
	r.logger.Info("reference resolved",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"has_reference", resolution.HasReference,
		"reference_type", resolution.ReferenceType,
		"source", resolution.Source,
		"confidence", resolution.Confidence,
		"needs_clarification", resolution.NeedsClarification,
	)
	return resolution, nil
}

func (r *Runtime) referenceClarificationResponse(message contracts.UserMessage, resolution *reference.Resolution) *contracts.AgentResponse {
	if resolution == nil || !resolution.HasReference || !resolution.NeedsClarification {
		return nil
	}
	if !hasReferenceCueText(message.Text) {
		return nil
	}
	question := strings.TrimSpace(resolution.ClarificationQuestion)
	if question == "" {
		question = "B?n mu?n nói t?i m?c nào trong cu?c trò chuy?n tru?c dó?"
	}
	return &contracts.AgentResponse{
		RequestID: message.RequestID,
		SessionID: message.SessionID,
		Status:    contracts.AgentStatusNeedClarification,
		Message:   question,
		Data:      r.traceData(nil, nil, resolution),
	}
}

func referenceContextPrompt(resolution *reference.Resolution) string {
	if !isUsableReference(resolution) {
		return ""
	}
	context := "{}"
	if resolution.ResolvedContext != nil {
		if data, err := json.MarshalIndent(resolution.ResolvedContext, "", "  "); err == nil {
			context = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`Reference resolver result for the current user message:
- has_reference: %t
- reference_type: %s
- reference_id: %s
- source: %s
- confidence: %.2f
- resolved_context:
%s

Use this only to understand phrases like "l?ch này", "cu?c h?p trên", "email v?a r?i", or "ch? d? dó".
Do not expose this resolver output directly to the user.
Do not use reference memory as approval. For any write/destructive action, still call the matching tool and let runtime request approval before execution.`,
		resolution.HasReference,
		resolution.ReferenceType,
		strings.TrimSpace(resolution.ReferenceID),
		resolution.Source,
		resolution.Confidence,
		context,
	))
}

func (r *Runtime) routeTurn(ctx context.Context, message contracts.UserMessage, recentHistory []string) (*TurnRoute, *contracts.ErrorShape) {
	if r.turnRouter == nil {
		route := TurnRoute{Mode: TurnModeToolEnabled, Reason: "router unavailable; exposing tools by default"}
		return &route, nil
	}
	// Continuation and revision messages are internally-generated trusted messages.
	// Skip the LLM turn router and expose tools directly so these messages are never
	// blocked or misclassified as prompt injection.
	if isRevisionMessage(message) {
		route := TurnRoute{Mode: TurnModeToolEnabled, Reason: "continuation/revision message; tools enabled by runtime"}
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

func (r *Runtime) providerTools() []providers.ToolDefinition {
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

func shouldForceToolEnabledForContextualDataFollowUp(route *TurnRoute, text string, history []string, memory sessions.SessionMemory) bool {
	if route == nil || route.Mode != TurnModeNoTool {
		return false
	}
	lower := foldVietnameseSearchText(strings.ToLower(strings.TrimSpace(text)))
	if lower == "" {
		return false
	}
	hasFollowUpCue := containsAnyText(lower,
		"thi sao", "con",
		"hom qua", "hom nay", "ngay mai",
		"tuan nay", "tuan truoc", "tuan sau",
		"thang nay", "thang truoc", "thang sau", "thang toi",
	)
	if !hasFollowUpCue {
		return false
	}
	context := foldVietnameseSearchText(strings.ToLower(strings.Join(history, "\n") + "\n" + memory.Summary))
	for _, result := range memory.LastActionResults {
		context += "\n" + foldVietnameseSearchText(strings.ToLower(result.ToolName+" "+result.Content))
	}
	return containsAnyText(context,
		"calendar", "lich", "calendar.listevents",
		"gmail", "email", "mail", "gmail.listemails", "gmail.listthreads",
		"google chat", "chat", "chat.listmessages",
	)
}

func shouldRetryTextualApprovalAsToolCall(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return false
	}
	if !containsAnyText(lower, "xác nh?n", "xac nhan", "confirm", "ti?n hành", "tien hanh") {
		return false
	}
	return containsAnyText(lower,
		"t?o", "tao", "create",
		"g?i", "gui", "send",
		"xóa", "xoa", "delete",
		"c?p nh?t", "cap nhat", "update",
	)
}

func isSideEffectToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "calendar.createEvent",
		"calendar.updateEvent",
		"calendar.deleteEvent",
		"gmail.createDraft",
		"gmail.updateDraft",
		"gmail.sendDraft",
		"gmail.deleteDraft",
		"gmail.replyDraft",
		"gmail.forwardDraft",
		"gmail.downloadAttachments",
		"gmail.modifyMessage",
		"gmail.batchModifyMessages",
		"gmail.trashMessage",
		"gmail.untrashMessage",
		"chat.sendMessage",
		"chat.updateMessage",
		"chat.deleteMessage",
		"chat.createSpace",
		"chat.addMember",
		"chat.removeMember",
		"sandbox.runPython",
		"sandbox.runShell":
		return true
	default:
		return false
	}
}

func (r *Runtime) legacyToolCallClarificationResponse(message contracts.UserMessage, toolCall providers.ToolCall, definition tools.ToolDefinition, found bool, activeClarification bool, evidenceText string) *contracts.AgentResponse {
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
			Data:      r.traceData(nil, nil, nil),
		}
	}
	if malformed := malformedToolArguments(toolCall); len(malformed) > 0 {
		return &contracts.AgentResponse{
			RequestID: message.RequestID,
			SessionID: message.SessionID,
			Status:    contracts.AgentStatusNeedClarification,
			Message:   missingToolArgumentQuestion(toolCall.Name, malformed),
			Data:      r.traceData(nil, nil, nil),
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
			Data:      r.traceData(nil, nil, nil),
		}
	}
	return nil
}

func legacyShouldResolveChatSpaceBeforeClarification(toolCall providers.ToolCall) bool {
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

func legacyChatSpaceResolutionObservation(toolCall providers.ToolCall) string {
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

func legacyPendingMissingFieldsForToolCall(toolCall providers.ToolCall, definition tools.ToolDefinition, found bool, activeClarification bool, evidenceText string) []string {
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

func legacySanitizeUnsupportedOptionalArguments(toolCall providers.ToolCall, evidenceText string) providers.ToolCall {
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

func legacyHasAttendeeEvidence(evidenceText string, attendees any) bool {
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

func legacyAttendeeStrings(value any) []string {
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

func legacyMissingRequiredArguments(schema tools.ToolSchema, args map[string]any) []string {
	required := requiredFieldsFromToolSchema(schema)
	missing := make([]string, 0, len(required))
	for _, field := range required {
		if isEmptyArgument(args[field]) {
			missing = append(missing, field)
		}
	}
	return missing
}

func legacyMalformedToolArguments(toolCall providers.ToolCall) []string {
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

func legacyContainsSpaceResourceName(value any) bool {
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

func legacyRequiredFieldsFromToolSchema(schema tools.ToolSchema) []string {
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

func legacyIsEmptyArgument(value any) bool {
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

func legacyMissingCurrentRequestEvidence(userText string, toolCall providers.ToolCall) []string {
	switch toolCall.Name {
	case "calendar.createEvent":
		return missingCalendarCreateEventEvidence(userText, toolCall.Arguments)
	default:
		return nil
	}
}

func legacyMissingCalendarCreateEventEvidence(userText string, args map[string]any) []string {
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

func legacyHasCalendarTitleEvidence(lowerText string, title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))
	if title != "" && strings.Contains(lowerText, title) {
		return true
	}
	return containsAnyText(lowerText,
		"tiêu d?", "tieu de", "ch? d?", "chu de", "n?i dung", "noi dung",
		"v? ", "ve ", "h?p v?", "hop ve", "meeting about",
	)
}

func legacyHasCalendarStartEvidence(lowerText string) bool {
	return hasTimeExpression(lowerText) ||
		containsAnyText(lowerText,
			"hôm nay", "hom nay", "ngày mai", "ngay mai",
			"tu?n này", "tuan nay", "tu?n sau", "tuan sau",
			"tháng này", "thang nay", "tháng t?i", "thang toi", "tháng sau", "thang sau",
			"today", "tomorrow", "this week", "next week", "this month", "next month",
		)
}

func legacyHasCalendarEndEvidence(lowerText string) bool {
	if containsAnyText(lowerText,
		"d?n", "den", "t?i", "toi", "k?t thúc", "ket thuc",
		"th?i lu?ng", "thoi luong", "trong vòng", "trong vong",
		"ti?ng", "tieng", "gi?", "gio", "phút", "phut",
		"hour", "hours", "minute", "minutes",
	) {
		return true
	}
	return countTimeExpressions(lowerText) >= 2 ||
		(strings.Contains(lowerText, "-") && hasTimeExpression(lowerText))
}

func legacyHasTimeExpression(text string) bool {
	return timeAnswerPattern.MatchString(text) || viTimeAnswerPattern.MatchString(text)
}

func legacyCountTimeExpressions(text string) int {
	return len(timeAnswerPattern.FindAllString(text, -1)) + len(viTimeAnswerPattern.FindAllString(text, -1))
}

func legacyStringArgument(args map[string]any, key string) string {
	value, ok := args[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func legacyMissingToolArgumentQuestion(toolName string, missing []string) string {
	if strings.HasPrefix(toolName, "chat.") && containsString(missing, "space") {
		return "B?n mu?n thao tác v?i Google Chat space nào? Hãy g?i resource name d?ng spaces/AAAA, ho?c nói rõ tên nhóm/ngu?i trong chat d? tôi tìm space tru?c."
	}
	if toolName == "calendar.createEvent" {
		if containsString(missing, "title") && containsString(missing, "start") {
			return "B?n mu?n t?o l?ch v?i tiêu d? gì, vào ngày gi? nào, và k?t thúc lúc m?y gi??"
		}
		if containsString(missing, "start") {
			return "B?n mu?n t?o l?ch vào ngày và gi? nào?"
		}
		if containsString(missing, "end") {
			return "B?n có th? cung c?p gi? k?t thúc ho?c th?i lu?ng c?a cu?c h?p không?"
		}
		if containsString(missing, "title") {
			return "B?n mu?n d?t tiêu d? cu?c h?p là gì?"
		}
	}
	return "B?n có th? b? sung thông tin còn thi?u cho " + toolName + ": " + strings.Join(missing, ", ") + "?"
}

func legacyContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (r *Runtime) legacyResolvePendingClarification(ctx context.Context, pending sessions.PendingClarification, userAnswer string, recentHistory []string) pendingClarificationResolution {
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
	if err := json.Unmarshal([]byte(extractPlannerJSONObject(resp.Text)), &resolved); err != nil {
		r.logger.Warn("pending clarification resolver returned invalid JSON; using heuristic fallback", "error", err, "response_preview", logPreview(resp.Text, 200))
		return fallback
	}
	resolved.UpdatedRequest = strings.TrimSpace(resolved.UpdatedRequest)
	resolved.Reason = strings.TrimSpace(resolved.Reason)
	if resolved.IsAnswer && resolved.UpdatedRequest == "" {
		resolved.UpdatedRequest = contextualPendingClarificationText(pending, userAnswer)
	}
	if !resolved.IsAnswer && fallback.IsAnswer {
		return fallback
	}
	return resolved
}

func legacyPendingClarificationResolverSystemPrompt() string {
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

func legacyPendingClarificationResolverUserPrompt(pending sessions.PendingClarification, userAnswer string, recentHistory []string) string {
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

func legacyFallbackPendingClarificationResolution(pending sessions.PendingClarification, userAnswer string) pendingClarificationResolution {
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

func legacyContextualPendingClarificationText(pending sessions.PendingClarification, userAnswer string) string {
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

func legacyHistoryWithPendingClarification(pending sessions.PendingClarification, history []string) []string {
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

func legacyPendingClarificationTranscript(pending sessions.PendingClarification) []providers.Message {
	messages := []providers.Message{}
	if strings.TrimSpace(pending.OriginalRequest) != "" {
		messages = append(messages, providers.Message{Role: providers.MessageRoleUser, Content: pending.OriginalRequest})
	}
	if strings.TrimSpace(pending.Question) != "" {
		messages = append(messages, providers.Message{Role: providers.MessageRoleAssistant, Content: pending.Question})
	}
	return messages
}

func legacyIsUsablePendingClarification(pending *sessions.PendingClarification) bool {
	return pending != nil &&
		(strings.TrimSpace(pending.OriginalRequest) != "" || strings.TrimSpace(pending.Question) != "")
}

func legacyClonePendingClarification(pending *sessions.PendingClarification) *sessions.PendingClarification {
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

func (r *Runtime) legacyStorePendingClarification(ctx context.Context, sessionID string, pending *sessions.PendingClarification) *contracts.ErrorShape {
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

func legacyPendingClarificationFromToolCall(runID string, originalRequest string, question string, toolCall providers.ToolCall, missing []string) *sessions.PendingClarification {
	return &sessions.PendingClarification{
		RunID:           strings.TrimSpace(runID),
		OriginalRequest: strings.TrimSpace(originalRequest),
		Question:        strings.TrimSpace(question),
		ToolName:        strings.TrimSpace(toolCall.Name),
		MissingFields:   append([]string(nil), missing...),
		PartialInput:    cloneAnyMap(toolCall.Arguments),
	}
}

func legacyCloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func sessionMemoryPrompt(memory sessions.SessionMemory) string {
	parts := []string{}
	if strings.TrimSpace(memory.Summary) != "" {
		parts = append(parts, "Conversation summary:\n"+strings.TrimSpace(memory.Summary))
	}
	if len(memory.LastActionResults) > 0 {
		lines := make([]string, 0, len(memory.LastActionResults))
		for _, result := range memory.LastActionResults {
			content := strings.TrimSpace(result.Content)
			if content == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(result.ToolName), truncateToolContentForLLM(content)))
		}
		if len(lines) > 0 {
			parts = append(parts, "Recent action results:\n"+strings.Join(lines, "\n"))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(`Session memory for understanding context only.
Use this memory to answer follow-up questions and maintain conversational continuity.
Do not use memory alone to fill required parameters for a new write, destructive, local file, or code execution action.
If the current user message does not explicitly provide required write parameters, ask a concise clarification question.

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

func responsePlan(planResult *TaskPlanResult) *contracts.Plan {
	if planResult == nil || len(planResult.Plan.Steps) == 0 {
		return nil
	}
	plan := planResult.Plan
	return &plan
}

func (r *Runtime) traceData(parts ...any) map[string]any {
	var classification *agentintent.ClassificationOutput
	var planResult *TaskPlanResult
	var resolution *reference.Resolution
	var routes []*TurnRoute
	for _, part := range parts {
		switch typed := part.(type) {
		case *agentintent.ClassificationOutput:
			classification = typed
		case *TaskPlanResult:
			planResult = typed
		case *reference.Resolution:
			resolution = typed
		case *TurnRoute:
			if typed != nil {
				routes = append(routes, typed)
			}
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
	if strings.TrimSpace(memory.Summary) == strings.TrimSpace(summary) {
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
	if len(memory.LastActionResults) > 5 {
		memory.LastActionResults = memory.LastActionResults[len(memory.LastActionResults)-5:]
	}
	return r.saveSessionMemory(ctx, sessionID, memory)
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
			strings.Contains(lower, "có th?") ||
			strings.Contains(lower, "co the") ||
			strings.Contains(lower, "b? sung") ||
			strings.Contains(lower, "bo sung") ||
			strings.Contains(lower, "cung c?p") ||
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
		"không có", "khong co", "không c?n", "khong can",
		"thêm", "them", "d?i", "doi", "s?a thành", "sua thanh",
		"tiêu d?", "tieu de", "n?i dung", "noi dung",
		"d?a di?m", "dia diem", "ngu?i tham gia", "nguoi tham gia",
		"th?i gian", "thoi gian", "gi?", "gio", "ti?ng", "tieng", "phút", "phut",
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
		"calendar", "l?ch", "lich",
		"gmail", "email", "mail",
		"google chat", "chat", "space", "nhóm", "nhom",
	)
	if !hasDomain {
		return false
	}
	hasConcreteScope := containsAnyText(lower,
		"hôm nay", "hom nay", "hôm qua", "hom qua", "ngày mai", "ngay mai",
		"tu?n này", "tuan nay", "tu?n tru?c", "tuan truoc", "tu?n sau", "tuan sau",
		"tháng này", "thang nay", "tháng tru?c", "thang truoc", "tháng sau", "thang sau",
		"g?n dây", "gan day", "recent", "latest",
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
	if containsAnyText(lower, "email", "mail") && containsAnyText(lower, "vi?t", "viet", "so?n", "soan", "g?i", "gui", "send", "draft") {
		return true
	}
	if containsAnyText(lower, "chat", "nhóm chat", "nhom chat", "google chat", "space") &&
		containsAnyText(lower, "g?i", "gui", "nh?n", "nhan", "thông báo", "thong bao", "send", "reply", "file") {
		return true
	}
	return containsAnyText(lower,
		"t?o l?ch", "tao lich", "t?o s? ki?n", "tao su kien", "d?t l?ch", "dat lich",
		"lên l?ch", "len lich", "schedule", "create event", "create meeting",
		"g?i email", "gui email", "so?n email", "soan email", "vi?t email", "viet email", "g?i mail", "gui mail", "vi?t mail", "viet mail",
		"g?i tin nh?n", "gui tin nhan", "send message", "nh?n tin", "nhan tin", "g?i file", "gui file",
		"g?i vào nhóm", "gui vao nhom", "g?i vào trong nhóm", "gui vao trong nhom",
		"xóa", "xoa", "delete", "remove",
		"c?p nh?t", "cap nhat", "update", "s?a l?ch", "sua lich",
		"ch?y l?nh", "chay lenh", "run command", "run python",
		"t?o file", "tao file", "s?a file", "sua file",
	)
}

func isLikelyReadRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return containsAnyText(lower,
		"li?t kê", "liet ke", "xem", "d?c", "doc", "ki?m tra", "kiem tra",
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
		"If the user says \"file này\", \"?nh này\", or asks to send/upload the attached file, use these paths in tool inputs that accept attachments.",
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
For meta questions like "tôi v?a nh?n gì", answer from recent_history.
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
		"l?ch này", "lich nay", "s? ki?n này", "su kien nay", "event này", "event nay",
		"cái này", "cai nay", "này có", "nay co", "v?a t?o", "vua tao",
		"có g?i mail", "co gui mail", "có g?i email", "co gui email",
		"mail thông báo", "mail thong bao", "email thông báo", "email thong bao",
		"ngu?i tham gia", "nguoi tham gia", "attendee", "attendees",
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

func isLikelyContextualFollowUpQuestion(text string, history []string, memory sessions.SessionMemory) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if isPotentialWriteRequest(lower) {
		return false
	}
	hasContext := len(history) > 0 ||
		strings.TrimSpace(memory.Summary) != "" ||
		len(memory.LastActionResults) > 0
	if !hasContext {
		return false
	}
	if containsAnyText(lower,
		"tôi v?a nh?n", "toi vua nhan",
		"tôi v?a h?i", "toi vua hoi",
		"tôi v?a nói", "toi vua noi",
		"mình v?a nh?n", "minh vua nhan",
		"mình v?a h?i", "minh vua hoi",
		"mình v?a nói", "minh vua noi",
		"tin nh?n tru?c", "tin nhan truoc",
		"câu tru?c", "cau truoc",
		"v?a nh?n gì", "vua nhan gi",
		"v?a h?i gì", "vua hoi gi",
	) {
		return true
	}
	if containsAnyText(lower, "thì sao", "thi sao", "còn", "con") &&
		containsAnyText(lower,
			"hôm qua", "hom qua", "hôm nay", "hom nay", "ngày mai", "ngay mai",
			"tu?n này", "tuan nay", "tu?n tru?c", "tuan truoc", "tu?n sau", "tuan sau",
			"tháng này", "thang nay", "tháng tru?c", "thang truoc", "tháng sau", "thang sau",
			"calendar", "l?ch", "lich", "email", "mail", "chat",
		) {
		return true
	}
	if strings.HasSuffix(lower, "?") &&
		containsAnyText(lower,
			"hôm qua", "hom qua", "hôm nay", "hom nay", "ngày mai", "ngay mai",
			"tu?n này", "tuan nay", "tu?n tru?c", "tuan truoc", "tu?n sau", "tuan sau",
			"tháng này", "thang nay", "tháng tru?c", "thang truoc", "tháng sau", "thang sau",
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
			"dã th?c hi?n", "da thuc hien",
			"dã t?o", "da tao",
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
			ContentForUser: "Tool l?i khi ch?y: " + toolCall.Name,
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
		baseQuery = normalizeRelativeProviderQuery(query, userText, gmailQueryIntentTerms())
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
		"hom kia", "hom qua", "ngay mai", "hom nay", "day before yesterday", "two days ago", "yesterday", "today", "tomorrow", "this week", "next week",
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
	case containsAnyText(lower, "tu?n sau", "tuan sau", "next week"):
		start := startOfWeekMonday(now).AddDate(0, 0, 7)
		return start, start.AddDate(0, 0, 7), true
	case containsAnyText(lower, "tu?n này", "tuan nay", "this week", "trong tu?n", "trong tuan"):
		start := startOfWeekMonday(now)
		return start, start.AddDate(0, 0, 7), true
	case containsAnyText(lower, "tháng t?i", "thang toi", "tháng sau", "thang sau", "next month"):
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
	case containsAnyText(lower, "tu?n sau", "tuan sau", "next week"):
		thisWeek := startOfWeekMonday(now)
		start = thisWeek.AddDate(0, 0, 7)
		end = start.AddDate(0, 0, 7)
	case containsAnyText(lower, "tu?n này", "tuan nay", "this week", "trong tu?n", "trong tuan"):
		start = startOfWeekMonday(now)
		end = start.AddDate(0, 0, 7)
	case containsAnyText(lower, "tháng t?i", "thang toi", "tháng sau", "thang sau", "next month"):
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
	if containsAnyText(lowerQuery, "tu?n này", "tuan nay", "tu?n sau", "tuan sau", "tháng này", "thang nay", "tháng t?i", "thang toi", "hôm nay", "hom nay", "today", "this week", "next week", "this month", "next month") &&
		containsAnyText(lowerQuery, "l?ch", "lich", "calendar", "s? ki?n", "su kien", "event") {
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
	if containsAnyText(lowerQuery, "tu?n này", "tuan nay", "tu?n sau", "tuan sau", "tháng này", "thang nay", "tháng t?i", "thang toi", "hôm nay", "hom nay", "today", "this week", "next week", "this month", "next month") &&
		containsAnyText(lowerQuery, "email", "mail", "gmail", "thu", "thu", "h?p thu", "hop thu") {
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

func (r *Runtime) legacyApprovalRequest(message contracts.UserMessage, toolCall providers.ToolCall, decision contracts.RiskDecision) contracts.ApprovalRequest {
	now := r.now()
	contractCall := contracts.ToolCall{
		ToolCallID: toolCall.ID,
		RequestID:  message.RequestID,
		SessionID:  message.SessionID,
		ToolName:   toolCall.Name,
		Input:      cloneArguments(toolCall.Arguments),
	}
	summary := approvalSummary(toolCall.Name, decision.RiskLevel)
	parentApprovalID := ""
	if message.Metadata != nil {
		if value, ok := message.Metadata["parentApprovalId"].(string); ok && strings.TrimSpace(value) != "" {
			parentApprovalID = strings.TrimSpace(value)
		} else if value, ok := message.Metadata["approvalId"].(string); ok && strings.TrimSpace(value) != "" {
			parentApprovalID = strings.TrimSpace(value)
		}
	}
	approval := contracts.ApprovalRequest{
		ApprovalID:       "appr_" + safeID(toolCall.ID),
		ParentApprovalID: parentApprovalID,
		RequestID:        message.RequestID,
		SessionID:        message.SessionID,
		ToolCallID:       toolCall.ID,
		Status:           contracts.ApprovalStatusPending,
		RiskLevel:        decision.RiskLevel,
		Summary:          summary,
		Details:          decision.Reason,
		ToolCall:         contractCall,
		CreatedAt:        now,
		ExpiresAt:        now.Add(approvalTTL),
	}
	r.logger.Info("approval request created",
		"request_id", message.RequestID,
		"session_id", message.SessionID,
		"approval_id", approval.ApprovalID,
		"tool_call_id", toolCall.ID,
		"tool_name", toolCall.Name,
		"risk_level", approval.RiskLevel,
		"parent_approval_id", approval.ParentApprovalID,
	)
	return approval
}

func (r *Runtime) legacyStorePendingApproval(pending pendingApproval) {
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
	r.logger.Info("approval request persisted",
		"request_id", pending.message.RequestID,
		"session_id", sessionID,
		"approval_id", approvalID,
		"tool_call_id", pending.request.ToolCallID,
		"tool_name", pending.request.ToolCall.ToolName,
	)
}

func (r *Runtime) legacyTakePendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	sessionID = strings.TrimSpace(sessionID)
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		approvalID = r.pendingBySession[sessionID]
	}
	r.logger.Info("approval request lookup attempted",
		"session_id", sessionID,
		"approval_id", approvalID,
	)
	if approvalID == "" {
		return pendingApproval{}, false
	}
	pending, ok := r.pendingApprovals[approvalID]
	if !ok {
		r.logger.Info("approval request lookup failed or was already resolved",
			"session_id", sessionID,
			"approval_id", approvalID,
		)
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

func (r *Runtime) legacyPeekPendingApproval(sessionID string, approvalID string) (pendingApproval, bool) {
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
	return pending, ok
}

func legacyBuildApprovalContinuationMessage(pending pendingApproval, result tools.ToolResult, now time.Time) contracts.UserMessage {
	var text string
	if len(pending.remainingToolCalls) > 0 {
		remainingNames := make([]string, 0, len(pending.remainingToolCalls))
		for _, tc := range pending.remainingToolCalls {
			remainingNames = append(remainingNames, tc.Name)
		}
		text = strings.TrimSpace(fmt.Sprintf(`Continuing the original multi-step request after an approved tool completed.
Luôn tr? l?i b?ng ti?ng Vi?t n?u ngu?i dùng dang nói ti?ng Vi?t.
Do not repeat the tool that was just executed.

Original request:
%s

Completed tool: %s
Result: %s

Continue by calling the remaining tools in the original plan: %s
Use any resource IDs or names returned by the completed tool's result when they are needed as input for the next tool.`,
			pending.message.Text,
			pending.toolCall.Name,
			result.ContentForLLM,
			strings.Join(remainingNames, ", "),
		))
	} else {
		text = strings.TrimSpace(fmt.Sprintf(`An approved tool just completed as part of the user's original request.
Luôn tr? l?i b?ng ti?ng Vi?t n?u ngu?i dùng dang nói ti?ng Vi?t.

Original request:
%s

Completed tool: %s
Result: %s

Check whether the original request contained additional tasks that have not yet been done.
If yes, call the necessary tool(s) now — do NOT ask the user again for information already given in the original request.
If all tasks are already complete, respond with a short Vietnamese summary of what was accomplished.
Do not repeat the tool that was just executed.`,
			pending.message.Text,
			pending.toolCall.Name,
			result.ContentForLLM,
		))
	}
	msg := pending.message
	msg.Text = text
	msg.Timestamp = now
	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}
	msg.Metadata["continuationOf"] = pending.request.ApprovalID
	msg.Metadata["completedTool"] = pending.toolCall.Name
	return msg
}

func legacyBuildRevisionRequest(pending pendingApproval, comment string) string {
	input := "{}"
	if len(pending.request.ToolCall.Input) > 0 {
		if data, err := json.MarshalIndent(pending.request.ToolCall.Input, "", "  "); err == nil {
			input = string(data)
		}
	}
	return strings.TrimSpace(fmt.Sprintf(`Ngu?i dùng mu?n ch?nh l?i m?t yêu c?u dang ch? xác nh?n.
Luôn tr? l?i b?ng ti?ng Vi?t n?u ngu?i dùng dang nói ti?ng Vi?t.
Không th?c thi tool call cu dang ch?.
Dùng yêu c?u ban d?u, input tool dang ch?, và ghi chú ch?nh s?a d? t?o plan/tool call m?i.
N?u hành d?ng sau khi ch?nh v?n có side effect, hãy g?i tool tuong ?ng d? runtime t?o yêu c?u xác nh?n m?i.
N?u còn thi?u thông tin, h?i m?t câu ng?n g?n b?ng ti?ng Vi?t.

Yêu c?u ban d?u:
%s

Tool dang ch?:
%s

Input dang ch?:
%s

Ghi chú ch?nh s?a:
%s`, pending.message.Text, pending.request.ToolCall.ToolName, input, comment))
}

func legacyApprovalSummary(toolName string, riskLevel contracts.RiskLevel) string {
	switch toolName {
	case "gmail.createDraft", "gmail.updateDraft", "gmail.replyDraft", "gmail.forwardDraft":
		return "Tôi c?n b?n xác nh?n tru?c khi t?o ho?c s?a Gmail draft."
	case "gmail.sendDraft":
		return "Tôi c?n b?n xác nh?n tru?c khi g?i email."
	case "gmail.deleteDraft":
		return "Tôi c?n b?n xác nh?n tru?c khi xóa Gmail draft."
	case "gmail.downloadAttachments":
		return "Tôi c?n b?n xác nh?n tru?c khi t?i attachment Gmail xu?ng máy local."
	case "gmail.modifyMessage", "gmail.batchModifyMessages":
		return "Tôi c?n b?n xác nh?n tru?c khi s?a tr?ng thái ho?c nhãn Gmail."
	case "gmail.trashMessage":
		return "Tôi c?n b?n xác nh?n tru?c khi chuy?n email vào thùng rác."
	case "gmail.untrashMessage":
		return "Tôi c?n b?n xác nh?n tru?c khi khôi ph?c email kh?i thùng rác."
	case "calendar.createEvent":
		return "Tôi c?n b?n xác nh?n tru?c khi t?o s? ki?n Calendar."
	case "calendar.updateEvent":
		return "Tôi c?n b?n xác nh?n tru?c khi s?a s? ki?n Calendar."
	case "calendar.deleteEvent":
		return "Tôi c?n b?n xác nh?n tru?c khi xóa s? ki?n Calendar."
	case "chat.sendMessage":
		return "Tôi c?n b?n xác nh?n tru?c khi g?i tin nh?n Google Chat."
	case "chat.updateMessage":
		return "Tôi c?n b?n xác nh?n tru?c khi s?a tin nh?n Google Chat."
	case "chat.deleteMessage":
		return "Tôi c?n b?n xác nh?n tru?c khi xóa tin nh?n Google Chat."
	case "chat.createSpace":
		return "Tôi c?n b?n xác nh?n tru?c khi t?o Google Chat space."
	case "chat.addMember":
		return "Tôi c?n b?n xác nh?n tru?c khi thêm thành viên Google Chat."
	case "chat.removeMember":
		return "Tôi c?n b?n xác nh?n tru?c khi xóa thành viên Google Chat."
	case "sandbox.runPython", "sandbox.runShell":
		return "Tôi c?n b?n xác nh?n tru?c khi ch?y code ho?c l?nh trong sandbox."
	default:
		return fmt.Sprintf("Tôi c?n b?n xác nh?n tru?c khi ch?y %s vì risk là %s.", toolName, riskLevel)
	}
}

func legacyApprovalExecutionMessage(result tools.ToolResult, contractResult contracts.ToolResult) string {
	if rendered := renderToolResultForUser(contractResult); rendered != "" {
		return rendered
	}
	if strings.TrimSpace(result.ContentForUser) != "" {
		return formatOutboundText(result.ContentForUser)
	}
	if result.Success {
		return "Ðã th?c hi?n thao tác sau khi b?n xác nh?n."
	}
	if result.Error != nil && strings.TrimSpace(result.Error.Message) != "" {
		return result.Error.Message
	}
	return "Tool không hoàn t?t."
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

func sanitizeProviderTranscriptForToolProtocol(messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return nil
	}
	sanitized := make([]providers.Message, 0, len(messages))
	for i := 0; i < len(messages); {
		message := messages[i]
		if message.Role == providers.MessageRoleTool {
			i++
			continue
		}
		if message.Role != providers.MessageRoleAssistant || len(message.ToolCalls) == 0 {
			sanitized = append(sanitized, cloneProviderMessages([]providers.Message{message})[0])
			i++
			continue
		}

		expected := make(map[string]bool, len(message.ToolCalls))
		for _, toolCall := range message.ToolCalls {
			toolCallID := strings.TrimSpace(toolCall.ID)
			if toolCallID != "" {
				expected[toolCallID] = false
			}
		}
		j := i + 1
		toolMessages := make([]providers.Message, 0, len(expected))
		for j < len(messages) && messages[j].Role == providers.MessageRoleTool {
			toolCallID := strings.TrimSpace(messages[j].ToolCallID)
			if _, ok := expected[toolCallID]; ok && !expected[toolCallID] {
				expected[toolCallID] = true
				toolMessages = append(toolMessages, cloneProviderMessages([]providers.Message{messages[j]})[0])
			}
			j++
		}
		allToolCallsAnswered := len(expected) > 0
		for _, answered := range expected {
			if !answered {
				allToolCallsAnswered = false
				break
			}
		}
		if allToolCallsAnswered {
			sanitized = append(sanitized, cloneProviderMessages([]providers.Message{message})[0])
			sanitized = append(sanitized, toolMessages...)
		} else if strings.TrimSpace(message.Content) != "" {
			fallback := message
			fallback.ToolCalls = nil
			fallback.ToolCallID = ""
			sanitized = append(sanitized, cloneProviderMessages([]providers.Message{fallback})[0])
		}
		i = j
	}
	return sanitized
}

func transcriptWithLastUserContent(transcript []providers.Message, content string) []providers.Message {
	cloned := cloneProviderMessages(transcript)
	content = strings.TrimSpace(content)
	if len(cloned) == 0 || content == "" {
		return cloned
	}
	for i := len(cloned) - 1; i >= 0; i-- {
		if cloned[i].Role == providers.MessageRoleUser {
			cloned[i].Content = content
			break
		}
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

// heuristicFirstResolver tries the heuristic resolver first. It only trusts the
// heuristic when the result is high-confidence and requires no clarification
// (isUsableReference). When the heuristic is uncertain — e.g. it finds a cue word
// but cannot locate a matching past result, or when the cue is a forward reference
// inside the same request ("s? ki?n này" referring to an event being created now) —
// it falls back to the LLM resolver so the LLM can make the correct judgment.
type heuristicFirstResolver struct {
	primary  reference.Resolver
	fallback reference.Resolver
}

func newHeuristicFirstResolver(primary reference.Resolver, fallback reference.Resolver) *heuristicFirstResolver {
	return &heuristicFirstResolver{primary: primary, fallback: fallback}
}

func (r *heuristicFirstResolver) Resolve(ctx context.Context, input reference.Input) (*reference.Resolution, error) {
	result, err := r.primary.Resolve(ctx, input)
	if err == nil && isUsableReference(result) {
		// Heuristic resolved with high confidence and no clarification needed — trust it.
		return result, nil
	}
	// Heuristic is uncertain (low confidence, needs clarification, or no match).
	// Delegate to LLM so it can distinguish forward references (e.g. "s? ki?n này"
	// referring to an event being created in the same request) from genuine
	// past-result references.
	return r.fallback.Resolve(ctx, input)
}
func (r *Runtime) handleContextError(ctx context.Context, runState RunState, toolResults []contracts.ToolResult) *contracts.AgentResponse {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	statusCode := contracts.ErrorInternal
	messageText := "request canceled"
	if err == context.DeadlineExceeded {
		statusCode = contracts.ErrorProviderTimeout
		messageText = "request timed out"
	}
	if finishErr := r.finishRunState(ctx, runState, RuntimeRunStatusFailed); finishErr != nil {
		return &contracts.AgentResponse{Error: finishErr, Message: finishErr.Message, Status: contracts.AgentStatusFailed}
	}
	return &contracts.AgentResponse{
		Status:      contracts.AgentStatusFailed,
		ToolResults: toolResults,
		Error: &contracts.ErrorShape{
			Code:      statusCode,
			Message:   messageText,
			Source:    contracts.ErrorSourceAgent,
			Retryable: false,
		},
		Message: messageText,
	}
}

