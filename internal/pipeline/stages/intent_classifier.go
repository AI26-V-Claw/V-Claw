package stages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/agent/prompts"
)

// LLMClient interface for calling LLM APIs
type LLMClient interface {
	// Generate generates a response from the LLM
	Generate(ctx context.Context, prompt string, options *GenerateOptions) (string, error)
}

// GenerateOptions contains options for LLM generation
type GenerateOptions struct {
	Temperature      float64
	MaxTokens        int
	ResponseMIMEType string
	Timeout          time.Duration
}

// IntentClassifier classifies user intents using LLM
type IntentClassifier struct {
	llmClient     LLMClient
	promptBuilder *prompts.PromptBuilder
	scorer        *agent.ConfidenceScorer
	config        agent.ConfidenceConfig
	maxRetries    int
	retryDelay    time.Duration
}

// IntentClassifierConfig contains configuration for the classifier
type IntentClassifierConfig struct {
	LLMClient         LLMClient
	ConfidenceConfig  agent.ConfidenceConfig
	MaxRetries        int
	RetryDelay        time.Duration
	IncludeToolRegistry bool
	IncludeSessionHistory bool
	MaxHistoryTurns   int
}

// NewIntentClassifier creates a new intent classifier
func NewIntentClassifier(config IntentClassifierConfig) *IntentClassifier {
	if config.MaxRetries == 0 {
		config.MaxRetries = 2
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = time.Second
	}
	if config.MaxHistoryTurns == 0 {
		config.MaxHistoryTurns = 5
	}

	return &IntentClassifier{
		llmClient:     config.LLMClient,
		promptBuilder: prompts.NewIntentClassifierPrompt(),
		scorer:        agent.NewConfidenceScorer(config.ConfidenceConfig),
		config:        config.ConfidenceConfig,
		maxRetries:    config.MaxRetries,
		retryDelay:    config.RetryDelay,
	}
}

// Classify classifies a user input into an intent
func (ic *IntentClassifier) Classify(ctx context.Context, input string) (*agent.ClassificationResult, error) {
	// Build prompt
	prompt := ic.promptBuilder.BuildWithUserInput(input)

	// Call LLM with retries
	var response string
	var err error
	for attempt := 0; attempt <= ic.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(ic.retryDelay * time.Duration(attempt))
		}

		response, err = ic.callLLM(ctx, prompt)
		if err == nil {
			break
		}

		// Check if error is retryable
		if !ic.isRetryableError(err) {
			return nil, fmt.Errorf("non-retryable error: %w", err)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed after %d retries: %w", ic.maxRetries, err)
	}

	// Validate JSON format
	if err := prompts.ValidateJSONResponse(response); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}

	// Parse into Intent
	var intent agent.Intent
	if err := json.Unmarshal([]byte(response), &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent: %w", err)
	}

	// Set timestamp
	intent.Timestamp = time.Now()

	// Validate confidence threshold
	minConfidence := ic.config.GetMinConfidence(intent.Type)
	if intent.Confidence < minConfidence {
		// Low confidence - need clarification
		return &agent.ClassificationResult{
			Intent:             &intent,
			NeedsClarification: true,
			Error:              nil,
		}, nil
	}

	// Check if in ambiguous range
	if ic.config.IsAmbiguous(intent.Confidence) {
		// Generate clarification options
		options := ic.generateClarificationOptions(input, &intent)
		return &agent.ClassificationResult{
			Intent:               &intent,
			NeedsClarification:   true,
			ClarificationOptions: options,
			Error:                nil,
		}, nil
	}

	// Check if missing parameters for dangerous actions
	if intent.Type == agent.IntentDangerousAction && len(intent.MissingParams) > 0 {
		intent.NeedsConfirm = true
	}

	return &agent.ClassificationResult{
		Intent:             &intent,
		NeedsClarification: false,
		Error:              nil,
	}, nil
}

// ClassifyWithContext classifies with additional context
func (ic *IntentClassifier) ClassifyWithContext(
	ctx context.Context,
	input string,
	userID string,
	workingDir string,
	sessionHistory []string,
) (*agent.ClassificationResult, error) {
	// Build prompt with context
	builder := prompts.NewIntentClassifierPrompt()

	// Add user context
	if userID != "" || workingDir != "" {
		builder = builder.WithUserContext(userID, workingDir)
	}

	// Add session history
	if len(sessionHistory) > 0 {
		builder = builder.WithSessionHistory(sessionHistory, 5)
	}

	// Add tool registry
	builder = builder.WithToolRegistry(ic.getToolRegistryMap())

	// Build final prompt
	prompt := builder.BuildWithUserInput(input)

	// Call LLM
	response, err := ic.callLLMWithRetry(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// Parse and validate
	return ic.parseAndValidate(response)
}

// callLLM calls the LLM API
func (ic *IntentClassifier) callLLM(ctx context.Context, prompt string) (string, error) {
	options := &GenerateOptions{
		Temperature:      0.0, // Deterministic for classification
		MaxTokens:        1000,
		ResponseMIMEType: "application/json",
		Timeout:          30 * time.Second,
	}

	return ic.llmClient.Generate(ctx, prompt, options)
}

// callLLMWithRetry calls LLM with retry logic
func (ic *IntentClassifier) callLLMWithRetry(ctx context.Context, prompt string) (string, error) {
	var response string
	var err error

	for attempt := 0; attempt <= ic.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(ic.retryDelay * time.Duration(attempt))
		}

		response, err = ic.callLLM(ctx, prompt)
		if err == nil {
			return response, nil
		}

		if !ic.isRetryableError(err) {
			return "", fmt.Errorf("non-retryable error: %w", err)
		}
	}

	return "", fmt.Errorf("failed after %d retries: %w", ic.maxRetries, err)
}

// parseAndValidate parses and validates the LLM response
func (ic *IntentClassifier) parseAndValidate(response string) (*agent.ClassificationResult, error) {
	// Validate JSON format
	if err := prompts.ValidateJSONResponse(response); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}

	// Parse into Intent
	var intent agent.Intent
	if err := json.Unmarshal([]byte(response), &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent: %w", err)
	}

	// Set timestamp
	intent.Timestamp = time.Now()

	// Validate intent type
	if !ic.isValidIntentType(intent.Type) {
		return nil, fmt.Errorf("invalid intent type: %s", intent.Type)
	}

	// Check confidence threshold
	minConfidence := ic.config.GetMinConfidence(intent.Type)
	needsClarification := intent.Confidence < minConfidence

	// Check if in ambiguous range
	if ic.config.IsAmbiguous(intent.Confidence) {
		options := ic.generateClarificationOptions("", &intent)
		return &agent.ClassificationResult{
			Intent:               &intent,
			NeedsClarification:   true,
			ClarificationOptions: options,
			Error:                nil,
		}, nil
	}

	// Check missing parameters for dangerous actions
	if intent.Type == agent.IntentDangerousAction && len(intent.MissingParams) > 0 {
		intent.NeedsConfirm = true
		needsClarification = true
	}

	return &agent.ClassificationResult{
		Intent:             &intent,
		NeedsClarification: needsClarification,
		Error:              nil,
	}, nil
}

// isValidIntentType checks if intent type is valid
func (ic *IntentClassifier) isValidIntentType(intentType agent.IntentType) bool {
	validTypes := []agent.IntentType{
		agent.IntentGreeting,
		agent.IntentReadInfo,
		agent.IntentDangerousAction,
		agent.IntentComposite,
		agent.IntentUnknown,
	}

	for _, valid := range validTypes {
		if intentType == valid {
			return true
		}
	}

	return false
}

// isRetryableError checks if an error is retryable
func (ic *IntentClassifier) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Retryable errors
	retryablePatterns := []string{
		"timeout",
		"temporary",
		"connection",
		"rate limit",
		"429",
		"503",
		"504",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// generateClarificationOptions generates multiple choice options
func (ic *IntentClassifier) generateClarificationOptions(
	input string,
	intent *agent.Intent,
) *agent.ClarificationOptions {
	// For ambiguous intents, provide multiple interpretations
	options := &agent.ClarificationOptions{
		Question: "Tôi có thể hiểu theo nhiều cách. Bạn muốn:",
		Options:  []agent.Option{},
	}

	// Add the primary interpretation
	options.Options = append(options.Options, agent.Option{
		ID:         "option_1",
		Label:      ic.getIntentLabel(intent.Type),
		IntentType: intent.Type,
		Confidence: intent.Confidence,
	})

	// Add alternative interpretations based on intent type
	if intent.Type == agent.IntentUnknown {
		// Suggest common alternatives
		options.Options = append(options.Options,
			agent.Option{
				ID:         "option_2",
				Label:      "Đọc/xem thông tin",
				IntentType: agent.IntentReadInfo,
				Confidence: 0.5,
			},
			agent.Option{
				ID:         "option_3",
				Label:      "Thực hiện hành động (sửa/xóa/gửi)",
				IntentType: agent.IntentDangerousAction,
				Confidence: 0.5,
			},
		)
	}

	return options
}

// getIntentLabel returns a human-readable label for an intent type
func (ic *IntentClassifier) getIntentLabel(intentType agent.IntentType) string {
	labels := map[agent.IntentType]string{
		agent.IntentGreeting:        "Chào hỏi/trò chuyện",
		agent.IntentReadInfo:        "Đọc/xem thông tin",
		agent.IntentDangerousAction: "Thực hiện hành động (sửa/xóa/gửi)",
		agent.IntentComposite:       "Thực hiện nhiều bước",
		agent.IntentUnknown:         "Không rõ ý định",
	}

	if label, exists := labels[intentType]; exists {
		return label
	}

	return string(intentType)
}

// getToolRegistryMap returns tool registry as a map for prompt building
func (ic *IntentClassifier) getToolRegistryMap() map[string]interface{} {
	result := make(map[string]interface{})

	for name, tool := range agent.ToolRegistry {
		result[name] = map[string]interface{}{
			"description": tool.Description,
			"category":    string(tool.Category),
			"dangerous":   tool.Dangerous,
		}
	}

	return result
}

// CalculateConfidenceHeuristic calculates confidence using heuristic method
// This is useful for validation or when LLM doesn't provide confidence
func (ic *IntentClassifier) CalculateConfidenceHeuristic(
	input string,
	intentType agent.IntentType,
) float64 {
	return ic.scorer.CalculateHeuristic(input, intentType)
}

// ValidateIntent validates an intent against business rules
func (ic *IntentClassifier) ValidateIntent(intent *agent.Intent) error {
	// Check intent type is valid
	if !ic.isValidIntentType(intent.Type) {
		return fmt.Errorf("invalid intent type: %s", intent.Type)
	}

	// Check confidence is in valid range
	if intent.Confidence < 0.0 || intent.Confidence > 1.0 {
		return fmt.Errorf("confidence out of range [0,1]: %.2f", intent.Confidence)
	}

	// For dangerous actions, ensure needs_confirm is true if missing params
	if intent.Type == agent.IntentDangerousAction {
		if len(intent.MissingParams) > 0 && !intent.NeedsConfirm {
			return fmt.Errorf("dangerous action with missing params must have needs_confirm=true")
		}
	}

	// Validate tool calls
	for _, toolCall := range intent.ToolCalls {
		if _, err := agent.GetTool(toolCall.Name); err != nil {
			return fmt.Errorf("invalid tool in tool_calls: %s", toolCall.Name)
		}
	}

	return nil
}

// GetStatistics returns classification statistics
func (ic *IntentClassifier) GetStatistics() map[string]interface{} {
	return map[string]interface{}{
		"max_retries":  ic.maxRetries,
		"retry_delay":  ic.retryDelay.String(),
		"config":       ic.config,
	}
}
