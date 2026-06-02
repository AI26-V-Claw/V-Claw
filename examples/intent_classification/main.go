package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"vclaw/internal/agent/intent"
	"vclaw/internal/memory"
	"vclaw/internal/providers"
	"vclaw/internal/providers/gemini"
	"vclaw/internal/safety/risk"
)

func main() {
	fmt.Println("V-Claw Intent Classification Example")
	fmt.Println("=====================================")
	fmt.Println()

	// Example 1: Heuristic Classifier (Fast, No API Key)
	fmt.Println("Example 1: Heuristic Classifier")
	fmt.Println("--------------------------------")
	runHeuristicExample()

	// Example 2: LLM Classifier (Production, Requires API Key)
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		fmt.Println("\nExample 2: LLM-based Classifier")
		fmt.Println("--------------------------------")
		runLLMExample(apiKey)
	} else {
		fmt.Println("\nExample 2: Skipped (Set GEMINI_API_KEY to run)")
	}

	// Example 3: Memory Isolation
	fmt.Println("\nExample 3: Memory Isolation for Dangerous Actions")
	fmt.Println("--------------------------------------------------")
	runMemoryIsolationExample()

	// Example 4: Safety Risk Assessment
	fmt.Println("\nExample 4: Safety Risk Assessment")
	fmt.Println("----------------------------------")
	runSafetyExample()
}

func runHeuristicExample() {
	ctx := context.Background()
	classifier := intent.NewClassifier(intent.DefaultConfig)

	testCases := []string{
		"Chào buổi sáng",
		"Đọc file config.json",
		"Xóa file /tmp/test.txt",
		"Xóa file config", // Missing path
		"Tìm các file log cũ và xóa chúng",
	}

	for _, input := range testCases {
		fmt.Printf("\nInput: %q\n", input)

		output, err := intent.Classify(ctx, classifier, input)
		if err != nil {
			log.Printf("Error: %v\n", err)
			continue
		}

		printClassificationOutput(output)
	}
}

func runLLMExample(apiKey string) {
	ctx := context.Background()

	// Create Gemini provider
	cfg := providers.DefaultConfig()
	cfg.APIKey = apiKey

	provider, err := gemini.NewClient(ctx, cfg)
	if err != nil {
		log.Printf("Failed to create Gemini client: %v\n", err)
		return
	}
	defer provider.Close()

	// Create LLM classifier
	classifier, err := intent.NewLLMClassifier(provider, intent.DefaultConfig)
	if err != nil {
		log.Printf("Failed to create LLM classifier: %v\n", err)
		return
	}

	testCases := []string{
		"Gửi email cho john@example.com với tiêu đề 'Meeting' và nội dung 'See you at 3pm'",
		"Gửi email cho sếp", // Missing all params
	}

	for _, input := range testCases {
		fmt.Printf("\nInput: %q\n", input)

		output, err := classifier.Classify(ctx, input)
		if err != nil {
			log.Printf("Error: %v\n", err)
			continue
		}

		printClassificationOutput(output)
	}
}

func runMemoryIsolationExample() {
	// Simulate a conversation where user mentioned a file path yesterday
	session := memory.NewSessionMemory(20)

	// Day 1: User mentions a file
	session.AddMessage(memory.RoleUser, "Tôi có file config ở /etc/app.conf")
	session.AddMessage(memory.RoleAssistant, "Tôi đã ghi nhận file config của bạn ở /etc/app.conf")

	// Day 2: User asks to delete "the config file" without specifying path
	session.AddMessage(memory.RoleUser, "Xóa file config đi")

	// Get isolated history for dangerous action
	recentHistory, isolationWarning := session.GetFilteredHistoryForDangerousAction(3)

	fmt.Println("Recent History (for context only):")
	for _, msg := range recentHistory {
		fmt.Printf("  %s\n", msg)
	}

	fmt.Println("\nIsolation Warning:")
	fmt.Println(isolationWarning)

	fmt.Println("\nExpected Behavior:")
	fmt.Println("  - System MUST NOT use /etc/app.conf from Day 1")
	fmt.Println("  - System MUST ask user to specify the path again")
	fmt.Println("  - missing_params should include 'path'")
}

func runSafetyExample() {
	classifier := risk.NewClassifier()

	testCases := []struct {
		toolName   string
		intentType intent.IntentType
	}{
		{"gmail.listEmails", intent.TypeReadInfo},
		{"sandbox.runShell", intent.TypeDangerousAction},
		{"gmail.sendEmail", intent.TypeDangerousAction},
		{"sandbox.runShell", intent.TypeDangerousAction},
		{"format_disk", intent.TypeDangerousAction},
	}

	for _, tc := range testCases {
		fmt.Printf("\nTool: %s (Intent: %s)\n", tc.toolName, tc.intentType)

		assessment, err := classifier.Assess(tc.toolName, tc.intentType)
		if err != nil {
			log.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("  Risk Level: %s\n", assessment.RiskLevel)
		fmt.Printf("  Decision: %s\n", assessment.Decision)
		fmt.Printf("  Requires Approval: %v\n", assessment.RequiresApproval)
		fmt.Printf("  Reason: %s\n", assessment.ReasonVi)
	}
}

func printClassificationOutput(output *intent.ClassificationOutput) {
	// Pretty print JSON
	data, _ := json.MarshalIndent(output, "  ", "  ")
	fmt.Printf("Output:\n  %s\n", string(data))

	// Highlight key decisions
	if output.NeedsClarification {
		fmt.Println("  ⚠️  NEEDS CLARIFICATION")
		if output.ClarificationMessage != "" {
			fmt.Printf("  Message: %s\n", output.ClarificationMessage)
		}
	}

	if output.Intent != nil {
		if output.Intent.NeedsConfirm {
			fmt.Println("  🔒 REQUIRES USER CONFIRMATION")
		}

		if len(output.Intent.MissingParams) > 0 {
			fmt.Printf("  ❌ Missing params: %v\n", output.Intent.MissingParams)
		}
	}
}
