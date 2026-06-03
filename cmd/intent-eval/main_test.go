package main

import (
	"encoding/json"
	"os"
	"testing"

	"vclaw/internal/agent/intent"
)

func TestIntentCasesDatasetIsValid(t *testing.T) {
	cases, err := loadIntentCases()
	if err != nil {
		t.Fatalf("loadIntentCases() returned error: %v", err)
	}

	if len(cases) < 50 {
		t.Fatalf("expected at least 50 cases, got %d", len(cases))
	}

	bytes, err := os.ReadFile(mustIntentCasesPath(t))
	if err != nil {
		t.Fatalf("failed to read dataset: %v", err)
	}
	if !json.Valid(bytes) {
		t.Fatal("intent cases file is not valid JSON")
	}

	for index, testCase := range cases {
		if !isValidIntent(testCase.Intent) {
			t.Fatalf("case %d has invalid intent: %q", index+1, testCase.Intent)
		}
		if !isValidSystemOpType(testCase.SystemOpType) {
			t.Fatalf("case %d has invalid system_op_type: %q", index+1, testCase.SystemOpType)
		}
	}
}

func isValidIntent(value string) bool {
	switch intent.Intent(value) {
	case intent.IntentGreeting, intent.IntentReadInfo, intent.IntentSystemOp, intent.IntentAmbiguous:
		return true
	default:
		return false
	}
}

func isValidSystemOpType(value string) bool {
	switch intent.SystemOpType(value) {
	case intent.SystemOpNone, intent.SystemOpSend, intent.SystemOpDelete, intent.SystemOpWrite, intent.SystemOpShell:
		return true
	default:
		return false
	}
}

func mustIntentCasesPath(t *testing.T) string {
	t.Helper()

	path, err := intentCasesPath()
	if err != nil {
		t.Fatalf("intentCasesPath() returned error: %v", err)
	}
	return path
}
