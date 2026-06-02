package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type AnthropicConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type AnthropicClient struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

func NewAnthropicClient(cfg AnthropicConfig) (*AnthropicClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("anthropic api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("anthropic model is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &AnthropicClient{
		apiKey:  strings.TrimSpace(cfg.APIKey),
		baseURL: baseURL,
		model:   strings.TrimSpace(cfg.Model),
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *AnthropicClient) Complete(ctx context.Context, system string, messages []ChatMessage) (string, error) {
	payload := map[string]any{
		"model":      c.model,
		"max_tokens": 512,
		"system":     strings.TrimSpace(system),
		"messages":   buildAnthropicMessages(messages),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("x-api-key", c.apiKey)
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("Content-Type", "application/json")

	response, err := c.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("anthropic api status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBytes)))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(responseBytes, &parsed); err != nil {
		return "", err
	}
	for _, block := range parsed.Content {
		if strings.TrimSpace(block.Text) != "" {
			return strings.TrimSpace(block.Text), nil
		}
	}

	return "", fmt.Errorf("anthropic response was empty")
}

func buildAnthropicMessages(messages []ChatMessage) []map[string]string {
	result := make([]map[string]string, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role != "assistant" {
			role = "user"
		}
		result = append(result, map[string]string{
			"role":    role,
			"content": strings.TrimSpace(message.Content),
		})
	}
	return result
}
