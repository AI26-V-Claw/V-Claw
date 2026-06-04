package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	agentintent "vclaw/internal/agent/intent"
	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
)

type fakeProvider struct {
	responses []providers.ChatResponse
	err       error
	calls     []providers.ChatRequest
}

type blockingRuntimeTool struct {
	release chan struct{}
}

type stubIntentClassifier struct {
	output       *agentintent.ClassificationOutput
	err          error
	calls        int
	historyCalls int
	lastHistory  []string
}

type stubTaskPlanner struct {
	result    *TaskPlanResult
	err       error
	calls     int
	lastInput TaskPlanningInput
}

type stubTurnRouter struct {
	route     TurnRoute
	err       error
	calls     int
	lastInput TurnRouteInput
}

func (c *stubIntentClassifier) Classify(context.Context, string) (*agentintent.ClassificationOutput, error) {
	c.calls++
	return c.output, c.err
}

func (c *stubIntentClassifier) ClassifyWithMemoryIsolation(_ context.Context, _ string, recentHistory []string) (*agentintent.ClassificationOutput, error) {
	c.historyCalls++
	c.lastHistory = append([]string(nil), recentHistory...)
	return c.output, c.err
}

func (p *stubTaskPlanner) Plan(_ context.Context, input TaskPlanningInput) (*TaskPlanResult, error) {
	p.calls++
	p.lastInput = input
	return p.result, p.err
}

func (r *stubTurnRouter) RouteTurn(_ context.Context, input TurnRouteInput) (TurnRoute, error) {
	r.calls++
	r.lastInput = input
	if r.err != nil {
		return TurnRoute{}, r.err
	}
	if r.route.Mode == "" {
		r.route.Mode = TurnModeToolEnabled
	}
	return r.route, nil
}

func testToolEnabledRouter() TurnRouter {
	return &stubTurnRouter{route: TurnRoute{Mode: TurnModeToolEnabled, Reason: "test"}}
}

func testNoToolRouter() TurnRouter {
	return &stubTurnRouter{route: TurnRoute{Mode: TurnModeNoTool, Reason: "test"}}
}

func (blockingRuntimeTool) Name() string                 { return "test.blocking" }
func (blockingRuntimeTool) Description() string          { return "Blocks until released." }
func (blockingRuntimeTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (blockingRuntimeTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (blockingRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }
func (t blockingRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	<-t.release
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "released",
		ContentForUser: "released",
	}
}

func (p *fakeProvider) Chat(_ context.Context, request providers.ChatRequest) (providers.ChatResponse, error) {
	p.calls = append(p.calls, request)
	if p.err != nil {
		return providers.ChatResponse{}, p.err
	}
	if len(p.responses) == 0 {
		return providers.ChatResponse{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "fallback"}}, nil
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func (p *fakeProvider) Generate(ctx context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Model: req.Model,
		Messages: []providers.Message{
			{Role: providers.MessageRoleSystem, Content: req.SystemPrompt},
			{Role: providers.MessageRoleUser, Content: req.UserPrompt},
		},
	})
	if err != nil {
		return nil, err
	}
	return &providers.GenerateResponse{Text: resp.Message.Content, Model: req.Model}, nil
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Close() error { return nil }

func runtimeTestMessage() contracts.UserMessage {
	return contracts.UserMessage{
		RequestID: "req_001",
		SessionID: "sess_001",
		Channel:   "dev",
		Text:      "hello",
		Timestamp: time.Date(2026, 5, 29, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60)),
	}
}

func TestRuntimeCompletesNormalChat(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Xin chào!"},
	}}}
	registry := tools.NewToolRegistry()
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   registry,
		TurnRouter: testNoToolRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if response.Message != "Xin chào!" {
		t.Fatalf("unexpected message: %q", response.Message)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(provider.calls))
	}
}

func TestRuntimeBypassesIntentClarificationForSafeChat(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Tôi là V-Claw."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.4,
			Reasoning:  "Yêu cầu chưa rõ.",
		},
		NeedsClarification:   true,
		ClarificationMessage: "Bạn muốn tôi tra cứu thông tin gì?",
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		TurnRouter:       testNoToolRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if response.Message != "Tôi là V-Claw." {
		t.Fatalf("unexpected message: %q", response.Message)
	}
	if classifier.calls != 0 {
		t.Fatalf("intent classifier should be bypassed, got %d calls", classifier.calls)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider chat should be called once, got %d calls", len(provider.calls))
	}
	if len(provider.calls[0].Tools) != 0 || provider.calls[0].ToolChoice != "none" {
		t.Fatalf("safe chat must run no-tool, got tools=%#v choice=%q", provider.calls[0].Tools, provider.calls[0].ToolChoice)
	}
}

func TestRuntimeBypassesTaskPlannerBeforeProviderChat(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"},
	}}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{Plan: contracts.Plan{Steps: []contracts.PlanStep{{
		ID:          "step_1",
		Description: "gmail.listEmails: đọc email gần đây",
		Status:      "pending",
	}}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:    provider,
		Registry:    tools.NewToolRegistry(),
		TaskPlanner: planner,
		TurnRouter:  testNoToolRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if planner.calls != 0 {
		t.Fatalf("task planner should be bypassed by default, got %d calls", planner.calls)
	}
	if response.Plan != nil {
		t.Fatalf("default runtime should not attach legacy planner output, got %#v", response.Plan)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	for _, msg := range provider.calls[0].Messages {
		if msg.Role == providers.MessageRoleSystem && strings.Contains(msg.Content, "Task planner result") {
			t.Fatalf("planner context should not be injected by default, got %#v", provider.calls[0].Messages)
		}
	}
}

func TestRuntimeReturnsClarificationFromClarifyTool(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:   "call_clarify",
				Name: clarifyToolName,
				Arguments: map[string]any{
					"question":       "Bạn muốn gửi email cho ai?",
					"missing_fields": []any{"recipient"},
				},
			}},
		},
	}}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{
		NeedsClarification:   true,
		ClarificationMessage: "Bạn muốn gửi email cho ai?",
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:    provider,
		Registry:    tools.NewToolRegistry(),
		TaskPlanner: planner,
		TurnRouter:  testToolEnabledRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("expected need_clarification, got %#v", response)
	}
	if response.Message != "Bạn muốn gửi email cho ai?" {
		t.Fatalf("unexpected clarification message: %q", response.Message)
	}
	if planner.calls != 0 {
		t.Fatalf("planner should be bypassed; clarify must come from clarify tool, got %d planner calls", planner.calls)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d calls", len(provider.calls))
	}
}

func TestRuntimePassesRecentSessionHistoryToTurnRouter(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "ok"},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeDangerousAction,
			Confidence: 0.95,
		},
	}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{}}
	router := &stubTurnRouter{route: TurnRoute{Mode: TurnModeToolEnabled, Reason: "test"}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Create a meeting with Bao tomorrow at 10am about sprint1",
	}); err != nil {
		t.Fatalf("append prior user message: %v", err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "What time should the meeting end?",
	}); err != nil {
		t.Fatalf("append prior assistant message: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		TaskPlanner:      planner,
		SessionStore:     store,
		TurnRouter:       router,
	})
	message := runtimeTestMessage()
	message.Text = "11am"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if classifier.historyCalls != 0 || planner.calls != 0 {
		t.Fatalf("classifier/planner should be bypassed, classifier=%d planner=%d", classifier.historyCalls, planner.calls)
	}
	joinedHistory := strings.Join(router.lastInput.RecentHistory, "\n")
	if !strings.Contains(joinedHistory, "10am") || !strings.Contains(joinedHistory, "meeting end") {
		t.Fatalf("expected prior request and clarification in router history, got %#v", router.lastInput.RecentHistory)
	}
}

func TestRuntimeStoresClarificationInSessionTranscript(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_clarify",
				Name:      clarifyToolName,
				Arguments: map[string]any{"question": "Please clarify the request."},
			}},
		},
	}}}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     tools.NewToolRegistry(),
		SessionStore: store,
		TurnRouter:   testToolEnabledRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("expected need_clarification, got %#v", response)
	}
	transcript, err := store.LoadTranscript(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(transcript) != 3 {
		t.Fatalf("expected user, assistant clarify call, and tool observation in transcript, got %#v", transcript)
	}
	if transcript[1].Role != providers.MessageRoleAssistant || len(transcript[1].ToolCalls) != 1 {
		t.Fatalf("expected assistant clarify tool call stored, got %#v", transcript[1])
	}
	if transcript[2].Role != providers.MessageRoleTool || !strings.Contains(transcript[2].Content, "Please clarify the request.") {
		t.Fatalf("expected clarify tool observation stored, got %#v", transcript[2])
	}
}

func TestRuntimeSystemPromptIncludesCurrentTimeAndCalendarRangeRules(t *testing.T) {
	now := time.Date(2026, 6, 3, 17, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	prompt := runtimeSystemPrompt(now)

	if !strings.Contains(prompt, "2026-06-03T17:30:00+07:00") {
		t.Fatalf("expected current time in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "this week") || !strings.Contains(prompt, "next Monday") {
		t.Fatalf("expected calendar range guidance in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "gmail.listEmails") || !strings.Contains(prompt, "date-only YYYY-MM-DD") || !strings.Contains(prompt, "never RFC3339") {
		t.Fatalf("expected Gmail date-only guidance in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "people.searchDirectory") || !strings.Contains(prompt, "Attendees must be valid email addresses") {
		t.Fatalf("expected attendee resolution guidance in prompt, got: %s", prompt)
	}
}

func TestNormalizeCalendarListEventsThisWeekOverridesWrongModelRange(t *testing.T) {
	now := time.Date(2026, 6, 3, 17, 39, 0, 0, time.FixedZone("ICT", 7*60*60))
	call := providers.ToolCall{
		Name: "calendar.listEvents",
		Arguments: map[string]any{
			"timeMin": "2026-06-05T00:00:00+07:00",
			"timeMax": "2026-06-12T00:00:00+07:00",
			"query":   "",
		},
	}

	normalized := normalizeProviderToolCall(now, call, "lịch trình tuần này như thế nào")

	if normalized.Arguments["timeMin"] != "2026-06-01T00:00:00+07:00" {
		t.Fatalf("unexpected timeMin: %#v", normalized.Arguments["timeMin"])
	}
	if normalized.Arguments["timeMax"] != "2026-06-08T00:00:00+07:00" {
		t.Fatalf("unexpected timeMax: %#v", normalized.Arguments["timeMax"])
	}
}

func TestNormalizeCalendarListEventsNextMonth(t *testing.T) {
	now := time.Date(2026, 6, 3, 17, 39, 0, 0, time.FixedZone("ICT", 7*60*60))
	call := providers.ToolCall{Name: "calendar.listEvents", Arguments: map[string]any{}}

	normalized := normalizeProviderToolCall(now, call, "lịch trình tháng tới")

	if normalized.Arguments["timeMin"] != "2026-07-01T00:00:00+07:00" {
		t.Fatalf("unexpected timeMin: %#v", normalized.Arguments["timeMin"])
	}
	if normalized.Arguments["timeMax"] != "2026-08-01T00:00:00+07:00" {
		t.Fatalf("unexpected timeMax: %#v", normalized.Arguments["timeMax"])
	}
}

func TestNormalizeGmailListEmailsTodayUsesDateOnlyRange(t *testing.T) {
	now := time.Date(2026, 6, 4, 9, 59, 40, 0, time.FixedZone("ICT", 7*60*60))
	userText := "kiem tra xem h\u00f4m nay email cua toi co nhung gi"
	call := providers.ToolCall{
		Name: "gmail.listEmails",
		Arguments: map[string]any{
			"after":  "today",
			"before": "today",
			"query":  "kiem tra h\u00f4m nay email cua toi co nhung gi",
		},
	}

	normalized := normalizeProviderToolCall(now, call, userText)

	if normalized.Arguments["after"] != "2026-06-04" {
		t.Fatalf("unexpected after: %#v", normalized.Arguments["after"])
	}
	if normalized.Arguments["before"] != "2026-06-05" {
		t.Fatalf("unexpected before: %#v", normalized.Arguments["before"])
	}
	if normalized.Arguments["query"] != "" {
		t.Fatalf("unexpected query: %#v", normalized.Arguments["query"])
	}
}

func TestNormalizeGmailListThreadsTodayUsesDateOnlyRange(t *testing.T) {
	now := time.Date(2026, 6, 4, 9, 59, 40, 0, time.FixedZone("ICT", 7*60*60))
	call := providers.ToolCall{
		Name: "gmail.listThreads",
		Arguments: map[string]any{
			"query": "today email",
		},
	}

	normalized := normalizeProviderToolCall(now, call, "check today email threads")

	if normalized.Arguments["after"] != "2026-06-04" {
		t.Fatalf("unexpected after: %#v", normalized.Arguments["after"])
	}
	if normalized.Arguments["before"] != "2026-06-05" {
		t.Fatalf("unexpected before: %#v", normalized.Arguments["before"])
	}
	if normalized.Arguments["query"] != "" {
		t.Fatalf("unexpected query: %#v", normalized.Arguments["query"])
	}
}

func TestNormalizeGmailListEmailsKeepsNonRelativeArgs(t *testing.T) {
	now := time.Date(2026, 6, 4, 9, 59, 40, 0, time.FixedZone("ICT", 7*60*60))
	call := providers.ToolCall{
		Name: "gmail.listEmails",
		Arguments: map[string]any{
			"after": "2026-06-01",
			"query": "from:alice@example.com",
		},
	}

	normalized := normalizeProviderToolCall(now, call, "email from alice")

	if normalized.Arguments["after"] != "2026-06-01" {
		t.Fatalf("unexpected after: %#v", normalized.Arguments["after"])
	}
	if normalized.Arguments["query"] != "from:alice@example.com" {
		t.Fatalf("unexpected query: %#v", normalized.Arguments["query"])
	}
	if _, ok := normalized.Arguments["before"]; ok {
		t.Fatalf("unexpected before: %#v", normalized.Arguments["before"])
	}
}

func TestRuntimeExecutesReadOnlyToolAndContinuesToFinalAnswer(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role:    providers.MessageRoleAssistant,
			Content: "checking time",
			ToolCalls: []providers.ToolCall{{
				ID:   "call_time",
				Name: "get_current_time",
			}},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register current time: %v", err)
	}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
		TurnRouter:   testToolEnabledRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if len(response.ToolResults) != 1 || !response.ToolResults[0].Success {
		t.Fatalf("expected successful tool result, got %#v", response.ToolResults)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(provider.calls))
	}
	secondMessages := provider.calls[1].Messages
	if len(secondMessages) != 5 {
		t.Fatalf("expected system, router context, user, assistant tool call, tool result; got %#v", secondMessages)
	}
	if secondMessages[0].Role != providers.MessageRoleSystem {
		t.Fatalf("expected system prompt first, got %#v", secondMessages[0])
	}
	if secondMessages[1].Role != providers.MessageRoleSystem || !strings.Contains(secondMessages[1].Content, "not an intent label") {
		t.Fatalf("expected router context prompt second, got %#v", secondMessages[1])
	}
	if secondMessages[4].Role != providers.MessageRoleTool || secondMessages[4].ToolCallID != "call_time" {
		t.Fatalf("unexpected tool observation message: %#v", secondMessages[4])
	}
}

func TestRuntimeEmitsProgressForToolExecution(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:   "call_time",
				Name: "get_current_time",
			}},
		}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "done"}},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register current time: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{Provider: provider, Registry: registry, TurnRouter: testToolEnabledRouter()})

	events := []ProgressEvent{}
	ctx := WithProgressSink(context.Background(), func(_ context.Context, event ProgressEvent) {
		events = append(events, event)
	})
	response, err := runtime.Run(ctx, runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}

	if !hasProgressEvent(events, ProgressStageToolStarted, "get_current_time") {
		t.Fatalf("missing tool started progress event: %#v", events)
	}
	if !hasProgressEvent(events, ProgressStageToolCompleted, "get_current_time") {
		t.Fatalf("missing tool completed progress event: %#v", events)
	}
}

func hasProgressEvent(events []ProgressEvent, stage ProgressStage, toolName string) bool {
	for _, event := range events {
		if event.Stage == stage && event.ToolName == toolName {
			return true
		}
	}
	return false
}

func TestRuntimeReturnsApprovalRequiredForSideEffectTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_write",
				Name:      "danger.count",
				Arguments: map[string]any{"value": "x"},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   registry,
		TurnRouter: testToolEnabledRouter(),
		Now:        func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}
	if response.ApprovalRequest == nil {
		t.Fatalf("expected approval request")
	}
	if response.ApprovalRequest.ToolCall.ToolName != "danger.count" {
		t.Fatalf("unexpected approval tool call: %#v", response.ApprovalRequest.ToolCall)
	}
	if executions != 0 {
		t.Fatalf("side-effect tool must not execute before approval, executions=%d", executions)
	}
}

func TestRuntimeResolvesApprovedPendingApprovalExecutesTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_write",
				Name:      "danger.count",
				Arguments: map[string]any{"value": "x"},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   registry,
		TurnRouter: testToolEnabledRouter(),
		Now:        func() time.Time { return runtimeTestMessage().Timestamp },
	})

	pending, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if pending.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", pending)
	}
	if executions != 0 {
		t.Fatalf("side-effect tool must not execute before approval, executions=%d", executions)
	}

	response, err := runtime.ResolveApproval(context.Background(), runtimeTestMessage().SessionID, contracts.ApprovalDecision{
		ApprovalID: pending.ApprovalID,
		RequestID:  "req_approval",
		Decision:   contracts.ApprovalDecisionApproved,
		DecidedBy:  "owner",
		DecidedAt:  runtimeTestMessage().Timestamp.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("resolve approval: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed after approval, got %#v", response)
	}
	if executions != 1 {
		t.Fatalf("expected side-effect tool to execute once after approval, executions=%d", executions)
	}
	if len(response.ToolResults) != 1 || !response.ToolResults[0].Success {
		t.Fatalf("expected successful tool result, got %#v", response.ToolResults)
	}
}

func TestRuntimeRejectsPendingApprovalWithoutExecutingTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role:      providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{ID: "call_write", Name: "danger.count"}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   registry,
		TurnRouter: testToolEnabledRouter(),
		Now:        func() time.Time { return runtimeTestMessage().Timestamp },
	})

	pending, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	response, err := runtime.ResolveApproval(context.Background(), runtimeTestMessage().SessionID, contracts.ApprovalDecision{
		ApprovalID: pending.ApprovalID,
		RequestID:  "req_reject",
		Decision:   contracts.ApprovalDecisionRejected,
		DecidedBy:  "owner",
		DecidedAt:  runtimeTestMessage().Timestamp.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("resolve approval: %v", err)
	}
	if response.Status != contracts.AgentStatusBlocked {
		t.Fatalf("expected blocked after rejection, got %#v", response)
	}
	if executions != 0 {
		t.Fatalf("rejected tool must not execute, executions=%d", executions)
	}
}

func TestRuntimeRevisionCommentReturnsClarificationWithoutExecutingTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role:      providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{ID: "call_write", Name: "danger.count"}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   registry,
		TurnRouter: testToolEnabledRouter(),
		Now:        func() time.Time { return runtimeTestMessage().Timestamp },
	})

	pending, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	response, err := runtime.ResolveApproval(context.Background(), runtimeTestMessage().SessionID, contracts.ApprovalDecision{
		ApprovalID: pending.ApprovalID,
		RequestID:  "req_revise",
		Decision:   contracts.ApprovalDecisionRejected,
		DecidedBy:  "owner",
		DecidedAt:  runtimeTestMessage().Timestamp.Add(time.Second),
		Comment:    "đổi giờ sang 10:00",
	})
	if err != nil {
		t.Fatalf("resolve approval: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("expected need_clarification after revision comment, got %#v", response)
	}
	if !strings.Contains(response.Message, "đổi giờ sang 10:00") {
		t.Fatalf("expected revision comment in response, got %q", response.Message)
	}
	if executions != 0 {
		t.Fatalf("revision must not execute original tool, executions=%d", executions)
	}
}

func TestRuntimeStoresToolObservationForApprovalRequiredTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_write",
				Name:      "danger.count",
				Arguments: map[string]any{"value": "x"},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
		TurnRouter:   testToolEnabledRouter(),
		Now:          func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}

	transcript, err := store.LoadTranscript(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(transcript) != 3 {
		t.Fatalf("expected user, assistant tool call, policy tool observation; got %#v", transcript)
	}
	if transcript[2].Role != providers.MessageRoleTool || transcript[2].ToolCallID != "call_write" {
		t.Fatalf("expected tool observation for approval-required call, got %#v", transcript[2])
	}
}

func TestRuntimeStoresSkippedObservationsForRemainingToolCalls(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{
				{ID: "call_write", Name: "danger.count"},
				{ID: "call_time", Name: "get_current_time"},
			},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	if err := registry.Register(tools.NewCurrentTimeToolWithClock(fixedTestTime)); err != nil {
		t.Fatalf("register time tool: %v", err)
	}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
		TurnRouter:   testToolEnabledRouter(),
		Now:          func() time.Time { return runtimeTestMessage().Timestamp },
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}

	transcript, err := store.LoadTranscript(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(transcript) != 4 {
		t.Fatalf("expected user, assistant, and two tool observations; got %#v", transcript)
	}
	if transcript[2].ToolCallID != "call_write" || transcript[3].ToolCallID != "call_time" {
		t.Fatalf("missing tool observations for all tool calls: %#v", transcript)
	}
}

func TestRuntimeProviderErrorReturnsFailedErrorShape(t *testing.T) {
	provider := &fakeProvider{err: fmt.Errorf("network down")}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   tools.NewToolRegistry(),
		TurnRouter: testToolEnabledRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderError {
		t.Fatalf("expected provider error shape, got %#v", response.Error)
	}
}

func TestRuntimeRetryableProviderErrorReturnsUnavailableShape(t *testing.T) {
	provider := &fakeProvider{err: providers.NewRetryableError(fmt.Errorf("connection reset"))}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   tools.NewToolRegistry(),
		TurnRouter: testToolEnabledRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("unexpected runtime error: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderUnavailable || !response.Error.Retryable {
		t.Fatalf("expected retryable provider unavailable error shape, got %#v", response.Error)
	}
}

func TestRuntimeToolErrorReturnsFailedErrorShape(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:        "call_invalid",
				Name:      "calculator",
				Arguments: map[string]any{"operation": "divide", "a": 10, "b": 0},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:   provider,
		Registry:   registry,
		TurnRouter: testToolEnabledRouter(),
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorToolInputInvalid {
		t.Fatalf("expected tool input error, got %#v", response.Error)
	}
}

func TestRuntimeToolTimeoutReturnsFailedErrorShape(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role:      providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{ID: "call_block", Name: "test.blocking"}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(blockingRuntimeTool{release: make(chan struct{})}); err != nil {
		t.Fatalf("register blocking tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:    provider,
		Registry:    registry,
		TurnRouter:  testToolEnabledRouter(),
		ToolTimeout: time.Millisecond,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderTimeout {
		t.Fatalf("expected timeout error, got %#v", response.Error)
	}
}

func TestRuntimeStopsAtMaxIterations(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{Role: providers.MessageRoleAssistant, ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 1, "b": 1}}}}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, ToolCalls: []providers.ToolCall{{ID: "call_2", Name: "calculator", Arguments: map[string]any{"operation": "add", "a": 2, "b": 2}}}}},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(tools.NewCalculatorTool()); err != nil {
		t.Fatalf("register calculator: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:      provider,
		Registry:      registry,
		TurnRouter:    testToolEnabledRouter(),
		MaxIterations: 2,
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorMaxIterationsExceeded {
		t.Fatalf("expected max iteration error, got %#v", response.Error)
	}
}
