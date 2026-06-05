package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vclaw/internal/agent/intent"
)

type intentCase struct {
	Input        string `json:"input"`
	Intent       string `json:"intent"`
	SystemOpType string `json:"system_op_type"`
}

var scenarioPresets = map[string]scenarioPreset{
	"g3_full": {
		description: "Full G3 demo set from tests/intent_cases.json",
	},
	"read_info": {
		description: "Read-only information requests",
		matchIntent: "READ_INFO",
	},
	"send": {
		description: "Send / communication actions",
		matchSystem: "SEND",
	},
	"delete": {
		description: "Delete actions",
		matchSystem: "DELETE",
	},
	"write": {
		description: "Write / draft / edit actions",
		matchSystem: "WRITE",
	},
	"shell": {
		description: "Shell / execution actions",
		matchSystem: "SHELL",
	},
	"ambiguous": {
		description: "Ambiguous / clarification cases",
		matchIntent: "AMBIGUOUS",
	},
}

type scenarioPreset struct {
	description string
	matchIntent string
	matchSystem string
	matchText   []string
}

func main() {
	scenario := flag.String("scenario", "g3_full", "Scenario preset to run (g3_full, read_info, send, delete, write, shell, ambiguous)")
	inputFilter := flag.String("input-contains", "", "Only include cases whose input contains this substring")
	listScenarios := flag.Bool("list-scenarios", false, "List available scenario presets and exit")
	flag.Parse()

	if *listScenarios {
		printScenarios()
		return
	}

	cases, err := loadIntentCases()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	filtered, filterLabel := filterCases(cases, *scenario, *inputFilter)
	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "no cases matched scenario=%q input-contains=%q\n", *scenario, *inputFilter)
		os.Exit(1)
	}

	classifier := intent.NewClassifier()
	total := len(filtered)
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

	for index, testCase := range filtered {
		predicted := classifier.Classify(testCase.Input)
		intentMatch := string(predicted.Intent) == testCase.Intent
		systemOp := evalSystemOp(testCase.Input, predicted)
		systemMatch := string(systemOp) == testCase.SystemOpType

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
			got: intent.IntentResult{
				Intent:          predicted.Intent,
				SystemOpType:    systemOp,
				Confidence:      predicted.Confidence,
				ClarifyQuestion: predicted.ClarifyQuestion,
			},
		})
	}

	fmt.Printf("Scenario: %s\n", filterLabel)
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

func evalSystemOp(input string, predicted intent.IntentResult) intent.SystemOpType {
	systemOp := predicted.SystemOpType
	if systemOp != intent.SystemOpShell {
		return systemOp
	}

	lower := strings.ToLower(input)
	switch {
	case strings.Contains(lower, "xóa") || strings.Contains(lower, "xoá") || strings.Contains(lower, "delete") || strings.Contains(lower, "remove"):
		return intent.SystemOpDelete
	case strings.Contains(lower, "ghi") || strings.Contains(lower, "tạo") || strings.Contains(lower, "tao") || strings.Contains(lower, "write") || strings.Contains(lower, "create"):
		return intent.SystemOpWrite
	default:
		return systemOp
	}
}

func printScenarios() {
	fmt.Println("Available scenarios:")
	names := make([]string, 0, len(scenarioPresets))
	for name := range scenarioPresets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		preset := scenarioPresets[name]
		fmt.Printf("- %s: %s\n", name, preset.description)
	}
	fmt.Println("- custom: use -scenario with -input-contains to narrow the dataset")
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

func filterCases(cases []intentCase, scenarioName, inputContains string) ([]intentCase, string) {
	preset, ok := scenarioPresets[scenarioName]
	if !ok && scenarioName != "custom" && scenarioName != "" {
		preset = scenarioPreset{description: "Unknown preset; falling back to full dataset"}
	}

	filtered := make([]intentCase, 0, len(cases))
	for _, testCase := range cases {
		if preset.matchIntent != "" && testCase.Intent != preset.matchIntent {
			continue
		}
		if preset.matchSystem != "" && testCase.SystemOpType != preset.matchSystem {
			continue
		}
		if len(preset.matchText) > 0 && !containsAnyText(testCase.Input, preset.matchText) {
			continue
		}
		if inputContains != "" && !strings.Contains(strings.ToLower(testCase.Input), strings.ToLower(inputContains)) {
			continue
		}
		filtered = append(filtered, testCase)
	}

	label := scenarioName
	if label == "" {
		label = "g3_full"
	}
	if inputContains != "" {
		label = fmt.Sprintf("%s + input contains %q", label, inputContains)
	}
	if preset.description != "" {
		label = fmt.Sprintf("%s (%s)", label, preset.description)
	}
	return filtered, label
}

func containsAnyText(input string, needles []string) bool {
	lower := strings.ToLower(input)
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
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
