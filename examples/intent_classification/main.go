package main

import (
	"context"
	"fmt"
	"os"

	"vclaw/internal/agent"
	"vclaw/internal/pipeline/stages"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	userInput := os.Args[1]
	
	// Create intent classifier
	classifier := agent.NewIntentClassifier(agent.DefaultConfidenceConfig)
	
	// Classify user input
	fmt.Printf("📝 User Input: %q\n\n", userInput)
	
	result, err := classifier.Classify(context.Background(), userInput)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}
	
	if result.Error != nil {
		fmt.Printf("❌ Classification Error: %v\n", result.Error)
		return
	}
	
	intent := result.Intent
	
	// Display classification result
	fmt.Printf("🎯 Intent Type: %s\n", intent.Type)
	fmt.Printf("📊 Confidence: %.2f%%\n", intent.Confidence*100)
	fmt.Printf("💭 Reasoning: %s\n\n", intent.Reasoning)
	
	// Check if clarification is needed
	if result.NeedsClarification {
		fmt.Println("❓ Clarification Needed")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		
		if result.ClarificationOptions != nil {
			fmt.Println(result.ClarificationOptions.Question)
			fmt.Println()
			
			if len(result.ClarificationOptions.Options) > 0 {
				for _, opt := range result.ClarificationOptions.Options {
					fmt.Printf("%s) %s (confidence: %.2f%%)\n", 
						opt.ID, opt.Label, opt.Confidence*100)
				}
			}
		}
		return
	}
	
	// Display tool calls
	if len(intent.ToolCalls) > 0 {
		fmt.Println("🔧 Tool Calls:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		
		validator := stages.NewParamValidator()
		
		for i, toolCall := range intent.ToolCalls {
			fmt.Printf("\n%d. %s (%s)\n", i+1, toolCall.Name, toolCall.Category)
			fmt.Printf("   Timeout: %ds\n", toolCall.Timeout)
			
			// Validate parameters
			validation, err := validator.Validate(toolCall)
			if err != nil {
				fmt.Printf("   ❌ Validation Error: %v\n", err)
				continue
			}
			
			// Display parameters
			if len(toolCall.Parameters) > 0 {
				fmt.Println("   Parameters:")
				for key, value := range toolCall.Parameters {
					fmt.Printf("     - %s: %v\n", key, value)
				}
			}
			
			// Display validation result
			if !validation.IsValid {
				fmt.Printf("   ⚠️  Missing Parameters: %v\n", validation.Missing)
				
				clarification := validator.GenerateClarificationRequest(validation, toolCall.Name)
				fmt.Printf("\n   💬 %s\n", clarification)
			} else {
				fmt.Println("   ✅ All parameters valid")
			}
		}
	} else {
		fmt.Println("ℹ️  No tool calls required (direct response)")
	}
	
	// Display confirmation requirement
	fmt.Println()
	if intent.NeedsConfirm {
		fmt.Println("⚠️  CONFIRMATION REQUIRED")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("This action is dangerous and requires explicit user confirmation.")
		fmt.Println("User must approve before execution.")
	} else {
		fmt.Println("✅ Safe to execute without confirmation")
	}
	
	// Display missing parameters summary
	if len(intent.MissingParams) > 0 {
		fmt.Println()
		fmt.Println("📋 Missing Parameters Summary:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		for _, param := range intent.MissingParams {
			fmt.Printf("  - %s\n", param)
		}
		fmt.Println("\n⚠️  Cannot execute until all parameters are provided.")
	}
}

func printUsage() {
	fmt.Println("Intent Classification Demo")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go run examples/intent_classification/main.go \"<user input>\"")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  go run examples/intent_classification/main.go \"Chào buổi sáng\"")
	fmt.Println("  go run examples/intent_classification/main.go \"Đọc file config.json\"")
	fmt.Println("  go run examples/intent_classification/main.go \"Xóa file config.json\"")
	fmt.Println("  go run examples/intent_classification/main.go \"Tìm và xóa các file log cũ\"")
	fmt.Println("  go run examples/intent_classification/main.go \"Xử lý file config\"")
}
