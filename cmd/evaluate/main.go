package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/yourusername/goclaw/internal/evaluation"
)

func main() {
	// Command line flags
	datasetFile := flag.String("dataset", "internal/evaluation/test_cases.json", "Path to test dataset JSON file")
	outputFile := flag.String("output", "evaluation_results.json", "Path to output results JSON file")
	generate := flag.Bool("generate", false, "Generate additional test cases")
	verbose := flag.Bool("verbose", false, "Verbose output")
	
	flag.Parse()

	fmt.Println("="*80)
	fmt.Println("Intent Classification Evaluation Tool")
	fmt.Println("="*80)

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

	// TODO: Initialize intent classifier
	// For now, we'll show a message that the classifier needs to be implemented
	fmt.Println("\n⚠️  Intent Classifier not yet implemented")
	fmt.Println("To run evaluation, you need to:")
	fmt.Println("  1. Implement IntentClassifier interface")
	fmt.Println("  2. Initialize it with LLM API (Gemini/GPT)")
	fmt.Println("  3. Pass it to the evaluator")
	fmt.Println("\nExample code:")
	fmt.Println(`
  // Initialize classifier
  classifier := NewLLMIntentClassifier(apiKey, model)
  
  // Create evaluator
  evaluator := evaluation.NewEvaluator(classifier, dataset)
  
  // Run evaluation
  err := evaluator.Run()
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
`)

	fmt.Println("\n" + "="*80)
	fmt.Println("Next Steps:")
	fmt.Println("="*80)
	fmt.Println("1. Implement internal/pipeline/stages/intent_classifier.go")
	fmt.Println("2. Add LLM API integration (Gemini 1.5 Flash recommended)")
	fmt.Println("3. Run: go run cmd/evaluate/main.go")
	fmt.Println("4. Review evaluation_results.json")
	fmt.Println("5. Iterate on prompt if accuracy < 80%")
	fmt.Println()

	os.Exit(0)
}
