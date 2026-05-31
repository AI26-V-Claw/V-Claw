package llm

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
	"vclaw/internal/pipeline/stages"
)

// OpenAIClient implements stages.LLMClient using OpenAI API
type OpenAIClient struct {
	client *openai.Client
	model  string
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey string, model string) (*OpenAIClient, error) {
	if model == "" {
		model = openai.GPT4oMini
	}
	
	client := openai.NewClient(apiKey)
	
	return &OpenAIClient{
		client: client,
		model:  model,
	}, nil
}

// Generate generates a response from the LLM
func (c *OpenAIClient) Generate(ctx context.Context, prompt string, options *stages.GenerateOptions) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	}

	if options != nil {
		if options.Temperature >= 0 {
			req.Temperature = float32(options.Temperature)
		}
		if options.MaxTokens > 0 {
			req.MaxTokens = options.MaxTokens
		}
		if options.ResponseMIMEType == "application/json" {
			req.ResponseFormat = &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			}
		}
	}

	// Handle timeout if provided
	if options != nil && options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned")
	}

	return resp.Choices[0].Message.Content, nil
}

// Close closes the underlying client if needed
func (c *OpenAIClient) Close() {
	// OpenAI client doesn't need to be closed explicitly
}
