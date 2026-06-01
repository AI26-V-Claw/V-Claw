package safety_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"vclaw/internal/agent/intent"
)

// TestCase represents a single test case from the evaluation dataset.
type TestCase struct {
	ID                         string `json:"id"`
	Input                      string `json:"input"`
	ExpectedIntent             string `json:"expected_intent"`
	ExpectedNeedsClarification bool   `json:"expected_needs_clarification"`
	Description                string `json:"description"`
}

// TestIntentClassifier_EvalDataset runs the full evaluation dataset
// against the intent classifier and asserts overall accuracy > 80%.
func TestIntentClassifier_EvalDataset(t *testing.T) {
	// Load dataset
	data, err := os.ReadFile("intent_eval_dataset.json")
	if err != nil {
		t.Fatalf("Failed to read eval dataset: %v", err)
	}

	var testCases []TestCase
	if err := json.Unmarshal(data, &testCases); err != nil {
		t.Fatalf("Failed to parse eval dataset: %v", err)
	}

	if len(testCases) == 0 {
		t.Fatal("Eval dataset is empty")
	}

	classifier := intent.NewClassifier(intent.DefaultConfig)
	ctx := context.Background()

	totalCases := len(testCases)
	intentCorrect := 0
	clarifyCorrect := 0
	bothCorrect := 0

	var failures []string

	for _, tc := range testCases {
		out, err := intent.Classify(ctx, classifier, tc.Input)
		if err != nil {
			failures = append(failures, fmt.Sprintf("[%s] ERROR: %v", tc.ID, err))
			continue
		}

		intentMatch := string(out.Intent.Type) == tc.ExpectedIntent
		clarifyMatch := out.NeedsClarification == tc.ExpectedNeedsClarification

		if intentMatch {
			intentCorrect++
		}
		if clarifyMatch {
			clarifyCorrect++
		}
		if intentMatch && clarifyMatch {
			bothCorrect++
		}

		if !intentMatch || !clarifyMatch {
			detail := fmt.Sprintf("[%s] %q\n  Intent: got=%s want=%s (match=%v)\n  Clarify: got=%v want=%v (match=%v)\n  Desc: %s",
				tc.ID, tc.Input,
				out.Intent.Type, tc.ExpectedIntent, intentMatch,
				out.NeedsClarification, tc.ExpectedNeedsClarification, clarifyMatch,
				tc.Description,
			)
			failures = append(failures, detail)
		}
	}

	// ── Report ──────────────────────────────────────────────────
	intentAccuracy := float64(intentCorrect) / float64(totalCases) * 100
	clarifyAccuracy := float64(clarifyCorrect) / float64(totalCases) * 100
	overallAccuracy := float64(bothCorrect) / float64(totalCases) * 100

	t.Logf("\n══════════════════════════════════════════")
	t.Logf("  INTENT CLASSIFIER EVALUATION REPORT")
	t.Logf("══════════════════════════════════════════")
	t.Logf("  Total test cases:       %d", totalCases)
	t.Logf("  Intent accuracy:        %.1f%% (%d/%d)", intentAccuracy, intentCorrect, totalCases)
	t.Logf("  Clarification accuracy: %.1f%% (%d/%d)", clarifyAccuracy, clarifyCorrect, totalCases)
	t.Logf("  Overall accuracy:       %.1f%% (%d/%d)", overallAccuracy, bothCorrect, totalCases)
	t.Logf("══════════════════════════════════════════")

	if len(failures) > 0 {
		t.Logf("\n── Failures (%d) ──", len(failures))
		for _, f := range failures {
			t.Logf("  %s", f)
		}
	}

	// ── Assertion: G3 KPI = intent accuracy > 80% ────────────
	if intentAccuracy < 80.0 {
		t.Errorf("FAIL: Intent accuracy %.1f%% is below the 80%% target required by G3", intentAccuracy)
	}

	if overallAccuracy < 75.0 {
		t.Errorf("FAIL: Overall accuracy %.1f%% is below the 75%% minimum", overallAccuracy)
	}
}
