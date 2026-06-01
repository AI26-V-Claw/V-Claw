package agent

import (
	"math"
	"strings"
)

// ConfidenceScorer calculates confidence scores for intent classification
type ConfidenceScorer struct {
	config ConfidenceConfig
}

// NewConfidenceScorer creates a new confidence scorer
func NewConfidenceScorer(config ConfidenceConfig) *ConfidenceScorer {
	return &ConfidenceScorer{
		config: config,
	}
}

// CalculateFromLogprobs calculates confidence from log probabilities
// This would be used when LLM API provides logprobs
func (cs *ConfidenceScorer) CalculateFromLogprobs(logprobs []float64) float64 {
	if len(logprobs) == 0 {
		return 0.0
	}

	// Take average of top-3 tokens
	count := min(3, len(logprobs))
	sum := 0.0
	for i := 0; i < count; i++ {
		sum += logprobs[i]
	}
	avgLogprob := sum / float64(count)

	// Convert log probability to confidence (0-1)
	confidence := math.Exp(avgLogprob)

	// Clamp to [0, 1]
	return math.Max(0.0, math.Min(1.0, confidence))
}

// CalculateHeuristic calculates confidence using heuristic rules
// This is a fallback when logprobs are not available
func (cs *ConfidenceScorer) CalculateHeuristic(userInput string, intentType IntentType) float64 {
	input := strings.ToLower(strings.TrimSpace(userInput))

	// Base confidence by intent type
	baseConfidence := 0.5

	switch intentType {
	case IntentGreeting:
		baseConfidence = cs.scoreGreeting(input)
	case IntentReadInfo:
		baseConfidence = cs.scoreReadInfo(input)
	case IntentDangerousAction:
		baseConfidence = cs.scoreDangerousAction(input)
	case IntentComposite:
		baseConfidence = cs.scoreComposite(input)
	default:
		baseConfidence = 0.3
	}

	return baseConfidence
}

// scoreGreeting scores greeting intents
func (cs *ConfidenceScorer) scoreGreeting(input string) float64 {
	greetingKeywords := []string{
		"chào", "hello", "hey", "xin chào",
		"cảm ơn", "thank", "thanks", "tạm biệt", "bye", "goodbye",
	}

	// Check for whole-word matches to avoid substring false positives (e.g. "hi" in "this")
	for _, keyword := range greetingKeywords {
		if containsWholeWord(input, keyword) {
			return 0.95
		}
	}

	// "hi" requires whole-word match (short, easily matched as substring)
	if containsWholeWord(input, "hi") {
		return 0.95
	}

	// Short inputs are likely greetings
	if len(input) < 20 && !strings.Contains(input, "file") && !strings.Contains(input, "xóa") {
		return 0.7
	}

	return 0.3
}

// containsWholeWord checks if input contains keyword as a whole word (space-delimited)
func containsWholeWord(input, keyword string) bool {
	// Exact match
	if input == keyword {
		return true
	}
	// Check with surrounding spaces or at boundaries
	if strings.HasPrefix(input, keyword+" ") {
		return true
	}
	if strings.HasSuffix(input, " "+keyword) {
		return true
	}
	if strings.Contains(input, " "+keyword+" ") {
		return true
	}
	return false
}

// scoreReadInfo scores read info intents
func (cs *ConfidenceScorer) scoreReadInfo(input string) float64 {
	readKeywords := []string{
		"đọc", "read", "xem", "view", "show", "hiển thị",
		"tìm", "find", "search", "tìm kiếm",
		"list", "danh sách", "liệt kê",
		"cho tôi xem", "cho tôi biết",
	}

	score := 0.5
	for _, keyword := range readKeywords {
		if strings.Contains(input, keyword) {
			score += 0.2
		}
	}

	// Penalize if contains dangerous keywords
	dangerousKeywords := []string{"xóa", "delete", "gửi", "send", "chạy", "run", "exec"}
	for _, keyword := range dangerousKeywords {
		if strings.Contains(input, keyword) {
			score -= 0.3
		}
	}

	return math.Max(0.0, math.Min(1.0, score))
}

// scoreDangerousAction scores dangerous action intents
func (cs *ConfidenceScorer) scoreDangerousAction(input string) float64 {
	dangerousKeywords := []string{
		"xóa", "delete", "remove",
		"gửi", "send",
		"chạy", "run", "exec", "execute",
		"sửa", "edit", "modify", "update",
		"tạo", "create", "write",
	}

	score := 0.5
	for _, keyword := range dangerousKeywords {
		if strings.Contains(input, keyword) {
			score += 0.25
		}
	}

	// Boost if contains file/email/command references
	if strings.Contains(input, "file") || strings.Contains(input, "email") || strings.Contains(input, "command") {
		score += 0.15
	}

	return math.Max(0.0, math.Min(1.0, score))
}

// scoreComposite scores composite action intents
func (cs *ConfidenceScorer) scoreComposite(input string) float64 {
	// Look for conjunctions indicating multiple steps
	compositeIndicators := []string{
		"và", "and", "rồi", "then",
		"sau đó", "after that",
		"tìm và", "find and",
	}

	score := 0.5
	for _, indicator := range compositeIndicators {
		if strings.Contains(input, indicator) {
			score += 0.2
		}
	}

	// Check if contains both read and dangerous keywords
	hasRead := strings.Contains(input, "tìm") || strings.Contains(input, "find") || strings.Contains(input, "đọc")
	hasDangerous := strings.Contains(input, "xóa") || strings.Contains(input, "delete") || strings.Contains(input, "gửi")

	if hasRead && hasDangerous {
		score += 0.3
	}

	return math.Max(0.0, math.Min(1.0, score))
}

// ShouldAskForClarification determines if clarification is needed
func (cs *ConfidenceScorer) ShouldAskForClarification(confidence float64, intentType IntentType) bool {
	// Greetings never need clarification - they are always safe to proceed
	if intentType == IntentGreeting {
		return false
	}

	// Always ask for clarification if confidence is too low
	if confidence < cs.config.AmbiguousRangeLow {
		return true
	}

	// For dangerous actions, require high confidence
	if intentType == IntentDangerousAction && confidence < cs.config.DangerousActionMinConfidence {
		return true
	}

	// Check if in ambiguous range
	return cs.config.IsAmbiguous(confidence)
}

