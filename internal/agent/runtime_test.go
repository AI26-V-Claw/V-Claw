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

type calendarCreateRuntimeTool struct {
	executions *int
}

type chatListSpacesRuntimeTool struct {
	executions *int
}

type chatSendRuntimeTool struct {
	executions *int
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

func (calendarCreateRuntimeTool) Name() string { return "calendar.createEvent" }
func (calendarCreateRuntimeTool) Description() string {
	return "Create a new event in Google Calendar."
}
func (calendarCreateRuntimeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string"},
			"start": map[string]any{"type": "string"},
			"end":   map[string]any{"type": "string"},
		},
		"required": []string{"title", "start", "end"},
	}
}
func (calendarCreateRuntimeTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (calendarCreateRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (t calendarCreateRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.executions != nil {
		(*t.executions)++
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "created",
		ContentForUser: "created",
	}
}

func (chatListSpacesRuntimeTool) Name() string        { return "chat.listSpaces" }
func (chatListSpacesRuntimeTool) Description() string { return "List Google Chat spaces." }
func (chatListSpacesRuntimeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{}}
}
func (chatListSpacesRuntimeTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (chatListSpacesRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }
func (t chatListSpacesRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.executions != nil {
		(*t.executions)++
	}
	content := "- spaces/A | VClaw | GROUP_CHAT | https://chat.google.com/room/A"
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

func (chatSendRuntimeTool) Name() string        { return "chat.sendMessage" }
func (chatSendRuntimeTool) Description() string { return "Send a Google Chat message." }
func (chatSendRuntimeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space": map[string]any{"type": "string"},
			"text":  map[string]any{"type": "string"},
		},
		"required": []string{"space", "text"},
	}
}
func (chatSendRuntimeTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (chatSendRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (t chatSendRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.executions != nil {
		(*t.executions)++
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "sent",
		ContentForUser: "sent",
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

func providerMessagesContent(messages []providers.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}

func transcriptContains(messages []providers.Message, needle string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}

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
		Provider: provider,
		Registry: registry,
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

func TestRuntimeReturnsClarificationFromIntentClassifierBeforeProviderChat(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "should not be called"},
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
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("expected need_clarification, got %#v", response)
	}
	if response.Message != "Bạn muốn tôi tra cứu thông tin gì?" {
		t.Fatalf("unexpected clarification message: %q", response.Message)
	}
	if classifier.calls != 1 {
		t.Fatalf("expected classifier to be called once, got %d", classifier.calls)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider chat should not be called before clarification, got %d calls", len(provider.calls))
	}
}

func TestRuntimeSuppressesGenericIntentClarificationForActionableEmailRequest(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Bạn muốn dùng chủ đề và nội dung email là gì?"},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
			Reasoning:  "Chưa đủ thông tin.",
		},
		NeedsClarification:   true,
		ClarificationMessage: "Bạn có thể nói rõ hơn bạn muốn tôi làm gì không?",
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
	})
	message := runtimeTestMessage()
	message.Text = "Viết cho tôi một email gửi tới baolnc@vclaw.site"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed provider response, got %#v", response)
	}
	if response.Message != "Bạn muốn dùng chủ đề và nội dung email là gì?" {
		t.Fatalf("unexpected response: %q", response.Message)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call after suppressing generic clarification, got %d", len(provider.calls))
	}
}

func TestRuntimeDefersGenericIntentClarificationToProviderForCalendarRead(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Hom nay ban co mot lich."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
			Reasoning:  "Chua chac chan.",
		},
		NeedsClarification:   true,
		ClarificationMessage: "Ban co the noi ro hon ban muon toi lam gi khong?",
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
	})
	message := runtimeTestMessage()
	message.Text = "trong calendar hom nay co cuoc hop nao khong"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed provider response, got %#v", response)
	}
	if response.Message != "Hom nay ban co mot lich." {
		t.Fatalf("unexpected response: %q", response.Message)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call after deferring generic clarification, got %d", len(provider.calls))
	}
}

func TestRuntimeIncludesAttachmentPathsInProviderUserMessage(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "ok"},
	}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: tools.NewToolRegistry(),
	})
	message := runtimeTestMessage()
	message.Text = "gửi file này vào nhóm VClaw"
	message.Metadata = map[string]any{"attachmentPaths": []string{`D:\tmp\demo.png`}}

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if !strings.Contains(joined, "Attachment paths") || !strings.Contains(joined, `D:\tmp\demo.png`) {
		t.Fatalf("expected attachment context in provider messages, got %s", joined)
	}
}

func TestRuntimePassesAttachmentPathsToTaskPlanner(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "ok"},
	}}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:    provider,
		Registry:    tools.NewToolRegistry(),
		TaskPlanner: planner,
	})
	message := runtimeTestMessage()
	message.Text = "Viết email kèm file tôi đã gửi tới baolnc@vclaw.site"
	message.Metadata = map[string]any{"attachmentPaths": []string{`D:\tmp\v_claw_architect_claude.pdf`}}

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if planner.calls != 1 {
		t.Fatalf("expected planner call, got %d", planner.calls)
	}
	if !strings.Contains(planner.lastInput.Message.Text, "Attachment paths") ||
		!strings.Contains(planner.lastInput.Message.Text, `D:\tmp\v_claw_architect_claude.pdf`) {
		t.Fatalf("expected planner to receive attachment context, got %q", planner.lastInput.Message.Text)
	}
}

func TestRuntimeAddsTaskPlanBeforeProviderChat(t *testing.T) {
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
	})

	response, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if planner.calls != 1 {
		t.Fatalf("expected planner call, got %d", planner.calls)
	}
	if response.Plan == nil || len(response.Plan.Steps) != 1 {
		t.Fatalf("expected response plan, got %#v", response.Plan)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	foundPlanPrompt := false
	for _, msg := range provider.calls[0].Messages {
		if msg.Role == providers.MessageRoleSystem && strings.Contains(msg.Content, "Task planner result") {
			foundPlanPrompt = true
			break
		}
	}
	if !foundPlanPrompt {
		t.Fatalf("expected task planner context prompt, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeReturnsClarificationFromTaskPlannerBeforeProviderChat(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "should not be called"},
	}}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{
		NeedsClarification:   true,
		ClarificationMessage: "Bạn muốn gửi email cho ai?",
	}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:    provider,
		Registry:    tools.NewToolRegistry(),
		TaskPlanner: planner,
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
	if len(provider.calls) != 0 {
		t.Fatalf("provider chat should not be called before planning clarification, got %d calls", len(provider.calls))
	}
}

func TestRuntimePassesRecentSessionHistoryToClassifierAndPlanner(t *testing.T) {
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
	if classifier.historyCalls != 1 {
		t.Fatalf("expected memory-aware classifier call, got %d", classifier.historyCalls)
	}
	joinedHistory := strings.Join(classifier.lastHistory, "\n")
	if !strings.Contains(joinedHistory, "10am") || !strings.Contains(joinedHistory, "meeting end") {
		t.Fatalf("expected prior request and clarification in classifier history, got %#v", classifier.lastHistory)
	}
	plannerHistory := strings.Join(planner.lastInput.RecentHistory, "\n")
	if !strings.Contains(plannerHistory, "10am") || !strings.Contains(plannerHistory, "meeting end") {
		t.Fatalf("expected prior request and clarification in planner history, got %#v", planner.lastInput.RecentHistory)
	}
}

func TestRuntimeStoresClarificationInSessionTranscript(t *testing.T) {
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
		},
		NeedsClarification:   true,
		ClarificationMessage: "Please clarify the request.",
	}}
	store := sessions.NewInMemoryStore()
	runtime := NewRuntime(RuntimeConfig{
		Provider:         &fakeProvider{},
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
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
	if len(transcript) != 2 {
		t.Fatalf("expected user and assistant clarification in transcript, got %#v", transcript)
	}
	if transcript[1].Role != providers.MessageRoleAssistant || transcript[1].Content != "Please clarify the request." {
		t.Fatalf("expected assistant clarification stored, got %#v", transcript[1])
	}
	memory, err := store.LoadMemory(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if memory.PendingClarification == nil || memory.PendingClarification.Question != "Please clarify the request." {
		t.Fatalf("expected pending clarification stored, got %#v", memory.PendingClarification)
	}
}

func TestRuntimeActiveFollowUpBypassesClassifierClarification(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "continuing"},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
		},
		NeedsClarification:   true,
		ClarificationMessage: "Please clarify the request.",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Create a meeting with Bao tomorrow at 10am",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "What time should the meeting end?",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "11am"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed instead of clarification, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider to receive active follow-up, got %d calls", len(provider.calls))
	}
	if classifier.historyCalls != 1 {
		t.Fatalf("expected memory-aware classifier call, got %d", classifier.historyCalls)
	}
}

func TestRuntimeCalendarTimeRangeFollowUpBypassesClassifierClarification(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "continuing"},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
		},
		NeedsClarification:   true,
		ClarificationMessage: "Ban co the noi ro hon ban muon toi lam gi khong?",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Tao lich hop ngay mai cho toi",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Vui long cung cap thoi gian bat dau va ket thuc cho cuoc hop ngay mai cua ban?",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "tu 17h00 den 18h00"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed instead of clarification, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider to receive calendar time follow-up, got %d calls", len(provider.calls))
	}
	if classifier.historyCalls != 1 {
		t.Fatalf("expected memory-aware classifier call, got %d", classifier.historyCalls)
	}
	if !strings.Contains(providerMessagesContent(provider.calls[0].Messages), "17h00") {
		t.Fatalf("expected provider transcript to include time range follow-up, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimePendingClarificationUsesLLMResolverForNaturalFollowUp(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: `{"is_answer":true,"is_new_request":false,"updated_request":"Tao lich hop ngay mai luc 17h00, ket thuc 18h00","provided_fields":["start","end"],"still_missing":[],"reason":"Nguoi dung tra loi thoi gian hop."}`}},
		{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "continuing"}},
	}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
		},
		NeedsClarification:   true,
		ClarificationMessage: "Please clarify the request.",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.SaveMemory(ctx, "sess_001", sessions.SessionMemory{
		PendingClarification: &sessions.PendingClarification{
			OriginalRequest: "Tao lich hop ngay mai cho toi",
			Question:        "Vui long cung cap thoi gian bat dau va ket thuc cho cuoc hop ngay mai cua ban?",
			ToolName:        "calendar.createEvent",
			MissingFields:   []string{"start", "end"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "luc tan hoc"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed instead of clarification, got %#v", response)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected resolver and main provider calls, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[1].Messages)
	if !strings.Contains(joined, "Tao lich hop ngay mai luc 17h00") {
		t.Fatalf("expected provider to receive LLM-merged request, got %s", joined)
	}
	memory, err := store.LoadMemory(ctx, "sess_001")
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if memory.PendingClarification != nil {
		t.Fatalf("expected pending clarification cleared after answer, got %#v", memory.PendingClarification)
	}
}

func TestRuntimeActiveFollowUpBypassesPlannerClarification(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "continuing"},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeDangerousAction,
			Confidence: 0.9,
		},
	}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{
		NeedsClarification:   true,
		ClarificationMessage: "Please clarify the request.",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Create a meeting with Bao tomorrow at 10am",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Do you want to add a location?",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		TaskPlanner:      planner,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "no"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed instead of planner clarification, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider to receive active follow-up, got %d calls", len(provider.calls))
	}
	if !strings.Contains(planner.lastInput.Message.Text, "current_user_answer") {
		t.Fatalf("expected contextual follow-up text for planner, got %q", planner.lastInput.Message.Text)
	}
}

func TestRuntimeDoesNotReuseOldWriteDetailsForNewRequest(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Bạn muốn tạo lịch vào thời gian nào?"},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeDangerousAction,
			Confidence: 0.9,
		},
	}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.SaveMemory(ctx, "sess_001", sessions.SessionMemory{
		Summary: "Old request: create a meeting with Bao at 10am about Hoàn thành chức năng HITL.",
		LastActionResults: []sessions.ActionResult{{
			ToolName:  "calendar.createEvent",
			Content:   "Event created with attendee baolnc@vclaw.site at 10am",
			CreatedAt: runtimeTestMessage().Timestamp,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Tạo lịch họp với Bao ngày mai 10am-11am, tiêu đề Hoàn thành chức năng HITL",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Bạn có muốn thêm địa điểm cho cuộc họp không?",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		TaskPlanner:      planner,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "Tạo lịch họp cho tôi"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if classifier.historyCalls != 0 {
		t.Fatalf("new write request must not use classifier history, got %d history calls", classifier.historyCalls)
	}
	if len(planner.lastInput.RecentHistory) != 0 {
		t.Fatalf("new write request must not pass planner history, got %#v", planner.lastInput.RecentHistory)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if strings.Contains(joined, "Hoàn thành chức năng HITL") || strings.Contains(joined, "baolnc") || strings.Contains(joined, "10am") {
		t.Fatalf("provider should not receive old write details, got: %s", joined)
	}
	if !strings.Contains(joined, "Tạo lịch họp cho tôi") {
		t.Fatalf("provider should receive current request, got: %s", joined)
	}
}

func TestRuntimeClarifiesCalendarCreateEventWhenCurrentRequestIsUnderspecified(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:   "call_calendar",
				Name: "calendar.createEvent",
				Arguments: map[string]any{
					"title": "Hoàn thành chức năng HITL",
					"start": "2026-06-04T10:00:00+07:00",
					"end":   "2026-06-04T11:00:00+07:00",
				},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(calendarCreateRuntimeTool{executions: &executions}); err != nil {
		t.Fatalf("register calendar tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})
	message := runtimeTestMessage()
	message.Text = "Tạo lịch họp cho tôi"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusNeedClarification {
		t.Fatalf("expected need_clarification, got %#v", response)
	}
	if !strings.Contains(response.Message, "tiêu đề") || !strings.Contains(response.Message, "ngày giờ") {
		t.Fatalf("expected missing information question, got %q", response.Message)
	}
	if executions != 0 {
		t.Fatalf("calendar create must not execute when underspecified, executions=%d", executions)
	}
	if runtime.HasPendingApproval(message.SessionID) {
		t.Fatal("underspecified calendar create must not create pending approval")
	}
}

func TestRuntimeRemovesStaleAttendeesFromActiveFollowUpApproval(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{
			Role: providers.MessageRoleAssistant,
			ToolCalls: []providers.ToolCall{{
				ID:   "call_calendar",
				Name: "calendar.createEvent",
				Arguments: map[string]any{
					"title":     "Họp",
					"start":     "2026-06-04T10:00:00+07:00",
					"end":       "2026-06-04T12:00:00+07:00",
					"attendees": []any{"baolnc@vclaw.site"},
				},
			}},
		},
	}}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(calendarCreateRuntimeTool{executions: &executions}); err != nil {
		t.Fatalf("register calendar tool: %v", err)
	}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Tạo lịch họp với Bao ngày mai 10am-11am, tiêu đề Hoàn thành chức năng HITL",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Bạn có muốn thêm địa điểm cho cuộc họp không?",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "Tạo lịch họp cho tôi",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Bạn muốn đặt tiêu đề và thời gian nào?",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     registry,
		SessionStore: store,
	})
	message := runtimeTestMessage()
	message.Text = "Thời gian từ 10am đến 12am"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}
	if response.ApprovalRequest == nil {
		t.Fatal("expected approval request")
	}
	if _, ok := response.ApprovalRequest.ToolCall.Input["attendees"]; ok {
		t.Fatalf("stale attendees must be removed from approval input, got %#v", response.ApprovalRequest.ToolCall.Input)
	}
	if executions != 0 {
		t.Fatalf("calendar create must wait for approval, executions=%d", executions)
	}
}

func TestRuntimeRetriesTextualApprovalRequestAsToolCall(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{
			Message: providers.Message{
				Role: providers.MessageRoleAssistant,
				Content: `Tôi sẽ tạo một sự kiện lịch họp với những thông tin sau:
- Tiêu đề: hoàn thành chức năng HITL
- Thời gian bắt đầu: 10:00
- Thời gian kết thúc: 11:00

Xin vui lòng xác nhận để tôi tiến hành tạo sự kiện này.`,
			},
		},
		{
			Message: providers.Message{
				Role: providers.MessageRoleAssistant,
				ToolCalls: []providers.ToolCall{{
					ID:   "call_calendar",
					Name: "calendar.createEvent",
					Arguments: map[string]any{
						"title": "hoàn thành chức năng HITL",
						"start": "2026-06-04T10:00:00+07:00",
						"end":   "2026-06-04T11:00:00+07:00",
					},
				}},
			},
		},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(calendarCreateRuntimeTool{executions: &executions}); err != nil {
		t.Fatalf("register calendar tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
	})
	message := runtimeTestMessage()
	message.Text = "Tạo lịch họp tiêu đề hoàn thành chức năng HITL vào ngày mai từ 10am đến 11am"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required, got %#v", response)
	}
	if response.ApprovalRequest == nil {
		t.Fatal("expected approval request")
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected provider retry for tool call, got %d calls", len(provider.calls))
	}
	if executions != 0 {
		t.Fatalf("calendar create must wait for approval, executions=%d", executions)
	}
}

func TestClarificationAnswerDetection(t *testing.T) {
	trueCases := []string{
		"không",
		"thời gian họp là 1 tiếng",
		"11am",
		"17h00",
		"tu 17h00 den 18h00",
		"thêm baolnc@vclaw.site",
	}
	for _, text := range trueCases {
		if !isLikelyClarificationAnswer(text) {
			t.Fatalf("expected clarification answer for %q", text)
		}
	}

	falseCases := []string{
		"Tạo lịch họp cho tôi",
		"liệt kê email gần đây",
		"xóa file test.md",
		"tiep theo bay gio gui vao trong nhom chat VClaw, thong bao ve cuoc hop Demo Sprint1",
	}
	for _, text := range falseCases {
		if isLikelyClarificationAnswer(text) {
			t.Fatalf("expected new request, not clarification answer, for %q", text)
		}
	}
}

func TestPotentialWriteRequestDetectsGoogleChatGroupSend(t *testing.T) {
	text := "tiep theo bay gio gui vao trong nhom chat VClaw, thong bao ve cuoc hop Demo Sprint1"
	if !isPotentialWriteRequest(text) {
		t.Fatalf("expected Google Chat group send to be a write request")
	}
}

func TestRuntimeRecordActionResultClearsPendingClarification(t *testing.T) {
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.SaveMemory(ctx, "sess_001", sessions.SessionMemory{
		PendingClarification: &sessions.PendingClarification{
			OriginalRequest: "viet email cho Bao",
			Question:        "Ban muon dung subject nao?",
			ToolName:        "gmail.createDraft",
			MissingFields:   []string{"subject"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     &fakeProvider{},
		Registry:     tools.NewToolRegistry(),
		SessionStore: store,
	})
	errShape := runtime.recordActionResult(ctx, "sess_001", tools.ToolResult{
		ToolCallID:    "call_001",
		ToolName:      "gmail.sendDraft",
		Success:       true,
		ContentForLLM: "Email sent.",
	})
	if errShape != nil {
		t.Fatalf("record action result: %#v", errShape)
	}
	memory, err := store.LoadMemory(ctx, "sess_001")
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if memory.PendingClarification != nil {
		t.Fatalf("expected pending clarification cleared after successful action, got %#v", memory.PendingClarification)
	}
	if len(memory.LastActionResults) != 1 || memory.LastActionResults[0].ToolName != "gmail.sendDraft" {
		t.Fatalf("expected action result retained, got %#v", memory.LastActionResults)
	}
}

func TestMalformedToolArgumentsRejectsChatDisplayNameAsSpace(t *testing.T) {
	missing := malformedToolArguments(providers.ToolCall{
		Name:      "chat.sendMessage",
		Arguments: map[string]any{"space": "VClaw", "text": "Hello"},
	})
	if len(missing) != 1 || missing[0] != "space" {
		t.Fatalf("expected malformed space, got %#v", missing)
	}

	missing = malformedToolArguments(providers.ToolCall{
		Name:      "chat.sendMessage",
		Arguments: map[string]any{"space": "- spaces/A | VClaw", "text": "Hello"},
	})
	if len(missing) != 0 {
		t.Fatalf("expected embedded space resource to be accepted, got %#v", missing)
	}
}

func TestRuntimeResolvesNamedChatSpaceBeforeApproval(t *testing.T) {
	listExecutions := 0
	sendExecutions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{
			Message: providers.Message{
				Role: providers.MessageRoleAssistant,
				ToolCalls: []providers.ToolCall{{
					ID:        "call_bad_send",
					Name:      "chat.sendMessage",
					Arguments: map[string]any{"space": "VClaw", "text": "Hello"},
				}},
			},
		},
		{
			Message: providers.Message{
				Role: providers.MessageRoleAssistant,
				ToolCalls: []providers.ToolCall{{
					ID:        "call_list_spaces",
					Name:      "chat.listSpaces",
					Arguments: map[string]any{},
				}},
			},
		},
		{
			Message: providers.Message{
				Role: providers.MessageRoleAssistant,
				ToolCalls: []providers.ToolCall{{
					ID:        "call_good_send",
					Name:      "chat.sendMessage",
					Arguments: map[string]any{"space": "spaces/A", "text": "Hello"},
				}},
			},
		},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(chatListSpacesRuntimeTool{executions: &listExecutions}); err != nil {
		t.Fatalf("register chat list spaces: %v", err)
	}
	if err := registry.Register(chatSendRuntimeTool{executions: &sendExecutions}); err != nil {
		t.Fatalf("register chat send: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
		Now:      func() time.Time { return runtimeTestMessage().Timestamp },
	})
	message := runtimeTestMessage()
	message.Text = "gui tin nhan vao nhom chat VClaw, thong bao ve cuoc hop Demo Sprint1"

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusApprovalRequired {
		t.Fatalf("expected approval_required after resolving space, got %#v", response)
	}
	if listExecutions != 1 {
		t.Fatalf("expected chat.listSpaces to run once, got %d", listExecutions)
	}
	if sendExecutions != 0 {
		t.Fatalf("chat.sendMessage must wait for approval, executions=%d", sendExecutions)
	}
	if response.ApprovalRequest == nil || response.ApprovalRequest.ToolCall.Input["space"] != "spaces/A" {
		t.Fatalf("expected approval for resolved spaces/A, got %#v", response.ApprovalRequest)
	}
	if len(provider.calls) != 3 {
		t.Fatalf("expected provider to retry after space resolution observation, got %d calls", len(provider.calls))
	}
	secondCallMessages := providerMessagesContent(provider.calls[1].Messages)
	if !strings.Contains(secondCallMessages, "NEEDS_SPACE_RESOLUTION") ||
		!strings.Contains(secondCallMessages, "chat.listSpaces") {
		t.Fatalf("expected provider to receive space resolution guidance, got %#v", provider.calls[1].Messages)
	}
}

func TestCalendarEvidenceDetectsVietnameseHourRange(t *testing.T) {
	text := "tu 17h00 den 18h00"
	if !hasCalendarStartEvidence(text) {
		t.Fatalf("expected start evidence for %q", text)
	}
	if !hasCalendarEndEvidence(text) {
		t.Fatalf("expected end evidence for %q", text)
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
	if len(secondMessages) != 4 {
		t.Fatalf("expected system, user, assistant tool call, tool result; got %#v", secondMessages)
	}
	if secondMessages[0].Role != providers.MessageRoleSystem {
		t.Fatalf("expected system prompt first, got %#v", secondMessages[0])
	}
	if secondMessages[3].Role != providers.MessageRoleTool || secondMessages[3].ToolCallID != "call_time" {
		t.Fatalf("unexpected tool observation message: %#v", secondMessages[3])
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
	runtime := NewRuntime(RuntimeConfig{Provider: provider, Registry: registry})

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
		Provider: provider,
		Registry: registry,
		Now:      func() time.Time { return runtimeTestMessage().Timestamp },
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
		Provider:     provider,
		Registry:     registry,
		Now:          func() time.Time { return runtimeTestMessage().Timestamp },
		SessionStore: sessions.NewInMemoryStore(),
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
	transcript, err := runtime.sessionStore.LoadTranscript(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if !transcriptContains(transcript, "danger executed") {
		t.Fatalf("expected approved tool result stored in transcript, got %#v", transcript)
	}
	memory, err := runtime.sessionStore.(sessions.MemoryStore).LoadMemory(context.Background(), runtimeTestMessage().SessionID)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if len(memory.LastActionResults) != 1 || !strings.Contains(memory.LastActionResults[0].Content, "danger executed") {
		t.Fatalf("expected approved tool result stored in memory, got %#v", memory)
	}
}

func TestRuntimeResultFollowUpUsesRecentApprovedActionContext(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Có. Calendar sẽ gửi email thông báo cho attendee nếu sự kiện được tạo với người tham gia."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
		},
		NeedsClarification:   true,
		ClarificationMessage: "Bạn có thể nói rõ hơn bạn muốn tôi làm gì không?",
	}}
	planner := &stubTaskPlanner{result: &TaskPlanResult{
		NeedsClarification:   true,
		ClarificationMessage: "Bạn có thể nói rõ hơn bạn muốn tôi làm gì không?",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: `Event created: {"id":"evt_1","title":"Hoàn thành chức năng HITL","start":"2026-06-04T10:00:00+07:00","end":"2026-06-04T11:00:00+07:00","attendees":[{"email":"baolnc@vclaw.site","responseStatus":"needsAction"}]}`,
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		TaskPlanner:      planner,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "Tạo lịch này có gửi mail thông báo cho người tham gia không"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	if !strings.Contains(planner.lastInput.Message.Text, "current_user_question") {
		t.Fatalf("expected contextual result follow-up text, got %q", planner.lastInput.Message.Text)
	}
	if !strings.Contains(providerMessagesContent(provider.calls[0].Messages), "Event created") {
		t.Fatalf("expected provider to receive recent event result context, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeResultFollowUpUsesLastActionResultMemory(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Có. Sự kiện vừa tạo có người tham gia nên Calendar có thể gửi thông báo."},
	}}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.SaveMemory(ctx, "sess_001", sessions.SessionMemory{
		LastActionResults: []sessions.ActionResult{{
			ToolName:  "calendar.createEvent",
			Content:   `Event created: {"id":"evt_1","attendees":[{"email":"baolnc@vclaw.site"}]}`,
			CreatedAt: runtimeTestMessage().Timestamp,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: tools.NewToolRegistry(),
		IntentClassifier: &stubIntentClassifier{output: &agentintent.ClassificationOutput{
			Intent:               &agentintent.Result{Type: agentintent.TypeUnknown, Confidence: 0.3},
			NeedsClarification:   true,
			ClarificationMessage: "Bạn có thể nói rõ hơn bạn muốn tôi làm gì không?",
		}},
		SessionStore: store,
	})
	message := runtimeTestMessage()
	message.Text = "Lịch này có gửi mail thông báo cho người tham gia không"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if !strings.Contains(joined, "Recent action results") || !strings.Contains(joined, "Event created") {
		t.Fatalf("expected memory action result prompt, got %#v", provider.calls[0].Messages)
	}
	if !strings.Contains(joined, "Reference resolver result") || !strings.Contains(joined, "calendar_event") {
		t.Fatalf("expected reference resolver prompt, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeWriteRequestCanUseExplicitMeetingReferenceFromMemory(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Can tao email draft moi hop cuoc hop tren."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{
			Type:       agentintent.TypeUnknown,
			Confidence: 0.3,
		},
		NeedsClarification:   true,
		ClarificationMessage: "Ban co the noi ro hon ban muon toi lam gi khong?",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.SaveMemory(ctx, "sess_001", sessions.SessionMemory{
		LastActionResults: []sessions.ActionResult{{
			ToolName:  "calendar.listEvents",
			Content:   "Upcoming meeting: Test HITL, 2026-06-05 09:30-10:30, attendee baolnc@vclaw.site",
			CreatedAt: runtimeTestMessage().Timestamp,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "viet email cho baolnc@vclaw.site moi tham du cuoc hop tren"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed provider response, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if !strings.Contains(joined, "Reference resolver result") ||
		!strings.Contains(joined, "Test HITL") ||
		!strings.Contains(joined, "cuoc hop tren") {
		t.Fatalf("expected provider to receive explicit meeting reference context, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeContextualTemporalFollowUpUsesRecentHistory(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Hom qua ban khong co lich nao."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent:               &agentintent.Result{Type: agentintent.TypeUnknown, Confidence: 0.3},
		NeedsClarification:   true,
		ClarificationMessage: "Ban co the noi ro hon ban muon toi lam gi khong?",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "trong calendar hom nay co cuoc hop nao khong",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Hom nay ban co cac cuoc hop sau trong lich: Hoan thanh Sprint 1.",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "hom qua thi sao"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed contextual follow-up, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if !strings.Contains(joined, "contextual follow-up") || !strings.Contains(joined, "calendar") {
		t.Fatalf("expected contextual follow-up prompt with recent calendar history, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeStandaloneReadRequestDoesNotUseStaleTemporalHistory(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Hom nay ban co mot lich."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent: &agentintent.Result{Type: agentintent.TypeReadInfo, Confidence: 0.9},
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "hom qua thi sao",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Hom qua khong co cuoc hop nao.",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "trong calendar hom nay co cuoc hop nao khong"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed standalone read request, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if strings.Contains(joined, "Hom qua khong co cuoc hop nao") {
		t.Fatalf("expected stale yesterday history to be isolated, got %#v", provider.calls[0].Messages)
	}
	if !strings.Contains(joined, "trong calendar hom nay") {
		t.Fatalf("expected current read request in provider messages, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeStandaloneTomorrowCalendarQuestionDoesNotDependOnPriorCalendarAnswer(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Ngay mai ban co mot lich."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent:               &agentintent.Result{Type: agentintent.TypeUnknown, Confidence: 0.3},
		NeedsClarification:   true,
		ClarificationMessage: "Ban co the noi ro hon ban muon toi lam gi khong?",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "hom qua thi sao",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleAssistant,
		Content: "Hom qua ban co lich Abc.",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "ngay mai thi co lich gi"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed tomorrow calendar request, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if strings.Contains(joined, "Hom qua ban co lich Abc") {
		t.Fatalf("expected stale calendar answer to be isolated, got %#v", provider.calls[0].Messages)
	}
	if !strings.Contains(joined, "ngay mai thi co lich gi") {
		t.Fatalf("expected current tomorrow question in provider messages, got %#v", provider.calls[0].Messages)
	}
}

func TestRuntimeConversationMemoryMetaQuestionUsesRecentHistory(t *testing.T) {
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Ban vua hoi lich hom nay trong Calendar."},
	}}}
	classifier := &stubIntentClassifier{output: &agentintent.ClassificationOutput{
		Intent:               &agentintent.Result{Type: agentintent.TypeUnknown, Confidence: 0.3},
		NeedsClarification:   true,
		ClarificationMessage: "Ban co the noi ro hon ban muon toi lam gi khong?",
	}}
	store := sessions.NewInMemoryStore()
	ctx := context.Background()
	if err := store.AppendMessage(ctx, "sess_001", providers.Message{
		Role:    providers.MessageRoleUser,
		Content: "trong calendar hom nay co cuoc hop nao khong",
	}); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider:         provider,
		Registry:         tools.NewToolRegistry(),
		IntentClassifier: classifier,
		SessionStore:     store,
	})
	message := runtimeTestMessage()
	message.Text = "toi vua nhan cai gi"

	response, err := runtime.Run(ctx, message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed memory meta question, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected provider call, got %d", len(provider.calls))
	}
	joined := providerMessagesContent(provider.calls[0].Messages)
	if !strings.Contains(joined, "toi vua nhan cai gi") || !strings.Contains(joined, "trong calendar hom nay") {
		t.Fatalf("expected provider to receive current question and recent history, got %#v", provider.calls[0].Messages)
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
		Provider: provider,
		Registry: registry,
		Now:      func() time.Time { return runtimeTestMessage().Timestamp },
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
		Provider: provider,
		Registry: registry,
		Now:      func() time.Time { return runtimeTestMessage().Timestamp },
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

func TestRuntimeReviseApprovalReplansWithoutExecutingOriginalTool(t *testing.T) {
	executions := 0
	provider := &fakeProvider{responses: []providers.ChatResponse{
		{
			Message: providers.Message{
				Role:      providers.MessageRoleAssistant,
				ToolCalls: []providers.ToolCall{{ID: "call_write", Name: "danger.count", Arguments: map[string]any{"value": "old"}}},
			},
		},
		{
			Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "replanned"},
		},
	}}
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register dangerous tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Provider: provider,
		Registry: registry,
		Now:      func() time.Time { return runtimeTestMessage().Timestamp },
	})

	pending, err := runtime.Run(context.Background(), runtimeTestMessage())
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	response, err := runtime.ReviseApproval(context.Background(), runtimeTestMessage().SessionID, "req_revise", pending.ApprovalID, "doi gio sang 10:00")
	if err != nil {
		t.Fatalf("revise approval: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed after replanning, got %#v", response)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("expected provider to be called for original and revision, got %d", len(provider.calls))
	}
	joined := ""
	for _, message := range provider.calls[1].Messages {
		joined += "\n" + message.Content
	}
	if !strings.Contains(joined, "Ghi chú chỉnh sửa") || !strings.Contains(joined, "doi gio sang 10:00") {
		t.Fatalf("expected revision context in provider messages, got %s", joined)
	}
	if executions != 0 {
		t.Fatalf("revision must not execute original tool, executions=%d", executions)
	}
	if !runtime.HasPendingApproval(runtimeTestMessage().SessionID) {
		t.Fatal("revision should keep original approval pending until replaced by a new approval")
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
		Provider: provider,
		Registry: tools.NewToolRegistry(),
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
		Provider: provider,
		Registry: tools.NewToolRegistry(),
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
		Provider: provider,
		Registry: registry,
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
