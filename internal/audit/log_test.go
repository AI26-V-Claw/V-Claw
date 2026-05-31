package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoggerWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(filepath.Join(dir, "audit.jsonl"))

	entry := Entry{
		UpdateID:     1,
		ChatID:       2,
		UserID:       3,
		Input:        "hello",
		Intent:       "GREETING",
		SystemOpType: "NONE",
		Confidence:   0.99,
		ActionTaken:  "greeting_reply",
		Output:       "Chào bạn, tôi là V-Claw.",
		HitlRequired: false,
	}
	if err := logger.Record(entry); err != nil {
		t.Fatalf("Record() returned error: %v", err)
	}

	bytes, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	var got Entry
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatalf("audit file is not valid JSON: %v", err)
	}
	if got.UpdateID != entry.UpdateID || got.ChatID != entry.ChatID || got.UserID != entry.UserID {
		t.Fatalf("unexpected entry: %+v", got)
	}
}
