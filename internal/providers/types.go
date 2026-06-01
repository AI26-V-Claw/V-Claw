package providers

import (
	"context"
	"time"
)

// Provider is the interface that all LLM providers must implement.
// This allows the agent core to work with any LLM backend (Gemini, OpenAI, Anthropic, etc.)
// without coupling to a specific vendor.
type Provider interface {
	// Generate sends a prompt to the LLM and returns the response text.
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)

	// Name returns the provider name (e.g., "gemini", "openai", "anthropic")
	Name() string

	// Close releases any resources held by the provider
	Close() error
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

// Config holds provider configuration.
type Config struct {
	// Provider name ("gemini", "openai", "anthropic")
	Provider string

	// APIKey for authentication
	APIKey string

	// Model to use (e.g., "gemini-1.5-flash", "gpt-4", "claude-3-sonnet")
	Model string

	// BaseURL for custom endpoints (optional)
	BaseURL string

	// DefaultTemperature for requests
	DefaultTemperature float64

	// DefaultMaxTokens for responses
	DefaultMaxTokens int

	// Timeout for requests
	Timeout time.Duration
}

// DefaultConfig returns a production-ready configuration for Gemini 1.5 Flash.
// This is the recommended model for intent classification due to its speed and cost.
func DefaultConfig() *Config {
	return &Config{
		Provider:           "gemini",
		Model:              "gemini-1.5-flash",
		DefaultTemperature: 0.3, // Low temperature for consistent classification
		DefaultMaxTokens:   2048,
		Timeout:            30 * time.Second,
	}
}
