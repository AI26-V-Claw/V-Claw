package tools

import (
	"strings"
	"testing"
)

func TestRedactLeavesLowRiskUnchanged(t *testing.T) {
	result := ToolResult{
		ToolCallID:     "call_001",
		ToolName:       "calculator",
		Success:        true,
		ContentForLLM:  "add(1, 2) = 3",
		ContentForUser: "add(1, 2) = 3",
	}
	out := RedactResult(result, RiskLevelSafeCompute)

	if out.ContentForLLM != result.ContentForLLM {
		t.Errorf("low-risk result should not be modified; got %q", out.ContentForLLM)
	}
	if out.ContentForUser != result.ContentForUser {
		t.Errorf("ContentForUser should never be modified; got %q", out.ContentForUser)
	}
	if out.Redacted {
		t.Error("low-risk result should not be flagged Redacted")
	}
}

func TestRedactSensitiveReadTruncatesLLMContent(t *testing.T) {
	body := "File: secret.key (10 lines)\nBEGIN PRIVATE KEY\n-----\nMIIEvgIBADANBg..."
	result := ToolResult{
		ToolCallID:     "call_002",
		ToolName:       "filesystem.readFile",
		Success:        true,
		ContentForLLM:  body,
		ContentForUser: body,
	}
	out := RedactResult(result, RiskLevelSensitiveRead)

	if out.ContentForUser != body {
		t.Error("ContentForUser must never be modified by redaction")
	}
	if strings.Contains(out.ContentForLLM, "BEGIN PRIVATE KEY") {
		t.Errorf("LLM content should not contain sensitive body after redaction; got: %q", out.ContentForLLM)
	}
	if !strings.Contains(out.ContentForLLM, "[sensitive content redacted from LLM context]") {
		t.Errorf("expected redaction notice in ContentForLLM; got: %q", out.ContentForLLM)
	}
	if !out.Redacted {
		t.Error("expected Redacted=true after sensitive redaction")
	}
}

func TestRedactMasksBearerToken(t *testing.T) {
	content := "Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig"
	result := ToolResult{
		ToolCallID:    "call_003",
		ToolName:      "web.fetch",
		Success:       true,
		ContentForLLM: content,
	}
	out := RedactResult(result, RiskLevelSafeRead)

	if strings.Contains(out.ContentForLLM, "eyJhbGci") {
		t.Errorf("Bearer token should be masked; got: %q", out.ContentForLLM)
	}
	if !strings.Contains(out.ContentForLLM, redactedPlaceholder) {
		t.Errorf("expected %q placeholder in output; got: %q", redactedPlaceholder, out.ContentForLLM)
	}
}

func TestRedactMasksPasswordField(t *testing.T) {
	content := `{"username":"alice","password":"super_secret_pass_123","email":"alice@example.com"}`
	result := ToolResult{
		ToolCallID:    "call_004",
		ToolName:      "web.fetch",
		Success:       true,
		ContentForLLM: content,
	}
	out := RedactResult(result, RiskLevelSafeRead)

	if strings.Contains(out.ContentForLLM, "super_secret_pass_123") {
		t.Errorf("password value should be masked; got: %q", out.ContentForLLM)
	}
	if !strings.Contains(out.ContentForLLM, "alice@example.com") {
		t.Error("non-sensitive fields should remain intact")
	}
}

func TestRedactMasksAPIKeyField(t *testing.T) {
	content := `{"api_key": "sk-abc123XYZ987defGHI456jklMNO789pqr"}`
	result := ToolResult{
		ToolCallID:    "call_005",
		ToolName:      "web.fetch",
		Success:       true,
		ContentForLLM: content,
	}
	out := RedactResult(result, RiskLevelSafeRead)

	if strings.Contains(out.ContentForLLM, "sk-abc123XYZ987") {
		t.Errorf("api_key value should be masked; got: %q", out.ContentForLLM)
	}
}

func TestRedactIsIdempotent(t *testing.T) {
	content := `{"token": "eyJhbGciOiJSUzI1NiJ9.abc.def"}`
	result := ToolResult{
		ToolCallID:    "call_006",
		ToolName:      "web.fetch",
		Success:       true,
		ContentForLLM: content,
	}
	once := RedactResult(result, RiskLevelSafeRead)
	twice := RedactResult(once, RiskLevelSafeRead)

	if once.ContentForLLM != twice.ContentForLLM {
		t.Errorf("redaction should be idempotent;\nonce:  %q\ntwice: %q", once.ContentForLLM, twice.ContentForLLM)
	}
}

func TestRedactDoesNotModifyOriginalResult(t *testing.T) {
	original := "my original content"
	result := ToolResult{
		ToolCallID:    "call_007",
		ToolName:      "filesystem.readFile",
		Success:       true,
		ContentForLLM: original,
		Metadata:      map[string]any{"lines": 10},
	}
	_ = RedactResult(result, RiskLevelSensitiveRead)

	if result.ContentForLLM != original {
		t.Error("RedactResult must not mutate the original result's ContentForLLM")
	}
	if result.Redacted {
		t.Error("RedactResult must not mutate the original result's Redacted flag")
	}
}

func TestRedactPreservesHeaderLineForSensitiveRead(t *testing.T) {
	content := "File: report.docx (42 lines)\nConfidential content here..."
	result := ToolResult{
		ToolCallID:    "call_008",
		ToolName:      "filesystem.readFile",
		Success:       true,
		ContentForLLM: content,
	}
	out := RedactResult(result, RiskLevelSensitiveRead)

	if !strings.HasPrefix(out.ContentForLLM, "File: report.docx") {
		t.Errorf("header line should be preserved; got: %q", out.ContentForLLM)
	}
}

func TestRedactFailedResultUnchanged(t *testing.T) {
	result := ToolResult{
		ToolCallID:    "call_009",
		ToolName:      "filesystem.readFile",
		Success:       false,
		ContentForLLM: "file not found: secret.key",
		Error:         &ToolError{Code: ErrorInvalidArgument, Message: "file not found"},
	}
	out := RedactResult(result, RiskLevelSensitiveRead)

	// Failed results with sensitive risk should still apply pattern masking
	// but not the body-suppression (since Success=false, no sensitive body to hide).
	if out.ContentForUser != result.ContentForUser {
		t.Error("ContentForUser must never change")
	}
}
