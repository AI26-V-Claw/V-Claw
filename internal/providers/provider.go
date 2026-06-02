package providers

import "context"

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type Provider interface {
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)
}

type ChatRequest struct {
	Model      string
	Messages   []Message
	Tools      []ToolDefinition
	ToolChoice string
}

type ChatResponse struct {
	Message Message
}

type Message struct {
	Role       MessageRole
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}
