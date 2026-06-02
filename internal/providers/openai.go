package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const (
	DefaultOpenAIModel   = "gpt-4o"
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
)

type OpenAIConfig struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

type OpenAIClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewOpenAIClient(config OpenAIConfig) (*OpenAIClient, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("openai api key is required")
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		model = DefaultOpenAIModel
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIClient{
		apiKey:     config.APIKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

func (c *OpenAIClient) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	if c == nil {
		return ChatResponse{}, fmt.Errorf("openai client is nil")
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = c.model
	}

	nameMap := newOpenAIToolNameMap(request.Tools)
	wireRequest := openAIChatRequest{
		Model:      model,
		Messages:   make([]openAIMessage, 0, len(request.Messages)),
		ToolChoice: "auto",
	}
	for _, message := range request.Messages {
		wireRequest.Messages = append(wireRequest.Messages, openAIMessageFromProvider(message, nameMap.safeName))
	}
	if len(request.Tools) > 0 {
		wireRequest.Tools = make([]openAITool, 0, len(request.Tools))
		for _, tool := range request.Tools {
			wireRequest.Tools = append(wireRequest.Tools, openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        nameMap.safeName(tool.Name),
					Description: tool.Description,
					Parameters:  tool.Parameters,
				},
			})
		}
	}
	if strings.TrimSpace(request.ToolChoice) != "" {
		wireRequest.ToolChoice = request.ToolChoice
	}

	body, err := json.Marshal(wireRequest)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal openai request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return ChatResponse{}, err
	}
	defer httpResponse.Body.Close()

	var wireResponse openAIChatResponse
	if err := json.NewDecoder(httpResponse.Body).Decode(&wireResponse); err != nil {
		return ChatResponse{}, fmt.Errorf("decode openai response: %w", err)
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		message := strings.TrimSpace(wireResponse.Error.Message)
		if message == "" {
			message = httpResponse.Status
		}
		return ChatResponse{}, fmt.Errorf("openai chat failed: %s", message)
	}
	if len(wireResponse.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("openai response contained no choices")
	}

	return ChatResponse{Message: providerMessageFromOpenAI(wireResponse.Choices[0].Message, nameMap.contractName)}, nil
}

type openAIChatRequest struct {
	Model      string          `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	Tools      []openAITool    `json:"tools,omitempty"`
	ToolChoice string          `json:"tool_choice,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func openAIMessageFromProvider(message Message, safeName func(string) string) openAIMessage {
	wire := openAIMessage{
		Role:       string(message.Role),
		Content:    message.Content,
		ToolCallID: message.ToolCallID,
	}
	for _, toolCall := range message.ToolCalls {
		args, _ := json.Marshal(toolCall.Arguments)
		wire.ToolCalls = append(wire.ToolCalls, openAIToolCall{
			ID:   toolCall.ID,
			Type: "function",
			Function: openAIToolFunction{
				Name:      safeName(toolCall.Name),
				Arguments: string(args),
			},
		})
	}
	return wire
}

func providerMessageFromOpenAI(message openAIMessage, contractName func(string) string) Message {
	providerMessage := Message{
		Role:       MessageRole(message.Role),
		Content:    message.Content,
		ToolCallID: message.ToolCallID,
	}
	for _, toolCall := range message.ToolCalls {
		args := map[string]any{}
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				args = map[string]any{"_raw": toolCall.Function.Arguments}
			}
		}
		providerMessage.ToolCalls = append(providerMessage.ToolCalls, ToolCall{
			ID:        toolCall.ID,
			Name:      contractName(toolCall.Function.Name),
			Arguments: args,
		})
	}
	return providerMessage
}

type openAIToolNameMap struct {
	toSafe     map[string]string
	toContract map[string]string
}

func newOpenAIToolNameMap(tools []ToolDefinition) openAIToolNameMap {
	m := openAIToolNameMap{
		toSafe:     map[string]string{},
		toContract: map[string]string{},
	}
	for _, tool := range tools {
		contract := strings.TrimSpace(tool.Name)
		if contract == "" {
			continue
		}
		base := openAISafeToolName(contract)
		safe := base
		for i := 2; ; i++ {
			existing, exists := m.toContract[safe]
			if !exists || existing == contract {
				break
			}
			safe = fmt.Sprintf("%s_%d", base, i)
		}
		m.toSafe[contract] = safe
		m.toContract[safe] = contract
	}
	return m
}

func (m openAIToolNameMap) safeName(name string) string {
	if safe, ok := m.toSafe[name]; ok {
		return safe
	}
	return openAISafeToolName(name)
}

func (m openAIToolNameMap) contractName(name string) string {
	if contract, ok := m.toContract[name]; ok {
		return contract
	}
	return name
}

var openAIToolNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func openAISafeToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}
	name = strings.ReplaceAll(name, ".", "__dot__")
	name = openAIToolNameUnsafe.ReplaceAllString(name, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return strings.Trim(name, "_")
}
