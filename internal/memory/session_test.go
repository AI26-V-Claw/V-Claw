package memory

import (
	"strings"
	"testing"
)

func TestNewSessionMemory(t *testing.T) {
	sm := NewSessionMemory(10)
	if sm == nil {
		t.Fatal("Expected non-nil SessionMemory")
	}
	if sm.maxTurns != 10 {
		t.Errorf("Expected maxTurns=10, got %d", sm.maxTurns)
	}
	if sm.Len() != 0 {
		t.Errorf("Expected 0 messages, got %d", sm.Len())
	}
}

func TestNewSessionMemory_DefaultMaxTurns(t *testing.T) {
	sm := NewSessionMemory(0)
	if sm.maxTurns != 20 {
		t.Errorf("Expected default maxTurns=20, got %d", sm.maxTurns)
	}

	sm2 := NewSessionMemory(-5)
	if sm2.maxTurns != 20 {
		t.Errorf("Expected default maxTurns=20 for negative input, got %d", sm2.maxTurns)
	}
}

func TestAddMessage(t *testing.T) {
	sm := NewSessionMemory(5)

	sm.AddMessage(RoleUser, "Hello")
	sm.AddMessage(RoleAssistant, "Hi there")

	if sm.Len() != 2 {
		t.Errorf("Expected 2 messages, got %d", sm.Len())
	}
}

func TestAddMessage_TrimExcess(t *testing.T) {
	sm := NewSessionMemory(3)

	sm.AddMessage(RoleUser, "msg1")
	sm.AddMessage(RoleAssistant, "msg2")
	sm.AddMessage(RoleUser, "msg3")
	sm.AddMessage(RoleAssistant, "msg4") // Should trim msg1

	if sm.Len() != 3 {
		t.Errorf("Expected 3 messages after trim, got %d", sm.Len())
	}

	history := sm.GetFullHistory()
	if history[0].Content != "msg2" {
		t.Errorf("Expected first message to be 'msg2' after trim, got %q", history[0].Content)
	}
}

func TestGetFullHistory(t *testing.T) {
	sm := NewSessionMemory(10)

	sm.AddMessage(RoleUser, "Hello")
	sm.AddMessage(RoleAssistant, "Hi")
	sm.AddMessage(RoleUser, "How are you?")

	history := sm.GetFullHistory()
	if len(history) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(history))
	}

	// Verify it's a copy (modifying returned slice doesn't affect original)
	history[0].Content = "modified"
	original := sm.GetFullHistory()
	if original[0].Content == "modified" {
		t.Error("GetFullHistory should return a copy, not a reference")
	}
}

func TestGetRecentHistory(t *testing.T) {
	sm := NewSessionMemory(10)

	sm.AddMessage(RoleUser, "msg1")
	sm.AddMessage(RoleAssistant, "msg2")
	sm.AddMessage(RoleUser, "msg3")
	sm.AddMessage(RoleAssistant, "msg4")
	sm.AddMessage(RoleUser, "msg5")

	// Get last 2
	recent := sm.GetRecentHistory(2)
	if len(recent) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(recent))
	}
	if recent[0].Content != "msg4" {
		t.Errorf("Expected 'msg4', got %q", recent[0].Content)
	}
	if recent[1].Content != "msg5" {
		t.Errorf("Expected 'msg5', got %q", recent[1].Content)
	}
}

func TestGetRecentHistory_EmptyAndZero(t *testing.T) {
	sm := NewSessionMemory(10)

	// Empty session
	recent := sm.GetRecentHistory(5)
	if len(recent) != 0 {
		t.Errorf("Expected 0 messages for empty session, got %d", len(recent))
	}

	// Zero count
	sm.AddMessage(RoleUser, "msg1")
	recent = sm.GetRecentHistory(0)
	if len(recent) != 0 {
		t.Errorf("Expected 0 messages for zero count, got %d", len(recent))
	}
}

func TestGetFilteredHistoryForDangerousAction(t *testing.T) {
	sm := NewSessionMemory(20)

	// Simulate a conversation
	sm.AddMessage(RoleUser, "Tìm file document.pdf")
	sm.AddMessage(RoleAssistant, "Tìm thấy file document.pdf tại /home/user/docs/document.pdf")
	sm.AddMessage(RoleUser, "Xóa file test.txt")
	sm.AddMessage(RoleAssistant, "Bạn muốn xóa file nào?")
	sm.AddMessage(RoleUser, "Xóa file đó đi") // Ambiguous - "that file"

	history, warning := sm.GetFilteredHistoryForDangerousAction(3)

	// Should only get last 3 messages
	if len(history) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(history))
	}

	// Warning should contain isolation instructions
	if !strings.Contains(warning, "CÁCH LY BỘ NHỚ") {
		t.Error("Warning should contain memory isolation instructions")
	}
	if !strings.Contains(warning, "DANGEROUS_ACTION") {
		t.Error("Warning should mention DANGEROUS_ACTION")
	}
	if !strings.Contains(warning, "needs_clarification") {
		t.Error("Warning should mention needs_clarification")
	}
}

func TestGetFilteredHistoryForDangerousAction_DefaultTurns(t *testing.T) {
	sm := NewSessionMemory(20)

	sm.AddMessage(RoleUser, "msg1")
	sm.AddMessage(RoleAssistant, "msg2")
	sm.AddMessage(RoleUser, "msg3")
	sm.AddMessage(RoleAssistant, "msg4")
	sm.AddMessage(RoleUser, "msg5")

	history, _ := sm.GetFilteredHistoryForDangerousAction(0) // Should default to 3
	if len(history) != 3 {
		t.Errorf("Expected 3 messages with default turns, got %d", len(history))
	}
}

func TestGetHistoryForReadInfo(t *testing.T) {
	sm := NewSessionMemory(20)

	for i := 0; i < 15; i++ {
		sm.AddMessage(RoleUser, "msg")
	}

	// Read info gets more context
	history := sm.GetHistoryForReadInfo(10)
	if len(history) != 10 {
		t.Errorf("Expected 10 messages, got %d", len(history))
	}

	// Default max turns
	history = sm.GetHistoryForReadInfo(0)
	if len(history) != 10 {
		t.Errorf("Expected 10 messages with default, got %d", len(history))
	}
}

func TestGetLastUserMessage(t *testing.T) {
	sm := NewSessionMemory(10)

	// Empty session
	_, found := sm.GetLastUserMessage()
	if found {
		t.Error("Expected not found for empty session")
	}

	sm.AddMessage(RoleUser, "First user msg")
	sm.AddMessage(RoleAssistant, "Response")
	sm.AddMessage(RoleUser, "Second user msg")
	sm.AddMessage(RoleAssistant, "Response 2")

	msg, found := sm.GetLastUserMessage()
	if !found {
		t.Fatal("Expected to find last user message")
	}
	if msg.Content != "Second user msg" {
		t.Errorf("Expected 'Second user msg', got %q", msg.Content)
	}
}

func TestClear(t *testing.T) {
	sm := NewSessionMemory(10)

	sm.AddMessage(RoleUser, "msg1")
	sm.AddMessage(RoleAssistant, "msg2")

	if sm.Len() != 2 {
		t.Fatalf("Expected 2 messages before clear, got %d", sm.Len())
	}

	sm.Clear()

	if sm.Len() != 0 {
		t.Errorf("Expected 0 messages after clear, got %d", sm.Len())
	}
}

// TestMemoryIsolation_DangerousVsReadInfo verifies that dangerous actions
// get much narrower context than read info actions.
func TestMemoryIsolation_DangerousVsReadInfo(t *testing.T) {
	sm := NewSessionMemory(20)

	// Simulate a long conversation
	for i := 0; i < 10; i++ {
		sm.AddMessage(RoleUser, "user message")
		sm.AddMessage(RoleAssistant, "assistant response")
	}

	// Dangerous actions get narrow context
	dangerousHistory, _ := sm.GetFilteredHistoryForDangerousAction(3)
	// Read info gets broad context
	readInfoHistory := sm.GetHistoryForReadInfo(10)

	if len(dangerousHistory) >= len(readInfoHistory) {
		t.Errorf("Dangerous history (%d) should be narrower than read info history (%d)",
			len(dangerousHistory), len(readInfoHistory))
	}
}

func BenchmarkAddMessage(b *testing.B) {
	sm := NewSessionMemory(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.AddMessage(RoleUser, "benchmark message")
	}
}

func BenchmarkGetFilteredHistoryForDangerousAction(b *testing.B) {
	sm := NewSessionMemory(100)
	for i := 0; i < 50; i++ {
		sm.AddMessage(RoleUser, "user message")
		sm.AddMessage(RoleAssistant, "assistant response")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.GetFilteredHistoryForDangerousAction(3)
	}
}
