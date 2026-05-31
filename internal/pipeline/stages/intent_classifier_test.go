package stages

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vclaw/internal/agent"
)

// MockLLMClient is a mock implementation of LLMClient for testing
type MockLLMClient struct {
	Response      string
	Error         error
	CallCount     int
	LastPrompt    string
	LastOptions   *GenerateOptions
	ShouldFailN   int // Fail first N calls
}

func (m *MockLLMClient) Generate(ctx context.Context, prompt string, options *GenerateOptions) (string, error) {
	m.CallCount++
	m.LastPrompt = prompt
	m.LastOptions = options

	// Simulate failures for retry testing
	if m.ShouldFailN > 0 {
		m.ShouldFailN--
		return "", errors.New("temporary error")
	}

	if m.Error != nil {
		return "", m.Error
	}

	return m.Response, nil
}

func TestNewIntentClassifier(t *testing.T) {
	mockClient := &MockLLMClient{}
	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)

	if classifier == nil {
		t.Fatal("Expected non-nil classifier")
	}

	if classifier.maxRetries != 2 {
		t.Errorf("Expected default maxRetries=2, got %d", classifier.maxRetries)
	}

	if classifier.retryDelay != time.Second {
		t.Errorf("Expected default retryDelay=1s, got %v", classifier.retryDelay)
	}
}

func TestClassify_Greeting(t *testing.T) {
	intent := agent.Intent{
		Type:           agent.IntentGreeting,
		Confidence:     0.95,
		RequiredParams: []string{},
		ProvidedParams: map[string]interface{}{},
		MissingParams:  []string{},
		ToolCalls:      []agent.ToolCall{},
		NeedsConfirm:   false,
		Reasoning:      "Simple greeting",
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response: string(responseJSON),
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	result, err := classifier.Classify(context.Background(), "Chào buổi sáng")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Intent.Type != agent.IntentGreeting {
		t.Errorf("Expected GREETING, got %s", result.Intent.Type)
	}

	if result.NeedsClarification {
		t.Error("Greeting should not need clarification")
	}

	if mockClient.CallCount != 1 {
		t.Errorf("Expected 1 LLM call, got %d", mockClient.CallCount)
	}
}

func TestClassify_ReadInfo(t *testing.T) {
	intent := agent.Intent{
		Type:       agent.IntentReadInfo,
		Confidence: 0.90,
		ToolCalls: []agent.ToolCall{
			{
				Name:     "read_file",
				Category: agent.ToolCategorySafeRead,
				Parameters: map[string]interface{}{
					"path": "/etc/config.json",
				},
			},
		},
		RequiredParams: []string{"path"},
		ProvidedParams: map[string]interface{}{
			"path": "/etc/config.json",
		},
		MissingParams: []string{},
		NeedsConfirm:  false,
		Reasoning:     "User wants to read a file",
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response: string(responseJSON),
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	result, err := classifier.Classify(context.Background(), "Đọc file /etc/config.json")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Intent.Type != agent.IntentReadInfo {
		t.Errorf("Expected READ_INFO, got %s", result.Intent.Type)
	}

	if result.NeedsClarification {
		t.Error("Should not need clarification with high confidence")
	}

	if len(result.Intent.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(result.Intent.ToolCalls))
	}
}

func TestClassify_DangerousAction_WithMissingParams(t *testing.T) {
	intent := agent.Intent{
		Type:           agent.IntentDangerousAction,
		Confidence:     0.85,
		RequiredParams: []string{"path", "confirm"},
		ProvidedParams: map[string]interface{}{},
		MissingParams:  []string{"path", "confirm"},
		ToolCalls: []agent.ToolCall{
			{
				Name:       "delete_file",
				Category:   agent.ToolCategoryDangerousWrite,
				Parameters: map[string]interface{}{},
			},
		},
		NeedsConfirm: false, // Will be set to true by classifier
		Reasoning:    "User wants to delete but missing path",
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response: string(responseJSON),
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	result, err := classifier.Classify(context.Background(), "Xóa file config")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Intent.Type != agent.IntentDangerousAction {
		t.Errorf("Expected DANGEROUS_ACTION, got %s", result.Intent.Type)
	}

	// Should need confirmation due to missing params
	if !result.Intent.NeedsConfirm {
		t.Error("Dangerous action with missing params should need confirmation")
	}

	if len(result.Intent.MissingParams) == 0 {
		t.Error("Expected missing params")
	}
}

func TestClassify_LowConfidence(t *testing.T) {
	intent := agent.Intent{
		Type:       agent.IntentReadInfo,
		Confidence: 0.50, // Below threshold (0.70)
		Reasoning:  "Low confidence classification",
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response: string(responseJSON),
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	result, err := classifier.Classify(context.Background(), "Ambiguous input")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should need clarification due to low confidence
	if !result.NeedsClarification {
		t.Error("Low confidence should trigger clarification")
	}
}

func TestClassify_AmbiguousRange(t *testing.T) {
	intent := agent.Intent{
		Type:       agent.IntentReadInfo,
		Confidence: 0.75, // In ambiguous range (0.60-0.85)
		Reasoning:  "Ambiguous classification",
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response: string(responseJSON),
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	result, err := classifier.Classify(context.Background(), "Xử lý file config")

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should need clarification due to ambiguous confidence
	if !result.NeedsClarification {
		t.Error("Ambiguous confidence should trigger clarification")
	}

	// Should have clarification options
	if result.ClarificationOptions == nil {
		t.Error("Expected clarification options")
	}

	if len(result.ClarificationOptions.Options) == 0 {
		t.Error("Expected at least one clarification option")
	}
}

func TestClassify_InvalidJSON(t *testing.T) {
	mockClient := &MockLLMClient{
		Response: "This is not valid JSON",
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	_, err := classifier.Classify(context.Background(), "Test input")

	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestClassify_RetryOnError(t *testing.T) {
	intent := agent.Intent{
		Type:       agent.IntentGreeting,
		Confidence: 0.95,
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response:    string(responseJSON),
		ShouldFailN: 1, // Fail first call, succeed on retry
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
		MaxRetries:       2,
		RetryDelay:       10 * time.Millisecond,
	}

	classifier := NewIntentClassifier(config)
	result, err := classifier.Classify(context.Background(), "Hello")

	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}

	if result.Intent.Type != agent.IntentGreeting {
		t.Errorf("Expected GREETING, got %s", result.Intent.Type)
	}

	// Should have called LLM twice (1 failure + 1 success)
	if mockClient.CallCount != 2 {
		t.Errorf("Expected 2 LLM calls (with retry), got %d", mockClient.CallCount)
	}
}

func TestClassify_NonRetryableError(t *testing.T) {
	mockClient := &MockLLMClient{
		Error: errors.New("invalid API key"), // Non-retryable
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
		MaxRetries:       2,
	}

	classifier := NewIntentClassifier(config)
	_, err := classifier.Classify(context.Background(), "Test")

	if err == nil {
		t.Error("Expected error for non-retryable error")
	}

	// Should only call once (no retry for non-retryable errors)
	if mockClient.CallCount != 1 {
		t.Errorf("Expected 1 LLM call (no retry), got %d", mockClient.CallCount)
	}
}

func TestIsRetryableError(t *testing.T) {
	classifier := NewIntentClassifier(IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	})

	tests := []struct {
		name       string
		err        error
		retryable  bool
	}{
		{"Nil error", nil, false},
		{"Timeout", errors.New("request timeout"), true},
		{"Connection", errors.New("connection refused"), true},
		{"Rate limit", errors.New("rate limit exceeded"), true},
		{"429 status", errors.New("HTTP 429"), true},
		{"503 status", errors.New("HTTP 503"), true},
		{"Invalid API key", errors.New("invalid API key"), false},
		{"Not found", errors.New("resource not found"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifier.isRetryableError(tt.err)
			if result != tt.retryable {
				t.Errorf("Expected retryable=%v for error %q, got %v",
					tt.retryable, tt.err, result)
			}
		})
	}
}

func TestIsValidIntentType(t *testing.T) {
	classifier := NewIntentClassifier(IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	})

	tests := []struct {
		intentType agent.IntentType
		valid      bool
	}{
		{agent.IntentGreeting, true},
		{agent.IntentReadInfo, true},
		{agent.IntentDangerousAction, true},
		{agent.IntentComposite, true},
		{agent.IntentUnknown, true},
		{"INVALID_TYPE", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.intentType), func(t *testing.T) {
			result := classifier.isValidIntentType(tt.intentType)
			if result != tt.valid {
				t.Errorf("Expected valid=%v for intent type %q, got %v",
					tt.valid, tt.intentType, result)
			}
		})
	}
}

func TestValidateIntent(t *testing.T) {
	classifier := NewIntentClassifier(IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	})

	tests := []struct {
		name      string
		intent    *agent.Intent
		expectErr bool
	}{
		{
			name: "Valid greeting",
			intent: &agent.Intent{
				Type:       agent.IntentGreeting,
				Confidence: 0.95,
			},
			expectErr: false,
		},
		{
			name: "Invalid intent type",
			intent: &agent.Intent{
				Type:       "INVALID",
				Confidence: 0.95,
			},
			expectErr: true,
		},
		{
			name: "Confidence out of range (too high)",
			intent: &agent.Intent{
				Type:       agent.IntentGreeting,
				Confidence: 1.5,
			},
			expectErr: true,
		},
		{
			name: "Confidence out of range (negative)",
			intent: &agent.Intent{
				Type:       agent.IntentGreeting,
				Confidence: -0.1,
			},
			expectErr: true,
		},
		{
			name: "Dangerous action with missing params but no confirm",
			intent: &agent.Intent{
				Type:          agent.IntentDangerousAction,
				Confidence:    0.95,
				MissingParams: []string{"path"},
				NeedsConfirm:  false,
			},
			expectErr: true,
		},
		{
			name: "Dangerous action with missing params and confirm",
			intent: &agent.Intent{
				Type:          agent.IntentDangerousAction,
				Confidence:    0.95,
				MissingParams: []string{"path"},
				NeedsConfirm:  true,
			},
			expectErr: false,
		},
		{
			name: "Invalid tool in tool_calls",
			intent: &agent.Intent{
				Type:       agent.IntentReadInfo,
				Confidence: 0.90,
				ToolCalls: []agent.ToolCall{
					{Name: "non_existent_tool"},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifier.ValidateIntent(tt.intent)

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}

			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestGetIntentLabel(t *testing.T) {
	classifier := NewIntentClassifier(IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	})

	tests := []struct {
		intentType agent.IntentType
		expected   string
	}{
		{agent.IntentGreeting, "Chào hỏi/trò chuyện"},
		{agent.IntentReadInfo, "Đọc/xem thông tin"},
		{agent.IntentDangerousAction, "Thực hiện hành động (sửa/xóa/gửi)"},
		{agent.IntentComposite, "Thực hiện nhiều bước"},
		{agent.IntentUnknown, "Không rõ ý định"},
	}

	for _, tt := range tests {
		t.Run(string(tt.intentType), func(t *testing.T) {
			result := classifier.getIntentLabel(tt.intentType)
			if result != tt.expected {
				t.Errorf("Expected label %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCalculateConfidenceHeuristic(t *testing.T) {
	classifier := NewIntentClassifier(IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	})

	tests := []struct {
		input      string
		intentType agent.IntentType
		minScore   float64
	}{
		{"Chào buổi sáng", agent.IntentGreeting, 0.90},
		{"Đọc file config.json", agent.IntentReadInfo, 0.60},
		{"Xóa file test.txt", agent.IntentDangerousAction, 0.70},
		{"Tìm và xóa file log", agent.IntentComposite, 0.70},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := classifier.CalculateConfidenceHeuristic(tt.input, tt.intentType)

			if score < tt.minScore {
				t.Errorf("Expected score >= %.2f for %q, got %.2f",
					tt.minScore, tt.input, score)
			}
		})
	}
}

func TestGetStatistics(t *testing.T) {
	config := IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
		MaxRetries:       3,
		RetryDelay:       2 * time.Second,
	}

	classifier := NewIntentClassifier(config)
	stats := classifier.GetStatistics()

	if stats["max_retries"] != 3 {
		t.Errorf("Expected max_retries=3, got %v", stats["max_retries"])
	}

	if stats["retry_delay"] != "2s" {
		t.Errorf("Expected retry_delay=2s, got %v", stats["retry_delay"])
	}
}

func BenchmarkClassify(b *testing.B) {
	intent := agent.Intent{
		Type:       agent.IntentGreeting,
		Confidence: 0.95,
	}

	responseJSON, _ := json.Marshal(intent)
	mockClient := &MockLLMClient{
		Response: string(responseJSON),
	}

	config := IntentClassifierConfig{
		LLMClient:        mockClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	}

	classifier := NewIntentClassifier(config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = classifier.Classify(ctx, "Hello")
	}
}

func BenchmarkValidateIntent(b *testing.B) {
	classifier := NewIntentClassifier(IntentClassifierConfig{
		LLMClient:        &MockLLMClient{},
		ConfidenceConfig: agent.DefaultConfidenceConfig,
	})

	intent := &agent.Intent{
		Type:       agent.IntentGreeting,
		Confidence: 0.95,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifier.ValidateIntent(intent)
	}
}
