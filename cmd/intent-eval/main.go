package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"vclaw/internal/agent/intent"
)

type intentCase struct {
	Input        string `json:"input"`
	Intent       string `json:"intent"`
	SystemOpType string `json:"system_op_type"`
}

func main() {
	cases, err := loadIntentCases()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	classifier := intent.NewClassifier()
	total := len(cases)
	intentCorrect := 0
	systemOpCorrect := 0
	exactMatchCorrect := 0
	type failure struct {
		index    int
		input    string
		expected intentCase
		got      intent.IntentResult
	}
	failures := make([]failure, 0)

	for index, testCase := range cases {
		predicted := classifier.Classify(testCase.Input)
		intentMatch := string(predicted.Intent) == testCase.Intent
		systemMatch := string(predicted.SystemOpType) == testCase.SystemOpType

		if intentMatch {
			intentCorrect++
		}
		if systemMatch {
			systemOpCorrect++
		}
		if intentMatch && systemMatch {
			exactMatchCorrect++
			continue
		}

		failures = append(failures, failure{
			index:    index + 1,
			input:    testCase.Input,
			expected: testCase,
			got:      predicted,
		})
	}

	fmt.Printf("Total cases: %d\n", total)
	fmt.Printf("Intent accuracy: %.2f%%\n", percent(intentCorrect, total))
	fmt.Printf("System op accuracy: %.2f%%\n", percent(systemOpCorrect, total))
	fmt.Printf("Exact-match accuracy: %.2f%%\n", percent(exactMatchCorrect, total))
	fmt.Println()
	fmt.Printf("Failed cases: %d\n", len(failures))
	for _, failure := range failures {
		fmt.Printf("- #%d input=%q expected=(%s,%s) got=(%s,%s)\n",
			failure.index,
			failure.input,
			failure.expected.Intent,
			failure.expected.SystemOpType,
			failure.got.Intent,
			failure.got.SystemOpType,
		)
	}
}

func loadIntentCases() ([]intentCase, error) {
	path, err := intentCasesPath()
	if err != nil {
		return nil, err
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read intent cases: %w", err)
	}

	var cases []intentCase
	if err := json.Unmarshal(bytes, &cases); err != nil {
		return nil, fmt.Errorf("parse intent cases: %w", err)
	}
	return cases, nil
}

func intentCasesPath() (string, error) {
	candidates := []string{
		filepath.Join("tests", "intent_cases.json"),
		filepath.Join("..", "..", "tests", "intent_cases.json"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("intent cases file not found")
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
