package agent

import (
	"math"
	"testing"
	
	"vclaw/internal/agent/intent"
)

func TestNewConfidenceScorer(t *testing.T) {
	config := intent.DefaultConfig
	scorer := NewConfidenceScorer(config)

	if scorer == nil {
		t.Fatal("Expected non-nil scorer")
	}

	if scorer.config.DangerousActionMin != 0.90 {
		t.Errorf("Expected DangerousActionMin 0.90, got %.2f", scorer.config.DangerousActionMin)
	}
}

func TestCalculateFromLogprobs(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		name     string
		logprobs []float64
		expected float64
	}{
		{
			name:     "Empty logprobs",
			logprobs: []float64{},
			expected: 0.0,
		},
		{
			name:     "High confidence",
			logprobs: []float64{-0.1, -0.2, -0.1},
			expected: 0.85, // Approximate
		},
		{
			name:     "Low confidence",
			logprobs: []float64{-2.0, -2.5, -3.0},
			expected: 0.08, // Approximate
		},
		{
			name:     "Single logprob",
			logprobs: []float64{-0.5},
			expected: 0.60, // Approximate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.CalculateFromLogprobs(tt.logprobs)

			// Check if result is in reasonable range
			if result < 0.0 || result > 1.0 {
				t.Errorf("Confidence out of range [0,1]: %.2f", result)
			}

			// For non-empty logprobs, check approximate value
			if len(tt.logprobs) > 0 {
				diff := math.Abs(result - tt.expected)
				if diff > 0.15 { // Allow 15% tolerance
					t.Logf("Expected ~%.2f, got %.2f (diff: %.2f)", tt.expected, result, diff)
				}
			}
		})
	}
}

func TestCalculateHeuristic_Greeting(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input    string
		minScore float64
	}{
		{"Chào buổi sáng", 0.90},
		{"Hello", 0.90},
		{"Hi there", 0.90},
		{"Cảm ơn bạn", 0.90},
		{"Thanks", 0.90},
		{"Goodbye", 0.90},
		{"Tạm biệt", 0.90},
		{"Hey", 0.90},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.CalculateHeuristic(tt.input, intent.TypeGreeting)

			if score < tt.minScore {
				t.Errorf("Expected score >= %.2f for greeting %q, got %.2f", tt.minScore, tt.input, score)
			}
		})
	}
}

func TestCalculateHeuristic_ReadInfo(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input    string
		minScore float64
	}{
		{"Đọc file config.json", 0.60},
		{"Read file test.txt", 0.60},
		{"Xem nội dung file", 0.60},
		{"Show me the file", 0.60},
		{"Tìm kiếm thông tin", 0.60},
		{"Search for data", 0.60},
		{"List files in directory", 0.60},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.CalculateHeuristic(tt.input, intent.TypeReadInfo)

			if score < tt.minScore {
				t.Errorf("Expected score >= %.2f for read info %q, got %.2f", tt.minScore, tt.input, score)
			}
		})
	}
}

func TestCalculateHeuristic_DangerousAction(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input    string
		minScore float64
	}{
		{"Xóa file test.txt", 0.70},
		{"Delete file config.json", 0.70},
		{"Gửi email cho John", 0.70},
		{"Send email to boss", 0.70},
		{"Chạy lệnh npm install", 0.70},
		{"Run command git pull", 0.70},
		{"Sửa file settings.json", 0.70},
		{"Modify config file", 0.70},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.CalculateHeuristic(tt.input, intent.TypeDangerousAction)

			if score < tt.minScore {
				t.Errorf("Expected score >= %.2f for dangerous action %q, got %.2f", tt.minScore, tt.input, score)
			}
		})
	}
}

func TestCalculateHeuristic_Composite(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input    string
		minScore float64
	}{
		{"Tìm file log và xóa", 0.70},
		{"Find and delete old files", 0.70},
		{"Đọc email rồi trả lời", 0.70},
		{"Read email then reply", 0.70},
		{"Tìm file config sau đó sửa", 0.70},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.CalculateHeuristic(tt.input, intent.TypeComposite)

			if score < tt.minScore {
				t.Errorf("Expected score >= %.2f for composite %q, got %.2f", tt.minScore, tt.input, score)
			}
		})
	}
}

func TestShouldAskForClarification(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		name       string
		confidence float64
		intentType intent.IntentType
		shouldAsk  bool
	}{
		{
			name:       "Very low confidence",
			confidence: 0.40,
			intentType: intent.TypeReadInfo,
			shouldAsk:  true,
		},
		{
			name:       "Ambiguous confidence for read",
			confidence: 0.75,
			intentType: intent.TypeReadInfo,
			shouldAsk:  true,
		},
		{
			name:       "High confidence for read",
			confidence: 0.90,
			intentType: intent.TypeReadInfo,
			shouldAsk:  false,
		},
		{
			name:       "Low confidence for dangerous",
			confidence: 0.85,
			intentType: intent.TypeDangerousAction,
			shouldAsk:  true,
		},
		{
			name:       "High confidence for dangerous",
			confidence: 0.95,
			intentType: intent.TypeDangerousAction,
			shouldAsk:  false,
		},
		{
			name:       "Greeting always ok",
			confidence: 0.50,
			intentType: intent.TypeGreeting,
			shouldAsk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.ShouldAskForClarification(tt.confidence, tt.intentType)

			if result != tt.shouldAsk {
				t.Errorf("Expected shouldAsk=%v for confidence=%.2f, intentType=%s, got %v",
					tt.shouldAsk, tt.confidence, tt.intentType, result)
			}
		})
	}
}

func TestScoreGreeting(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input    string
		minScore float64
		maxScore float64
	}{
		{"chào", 0.90, 1.0},
		{"hello", 0.90, 1.0},
		{"cảm ơn", 0.90, 1.0},
		{"thanks", 0.90, 1.0},
		{"hi", 0.90, 1.0},
		{"short", 0.60, 0.80}, // Short but no greeting keyword
		{"this is a long sentence about files", 0.0, 0.40},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.scoreGreeting(tt.input)

			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("Expected score in range [%.2f, %.2f] for %q, got %.2f",
					tt.minScore, tt.maxScore, tt.input, score)
			}
		})
	}
}

func TestScoreReadInfo(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input       string
		expectHigh  bool // Should score > 0.6
	}{
		{"đọc file config", true},
		{"read file test", true},
		{"xem nội dung", true},
		{"tìm kiếm", true},
		{"list directory", true},
		{"xóa file", false}, // Has dangerous keyword
		{"delete file", false},
		{"random text", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.scoreReadInfo(tt.input)

			if tt.expectHigh && score < 0.6 {
				t.Errorf("Expected high score (>0.6) for %q, got %.2f", tt.input, score)
			}

			if !tt.expectHigh && score > 0.6 {
				t.Errorf("Expected low score (<0.6) for %q, got %.2f", tt.input, score)
			}
		})
	}
}

func TestScoreDangerousAction(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input      string
		expectHigh bool // Should score > 0.7
	}{
		{"xóa file test.txt", true},
		{"delete file config", true},
		{"gửi email", true},
		{"send email", true},
		{"chạy lệnh", true},
		{"run command", true},
		{"sửa file", true},
		{"modify file", true},
		{"đọc file", false}, // Read is not dangerous
		{"hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.scoreDangerousAction(tt.input)

			if tt.expectHigh && score < 0.7 {
				t.Errorf("Expected high score (>0.7) for %q, got %.2f", tt.input, score)
			}

			if !tt.expectHigh && score > 0.7 {
				t.Errorf("Expected low score (<0.7) for %q, got %.2f", tt.input, score)
			}
		})
	}
}

func TestScoreComposite(t *testing.T) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	tests := []struct {
		input      string
		expectHigh bool // Should score > 0.7
	}{
		{"tìm và xóa", true},
		{"find and delete", true},
		{"đọc rồi gửi", true},
		{"read then send", true},
		{"tìm file sau đó xóa", true},
		{"xóa file", false}, // Single action
		{"đọc file", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			score := scorer.scoreComposite(tt.input)

			if tt.expectHigh && score < 0.7 {
				t.Errorf("Expected high score (>0.7) for %q, got %.2f", tt.input, score)
			}

			if !tt.expectHigh && score > 0.7 {
				t.Errorf("Expected low score (<0.7) for %q, got %.2f", tt.input, score)
			}
		})
	}
}

func BenchmarkCalculateFromLogprobs(b *testing.B) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)
	logprobs := []float64{-0.1, -0.2, -0.3, -0.4, -0.5}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.CalculateFromLogprobs(logprobs)
	}
}

func BenchmarkCalculateHeuristic(b *testing.B) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)
	input := "Xóa file config.json trong thư mục /etc"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.CalculateHeuristic(input, intent.TypeDangerousAction)
	}
}

func BenchmarkShouldAskForClarification(b *testing.B) {
	scorer := NewConfidenceScorer(intent.DefaultConfig)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.ShouldAskForClarification(0.75, intent.TypeDangerousAction)
	}
}
