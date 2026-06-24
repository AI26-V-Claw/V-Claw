package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vclaw/internal/agent/reference"
	"vclaw/internal/contracts"
	"vclaw/internal/governance"
	"vclaw/internal/knowledge"
	"vclaw/internal/longmem"
	"vclaw/internal/orchestration"
	"vclaw/internal/policies"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
	"vclaw/internal/traceutil"
)

const (
	DefaultIterationBudget  = 8
	DefaultToolTimeout      = 30 * time.Second
	DefaultProviderTimeout  = 60 * time.Second
	approvalTTL             = 10 * time.Minute
	pendingClarificationTTL = 30 * time.Minute
)

var (
	emailAnswerPattern    = regexp.MustCompile(`(?i)\b[[:alnum:]._%+\-]+@[[:alnum:].\-]+\.[[:alpha:]]{2,}\b`)
	timeAnswerPattern     = regexp.MustCompile(`(?i)\b\d{1,2}(:\d{2})?\s*(am|pm)?\b`)
	viTimeAnswerPattern   = regexp.MustCompile(`(?i)\b\d{1,2}\s*(h|g|gio|giờ)(\s*\d{1,2})?\b`)
	runtimeRunCancelToken uint64
)

type RuntimeConfig struct {
	Provider                   providers.Provider
	Registry                   *tools.ToolRegistry
	Observer                   RuntimeObserver
	Telemetry                  RuntimeTelemetry
	ReferenceResolver          reference.Resolver
	Policy                     policies.ToolPolicy
	SessionStore               sessions.Store
	StateStore                 RuntimeStateStore
	Logger                     *slog.Logger
	ToolHooks                  toolhooks.Hooks
	IterationBudget            int
	ProviderTimeout            time.Duration
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
	LocalLocation              *time.Location // timezone for date calculations; nil falls back to time.Local
	Compactor                  *sessions.Compactor
	ContextWindow              int
	ContextBudget              ContextBudget // zero value = scaled defaults from ContextWindow
	MemoryClassifierModel      string
	LongMemDir                 string
	KnowledgeRetriever         knowledge.Retriever
}

type Runtime struct {
	provider                   providers.Provider
	registry                   *tools.ToolRegistry
	observer                   RuntimeObserver
	telemetry                  RuntimeTelemetry
	referenceResolver          reference.Resolver
	policy                     policies.ToolPolicy
	sessionStore               sessions.Store
	stateStore                 RuntimeStateStore
	logger                     *slog.Logger
	toolHooks                  toolhooks.Hooks
	approvalMu                 sync.Mutex
	pendingApprovals           map[string]pendingApproval
	pendingBySession           map[string]string
	iterationBudgetLimit       int
	providerTimeout            time.Duration
	toolTimeout                time.Duration
	parallelExecutionEnabled   bool
	parallelMaxWorkers         int
	parallelToolTimeoutDefault time.Duration
	subtaskMaxDepth            int
	subtaskDefaultTimeout      time.Duration
	subtaskMaxTimeout          time.Duration
	model                      string
	now                        func() time.Time
	localLocation              *time.Location
	compactor                  *sessions.Compactor
	contextWindow              int
	contextBudget              ContextBudget
	memoryClassifierModel      string
	// promptVersion is the content-hash fingerprint of the effective system
	// prompt (runtimeSystemPrompt). Computed once when the Runtime
	// is constructed and stamped onto every record this Runtime produces.
	promptVersion      string
	planStore          *PlanStore
	subtasks           *subtaskCoordinator
	ltMemLoader        longTermMemoryLoader  // nil = disabled
	ltMemFlusher       longTermMemoryFlusher // nil = disabled
	knowledgeRetriever knowledge.Retriever
	cancelMu           sync.Mutex
	activeCancels      map[string]activeRunCancel // sessionID → active run cancel state
}

type activeRunCancel struct {
	token  uint64
	cancel context.CancelFunc
}

type longTermMemoryLoader interface {
	Load() string
}

type longTermMemoryFlusher interface {
	Flush(context.Context, string) error
	RecordRepeatedHabits(context.Context, longmem.HabitInput) error
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
	provider := config.Provider
	if config.Telemetry != nil && provider != nil {
		provider = config.Telemetry.WrapProvider(provider)
	}
	iterationBudgetLimit := config.IterationBudget
	if iterationBudgetLimit <= 0 {
		iterationBudgetLimit = DefaultIterationBudget
	}
	toolTimeout := config.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = DefaultToolTimeout
	}
	providerTimeout := config.ProviderTimeout
	if providerTimeout <= 0 {
		providerTimeout = DefaultProviderTimeout
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
	localLocation := config.LocalLocation
	// Do not default to time.Local here.
	// Production always sets this via AgentRuntimeConfig.Timezone â†’ BuildRuntime.
	// Tests leave it nil so runtimeLocalLocation falls back to now().Location().
	referenceResolver := config.ReferenceResolver
	if referenceResolver == nil {
		if provider != nil {
			referenceResolver = newHeuristicFirstResolver(
				reference.NewHeuristicResolver(),
				reference.NewLLMResolver(provider, config.Model),
			)
		} else {
			referenceResolver = reference.NewHeuristicResolver()
		}
	}
	contextWindow := config.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 128_000
	}
	contextBudget := config.ContextBudget
	contextBudget.ContextWindow = contextWindow
	contextBudget = contextBudget.normalized()
	// Compute the prompt version once at construction from the static prompt
	// content only. runtimeSystemPromptStatic() substitutes a stable placeholder
	// for the dynamic datetime segment, so two Runtimes created at different
	// times produce the same promptVersion as long as the static prompt is
	// unchanged. runtimeSystemPrompt() is the single source of truth for the
	// effective system prompt; configs/SOUL.md is reference documentation only
	// and is not injected at runtime.
	promptVersion := governance.PromptVersion(runtimeSystemPromptStatic())
	subtasks := newSubtaskCoordinator(config.SubtaskMaxChildren)
	subtasks.now = now
	planStore := NewPlanStore()
	if config.Registry != nil {
		if _, found := config.Registry.GetDefinition(PlanToolName); !found {
			_ = config.Registry.RegisterWithEntry(NewPlanTool(planStore), tools.ToolRegistryEntry{Group: "builtin", Owner: "agent_core"})
		}
	}
	var ltLoader longTermMemoryLoader
	var ltFlusher longTermMemoryFlusher
	if dir := strings.TrimSpace(config.LongMemDir); dir != "" && config.Provider != nil {
		ltLoader = longmem.NewLoader(dir)
		ltFlusher = longmem.NewFlusher(dir, config.Provider, memoryClassifierModel(config))
	}
	return &Runtime{
		provider:                   provider,
		registry:                   config.Registry,
		observer:                   config.Observer,
		telemetry:                  config.Telemetry,
		referenceResolver:          referenceResolver,
		policy:                     config.Policy,
		sessionStore:               sessionStore,
		stateStore:                 stateStore,
		logger:                     logger,
		toolHooks:                  hooks,
		pendingApprovals:           make(map[string]pendingApproval),
		pendingBySession:           make(map[string]string),
		iterationBudgetLimit:       iterationBudgetLimit,
		providerTimeout:            providerTimeout,
		toolTimeout:                toolTimeout,
		parallelExecutionEnabled:   config.ParallelExecutionEnabled,
		parallelMaxWorkers:         parallelMaxWorkers,
		parallelToolTimeoutDefault: parallelToolTimeoutDefault,
		subtaskMaxDepth:            subtaskMaxDepth,
		subtaskDefaultTimeout:      subtaskDefaultTimeout,
		subtaskMaxTimeout:          subtaskMaxTimeout,
		model:                      config.Model,
		now:                        now,
		localLocation:              localLocation,
		compactor:                  config.Compactor,
		contextWindow:              contextWindow,
		contextBudget:              contextBudget,
		memoryClassifierModel:      memoryClassifierModel(config),
		promptVersion:              promptVersion,
		planStore:                  planStore,
		subtasks:                   subtasks,
		ltMemLoader:                ltLoader,
		ltMemFlusher:               ltFlusher,
		activeCancels:              make(map[string]activeRunCancel),
		knowledgeRetriever:         config.KnowledgeRetriever,
	}
}

func memoryClassifierModel(config RuntimeConfig) string {
	if model := strings.TrimSpace(config.MemoryClassifierModel); model != "" {
		return model
	}
	return strings.TrimSpace(config.Model)
}

type providerChatOutcome struct {
	response providers.ChatResponse
	err      error
}

func (r *Runtime) chatWithProviderTimeout(ctx context.Context, request providers.ChatRequest) (providers.ChatResponse, error) {
	if r.providerTimeout <= 0 {
		return r.provider.Chat(ctx, request)
	}
	providerCtx, cancel := context.WithTimeout(ctx, r.providerTimeout)
	defer cancel()
	outcomes := make(chan providerChatOutcome, 1)
	go func() {
		response, err := r.provider.Chat(providerCtx, request)
		outcomes <- providerChatOutcome{response: response, err: err}
	}()
	select {
	case outcome := <-outcomes:
		return outcome.response, outcome.err
	case <-providerCtx.Done():
		return providers.ChatResponse{}, providerCtx.Err()
	}
}

func (r *Runtime) Run(ctx context.Context, message contracts.UserMessage) (response contracts.AgentResponse, err error) {
	ctx = toolhooks.WithRequestContext(ctx, message.RequestID, message.SessionID)

	// Register a cancel function so that CancelSession can interrupt this run.
	runCtx, runCancel := context.WithCancel(ctx)
	runToken := atomic.AddUint64(&runtimeRunCancelToken, 1)
	defer func() {
		runCancel()
		r.cancelMu.Lock()
		if current, ok := r.activeCancels[message.SessionID]; ok && current.token == runToken {
			delete(r.activeCancels, message.SessionID)
		}
		r.cancelMu.Unlock()
	}()
	r.cancelMu.Lock()
	if existing, ok := r.activeCancels[message.SessionID]; ok {
		existing.cancel() // cancel any previous run for this session
	}
	r.activeCancels[message.SessionID] = activeRunCancel{token: runToken, cancel: runCancel}
	r.cancelMu.Unlock()
	ctx = runCtx

	if r.compactor != nil {
		sessionID := message.SessionID
		defer func() { go r.maybeCompactAsync(sessionID) }()
	}
	emitProgress(ctx, ProgressEvent{Stage: ProgressStageStarted, Message: "Agent run started"})
	base := contracts.AgentResponse{
		RequestID: message.RequestID,
		SessionID: message.SessionID,
		Status:    contracts.AgentStatusFailed,
		Data:      map[string]any{},
	}
	if traceID := traceutil.TraceIDFromContext(ctx); traceID != "" {
		base.Data["trace_id"] = traceID
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
	if errShape := r.expirePendingApprovalIfNeeded(ctx, message.SessionID); errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		base.FailureReason = string(orchestration.FailureReasonApprovalExpired)
		return base, nil
	}
	runState, errShape := r.startRunState(ctx, message)
	if errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	ctx = withParentRunID(ctx, runState.RunID)
	ctx = WithPlanScope(ctx, message.SessionID, runState.RunID)
	defer func() {
		currentState := runState
		if loaded, loadErr := r.stateStore.GetRun(context.WithoutCancel(ctx), runState.RunID); loadErr == nil {
			currentState = loaded
		}
		r.finishPlanLifecycle(context.WithoutCancel(ctx), currentState)
	}()
	ctx = providers.WithUsageRecorder(ctx, func(usage *providers.Usage) {
		r.recordLLMUsageCost(ctx, &runState, usage)
	})
	// Panic guard: ensure finishRunState always runs if a panic occurs after runState is initialized.
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("agent runtime panic recovered",
				"run_id", runState.RunID,
				"request_id", message.RequestID,
				"session_id", message.SessionID,
				"panic", rec,
			)
			currentState, getErr := r.stateStore.GetRun(context.WithoutCancel(ctx), runState.RunID)
			if getErr == nil && currentState.Status == RuntimeRunStatusRunning {
				_, _ = r.finishRunState(context.WithoutCancel(ctx), currentState, RuntimeRunStatusFailed, string(orchestration.FailureReasonAborted))
			}
			base.Status = contracts.AgentStatusFailed
			base.FailureReason = string(orchestration.FailureReasonAborted)
			base.Error = internalError("agent runtime panic", contracts.ErrorSourceAgent)
			base.Message = base.Error.Message
			response = base
			err = nil
		}
	}()
	failStartedRun := func(errShape *contracts.ErrorShape) (contracts.AgentResponse, error) {
		if _, finishErr := r.finishRunState(ctx, runState, RuntimeRunStatusFailed, string(orchestration.FailureReasonAborted)); finishErr != nil {
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
	r.hydratePlanFromTranscript(message.SessionID, runState.RunID, transcript)
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
				updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusCompleted, string(orchestration.FailureReasonNone))
				if errShape != nil {
					return failStartedRun(errShape)
				}
				base.FailureReason = updatedState.FailureReason
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
		runState.ShortLabel = strings.TrimSpace(turnMeta.ShortLabel)
		runState.Category = normalizeRunCategory(turnMeta.Category)
		if errShape := r.updateRunState(ctx, runState); errShape != nil {
			return failStartedRun(errShape)
		}
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
	freshWorkspaceReadRequest := !isRevision &&
		!activeClarification &&
		shouldIsolateMemoryForStandaloneReadRequest(message.Text)
	isolatedNewWriteRequest := !isRevision &&
		!resultFollowUp &&
		!resolvedReference &&
		turnMeta.MemoryMode == memoryModeFresh &&
		shouldIsolateMemoryForNewRequest(message.Text, activeClarification)

	userMessage := providers.Message{Role: providers.MessageRoleUser, Content: messageTextWithAttachmentContext(message)}
	if _, isContinuation := message.Metadata["continuationOf"]; isContinuation {
		if strings.TrimSpace(userMessage.Content) == "" {
			completedTool, _ := message.Metadata["completedTool"].(string)
			if completedTool != "" {
				userMessage.Content = fmt.Sprintf("Continuing after approved tool: %s", completedTool)
			} else {
				userMessage.Content = "Continuing after approved action"
			}
		}
	}
	transcript = append(transcript, userMessage)
	if errShape := r.appendTranscriptMessage(ctx, runState, userMessage); errShape != nil {
		errShape.Message = strings.Replace(errShape.Message, "append message:", "append user message:", 1)
		return failStartedRun(errShape)
	}
	r.recordRepeatedLongTermHabits(ctx, message.SessionID, runState.RunID, message.RequestID, transcript)
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
		providerTranscript = transcriptWithLastUserContent(transcript, message.Text)
	} else if isolatedNewWriteRequest || freshWorkspaceReadRequest {
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

	var providerKnowledge *knowledge.LinkedContext
	if r.knowledgeRetriever != nil && !freshWorkspaceReadRequest {
		linked, retrieveErr := r.knowledgeRetriever.Retrieve(ctx, knowledge.Query{
			Text:      understandingMessage.Text,
			SessionID: message.SessionID,
			RunID:     runState.RunID,
			RequestID: message.RequestID,
			Limit:     15,
			Now:       r.now().UTC(),
		})
		if retrieveErr != nil {
			r.logger.Warn("linked knowledge retrieval failed",
				"request_id", message.RequestID,
				"session_id", message.SessionID,
				"error", retrieveErr,
			)
		} else if len(linked.Items) > 0 {
			providerKnowledge = &linked
		}
	}

	toolResults := []contracts.ToolResult{}
	iterationBudget := NewIterationBudget(r.iterationBudgetLimit)
	housekeepingRefunds := 0
	housekeepingRefundLimit := r.iterationBudgetLimit
agentLoop:
	for iteration := 1; ; iteration++ {
		if !iterationBudget.Consume() {
			break agentLoop
		}
		runState.IterationCount = iteration
		if errShape := r.updateRunState(ctx, runState); errShape != nil {
			base.Error = errShape
			base.Message = errShape.Message
			return base, nil
		}
		r.logger.Debug("agent iteration started", "request_id", message.RequestID, "session_id", message.SessionID, "iteration", iteration)
		if resp := r.handleContextError(ctx, runState, toolResults); resp != nil {
			resp.RequestID = message.RequestID
			resp.SessionID = message.SessionID
			return *resp, nil
		}
		emitProgress(ctx, ProgressEvent{Stage: ProgressStageThinking, Message: "Agent is thinking"})
		providerMessages := r.withRuntimeSystemPromptOptions(providerTranscript, providerMemory, providerReference, runtimePromptOptions{IncludeLongTermMemory: !freshWorkspaceReadRequest, LinkedKnowledge: providerKnowledge})
		if freshWorkspaceReadRequest {
			providerMessages = append([]providers.Message{freshWorkspaceReadSystemMessage()}, providerMessages...)
		}
		if prompt := r.activePlanPrompt(message.SessionID, runState.RunID); prompt != "" {
			providerMessages = append([]providers.Message{{Role: providers.MessageRoleSystem, Content: prompt}}, providerMessages...)
		}
		providerResponse, err := r.chatWithProviderTimeout(ctx, providers.ChatRequest{
			Model:      r.model,
			Messages:   providerMessages,
			Tools:      r.providerTools(),
			ToolChoice: "auto",
		})
		if resp := r.handleContextError(ctx, runState, toolResults); resp != nil {
			resp.RequestID = message.RequestID
			resp.SessionID = message.SessionID
			return *resp, nil
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				code := contracts.ErrorProviderUnavailable
				messageText := "provider chat canceled: " + err.Error()
				base.Error = &contracts.ErrorShape{
					Code:      code,
					Message:   messageText,
					Source:    contracts.ErrorSourceProvider,
					Retryable: true,
				}
				r.appendRunEvent(ctx, runState.RunID, "provider.failed", map[string]any{
					"iteration": iteration,
					"code":      string(code),
					"retryable": true,
					"timeout":   false,
				})
				updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusCancelled, string(orchestration.FailureReasonAborted))
				if errShape != nil {
					base.Error = errShape
				}
				base.FailureReason = updatedState.FailureReason
				base.Message = base.Error.Message
				return base, nil
			}
			code := contracts.ErrorProviderError
			retryable := providers.IsRetryableError(err)
			messageText := "provider chat failed: " + err.Error()
			if errors.Is(err, context.DeadlineExceeded) {
				code = contracts.ErrorProviderUnavailable
				retryable = true
				messageText = fmt.Sprintf("provider chat timed out after %s", r.providerTimeout)
			}
			if retryable {
				code = contracts.ErrorProviderUnavailable
			}
			base.Error = &contracts.ErrorShape{
				Code:      code,
				Message:   messageText,
				Source:    contracts.ErrorSourceProvider,
				Retryable: retryable,
			}
			r.appendRunEvent(ctx, runState.RunID, "provider.failed", map[string]any{
				"iteration": iteration,
				"code":      string(code),
				"retryable": retryable,
				"timeout":   errors.Is(err, context.DeadlineExceeded),
			})
			reason := orchestration.FailureReasonProviderError
			if retryable {
				reason = orchestration.FailureReasonProviderUnavailable
			}
			updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusFailed, string(reason))
			if errShape != nil {
				base.Error = errShape
			}
			base.FailureReason = updatedState.FailureReason
			base.Message = base.Error.Message
			return base, nil
		}

		assistantMessage := providerResponse.Message
		if assistantMessage.Role == "" {
			assistantMessage.Role = providers.MessageRoleAssistant
		}
		if len(assistantMessage.ToolCalls) == 0 && freshWorkspaceReadRequest && len(toolResults) > 0 {
			if rendered, ok := freshWorkspaceReadAnswerFromToolResults(toolResults); ok {
				assistantMessage.Content = rendered
			}
		}
		transcript = append(transcript, assistantMessage)
		providerTranscript = append(providerTranscript, assistantMessage)
		if errShape := r.appendTranscriptMessage(ctx, runState, assistantMessage); errShape != nil {
			errShape.Message = strings.Replace(errShape.Message, "append message:", "append assistant message:", 1)
			return failStartedRun(errShape)
		}

		if len(assistantMessage.ToolCalls) == 0 {
			if freshWorkspaceReadRequest && len(toolResults) == 0 {
				r.logger.Info("assistant answered fresh workspace read without tool call; retrying for read tool",
					"request_id", message.RequestID,
					"session_id", message.SessionID,
					"iteration", iteration,
					"content_preview", logPreview(assistantMessage.Content, 180),
				)
				if len(providerTranscript) > 0 {
					providerTranscript = providerTranscript[:len(providerTranscript)-1]
				}
				providerTranscript = append(providerTranscript, providers.Message{
					Role: providers.MessageRoleSystem,
					Content: strings.TrimSpace(`The previous assistant response answered a fresh Google Workspace read request without calling a read tool.
Do not answer from memory, transcript, or older tool results.
Call the appropriate read tool now, then answer only from this request's tool result.`),
				})
				continue
			}
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
			if _, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusCompleted, string(orchestration.FailureReasonNone)); errShape != nil {
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
				Plan:        r.responsePlan(message.SessionID, runState.RunID),
			}, nil
		}

		evidenceText := providerTranscriptEvidenceText(providerTranscript)
		if batch, ok := r.prepareParallelBatch(ctx,
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
				decision := r.stampPolicyRef(runState.RunID, call.ID, r.decideToolCall(ctx, call, batch[i].definition, true))
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
				result.PolicyDecisionRef = decision.PolicyDecisionRef
				batchResults[i].result = result
				emitProgress(ctx, ProgressEvent{
					Stage:      stage,
					ToolName:   call.Name,
					ToolCallID: call.ID,
					Message:    "Tool finished",
				})
				if errShape := r.recordRuntimeToolCall(ctx, &runState, runState.RunID, call, result, outcome.duration, ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordActionResultForRun(ctx, message.SessionID, runState.RunID, message.RequestID, result); errShape != nil {
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
				updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusFailed, string(orchestration.FailureReasonToolError))
				if errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				base.FailureReason = updatedState.FailureReason
				base.ToolResults = toolResults
				base.Error = toolErrorShape(batchResults[0].result)
				base.Message = base.Error.Message
				return base, nil
			}
			if onlyPlanToolCalls(assistantMessage.ToolCalls) && housekeepingRefunds < housekeepingRefundLimit {
				iterationBudget.Refund()
				housekeepingRefunds++
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

			toolCallMissingFields := pendingMissingFieldsForToolCall(providerToolCall, definition, found, activeClarification, currentRequestText)
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
				if shouldResolveDriveMoveBeforeClarification(providerToolCall, currentRequestText, toolCallMissingFields) {
					toolMessage := providers.Message{
						Role:       providers.MessageRoleTool,
						ToolCallID: providerToolCall.ID,
						Content:    truncateToolContentForLLM(driveMoveResolutionObservation(toolCallMissingFields)),
					}
					transcript = append(transcript, toolMessage)
					providerTranscript = append(providerTranscript, toolMessage)
					if err := r.appendToolObservationForRun(ctx, message.SessionID, runState.RunID, runState.RequestID, toolMessage); err != nil {
						base.Error = err
						base.Message = err.Message
						return base, nil
					}
					for _, skipped := range skippedToolObservationMessages(assistantMessage.ToolCalls[index+1:], "ACTION_BLOCKED_BY_POLICY: skipped because the Drive move target must be resolved first") {
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
			decision := r.stampPolicyRef(runState.RunID, providerToolCall.ID, r.decideToolCall(ctx, providerToolCall, definition, found))
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
			if errShape := r.recordRuntimeRiskDecision(ctx, runState, providerToolCall, decision); errShape != nil {
				base.Error = errShape
				base.Message = errShape.Message
				return base, nil
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
				result.PolicyDecisionRef = decision.PolicyDecisionRef
				if errShape := r.recordRuntimeToolCall(ctx, &runState, runState.RunID, providerToolCall, result, time.Since(startedAt), ""); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if errShape := r.recordActionResultForRun(ctx, message.SessionID, runState.RunID, message.RequestID, result); errShape != nil {
					base.Error = errShape
					base.Message = errShape.Message
					return base, nil
				}
				if resp := r.handleContextError(ctx, runState, toolResults); resp != nil {
					resp.RequestID = message.RequestID
					resp.SessionID = message.SessionID
					return *resp, nil
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
					updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusFailed, string(orchestration.FailureReasonToolError))
					if errShape != nil {
						base.Error = errShape
						base.Message = errShape.Message
						return base, nil
					}
					base.FailureReason = updatedState.FailureReason
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
				if r.telemetry != nil {
					r.telemetry.RecordApproval(ctx, ApprovalTelemetryEvent{
						Status:     ActionStatusPendingApproval,
						ApprovalID: approval.ApprovalID,
						RequestID:  message.RequestID,
						SessionID:  message.SessionID,
						ToolCallID: approval.ToolCallID,
						ToolName:   providerToolCall.Name,
						RiskLevel:  approval.RiskLevel,
						ExpiresAt:  approval.ExpiresAt,
					})
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
				updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusBlocked, string(orchestration.FailureReasonPolicyBlocked))
				if errShape != nil {
					base.Error = errShape
				}
				base.FailureReason = updatedState.FailureReason
				return base, nil
			}
		}
		if onlyPlanToolCalls(assistantMessage.ToolCalls) && housekeepingRefunds < housekeepingRefundLimit {
			iterationBudget.Refund()
			housekeepingRefunds++
		}
	}

	updatedState, errShape := r.finishRunState(ctx, runState, RuntimeRunStatusIterationBudget, string(orchestration.FailureReasonIterationBudget))
	if errShape != nil {
		base.Error = errShape
		base.Message = errShape.Message
		return base, nil
	}
	base.FailureReason = updatedState.FailureReason
	return contracts.AgentResponse{
		RequestID:     message.RequestID,
		SessionID:     message.SessionID,
		Status:        contracts.AgentStatusIterationBudgetExhausted,
		FailureReason: updatedState.FailureReason,
		Message:       "agent exhausted iteration budget",
		Data:          r.traceData(referenceResolution),
		ToolResults:   toolResults,
		Error: &contracts.ErrorShape{
			Code:      contracts.ErrorIterationBudgetExhausted,
			Message:   "agent exhausted iteration budget",
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

func (r *Runtime) handleContextError(ctx context.Context, runState RunState, toolResults []contracts.ToolResult) *contracts.AgentResponse {
	err := ctx.Err()
	if err == nil {
		return nil
	}

	runStatus := RuntimeRunStatusFailed
	statusCode := contracts.ErrorProviderTimeout
	messageText := "request timed out"
	if errors.Is(err, context.Canceled) {
		runStatus = RuntimeRunStatusCancelled
		statusCode = contracts.ErrorInternal
		messageText = "request canceled"
	}

	contextReason := orchestration.FromContextError(err)
	updatedState, finishErr := r.finishRunState(context.WithoutCancel(ctx), runState, runStatus, string(contextReason))
	if finishErr != nil {
		return &contracts.AgentResponse{Error: finishErr, Message: finishErr.Message, Status: contracts.AgentStatusFailed}
	}

	return &contracts.AgentResponse{
		Status:        contracts.AgentStatusFailed,
		FailureReason: updatedState.FailureReason,
		ToolResults:   toolResults,
		Error: &contracts.ErrorShape{
			Code:      statusCode,
			Message:   messageText,
			Source:    contracts.ErrorSourceAgent,
			Retryable: false,
		},
		Message: messageText,
	}
}

// CancelSession cancels the active run for the given sessionID, if any.
// Returns true if a run was found and cancelled, false if no run was active.
func (r *Runtime) CancelSession(sessionID string) bool {
	r.cancelMu.Lock()
	entry, ok := r.activeCancels[sessionID]
	r.cancelMu.Unlock()
	if !ok {
		return false
	}
	entry.cancel()
	return true
}
