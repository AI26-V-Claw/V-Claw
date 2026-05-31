package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"vclaw/internal/agent"
	"vclaw/internal/evaluation"
	"vclaw/internal/llm"
	"vclaw/internal/pipeline/stages"
)

// intentClassifierAdapter wraps stages.IntentClassifier to implement evaluation.IntentClassifier
type intentClassifierAdapter struct {
	classifier *stages.IntentClassifier
}

func (a *intentClassifierAdapter) Classify(input string) (*agent.Intent, error) {
	result, err := a.classifier.Classify(context.Background(), input)
	if err != nil {
		return nil, err
	}
	return result.Intent, nil
}

func main() {
	_ = godotenv.Load()
	// Command line flags
	datasetFile := flag.String("dataset", "internal/evaluation/test_cases.json", "Path to test dataset JSON file")
	outputFile := flag.String("output", "evaluation_results.json", "Path to output results JSON file")
	generate := flag.Bool("generate", false, "Generate additional test cases")
	_ = flag.Bool("verbose", false, "Verbose output")
	
	flag.Parse()

	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("Intent Classification Evaluation Tool")
	fmt.Println(strings.Repeat("=", 80))

	// Generate additional test cases if requested
	if *generate {
		fmt.Println("\n📝 Generating additional test cases...")
		dataset := evaluation.GenerateAllTestCases()
		
		err := evaluation.SaveToFile(dataset, *datasetFile)
		if err != nil {
			log.Fatalf("Failed to save generated dataset: %v", err)
		}
		
		fmt.Printf("✅ Generated %d test cases and saved to %s\n", dataset.Metadata.TotalSamples, *datasetFile)
		fmt.Println("\nDistribution:")
		for intentType, count := range dataset.Metadata.Distribution {
			fmt.Printf("  - %s: %d (%.1f%%)\n", intentType, count, float64(count)/float64(dataset.Metadata.TotalSamples)*100)
		}
		fmt.Println()
	}

	// Load dataset
	fmt.Printf("\n📂 Loading dataset from %s...\n", *datasetFile)
	dataset, err := evaluation.LoadFromFile(*datasetFile)
	if err != nil {
		log.Fatalf("Failed to load dataset: %v", err)
	}
	
	fmt.Printf("✅ Loaded %d test cases\n", dataset.Metadata.TotalSamples)
	fmt.Println("\nDataset Distribution:")
	for intentType, count := range dataset.Metadata.Distribution {
		fmt.Printf("  - %s: %d (%.1f%%)\n", intentType, count, float64(count)/float64(dataset.Metadata.TotalSamples)*100)
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("\n⚠️  OPENAI_API_KEY environment variable is required")
		fmt.Println("To run evaluation, you need to:")
		fmt.Println("  export OPENAI_API_KEY=your_api_key_here")
		fmt.Println("  go run cmd/evaluate/main.go")
		os.Exit(1)
	}

	// Initialize LLM Client
	fmt.Println("\n🤖 Initializing OpenAI LLM Client...")
	llmClient, err := llm.NewOpenAIClient(apiKey, "gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to initialize LLM client: %v", err)
	}
	defer llmClient.Close()

	// Initialize classifier
	fmt.Println("🧠 Initializing Intent Classifier...")
	classifierConfig := stages.IntentClassifierConfig{
		LLMClient:        llmClient,
		ConfidenceConfig: agent.DefaultConfidenceConfig,
		MaxRetries:       3,
	}
	stagesClassifier := stages.NewIntentClassifier(classifierConfig)
	
	// Create adapter for evaluator
	classifier := &intentClassifierAdapter{classifier: stagesClassifier}
	
	// Create evaluator
	evaluator := evaluation.NewEvaluator(classifier, dataset)
	
	// Run evaluation
	err = evaluator.Run()
	if err != nil {
		log.Fatal(err)
	}
	
	// Print report
	evaluator.PrintReport()
	
	// Save results
	err = evaluator.SaveResults(*outputFile)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n✅ Evaluation results saved to %s\n", *outputFile)

	os.Exit(0)
}
