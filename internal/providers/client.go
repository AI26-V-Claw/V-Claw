package providers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string
	Content string
}

type ChatClient interface {
	Complete(ctx context.Context, system string, messages []ChatMessage) (string, error)
}

// Config holds provider configuration (unified from both implementations).
type Config struct {
	// Provider name ("gemini", "openai", "anthropic", "openai-compatible")
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

func NewClient(cfg Config) (ChatClient, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, nil
	}

	switch provider {
	case "anthropic":
		return NewAnthropicClient(AnthropicConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	case "openai-compatible", "openai", "deepseek":
		return NewOpenAICompatibleClient(OpenAICompatibleConfig{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
		})
	default:
		return nil, fmt.Errorf("unsupported llm provider: %s", cfg.Provider)
	}
}
