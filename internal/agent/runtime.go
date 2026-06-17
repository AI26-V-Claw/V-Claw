package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"vclaw/internal/agent/reference"
	"vclaw/internal/contracts"
	"vclaw/internal/governance"
	"vclaw/internal/longmem"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

const (
	DefaultMaxIterations    = 8
	DefaultToolTimeout      = 30 * time.Second
	approvalTTL             = 10 * time.Minute
	pendingClarificationTTL = 30 * time.Minute
)

var (
	emailAnswerPattern  = regexp.MustCompile(`(?i)\b[[:alnum:]._%+\-]+@[[:alnum:].\-]+\.[[:alpha:]]{2,}\b`)
	timeAnswerPattern   = regexp.MustCompile(`(?i)\b\d{1,2}(:\d{2})?\s*(am|pm)?\b`)
	viTimeAnswerPattern = regexp.MustCompile(`(?i)\b\d{1,2}\s*(h|g|gio|giờ)(\s*\d{1,2})?\b`)
)

type RuntimeConfig struct {
	Provider                   providers.Provider
	Registry                   *tools.ToolRegistry
	Observer                   RuntimeObserver
	ReferenceResolver          reference.Resolver
	Policy                     policies.ToolPolicy
	SessionStore               sessions.Store
	StateStore                 RuntimeStateStore
	Logger                     *slog.Logger
	ToolHooks                  toolhooks.Hooks
	MaxIterations              int
	ToolTimeout                time.Duration
	ParallelExecutionEnabled   bool
	ParallelMaxWorkers         int
	ParallelToolTimeoutDefault time.Duration
	SubtaskMaxChildren         int
	SubtaskMaxDepth            int
	SubtaskDefaultTimeout      time.Duration
	SubtaskMaxTimeout          time.Duration
	Model                      string
	Now                        func() time.Time
	Compactor                  *sessions.Compactor
	ContextWindow              int
	MemoryClassifierModel      string
	// SoulPrompt is the optional SOUL.md content loaded at startup. It is
	// hashed together with runtimeSystemPrompt() to produce the prompt
	// version. If empty, only runtimeSystemPrompt() contributes — useful for
	// tests that don't want to load the file.
	SoulPrompt string
	LongMemDir string // path to cache/memory/; empty disables long-term memory
}

// longTermMemoryLoader loads the long-term memory prompt for context injection.
type longTermMemoryLoader interface {
	Load() string
}

// longTermMemoryFlusher flushes a compaction summary to long-term memory files.
type longTermMemoryFlusher interface {
	Flush(ctx context.Context, summary string) error
}

type Runtime struct {
	provider                   providers.Provider
	registry                   *tools.ToolRegistry
	observer                   RuntimeObserver
	referenceResolver          reference.Resolver
	policy                     policies.ToolPolicy
	sessionStore               sessions.Store
	stateStore                 RuntimeStateStore
	logger                     *slog.Logger
	toolHooks                  toolhooks.Hooks
	approvalMu                 sync.Mutex
	pendingApprovals           map[string]pendingApproval
	pendingBySession           map[string]string
	maxIterations              int
	toolTimeout                time.Duration
	parallelExecutionEnabled   bool
	parallelMaxWorkers         int
	parallelToolTimeoutDefault time.Duration
	subtaskMaxDepth            int
	subtaskDefaultTimeout      time.Duration
	subtaskMaxTimeout          time.Duration
	model                      string
	now                        func() time.Time
	compactor                  *sessions.Compactor
	contextWindow              int
	memoryClassifierModel      string
	// promptVersion is the content-hash fingerprint of the effective system
	// prompt (runtimeSystemPrompt + SOUL.md). Computed once when the Runtime
	// is constructed and stamped onto every record this Runtime produces.
	promptVersion              string
	subtasks                   *subtaskCoordinator
	ltMemLoader                longTermMemoryLoader  // nil = disabled
	ltMemFlusher               longTermMemoryFlusher // nil = disabled
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
	subtaskMaxDepth := config.SubtaskMaxDepth
	if subtaskMaxDepth <= 0 {
		subtaskMaxDepth = defaultSubtaskMaxDepth
	}
	subtaskDefaultTimeout := config.SubtaskDefaultTimeout
	if subtaskDefaultTimeout <= 0 {
		subtaskDefaultTimeout = defaultSubtaskTimeout
	}
	subtaskMaxTimeout := config.SubtaskMaxTimeout
	if subtaskMaxTimeout <= 0 {
		subtaskMaxTimeout = maxSubtaskTimeout
	}
	if subtaskDefaultTimeout > subtaskMaxTimeout {
		subtaskDefaultTimeout = subtaskMaxTimeout
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
	hooks := config.ToolHooks
	if hooks == nil {
		hooks = toolhooks.NoopHooks{}
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
	// Compute the prompt version once at construction. We pass a zero time so
	// the dynamic "current time" segment of runtimeSystemPrompt doesn't shift
	// the hash on every Runtime creation. SOUL.md is hashed alongside so any
	// edit there bumps the version automatically.
	promptVersion := governance.PromptVersion(runtimeSystemPrompt(time.Time{}), config.SoulPrompt)
	subtasks := newSubtaskCoordinator(config.SubtaskMaxChildren)
	subtasks.now = now
	var ltLoader longTermMemoryLoader
	var ltFlusher longTermMemoryFlusher
	if dir := strings.TrimSpace(config.LongMemDir); dir != "" && config.Provider != nil {
		ltLoader = longmem.NewLoader(dir)
		ltFlusher = longmem.NewFlusher(dir, config.Provider, memoryClassifierModel(config))
	}
	return &Runtime{
		provider:                   config.Provider,
		registry:                   config.Registry,
		observer:                   config.Observer,
		referenceResolver:          referenceResolver,
		policy:                     config.Policy,
		sessionStore:               sessionStore,
		stateStore:                 stateStore,
		logger:                     logger,
		toolHooks:                  hooks,
		pendingApprovals:           make(map[string]pendingApproval),
		pendingBySession:           make(map[string]string),
		maxIterations:              maxIterations,
		toolTimeout:                toolTimeout,
		parallelExecutionEnabled:   config.ParallelExecutionEnabled,
		parallelMaxWorkers:         parallelMaxWorkers,
		parallelToolTimeoutDefault: parallelToolTimeoutDefault,
		subtaskMaxDepth:            subtaskMaxDepth,
		subtaskDefaultTimeout:      subtaskDefaultTimeout,
		subtaskMaxTimeout:          subtaskMaxTimeout,
		model:                      config.Model,
		now:                        now,
		compactor:                  config.Compactor,
		contextWindow:              contextWindow,
		memoryClassifierModel:      memoryClassifierModel(config),
		promptVersion:              promptVersion,
		subtasks:                   subtasks,
		ltMemLoader:                ltLoader,
		ltMemFlusher:               ltFlusher,
	}
}

func memoryClassifierModel(config RuntimeConfig) string {
	if model := strings.TrimSpace(config.MemoryClassifierModel); model != "" {
		return model
	}
	return strings.TrimSpace(config.Model)
}

func (r *Runtime) Run(ctx context.Context, message contracts.UserMessage) (contracts.AgentResponse, error) {
	ctx = toolhooks.WithRequestContext(ctx, message.RequestID, message.SessionID)
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
	runState, errShape := r.startRunState(ctx, message)
	if errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	ctx = withParentRunID(ctx, runState.RunID)
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
	if isUsablePendingClarification(pendingClarification, r.now()) {
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
			userMessage.Content = fmt.Sprintf("[Tiếp tục sau khi %s được xác nhận và thực thi]", completedTool)
		} else {
			userMessage.Content = "[Tiếp tục sau khi hành động được xác nhận và thực thi]"
		}
	}
	transcript = append(transcript, userMessage)
	if errShape := r.appendTranscriptMessage(ctx, runState, userMessage); errShape != nil {
		errShape.Message = strings.Replace(errShape.Message, "append message:", "append user message:", 1)
		return failStartedRun(errShape)
	}
	if clarification := r.referenceClarificationResponse(message, referenceResolution); clarification != nil {
		if errShape := r.appendAssistantTranscriptForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, clarification.Message); errShape != nil {
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
	if _, isContinuation := message.Metadata["continuationOf"]; isContinuation {
		// Restore the full continuation instructions (including "do not repeat tool") for the
		// provider. The stored transcript holds the short "[Tiếp tục...]" placeholder; the
		// provider needs the full text from buildApprovalContinuationMessage.
		providerTranscript = transcriptWithLastUserContent(transcript, message.Text)
	} else if isolatedNewWriteRequest || standaloneReadRequest {
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
		if errShape := r.appendTranscriptMessage(ctx, runState, assistantMessage); errShape != nil {
			errShape.Message = strings.Replace(errShape.Message, "append message:", "append assistant message:", 1)
			return failStartedRun(errShape)
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
			ctx,
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
				var execErr error
				if result.Error != nil && !result.Success {
					execErr = errors.New(result.Error.Message)
				}
				r.runPostToolHook(ctx, call, batch[i].definition, result, execErr, r.now().Add(-outcome.duration))
				r.logger.Info("parallel tool execution completed",
					"tool_call_id", call.ID,
					"tool_name", call.Name,
					"success", result.Success,
					"error_code", toolErrorCode(result),
					"duration", outcome.duration,
					"content_preview", logPreview(result.ContentForLLM, 260),
				)
				decision := r.stampPolicyRef(runState.RunID, call.ID, r.policy.DecideToolCall(call.ID, batch[i].definition, true, r.now()))
				if errShape := r.recordRuntimeRiskDecision(ctx, runState, call, decision); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordRuntimeToolCallStatus(ctx, runState, call, ToolCallStatusAllowed, "safe parallel tool", ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				emitProgress(ctx, ProgressEvent{
					Stage:      stage,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Message:    "Tool finished",
				})
				// Stamp the policy reference onto the result so the persisted
				// tool_calls row carries the same provenance as the risk_decisions row.
				result.PolicyDecisionRef = decision.PolicyDecisionRef
				batchResults[i].result = result
				if errShape := r.recordRuntimeToolCall(ctx, runState.RunID, call, result, outcome.duration, ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordActionResult(ctx, message.SessionID, result); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				toolResults = append(toolResults, contractToolResult(result, r.buildGovernanceMetadata(call.Name, decision.PolicyDecisionRef)))
				toolMessage := providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: call.ID,
					Content:    truncateToolContentForLLM(result.ContentForLLM),
				}
				transcript = append(transcript, toolMessage)
				providerTranscript = append(providerTranscript, toolMessage)
				if errShape := r.appendTranscriptMessage(ctx, runState, toolMessage); errShape != nil {
					base.Error = errShape
					base.Error.Message = strings.Replace(base.Error.Message, "append message:", "append tool message:", 1)
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
				if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM("CLARIFICATION_REQUESTED: " + clarification.question),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservationsForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because clarification is required first"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if errShape := r.appendAssistantTranscriptForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, clarification.question); errShape != nil {
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

			decision := r.decideToolCall(ctx, providerToolCall, definition, found)
			decision.PolicyDecisionRef = governance.PolicyRef(runState.RunID, providerToolCall.ID, decision.CheckedAt)
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
			if errShape := r.recordRuntimeRiskDecision(ctx, runState, providerToolCall, decision); errShape != nil {
				base.Error = errShape
				base.Message = errShape.Message
				return base, nil
			}
			if clarification := r.toolCallClarificationResponse(message, providerToolCall, definition, found, activeClarification, currentRequestText); clarification != nil {
				if errShape := r.recordRuntimeToolCallStatus(ctx, runState, providerToolCall, ToolCallStatusWaitingClarification, clarification.Message, ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if shouldResolveChatSpaceBeforeClarification(providerToolCall) {
					toolMessage := providers.Message{
						Role:       providers.MessageRoleTool,
						ToolCallID: providerToolCall.ID,
						Content:    truncateToolContentForLLM(chatSpaceResolutionObservation(providerToolCall)),
					}
					transcript = append(transcript, toolMessage)
					providerTranscript = append(providerTranscript, toolMessage)
					if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, toolMessage); err != nil {
						base.Error = err
						base.Message = err.Message
						return base, nil
					}
					for _, skipped := range skippedToolObservationMessages(assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because the current Google Chat target must be resolved first") {
						transcript = append(transcript, skipped)
						providerTranscript = append(providerTranscript, skipped)
						if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, skipped); err != nil {
							base.Error = err
							base.Message = err.Message
							return base, nil
						}
					}
					continue agentLoop
				}
				if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM("MISSING_REQUIRED_FIELD: " + clarification.Message),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservationsForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because the current tool call needs clarification"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if errShape := r.appendAssistantTranscriptForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, clarification.Message); errShape != nil {
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
				if errShape := r.recordRuntimeToolCallStatus(ctx, runState, providerToolCall, ToolCallStatusAllowed, decision.Reason, ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				startedAt := time.Now()
				result := r.executeAllowedTool(ctx, providerToolCall, definition)
				// Stamp the policy reference on the result so audit/N4 can
				// trace the result row back to the risk decision that allowed it.
				result.PolicyDecisionRef = decision.PolicyDecisionRef
				if errShape := r.recordRuntimeToolCall(ctx, runState.RunID, providerToolCall, result, time.Since(startedAt), ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordActionResult(ctx, message.SessionID, result); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				contractResult := contractToolResult(result, r.buildGovernanceMetadata(providerToolCall.Name, decision.PolicyDecisionRef))
				toolResults = append(toolResults, contractResult)

				toolMessage := providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM(r.toolContentForProvider(providerToolCall.Name, result.ContentForLLM)),
				}
				transcript = append(transcript, toolMessage)
				providerTranscript = append(providerTranscript, toolMessage)
				if errShape := r.appendTranscriptMessage(ctx, runState, toolMessage); errShape != nil {
					base.Error = errShape
					base.Error.Message = strings.Replace(base.Error.Message, "append message:", "append tool message:", 1)
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
				approval := r.approvalRequest(message, providerToolCall, decision, providerTranscript)
				action, errShape := r.createApprovalAction(ctx, runState, message, providerToolCall, decision, approval)
				if errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordRuntimeToolCallStatus(ctx, runState, providerToolCall, ToolCallStatusRequiresApproval, decision.Reason, action.ApprovalID); errShape != nil {
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
					if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, toolMessage); err != nil {
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
				if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM("ACTION_REQUIRES_APPROVAL: " + approval.Summary),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservationsForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because another tool call requires approval"); err != nil {
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
				if errShape := r.recordRuntimeToolCallStatus(ctx, runState, providerToolCall, ToolCallStatusBlocked, reason, ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, providers.Message{
					Role:       providers.MessageRoleTool,
					ToolCallID: providerToolCall.ID,
					Content:    truncateToolContentForLLM(policyErrorCode(found) + ": " + reason),
				}); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				if err := r.appendSkippedToolObservationsForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because another tool call was blocked"); err != nil {
					base.Error = err
					base.Message = err.Message
					return base, nil
				}
				base.ToolResults = toolResults
				base.Status = contracts.AgentStatusBlocked
				base.Message = "Hành động này không được phép thực hiện do chính sách bảo mật hiện tại."
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

func responsePlan(planResult *TaskPlanResult) *contracts.Plan {
	if planResult == nil || len(planResult.Plan.Steps) == 0 {
		return nil
	}
	plan := planResult.Plan
	return &plan
}
