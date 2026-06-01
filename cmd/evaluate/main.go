package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"vclaw/internal/agent"
	"vclaw/internal/agent/intent"
	"vclaw/internal/evaluation"
	"vclaw/internal/providers"
	"vclaw/internal/providers/gemini"
)

// SimpleClassifier wraps the intent classifier to match the evaluation interface
type SimpleClassifier struct {
	classifier *intent.Classifier
}

func (s *SimpleClassifier) Classify(input string) (*agent.Intent, error) {
	ctx := context.Background()
	output, err := intent.Classify(ctx, s.classifier, input)
	if err != nil {
		return nil, err
	}

	// Convert intent.Result to agent.Intent
	return convertToAgentIntent(output.Intent), nil
}

func convertToAgentIntent(result *intent.Result) *agent.Intent {
	toolCalls := make([]agent.ToolCall, len(result.ToolCalls))
	for i, tc := range result.ToolCalls {
		toolCalls[i] = agent.ToolCall{
			Name:       tc.Name,
			Category:   agent.ToolCategory(tc.Category),
			Parameters: tc.Parameters,
			Timeout:    tc.Timeout,
		}
	}

	return &agent.Intent{
		Type:           agent.IntentType(result.Type),
		Confidence:     result.Confidence,
		RequiredParams: result.RequiredParams,
		ProvidedParams: result.ProvidedParams,
		MissingParams:  result.MissingParams,
		ToolCalls:      toolCalls,
		NeedsConfirm:   result.NeedsConfirm,
		Reasoning:      result.Reasoning,
		Timestamp:      result.Timestamp,
	}
}

func main() {
	// Parse command-line flags
	datasetPath := flag.String("dataset", "internal/evaluation/test_cases.json", "Path to test dataset")
	outputPath := flag.String("output", "evaluation_results.json", "Path to save results")
	useLLM := flag.Bool("llm", false, "Use LLM-based classifier (requires GEMINI_API_KEY)")
	apiKey := flag.String("api-key", "", "Gemini API key (or set GEMINI_API_KEY env var)")
	flag.Parse()

	fmt.Println("V-Claw Intent Classification Evaluation")
	fmt.Println("========================================")
	fmt.Println()

	// Load test dataset
	fmt.Printf("Loading dataset from %s...\n", *datasetPath)
	dataset, err := loadDataset(*datasetPath)
	if err != nil {
		log.Fatalf("Failed to load dataset: %v", err)
	}
	fmt.Printf("✓ Loaded %d test cases\n\n", len(dataset.TestCases))

	// Create classifier
	var classifier evaluation.IntentClassifier
	if *useLLM {
		fmt.Println("Using LLM-based classifier (Gemini 1.5 Flash)")

		// Get API key
		key := *apiKey
		if key == "" {
			key = os.Getenv("GEMINI_API_KEY")
		}
		if key == "" {
			log.Fatal("Error: GEMINI_API_KEY not set. Use -api-key flag or set GEMINI_API_KEY environment variable.")
		}

		// Create Gemini provider
		ctx := context.Background()
		cfg := providers.DefaultConfig()
		cfg.APIKey = key

		provider, err := gemini.NewClient(ctx, cfg)
		if err != nil {
			log.Fatalf("Failed to create Gemini client: %v", err)
		}
		defer provider.Close()

		// Create LLM classifier
		llmClassifier, err := intent.NewLLMClassifier(provider, intent.DefaultConfig)
		if err != nil {
			log.Fatalf("Failed to create LLM classifier: %v", err)
		}

		// Wrap for evaluation
		classifier = &LLMClassifierWrapper{
			classifier: llmClassifier,
		}
	} else {
		fmt.Println("Using heuristic-based classifier")
		classifier = &SimpleClassifier{
			classifier: intent.NewClassifier(intent.DefaultConfig),
		}
	}

	// Create evaluator
	evaluator := evaluation.NewEvaluator(classifier, dataset)

	// Run evaluation
	fmt.Println("Running evaluation...")
	fmt.Println()
	startTime := time.Now()

	if err := evaluator.Run(); err != nil {
		log.Fatalf("Evaluation failed: %v", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("\nEvaluation completed in %v\n", duration)

	// Print report
	evaluator.PrintReport()

	// Save results
	fmt.Printf("Saving results to %s...\n", *outputPath)
	if err := evaluator.SaveResults(*outputPath); err != nil {
		log.Fatalf("Failed to save results: %v", err)
	}
	fmt.Println("✓ Results saved")
}

// LLMClassifierWrapper wraps the LLM classifier for evaluation
type LLMClassifierWrapper struct {
	classifier *intent.LLMClassifier
}

func (w *LLMClassifierWrapper) Classify(input string) (*agent.Intent, error) {
	ctx := context.Background()
	output, err := w.classifier.Classify(ctx, input)
	if err != nil {
		return nil, err
	}

	return convertToAgentIntent(output.Intent), nil
}

// loadDataset loads the test dataset from a JSON file
func loadDataset(path string) (*evaluation.TestDataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var dataset evaluation.TestDataset
	if err := json.Unmarshal(data, &dataset); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &dataset, nil
}
