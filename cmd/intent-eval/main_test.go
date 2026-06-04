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

func TestEvalSystemOp(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		predicted intent.IntentResult
		want     intent.SystemOpType
	}{
		{
			name: "delete shell",
			input: "xóa file cũ",
			predicted: intent.IntentResult{SystemOpType: intent.SystemOpShell},
			want: intent.SystemOpDelete,
		},
		{
			name: "write shell",
			input: "ghi file báo cáo tổng kết",
			predicted: intent.IntentResult{SystemOpType: intent.SystemOpShell},
			want: intent.SystemOpWrite,
		},
		{
			name: "keep shell",
			input: "mở shell và kiểm tra thư mục",
			predicted: intent.IntentResult{SystemOpType: intent.SystemOpShell},
			want: intent.SystemOpShell,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalSystemOp(tc.input, tc.predicted); got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
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
