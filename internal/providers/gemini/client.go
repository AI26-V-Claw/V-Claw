package gemini

import (
	"context"
	"fmt"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"vclaw/internal/providers"
)

// Client implements providers.Provider for Google Gemini.
type Client struct {
	client *genai.Client
	config *providers.Config
}

// NewClient creates a new Gemini provider client.
func NewClient(ctx context.Context, cfg *providers.Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("gemini: API key is required")
	}

	if cfg.Model == "" {
		cfg.Model = "gemini-1.5-flash"
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("gemini: failed to create client: %w", err)
	}

	return &Client{
		client: client,
		config: cfg,
	}, nil
}

// Generate sends a prompt to Gemini and returns the response.
func (c *Client) Generate(ctx context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	startTime := time.Now()

	// Select model
	modelName := req.Model
	if modelName == "" {
		modelName = c.config.Model
	}
	model := c.client.GenerativeModel(modelName)

	// Configure generation parameters
	temperature := req.Temperature
	if temperature == 0 {
		temperature = c.config.DefaultTemperature
	}
	model.SetTemperature(float32(temperature))

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.config.DefaultMaxTokens
	}
	if maxTokens > 0 {
		model.SetMaxOutputTokens(int32(maxTokens))
	}

	// Set response format if specified
	if req.ResponseFormat == "json" {
		model.ResponseMIMEType = "application/json"
	}

	// Apply timeout
	timeout := req.Timeout
	if timeout == 0 {
		timeout = c.config.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build prompt
	var prompt string
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt + "\n\n" + req.UserPrompt
	} else {
		prompt = req.UserPrompt
	}

	// Generate content
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, fmt.Errorf("gemini: generation failed: %w", err)
	}

	latency := time.Since(startTime)

	// Extract response text
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: no candidates returned")
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			text += string(txt)
		}
	}

	// Extract usage metadata
	usage := &providers.Usage{}
	if resp.UsageMetadata != nil {
		usage.PromptTokens = int(resp.UsageMetadata.PromptTokenCount)
		usage.CompletionTokens = int(resp.UsageMetadata.CandidatesTokenCount)
		usage.TotalTokens = int(resp.UsageMetadata.TotalTokenCount)
	}

	// Determine finish reason
	finishReason := "stop"
	if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason != genai.FinishReasonStop {
		finishReason = fmt.Sprintf("%v", resp.Candidates[0].FinishReason)
	}

	return &providers.GenerateResponse{
		Text:         text,
		FinishReason: finishReason,
		Usage:        usage,
		Latency:      latency,
		Model:        modelName,
	}, nil
}

// Name returns the provider name.
func (c *Client) Name() string {
	return "gemini"
}

// Close releases resources.
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}
