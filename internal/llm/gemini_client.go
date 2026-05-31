package llm

import (
	"context"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
	"vclaw/internal/pipeline/stages"
)

// GeminiClient implements stages.LLMClient using Google Gemini API
type GeminiClient struct {
	client *genai.Client
	model  string
}

// NewGeminiClient creates a new Gemini client
func NewGeminiClient(ctx context.Context, apiKey string, model string) (*GeminiClient, error) {
	if model == "" {
		model = "gemini-1.5-flash"
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	return &GeminiClient{
		client: client,
		model:  model,
	}, nil
}

// Generate generates a response from the LLM
func (c *GeminiClient) Generate(ctx context.Context, prompt string, options *stages.GenerateOptions) (string, error) {
	model := c.client.GenerativeModel(c.model)

	if options != nil {
		if options.Temperature >= 0 {
			model.SetTemperature(float32(options.Temperature))
		}
		if options.MaxTokens > 0 {
			model.SetMaxOutputTokens(int32(options.MaxTokens))
		}
		if options.ResponseMIMEType != "" {
			model.ResponseMIMEType = options.ResponseMIMEType
		}
	}

	// Handle timeout if provided
	if options != nil && options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidates returned")
	}

	var result string
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			result += string(txt)
		}
	}

	return result, nil
}

// Close closes the underlying client
func (c *GeminiClient) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
