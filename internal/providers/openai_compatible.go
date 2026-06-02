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

type OpenAICompatibleConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type OpenAICompatibleClient struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

func NewOpenAICompatibleClient(cfg OpenAICompatibleConfig) (*OpenAICompatibleClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("llm api key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("llm model is required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &OpenAICompatibleClient{
		apiKey:  strings.TrimSpace(cfg.APIKey),
		baseURL: baseURL,
		model:   strings.TrimSpace(cfg.Model),
		client:  &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *OpenAICompatibleClient) Complete(ctx context.Context, system string, messages []ChatMessage) (string, error) {
	payload := map[string]any{
		"model": c.model,
		"messages": buildOpenAIMessages(system, messages),
		"temperature": 0.7,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		return "", fmt.Errorf("llm api status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBytes)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBytes, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm response had no choices")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("llm response was empty")
	}

	return content, nil
}

func buildOpenAIMessages(system string, messages []ChatMessage) []map[string]string {
	result := make([]map[string]string, 0, len(messages)+1)
	if strings.TrimSpace(system) != "" {
		result = append(result, map[string]string{"role": "system", "content": strings.TrimSpace(system)})
	}
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "user"
		}
		result = append(result, map[string]string{
			"role":    role,
			"content": strings.TrimSpace(message.Content),
		})
	}
	return result
}
