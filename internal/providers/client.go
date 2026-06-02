package providers

import (
	"context"
	"fmt"
	"strings"
)

type ChatMessage struct {
	Role    string
	Content string
}

type ChatClient interface {
	Complete(ctx context.Context, system string, messages []ChatMessage) (string, error)
}

type Config struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
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
