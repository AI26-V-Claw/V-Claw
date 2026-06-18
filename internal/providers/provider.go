package providers

import (
	"context"
	"time"
)

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

// Provider is the interface that all LLM providers must implement.
// This allows the agent core to work with any LLM backend (Gemini, OpenAI, Anthropic, etc.)
// without coupling to a specific vendor.
type Provider interface {
	// Chat sends a chat request and returns the response
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)

	// Generate sends a prompt to the LLM and returns the response text.
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)

	// Name returns the provider name (e.g., "gemini", "openai", "anthropic")
	Name() string

	// Close releases any resources held by the provider
	Close() error
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

// GenerateRequest contains all parameters for an LLM generation request.
type GenerateRequest struct {
	// SystemPrompt is the system-level instruction (e.g., from SOUL.md)
	SystemPrompt string

	// UserPrompt is the user's input or the agent's query
	UserPrompt string

	// Temperature controls randomness (0.0 = deterministic, 1.0 = creative)
	Temperature float64

	// MaxTokens limits the response length
	MaxTokens int

	// ResponseFormat specifies the expected format (e.g., "json", "text")
	ResponseFormat string

	// Timeout for the request
	Timeout time.Duration

	// Model specifies which model to use (provider-specific)
	Model string
}

// GenerateResponse contains the LLM's response and metadata.
type GenerateResponse struct {
	// Text is the generated response
	Text string

	// FinishReason indicates why generation stopped ("stop", "length", "error")
	FinishReason string

	// Usage contains token usage statistics
	Usage *Usage

	// Latency is the time taken for the request
	Latency time.Duration

	// Model is the actual model used (may differ from requested if fallback occurred)
	Model string
}

// Usage tracks token consumption for billing and monitoring.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type usageRecorderKey struct{}

type UsageRecorder func(*Usage)

func WithUsageRecorder(ctx context.Context, recorder UsageRecorder) context.Context {
	if ctx == nil || recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, usageRecorderKey{}, recorder)
}

func RecordUsageFromContext(ctx context.Context, usage *Usage) {
	if usage == nil || ctx == nil {
		return
	}
	recorder, _ := ctx.Value(usageRecorderKey{}).(UsageRecorder)
	if recorder != nil {
		recorder(usage)
	}
}
