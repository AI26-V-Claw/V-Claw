package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// IntentClassifier classifies user intents
type IntentClassifier struct {
	config           ConfidenceConfig
	confidenceScorer *ConfidenceScorer
}

// NewIntentClassifier creates a new intent classifier
func NewIntentClassifier(config ConfidenceConfig) *IntentClassifier {
	return &IntentClassifier{
		config:           config,
		confidenceScorer: NewConfidenceScorer(config),
	}
}

// Classify classifies user input into an intent
func (ic *IntentClassifier) Classify(ctx context.Context, userInput string) (*ClassificationResult, error) {
	if strings.TrimSpace(userInput) == "" {
		return &ClassificationResult{
			Error: fmt.Errorf("user input cannot be empty"),
		}, nil
	}

	// Step 1: Determine intent type using heuristics
	// In production, this would call LLM API
	intentType := ic.determineIntentType(userInput)

	// Step 2: Calculate confidence score
	confidence := ic.confidenceScorer.CalculateHeuristic(userInput, intentType)

	// Step 3: Extract tool calls based on intent
	toolCalls := ic.extractToolCalls(userInput, intentType)

	// Step 4: Validate parameters
	requiredParams, providedParams, missingParams := ic.validateParameters(userInput, toolCalls)

	// Step 5: Determine if confirmation is needed
	needsConfirm := ic.needsConfirmation(intentType, toolCalls)

	// Step 6: Generate reasoning
	reasoning := ic.generateReasoning(userInput, intentType, confidence)

	// Create intent object
	intent := &Intent{
		Type:           intentType,
		Confidence:     confidence,
		RequiredParams: requiredParams,
		ProvidedParams: providedParams,
		MissingParams:  missingParams,
		ToolCalls:      toolCalls,
		NeedsConfirm:   needsConfirm,
		Reasoning:      reasoning,
		Timestamp:      time.Now(),
	}

	// Step 7: Check if clarification is needed
	if ic.confidenceScorer.ShouldAskForClarification(confidence, intentType) {
		clarificationOptions := ic.generateClarificationOptions(userInput, intentType, confidence)
		return &ClassificationResult{
			Intent:               intent,
			NeedsClarification:   true,
			ClarificationOptions: clarificationOptions,
		}, nil
	}

	// Step 8: Check if missing required parameters
	if len(missingParams) > 0 && intentType == IntentDangerousAction {
		return &ClassificationResult{
			Intent:             intent,
			NeedsClarification: true,
			ClarificationOptions: &ClarificationOptions{
				Question: ic.generateMissingParamsQuestion(missingParams, toolCalls),
				Options:  nil, // No multiple choice, just ask for params
			},
		}, nil
	}

	return &ClassificationResult{
		Intent:             intent,
		NeedsClarification: false,
	}, nil
}

// determineIntentType determines the intent type using heuristics
func (ic *IntentClassifier) determineIntentType(input string) IntentType {
	input = strings.ToLower(strings.TrimSpace(input))

	// Check for greeting patterns
	greetingPatterns := []string{
		"chào", "hello", "hi", "hey", "xin chào",
		"cảm ơn", "thank", "tạm biệt", "bye",
	}
	for _, pattern := range greetingPatterns {
		if strings.Contains(input, pattern) && len(input) < 50 {
			return IntentGreeting
		}
	}

	// Check for composite patterns (must check before individual patterns)
	compositePatterns := []string{
		"tìm và xóa", "find and delete",
		"đọc và gửi", "read and send",
		"tìm rồi", "find then",
	}
	for _, pattern := range compositePatterns {
		if strings.Contains(input, pattern) {
			return IntentComposite
		}
	}

	// Check for dangerous action patterns
	dangerousPatterns := []string{
		"xóa", "delete", "remove",
		"gửi email", "send email",
		"chạy", "run", "exec",
		"sửa", "edit", "modify",
		"tạo file", "create file", "write",
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(input, pattern) {
			return IntentDangerousAction
		}
	}

	// Check for read info patterns
	readPatterns := []string{
		"đọc", "read", "xem", "view", "show",
		"tìm", "find", "search",
		"list", "danh sách",
		"cho tôi xem", "cho tôi biết",
	}
	for _, pattern := range readPatterns {
		if strings.Contains(input, pattern) {
			return IntentReadInfo
		}
	}

	return IntentUnknown
}

// extractToolCalls extracts tool calls based on intent and input
func (ic *IntentClassifier) extractToolCalls(input string, intentType IntentType) []ToolCall {
	input = strings.ToLower(input)
	var toolCalls []ToolCall

	switch intentType {
	case IntentGreeting:
		// No tool calls for greetings
		return nil

	case IntentReadInfo:
		if strings.Contains(input, "file") || strings.Contains(input, "đọc") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "read_file",
				Category:   ToolCategorySafeRead,
				Parameters: ic.extractFileParams(input),
				Timeout:    30,
			})
		} else if strings.Contains(input, "tìm") || strings.Contains(input, "search") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "web_search",
				Category:   ToolCategorySafeRead,
				Parameters: ic.extractSearchParams(input),
				Timeout:    45,
			})
		} else if strings.Contains(input, "list") || strings.Contains(input, "danh sách") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "list_directory",
				Category:   ToolCategorySafeRead,
				Parameters: ic.extractDirectoryParams(input),
				Timeout:    30,
			})
		}

	case IntentDangerousAction:
		if strings.Contains(input, "xóa") || strings.Contains(input, "delete") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "delete_file",
				Category:   ToolCategoryDangerousWrite,
				Parameters: ic.extractFileParams(input),
				Timeout:    60,
			})
		} else if strings.Contains(input, "gửi") || strings.Contains(input, "send") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "send_email",
				Category:   ToolCategoryCommunication,
				Parameters: ic.extractEmailParams(input),
				Timeout:    60,
			})
		} else if strings.Contains(input, "chạy") || strings.Contains(input, "exec") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "exec",
				Category:   ToolCategoryExecution,
				Parameters: ic.extractExecParams(input),
				Timeout:    120,
			})
		} else if strings.Contains(input, "sửa") || strings.Contains(input, "write") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "write_file",
				Category:   ToolCategoryDangerousWrite,
				Parameters: ic.extractFileParams(input),
				Timeout:    60,
			})
		}

	case IntentComposite:
		// For composite actions, extract multiple tool calls
		// This is simplified - in production, would use more sophisticated parsing
		if strings.Contains(input, "tìm") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "web_search",
				Category:   ToolCategorySafeRead,
				Parameters: ic.extractSearchParams(input),
				Timeout:    45,
			})
		}
		if strings.Contains(input, "xóa") {
			toolCalls = append(toolCalls, ToolCall{
				Name:       "delete_file",
				Category:   ToolCategoryDangerousWrite,
				Parameters: ic.extractFileParams(input),
				Timeout:    60,
			})
		}
	}

	return toolCalls
}

// extractFileParams extracts file-related parameters from input
func (ic *IntentClassifier) extractFileParams(input string) map[string]interface{} {
	params := make(map[string]interface{})

	// Simple pattern matching for file paths
	// In production, would use more sophisticated NER
	words := strings.Fields(input)
	for _, word := range words {
		if strings.Contains(word, ".") || strings.Contains(word, "/") {
			params["path"] = word
			break
		}
	}

	return params
}

// extractSearchParams extracts search-related parameters
func (ic *IntentClassifier) extractSearchParams(input string) map[string]interface{} {
	params := make(map[string]interface{})
	// Use the input as search query (simplified)
	params["query"] = input
	return params
}

// extractDirectoryParams extracts directory-related parameters
func (ic *IntentClassifier) extractDirectoryParams(input string) map[string]interface{} {
	params := make(map[string]interface{})
	// Similar to file params
	words := strings.Fields(input)
	for _, word := range words {
		if strings.Contains(word, "/") {
			params["path"] = word
			break
		}
	}
	return params
}

// extractEmailParams extracts email-related parameters
func (ic *IntentClassifier) extractEmailParams(input string) map[string]interface{} {
	params := make(map[string]interface{})
	// Simplified email extraction
	words := strings.Fields(input)
	for _, word := range words {
		if strings.Contains(word, "@") {
			params["to"] = word
			break
		}
	}
	return params
}

// extractExecParams extracts execution-related parameters
func (ic *IntentClassifier) extractExecParams(input string) map[string]interface{} {
	params := make(map[string]interface{})
	// Use input as command (simplified)
	params["command"] = input
	return params
}

// validateParameters validates if all required parameters are provided
func (ic *IntentClassifier) validateParameters(input string, toolCalls []ToolCall) ([]string, map[string]interface{}, []string) {
	var allRequired []string
	allProvided := make(map[string]interface{})
	var allMissing []string

	for _, toolCall := range toolCalls {
		tool, err := GetTool(toolCall.Name)
		if err != nil {
			continue
		}

		for _, param := range tool.Parameters {
			if param.Required {
				allRequired = append(allRequired, param.Name)

				// Check if parameter is provided
				if val, exists := toolCall.Parameters[param.Name]; exists && val != nil && val != "" {
					allProvided[param.Name] = val
				} else {
					allMissing = append(allMissing, param.Name)
				}
			}
		}
	}

	return allRequired, allProvided, allMissing
}

// needsConfirmation determines if user confirmation is required
func (ic *IntentClassifier) needsConfirmation(intentType IntentType, toolCalls []ToolCall) bool {
	if intentType == IntentDangerousAction || intentType == IntentComposite {
		return true
	}

	for _, toolCall := range toolCalls {
		if IsDangerousTool(toolCall.Name) {
			return true
		}
	}

	return false
}

// generateReasoning generates explanation for the classification
func (ic *IntentClassifier) generateReasoning(input string, intentType IntentType, confidence float64) string {
	return fmt.Sprintf("Classified as %s with confidence %.2f based on input: %q",
		intentType, confidence, input)
}

// generateClarificationOptions generates multiple choice options for ambiguous intents
func (ic *IntentClassifier) generateClarificationOptions(input string, primaryIntent IntentType, confidence float64) *ClarificationOptions {
	options := []Option{
		{
			ID:         "A",
			Label:      "Đọc và hiển thị thông tin (READ_INFO)",
			IntentType: IntentReadInfo,
			Confidence: 0.7,
		},
		{
			ID:         "B",
			Label:      "Thực hiện thay đổi/xóa/gửi (DANGEROUS_ACTION)",
			IntentType: IntentDangerousAction,
			Confidence: 0.6,
		},
		{
			ID:         "C",
			Label:      "Hành động phức hợp nhiều bước (COMPOSITE_ACTION)",
			IntentType: IntentComposite,
			Confidence: 0.5,
		},
	}

	return &ClarificationOptions{
		Question: fmt.Sprintf("Tôi chưa hiểu rõ ý bạn với câu: %q\n\nBạn muốn làm gì?", input),
		Options:  options,
	}
}

// generateMissingParamsQuestion generates question for missing parameters
func (ic *IntentClassifier) generateMissingParamsQuestion(missingParams []string, toolCalls []ToolCall) string {
	if len(toolCalls) == 0 {
		return "Vui lòng cung cấp thêm thông tin để tôi thực hiện."
	}

	toolName := toolCalls[0].Name
	paramList := strings.Join(missingParams, ", ")

	return fmt.Sprintf("Để thực hiện %s, tôi cần thêm thông tin: %s\n\nVui lòng cung cấp đầy đủ.", toolName, paramList)
}
