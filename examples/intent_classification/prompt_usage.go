package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/yourusername/goclaw/internal/agent"
	"github.com/yourusername/goclaw/internal/agent/prompts"
)

// This example demonstrates how to use the Intent Classifier prompt system

func main() {
	fmt.Println("=== Intent Classifier Prompt Usage Examples ===\n")

	// Example 1: Basic usage
	example1_BasicUsage()

	// Example 2: With tool registry
	example2_WithToolRegistry()

	// Example 3: With session history
	example3_WithSessionHistory()

	// Example 4: Full context
	example4_FullContext()

	// Example 5: Validating responses
	example5_ValidatingResponses()
}

// Example 1: Basic usage - just the prompt and user input
func example1_BasicUsage() {
	fmt.Println("--- Example 1: Basic Usage ---")

	// Create prompt builder
	builder := prompts.NewIntentClassifierPrompt()

	// User input
	userInput := "Xóa file config.json"

	// Build full prompt
	fullPrompt := builder.BuildWithUserInput(userInput)

	fmt.Printf("Prompt length: %d characters\n", len(fullPrompt))
	fmt.Printf("User input: %s\n", userInput)
	fmt.Println()

	// In real usage, you would send fullPrompt to LLM here
	// response := callLLM(fullPrompt)
}

// Example 2: With tool registry
func example2_WithToolRegistry() {
	fmt.Println("--- Example 2: With Tool Registry ---")

	// Define available tools
	tools := map[string]interface{}{
		"read_file": map[string]interface{}{
			"description": "Read file content",
			"category":    "SAFE_READ",
			"params":      []string{"path"},
		},
		"delete_file": map[string]interface{}{
			"description": "Delete a file",
			"category":    "DANGEROUS_WRITE",
			"params":      []string{"path", "confirm"},
		},
		"exec": map[string]interface{}{
			"description": "Execute shell command",
			"category":    "EXECUTION",
			"params":      []string{"command"},
		},
	}

	// Build prompt with tool registry
	builder := prompts.NewIntentClassifierPrompt()
	fullPrompt := builder.
		WithToolRegistry(tools).
		BuildWithUserInput("Chạy lệnh npm install")

	fmt.Printf("Prompt includes %d tools\n", len(tools))
	fmt.Println("This helps the LLM know which tools are available in this session")
	fmt.Println()
}

// Example 3: With session history
func example3_WithSessionHistory() {
	fmt.Println("--- Example 3: With Session History ---")

	// Recent conversation history
	history := []string{
		"User: Tìm file config trong thư mục /etc",
		"AI: Tìm thấy 2 file: /etc/config.json và /etc/config.yaml",
		"User: Đọc file JSON",
		"AI: [Displayed content of /etc/config.json]",
	}

	// Build prompt with history
	builder := prompts.NewIntentClassifierPrompt()
	fullPrompt := builder.
		WithSessionHistory(history, 5). // Keep last 5 turns
		BuildWithUserInput("Xóa file đó")

	fmt.Printf("Included %d history items\n", len(history))
	fmt.Println("Note: The prompt includes a WARNING not to use history for dangerous actions")
	fmt.Println("Expected: AI should ask 'Which file do you want to delete?' instead of assuming")
	fmt.Println()
}

// Example 4: Full context - combining everything
func example4_FullContext() {
	fmt.Println("--- Example 4: Full Context ---")

	// Tools
	tools := map[string]interface{}{
		"read_file":   "Read file content",
		"delete_file": "Delete a file",
	}

	// History
	history := []string{
		"User: List files in /tmp",
		"AI: Found 5 files: test.txt, data.json, ...",
	}

	// Build comprehensive prompt
	builder := prompts.NewIntentClassifierPrompt()
	fullPrompt := builder.
		WithToolRegistry(tools).
		WithUserContext("user123", "/home/user/project").
		WithSessionHistory(history, 5).
		WithContext("User is working on a cleanup task").
		BuildWithUserInput("Xóa các file log cũ hơn 30 ngày")

	fmt.Printf("Full prompt length: %d characters\n", len(fullPrompt))
	fmt.Println("Includes: base prompt + tools + user context + history + custom context + user input")
	fmt.Println()
}

// Example 5: Validating and parsing responses
func example5_ValidatingResponses() {
	fmt.Println("--- Example 5: Validating Responses ---")

	// Simulate LLM responses
	testCases := []struct {
		name     string
		response string
		valid    bool
	}{
		{
			name: "Valid JSON response",
			response: `{
				"intent_type": "READ_INFO",
				"confidence": 0.95,
				"required_params": ["path"],
				"provided_params": {"path": "/etc/config.json"},
				"missing_params": [],
				"tool_calls": [
					{
						"name": "read_file",
						"category": "SAFE_READ",
						"parameters": {"path": "/etc/config.json"},
						"timeout": 30
					}
				],
				"needs_confirm": false,
				"reasoning": "User wants to read a specific file"
			}`,
			valid: true,
		},
		{
			name: "Invalid - wrapped in markdown",
			response: "```json\n{\n  \"intent_type\": \"READ_INFO\"\n}\n```",
			valid: false,
		},
		{
			name: "Invalid - has explanation before JSON",
			response: "Here's my analysis:\n{\n  \"intent_type\": \"READ_INFO\"\n}",
			valid: false,
		},
		{
			name: "Valid - with whitespace",
			response: "\n\n  {\n    \"intent_type\": \"GREETING\",\n    \"confidence\": 1.0\n  }\n\n",
			valid: true,
		},
	}

	for _, tc := range testCases {
		fmt.Printf("\nTest: %s\n", tc.name)
		
		// Validate JSON format
		err := prompts.ValidateJSONResponse(tc.response)
		
		if tc.valid && err != nil {
			fmt.Printf("  ❌ Expected valid, got error: %v\n", err)
		} else if !tc.valid && err == nil {
			fmt.Printf("  ❌ Expected invalid, but passed validation\n")
		} else if tc.valid && err == nil {
			fmt.Printf("  ✅ Valid JSON format\n")
			
			// Try to parse into Intent struct
			var intent agent.Intent
			if err := json.Unmarshal([]byte(tc.response), &intent); err != nil {
				fmt.Printf("  ⚠️  Valid format but failed to parse: %v\n", err)
			} else {
				fmt.Printf("  ✅ Successfully parsed into Intent struct\n")
				fmt.Printf("     Type: %s, Confidence: %.2f\n", intent.Type, intent.Confidence)
			}
		} else {
			fmt.Printf("  ✅ Correctly rejected invalid format\n")
		}
	}
	
	fmt.Println()
}

// Example helper function showing how to call LLM (pseudo-code)
func exampleLLMIntegration() {
	fmt.Println("--- Example: LLM Integration (Pseudo-code) ---")

	// 1. Build prompt
	builder := prompts.NewIntentClassifierPrompt()
	userInput := "Xóa file /tmp/test.txt"
	fullPrompt := builder.BuildWithUserInput(userInput)

	// 2. Call LLM (pseudo-code)
	fmt.Println("// Call LLM API")
	fmt.Println("response := gemini.GenerateContent(ctx, fullPrompt, &genai.GenerateContentConfig{")
	fmt.Println("    Temperature: 0.0, // Deterministic output")
	fmt.Println("    ResponseMIMEType: \"application/json\",")
	fmt.Println("})")

	// 3. Validate response
	fmt.Println("\n// Validate JSON format")
	fmt.Println("if err := prompts.ValidateJSONResponse(response); err != nil {")
	fmt.Println("    return fmt.Errorf(\"invalid response format: %w\", err)")
	fmt.Println("}")

	// 4. Parse into struct
	fmt.Println("\n// Parse into Intent struct")
	fmt.Println("var intent agent.Intent")
	fmt.Println("if err := json.Unmarshal([]byte(response), &intent); err != nil {")
	fmt.Println("    return fmt.Errorf(\"failed to parse intent: %w\", err)")
	fmt.Println("}")

	// 5. Check confidence and missing params
	fmt.Println("\n// Check if clarification needed")
	fmt.Println("if intent.NeedsConfirm || len(intent.MissingParams) > 0 {")
	fmt.Println("    return askUserForClarification(intent)")
	fmt.Println("}")

	// 6. Execute tool calls
	fmt.Println("\n// Execute tool calls")
	fmt.Println("for _, toolCall := range intent.ToolCalls {")
	fmt.Println("    result, err := executor.Execute(toolCall)")
	fmt.Println("    // Handle result...")
	fmt.Println("}")

	fmt.Println()
}

// Example showing different confidence scenarios
func exampleConfidenceHandling() {
	fmt.Println("--- Example: Confidence Handling ---")

	scenarios := []struct {
		confidence float64
		intentType agent.IntentType
		action     string
	}{
		{0.95, agent.IntentReadInfo, "Execute immediately (high confidence, safe operation)"},
		{0.75, agent.IntentReadInfo, "Execute with preview (medium confidence, safe operation)"},
		{0.92, agent.IntentDangerousAction, "Show confirmation dialog (high confidence, dangerous operation)"},
		{0.75, agent.IntentDangerousAction, "Show multiple choice options (medium confidence, dangerous)"},
		{0.45, agent.IntentUnknown, "Ask for clarification (low confidence)"},
	}

	config := agent.DefaultConfidenceConfig

	for _, scenario := range scenarios {
		fmt.Printf("\nScenario: %s with confidence %.2f\n", scenario.intentType, scenario.confidence)
		
		minRequired := config.GetMinConfidence(scenario.intentType)
		fmt.Printf("  Minimum required: %.2f\n", minRequired)
		
		if scenario.confidence >= minRequired {
			fmt.Printf("  ✅ Meets threshold\n")
		} else {
			fmt.Printf("  ❌ Below threshold\n")
		}
		
		if config.IsAmbiguous(scenario.confidence) {
			fmt.Printf("  ⚠️  In ambiguous range (%.2f - %.2f)\n", 
				config.AmbiguousRangeLow, config.AmbiguousRangeHigh)
		}
		
		fmt.Printf("  Action: %s\n", scenario.action)
	}

	fmt.Println()
}

// Run all examples
func init() {
	log.SetFlags(0) // Remove timestamp from logs
}

// Uncomment to run specific examples
/*
func main() {
	// Run specific example
	example1_BasicUsage()
	// example2_WithToolRegistry()
	// example3_WithSessionHistory()
	// example4_FullContext()
	// example5_ValidatingResponses()
	// exampleLLMIntegration()
	// exampleConfidenceHandling()
}
*/
