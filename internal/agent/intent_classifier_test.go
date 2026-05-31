package agent

import (
	"context"
	"testing"
)

func TestIntentClassifier_Classify_Greeting(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	testCases := []struct {
		name           string
		input          string
		expectedIntent IntentType
		minConfidence  float64
	}{
		{
			name:           "TC001: Vietnamese greeting",
			input:          "Chào buổi sáng",
			expectedIntent: IntentGreeting,
			minConfidence:  0.9,
		},
		{
			name:           "English greeting",
			input:          "Hello",
			expectedIntent: IntentGreeting,
			minConfidence:  0.9,
		},
		{
			name:           "Thank you",
			input:          "Cảm ơn",
			expectedIntent: IntentGreeting,
			minConfidence:  0.9,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Intent.Type != tc.expectedIntent {
				t.Errorf("expected intent %s, got %s", tc.expectedIntent, result.Intent.Type)
			}

			if result.Intent.Confidence < tc.minConfidence {
				t.Errorf("expected confidence >= %.2f, got %.2f", tc.minConfidence, result.Intent.Confidence)
			}

			if len(result.Intent.ToolCalls) > 0 {
				t.Errorf("greeting should not have tool calls, got %d", len(result.Intent.ToolCalls))
			}
		})
	}
}

func TestIntentClassifier_Classify_ReadInfo(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	testCases := []struct {
		name           string
		input          string
		expectedIntent IntentType
		minConfidence  float64
		expectedTool   string
	}{
		{
			name:           "TC002: Search request",
			input:          "Tìm cho tôi báo cáo tài chính quý 3",
			expectedIntent: IntentReadInfo,
			minConfidence:  0.7,
			expectedTool:   "web_search",
		},
		{
			name:           "Read file request",
			input:          "Đọc file config.json",
			expectedIntent: IntentReadInfo,
			minConfidence:  0.7,
			expectedTool:   "read_file",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Intent.Type != tc.expectedIntent {
				t.Errorf("expected intent %s, got %s", tc.expectedIntent, result.Intent.Type)
			}

			if result.Intent.Confidence < tc.minConfidence {
				t.Errorf("expected confidence >= %.2f, got %.2f", tc.minConfidence, result.Intent.Confidence)
			}

			if len(result.Intent.ToolCalls) == 0 {
				t.Errorf("expected tool calls, got none")
			} else if result.Intent.ToolCalls[0].Name != tc.expectedTool {
				t.Errorf("expected tool %s, got %s", tc.expectedTool, result.Intent.ToolCalls[0].Name)
			}
		})
	}
}

func TestIntentClassifier_Classify_DangerousAction(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	testCases := []struct {
		name           string
		input          string
		expectedIntent IntentType
		expectedTool   string
		shouldConfirm  bool
	}{
		{
			name:           "TC003: Delete file with path",
			input:          "Xóa file config.json",
			expectedIntent: IntentDangerousAction,
			expectedTool:   "delete_file",
			shouldConfirm:  true,
		},
		{
			name:           "Send email",
			input:          "Gửi email cho boss@company.com",
			expectedIntent: IntentDangerousAction,
			expectedTool:   "send_email",
			shouldConfirm:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Intent.Type != tc.expectedIntent {
				t.Errorf("expected intent %s, got %s", tc.expectedIntent, result.Intent.Type)
			}

			if result.Intent.NeedsConfirm != tc.shouldConfirm {
				t.Errorf("expected NeedsConfirm=%v, got %v", tc.shouldConfirm, result.Intent.NeedsConfirm)
			}

			if len(result.Intent.ToolCalls) == 0 {
				t.Errorf("expected tool calls, got none")
			} else if result.Intent.ToolCalls[0].Name != tc.expectedTool {
				t.Errorf("expected tool %s, got %s", tc.expectedTool, result.Intent.ToolCalls[0].Name)
			}
		})
	}
}

func TestIntentClassifier_Classify_MissingParameters(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	testCases := []struct {
		name                  string
		input                 string
		expectedIntent        IntentType
		shouldNeedClarify     bool
		expectedMissingParams int
	}{
		{
			name:                  "TC004: Delete without path",
			input:                 "Xóa mẹ nó đi",
			expectedIntent:        IntentDangerousAction,
			shouldNeedClarify:     true,
			expectedMissingParams: 1, // missing "path"
		},
		{
			name:                  "TC005: Send email without details",
			input:                 "Gửi email",
			expectedIntent:        IntentDangerousAction,
			shouldNeedClarify:     true,
			expectedMissingParams: 2, // missing "to", "subject", "body"
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Intent.Type != tc.expectedIntent {
				t.Errorf("expected intent %s, got %s", tc.expectedIntent, result.Intent.Type)
			}

			if result.NeedsClarification != tc.shouldNeedClarify {
				t.Errorf("expected NeedsClarification=%v, got %v", tc.shouldNeedClarify, result.NeedsClarification)
			}

			if len(result.Intent.MissingParams) < tc.expectedMissingParams {
				t.Errorf("expected at least %d missing params, got %d", tc.expectedMissingParams, len(result.Intent.MissingParams))
			}
		})
	}
}

func TestIntentClassifier_Classify_CompositeAction(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	testCases := []struct {
		name              string
		input             string
		expectedIntent    IntentType
		minToolCalls      int
		shouldConfirm     bool
	}{
		{
			name:           "TC008: Find and delete",
			input:          "Tìm và xóa các file log cũ hơn 30 ngày",
			expectedIntent: IntentComposite,
			minToolCalls:   2,
			shouldConfirm:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Intent.Type != tc.expectedIntent {
				t.Errorf("expected intent %s, got %s", tc.expectedIntent, result.Intent.Type)
			}

			if len(result.Intent.ToolCalls) < tc.minToolCalls {
				t.Errorf("expected at least %d tool calls, got %d", tc.minToolCalls, len(result.Intent.ToolCalls))
			}

			if result.Intent.NeedsConfirm != tc.shouldConfirm {
				t.Errorf("expected NeedsConfirm=%v, got %v", tc.shouldConfirm, result.Intent.NeedsConfirm)
			}
		})
	}
}

func TestIntentClassifier_Classify_EmptyInput(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	result, err := classifier.Classify(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == nil {
		t.Error("expected error for empty input")
	}
}

func TestIntentClassifier_Classify_AmbiguousInput(t *testing.T) {
	classifier := NewIntentClassifier(DefaultConfidenceConfig)

	testCases := []struct {
		name              string
		input             string
		shouldNeedClarify bool
	}{
		{
			name:              "TC010: Ambiguous action",
			input:             "Xử lý file config",
			shouldNeedClarify: true,
		},
		{
			name:              "TC011: Very vague",
			input:             "Làm như hôm qua",
			shouldNeedClarify: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := classifier.Classify(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.NeedsClarification != tc.shouldNeedClarify {
				t.Errorf("expected NeedsClarification=%v, got %v", tc.shouldNeedClarify, result.NeedsClarification)
			}

			if result.NeedsClarification && result.ClarificationOptions == nil {
				t.Error("expected clarification options when clarification is needed")
			}
		})
	}
}
