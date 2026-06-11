package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

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

type gmailListEmailsRuntimeTool struct {
	executions *int
	content    string
}

type gmailDownloadAttachmentsRuntimeTool struct {
	executions *int
}

type chatListSpacesRuntimeTool struct {
	executions *int
}

type chatSendRuntimeTool struct {
	executions *int
}

type gatedDangerousRuntimeTool struct {
	started    chan struct{}
	release    chan struct{}
	once       *sync.Once
	mu         *sync.Mutex
	executions *int
}

type countingDangerousTool struct {
	executions *int
}

type failingDangerousTool struct{}

type failingLoadSessionStore struct{}

type failingAssistantAppendSessionStore struct {
	*sessions.InMemoryStore
}

type toolEnabledRouter struct{}

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

func (gmailListEmailsRuntimeTool) Name() string        { return "gmail.listEmails" }
func (gmailListEmailsRuntimeTool) Description() string { return "List Gmail emails." }
func (gmailListEmailsRuntimeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{}}
}
func (gmailListEmailsRuntimeTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (gmailListEmailsRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }
func (t gmailListEmailsRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.executions != nil {
		(*t.executions)++
	}
	content := strings.TrimSpace(t.content)
	if content == "" {
		content = "- Demo Sprint: Bao asks to schedule a follow-up and notify VClaw chat."
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

func (gmailDownloadAttachmentsRuntimeTool) Name() string { return "gmail.downloadAttachments" }
func (gmailDownloadAttachmentsRuntimeTool) Description() string {
	return "Download Gmail attachments."
}
func (gmailDownloadAttachmentsRuntimeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"messageId": map[string]any{"type": "string"},
			"outputDir": map[string]any{"type": "string"},
			"filenames": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"messageId", "outputDir"},
	}
}
func (gmailDownloadAttachmentsRuntimeTool) Capability() tools.Capability {
	return tools.CapabilityMutating
}
func (gmailDownloadAttachmentsRuntimeTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelLocalWrite
}
func (t gmailDownloadAttachmentsRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.executions != nil {
		(*t.executions)++
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "downloaded",
		ContentForUser: "downloaded",
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

func (gatedDangerousRuntimeTool) Name() string        { return "danger.gated" }
func (gatedDangerousRuntimeTool) Description() string { return "Dangerous gated tool." }
func (gatedDangerousRuntimeTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object"}
}
func (gatedDangerousRuntimeTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (gatedDangerousRuntimeTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (t gatedDangerousRuntimeTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.mu != nil && t.executions != nil {
		t.mu.Lock()
		(*t.executions)++
		t.mu.Unlock()
	}
	if t.once != nil && t.started != nil {
		t.once.Do(func() { close(t.started) })
	}
	if t.release != nil {
		<-t.release
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "gated danger executed",
		ContentForUser: "gated danger executed",
	}
}

func (countingDangerousTool) Name() string                 { return "danger.count" }
func (countingDangerousTool) Description() string          { return "Dangerous counting tool." }
func (countingDangerousTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (countingDangerousTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (countingDangerousTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (t countingDangerousTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	(*t.executions)++
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "danger executed",
		ContentForUser: "danger executed",
	}
}

func (failingDangerousTool) Name() string                 { return "danger.fail" }
func (failingDangerousTool) Description() string          { return "Dangerous failing tool." }
func (failingDangerousTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (failingDangerousTool) Capability() tools.Capability { return tools.CapabilityMutating }
func (failingDangerousTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelExternalWrite }
func (failingDangerousTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "write failed",
		ContentForUser: "write failed",
		Error: &tools.ToolError{
			Code:    tools.ErrorExecutionFailed,
			Message: "write failed",
		},
	}
}

func (failingLoadSessionStore) LoadTranscript(context.Context, string) ([]providers.Message, error) {
	return nil, errors.New("load transcript failed")
}

func (failingLoadSessionStore) AppendMessage(context.Context, string, providers.Message) error {
	return nil
}

func (failingLoadSessionStore) SetTranscript(context.Context, string, []providers.Message) error {
	return nil
}

func (failingLoadSessionStore) ClearSession(context.Context, string) error {
	return nil
}

func (s failingAssistantAppendSessionStore) AppendMessage(ctx context.Context, sessionID string, message providers.Message) error {
	if message.Role == providers.MessageRoleAssistant {
		return errors.New("append assistant failed")
	}
	return s.InMemoryStore.AppendMessage(ctx, sessionID, message)
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

func fixedTestTime() time.Time {
	return time.Date(2026, 5, 29, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
}

func testToolEnabledRouter() TurnRouter {
	return toolEnabledRouter{}
}

func (toolEnabledRouter) RouteTurn(_ context.Context, _ TurnRouteInput) (TurnRoute, error) {
	return TurnRoute{Mode: TurnModeToolEnabled, Reason: "test"}, nil
}
