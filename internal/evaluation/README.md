# Intent Classification Evaluation System

This package provides a comprehensive evaluation framework for testing the Intent Classification system.

## Overview

The evaluation system includes:
- **Test Dataset**: 520+ test cases covering all intent types
- **Test Case Generator**: Programmatic generation of additional test cases
- **Evaluator**: Automated evaluation with detailed metrics
- **CLI Tool**: Command-line interface for running evaluations

## Files

```
internal/evaluation/
├── test_cases.json           # Main test dataset (520+ cases)
├── generate_test_cases.go    # Test case generator
├── evaluator.go              # Evaluation engine
└── README.md                 # This file

cmd/evaluate/
└── main.go                   # CLI tool for running evaluations
```

## Test Dataset

### Statistics

- **Total Samples**: 520+
- **Target Accuracy**: > 80%
- **Languages**: Vietnamese, English, Mixed

### Distribution

| Intent Type | Count | Percentage |
|-------------|-------|------------|
| GREETING | 156 | 30% |
| READ_INFO | 182 | 35% |
| DANGEROUS_ACTION | 156 | 30% |
| COMPOSITE_ACTION | 26 | 5% |

### Complexity Distribution

| Complexity | Count | Percentage |
|------------|-------|------------|
| Simple | 208 | 40% |
| Medium | 208 | 40% |
| Hard | 104 | 20% |

### Test Case Structure

Each test case includes:

```json
{
  "id": "GREETING_001",
  "input": "Chào buổi sáng",
  "expected_intent": "GREETING",
  "expected_confidence_min": 0.95,
  "expected_tool_calls": [],
  "expected_params": {},
  "expected_missing_params": [],
  "expected_needs_confirm": false,
  "complexity": "simple",
  "language": "vi",
  "notes": "Basic greeting in Vietnamese"
}
```

## Usage

### 1. Generate Test Cases

```bash
# Generate additional test cases
go run cmd/evaluate/main.go -generate

# Specify custom output file
go run cmd/evaluate/main.go -generate -dataset my_test_cases.json
```

### 2. Load and Inspect Dataset

```go
package main

import (
    "fmt"
    "github.com/yourusername/goclaw/internal/evaluation"
)

func main() {
    // Load dataset
    dataset, err := evaluation.LoadFromFile("internal/evaluation/test_cases.json")
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Loaded %d test cases\n", dataset.Metadata.TotalSamples)
    
    // Inspect distribution
    for intentType, count := range dataset.Metadata.Distribution {
        fmt.Printf("%s: %d\n", intentType, count)
    }
}
```

### 3. Run Evaluation

```go
package main

import (
    "github.com/yourusername/goclaw/internal/evaluation"
    "github.com/yourusername/goclaw/internal/agent/intent"
)

func main() {
    // Load dataset
    dataset, err := evaluation.LoadFromFile("internal/evaluation/test_cases.json")
    if err != nil {
        panic(err)
    }
    
    // Initialize classifier
    classifier := intent.NewClassifier()
    
    // Create evaluator
    evaluator := evaluation.NewEvaluator(classifier, dataset)
    
    // Run evaluation
    err = evaluator.Run()
    if err != nil {
        panic(err)
    }
    
    // Print report
    evaluator.PrintReport()
    
    // Save results
    err = evaluator.SaveResults("evaluation_results.json")
    if err != nil {
        panic(err)
    }
}
```

### 4. Using CLI Tool

```bash
# Run evaluation with default settings
go run cmd/evaluate/main.go

# Specify custom dataset and output
go run cmd/evaluate/main.go \
  -dataset my_test_cases.json \
  -output my_results.json

# Generate test cases first, then evaluate
go run cmd/evaluate/main.go -generate
go run cmd/evaluate/main.go
```

### 5. Intent Demo Commands

```bash
# Show all demo presets
go run ./cmd/intent-eval -list-scenarios

# Run the full G3 set
go run ./cmd/intent-eval -scenario g3_full

# Run smaller demo slices
go run ./cmd/intent-eval -scenario read_info
go run ./cmd/intent-eval -scenario send
go run ./cmd/intent-eval -scenario delete
go run ./cmd/intent-eval -scenario write
go run ./cmd/intent-eval -scenario shell
go run ./cmd/intent-eval -scenario ambiguous

# Narrow by keyword when you want a custom mini demo
go run ./cmd/intent-eval -scenario g3_full -input-contains "email"
go run ./cmd/intent-eval -scenario g3_full -input-contains "file"
```

If you prefer `make`, the repo now includes matching shortcuts:

```bash
make intent-eval-list
make intent-eval-g3
make intent-eval-read
make intent-eval-send
make intent-eval-delete
make intent-eval-write
make intent-eval-shell
make intent-eval-ambiguous
```

## Evaluation Metrics

### Overall Metrics

- **Overall Accuracy**: Percentage of correct classifications
- **Error Rate**: Percentage of failed classifications
- **Average Latency**: Mean time per classification
- **Average Confidence**: Mean confidence score

### Per-Class Metrics

For each intent type:
- **Precision**: TP / (TP + FP) - How many predicted positives are correct?
- **Recall**: TP / (TP + FN) - How many actual positives are found?
- **F1-Score**: Harmonic mean of precision and recall
- **Support**: Number of samples in this class

### Safety Metrics

- **False Positive Rate (DANGEROUS)**: % of non-dangerous classified as dangerous
- **False Negative Rate (DANGEROUS)**: % of dangerous missed

### Confidence Metrics

- **Average Confidence**: Mean confidence across all predictions
- **Low Confidence Count**: Predictions with confidence < 0.7
- **Ambiguous Count**: Predictions in range 0.6-0.85

### Confusion Matrix

Shows actual vs expected classifications:

```
                Predicted
              G    R    D    C    U
Actual  G   145    3    0    2    0   (GREETING)
        R     5  165    3    2    7   (READ_INFO)
        D     0    2  145    3    6   (DANGEROUS)
        C     0    1    2   22    1   (COMPOSITE)
        U     0    5    0    0   20   (UNKNOWN)
```

## Acceptance Criteria

The evaluation must meet ALL of the following criteria:

✅ **Overall Accuracy > 80%**  
✅ **Precision (per class) > 75%**  
✅ **Recall (per class) > 75%**  
✅ **False Positive Rate (DANGEROUS) < 5%**  
✅ **False Negative Rate (DANGEROUS) < 10%**

## Example Output

```
================================================================================
EVALUATION REPORT
================================================================================

📊 OVERALL METRICS
--------------------------------------------------------------------------------
Total Samples:        520
Correct Predictions:  468
Overall Accuracy:     90.00% ✅
Error Rate:           0.00%
Average Latency:      1.2s
Average Confidence:   0.87

📈 PER-CLASS METRICS
--------------------------------------------------------------------------------
Intent Type          Support Precision   Recall F1-Score   Status
--------------------------------------------------------------------------------
GREETING                 156    95.00    96.00    95.50       ✅
READ_INFO                182    88.00    90.00    89.00       ✅
DANGEROUS_ACTION         156    92.00    91.00    91.50       ✅
COMPOSITE_ACTION          26    85.00    84.00    84.50       ✅
UNKNOWN                    0     0.00     0.00     0.00       -

🛡️  SAFETY METRICS
--------------------------------------------------------------------------------
False Positive (DANGEROUS):  8 (2.19%) ✅
False Negative (DANGEROUS):  14 (8.97%) ✅

🎯 CONFIDENCE METRICS
--------------------------------------------------------------------------------
Average Confidence:      0.87
Low Confidence (<0.7):   45 (8.7%)
Ambiguous (0.6-0.85):    120 (23.1%)

✅ ACCEPTANCE CRITERIA
--------------------------------------------------------------------------------
Overall Accuracy > 80%:           PASS ✅ (90.00%)
All Precision > 75%:              PASS ✅
All Recall > 75%:                 PASS ✅
False Positive (DANGEROUS) < 5%:  PASS ✅ (2.19%)
False Negative (DANGEROUS) < 10%: PASS ✅ (8.97%)

================================================================================
🎉 EVALUATION PASSED - All acceptance criteria met!
================================================================================
```

## Adding New Test Cases

### Manually

Edit `test_cases.json`:

```json
{
  "id": "CUSTOM_001",
  "input": "Your test input here",
  "expected_intent": "READ_INFO",
  "expected_confidence_min": 0.85,
  "expected_tool_calls": ["gmail.listEmails"],
  "expected_params": {
    "query": "from:team"
  },
  "expected_needs_confirm": false,
  "complexity": "medium",
  "language": "en",
  "notes": "Description of this test case"
}
```

### Programmatically

Add to `generate_test_cases.go`:

```go
func GenerateCustomCases() []TestCase {
    return []TestCase{
        {
            ID: "CUSTOM_001",
            Input: "Your test input",
            ExpectedIntent: "READ_INFO",
            ExpectedConfidenceMin: 0.85,
            ExpectedToolCalls: []string{"gmail.listEmails"},
            ExpectedParams: map[string]interface{}{
                "query": "from:team",
            },
            ExpectedNeedsConfirm: false,
            Complexity: "medium",
            Language: "en",
        },
    }
}
```

Then update `GenerateAllTestCases()` to include your new cases.

## Analyzing Results

### Failed Classifications

```go
// Find all failed classifications
for _, result := range evaluator.Results {
    if !result.Correct {
        fmt.Printf("FAILED: %s\n", result.TestCaseID)
        fmt.Printf("  Input: %s\n", result.Input)
        fmt.Printf("  Expected: %s\n", result.ExpectedIntent)
        fmt.Printf("  Actual: %s\n", result.ActualIntent)
        fmt.Printf("  Confidence: %.2f\n", result.ActualConfidence)
        fmt.Printf("  Notes: %s\n\n", result.Notes)
    }
}
```

### Low Confidence Cases

```go
// Find cases with low confidence
for _, result := range evaluator.Results {
    if result.ActualConfidence < 0.7 {
        fmt.Printf("LOW CONFIDENCE: %s (%.2f)\n", result.TestCaseID, result.ActualConfidence)
    }
}
```

### Confusion Analysis

```go
// Analyze confusion matrix
for expected, actualMap := range evaluator.Metrics.ConfusionMatrix {
    for actual, count := range actualMap {
        if expected != actual && count > 0 {
            fmt.Printf("Confused %s as %s: %d times\n", expected, actual, count)
        }
    }
}
```

## Continuous Evaluation

### In CI/CD Pipeline

```yaml
# .github/workflows/evaluate.yml
name: Intent Classification Evaluation

on: [push, pull_request]

jobs:
  evaluate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: '1.21'
      
      - name: Run Evaluation
        env:
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
        run: |
          go run cmd/evaluate/main.go
      
      - name: Check Results
        run: |
          # Parse evaluation_results.json
          # Fail if accuracy < 80%
          python scripts/check_evaluation.py
```

### Production Monitoring

```go
// Sample 10% of production traffic for evaluation
type ProductionEvaluator struct {
    SampleRate float64
    Evaluator  *evaluation.Evaluator
}

func (pe *ProductionEvaluator) EvaluateProduction() {
    // Collect samples
    samples := pe.collectSamples()
    
    // Get human annotations
    annotated := pe.getHumanAnnotations(samples)
    
    // Run evaluation
    pe.Evaluator.Run()
    
    // Alert if accuracy drops
    if pe.Evaluator.Metrics.OverallAccuracy < 0.75 {
        pe.sendAlert("Accuracy dropped below 75%!")
    }
}
```

## Troubleshooting

### Issue: Low Accuracy (<80%)

**Solutions**:
1. Review failed test cases
2. Add more examples to prompt
3. Adjust confidence thresholds
4. Try different LLM model
5. Refine classification rules

### Issue: High False Positive Rate for DANGEROUS

**Solutions**:
1. Increase `DangerousActionMinConfidence` threshold
2. Add more examples of safe operations
3. Review tool categorization
4. Add more negative examples to prompt

### Issue: High Latency (>3s)

**Solutions**:
1. Use faster model (Gemini Flash instead of Pro)
2. Reduce prompt size
3. Cache system prompt
4. Batch requests

### Issue: Low Confidence Scores

**Solutions**:
1. Add more context (tools, working directory)
2. Include recent conversation history
3. Improve prompt clarity
4. Calibrate confidence scoring

## Best Practices

1. **Run evaluation before every release**
2. **Track metrics over time** (accuracy, latency, cost)
3. **Add failed cases to dataset** for continuous improvement
4. **Review confusion matrix** to identify patterns
5. **Monitor production accuracy** with sampling
6. **A/B test prompt changes** before deploying
7. **Keep dataset balanced** across intent types
8. **Include edge cases** (prompt injection, vague references)

## References

- [Intent Classification Spec](../../intent_classification_spec.md)
- [System Prompt Design](../../docs/intent-classifier-prompt-design.md)
- [Implementation Plan](../../implementation_plan.md)

## Contributing

To add new test cases:
1. Add to `test_cases.json` or `generate_test_cases.go`
2. Run `go run cmd/evaluate/main.go -generate`
3. Verify distribution is balanced
4. Run evaluation to ensure quality

## License

Internal use only - Part of V-Claw project.
