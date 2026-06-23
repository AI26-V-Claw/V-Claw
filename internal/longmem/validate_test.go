package longmem

import "testing"

func TestValidateMemoryContent_Rejection(t *testing.T) {
	if err := ValidateMemoryContent("OPENAI_API_KEY=sk-test-secret-value"); err == nil {
		t.Fatal("expected secret content to be rejected")
	}
}

func TestValidateMemoryContent_AllowsPlainFact(t *testing.T) {
	if err := ValidateMemoryContent("Agent prefers concise replies"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
