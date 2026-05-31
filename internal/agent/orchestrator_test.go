package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxhai/vclaw/internal/audit"
	"github.com/nxhai/vclaw/internal/intent"
	"github.com/nxhai/vclaw/internal/memory"
)

func TestHandleMessageReturnsHistorySummary(t *testing.T) {
	dir := t.TempDir()
	orchestrator := NewOrchestrator(memory.NewStore(), intent.NewClassifier(), audit.NewLogger(filepath.Join(dir, "audit.jsonl")))
	orchestrator.memory.Append(memory.RoleUser, "xin chào")
	orchestrator.memory.Append(memory.RoleAssistant, "Chào bạn, tôi là V-Claw.")

	outbound, err := orchestrator.HandleMessage(context.Background(), InboundMessage{
		UpdateID: 10,
		ChatID:   20,
		UserID:   30,
		Text:     "nãy mình nói gì",
		Source:   "telegram",
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}

	if !strings.Contains(outbound.Text, "xin chào") {
		t.Fatalf("expected history summary, got: %q", outbound.Text)
	}

	orchestrator.FinalizeAudit(InboundMessage{UpdateID: 10, ChatID: 20, UserID: 30, Text: "nãy mình nói gì"}, nil)
}

func TestHandleMessageRecognizesHistoryVariants(t *testing.T) {
	cases := []string{
		"nãy mình nói gì",
		"nãy mình vừa nói gì",
		"nãy t vừa nói gì",
		"hãy kể những việc tôi vừa nói",
		"kể những việc tôi vừa nói",
		"hãy kể lại những gì tôi vừa nói",
		"kể những việc mình vừa nói",
		"vừa rồi mình nói gì",
		"nãy tôi vừa nói gì",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			dir := t.TempDir()
			orchestrator := NewOrchestrator(memory.NewStore(), intent.NewClassifier(), audit.NewLogger(filepath.Join(dir, "audit.jsonl")))
			orchestrator.memory.Append(memory.RoleUser, "mình đã nhắc lịch họp")
			orchestrator.memory.Append(memory.RoleAssistant, "Tôi đã ghi nhận lịch họp.")

			outbound, err := orchestrator.HandleMessage(context.Background(), InboundMessage{
				UpdateID: 11,
				ChatID:   21,
				UserID:   31,
				Text:     input,
				Source:   "telegram",
			})
			if err != nil {
				t.Fatalf("HandleMessage() returned error: %v", err)
			}
			if !strings.Contains(outbound.Text, "mình đã nhắc lịch họp") {
				t.Fatalf("expected memory summary, got: %q", outbound.Text)
			}
			if strings.Contains(outbound.Text, "Bạn muốn tôi làm gì cụ thể hơn?") {
				t.Fatalf("expected recall path, got clarify response: %q", outbound.Text)
			}

			orchestrator.FinalizeAudit(InboundMessage{UpdateID: 11, ChatID: 21, UserID: 31, Text: input}, nil)
		})
	}
}

func TestHandleMessageFiltersPreviousRecallQueriesFromSummary(t *testing.T) {
	dir := t.TempDir()
	orchestrator := NewOrchestrator(memory.NewStore(), intent.NewClassifier(), audit.NewLogger(filepath.Join(dir, "audit.jsonl")))
	orchestrator.memory.Append(memory.RoleUser, "xin chào")
	orchestrator.memory.Append(memory.RoleAssistant, "Chào bạn, tôi là V-Claw.")
	orchestrator.memory.Append(memory.RoleUser, "nãy tôi vừa nói gì")
	orchestrator.memory.Append(memory.RoleAssistant, "Đây là những gì bạn đã nói gần đây:\n1. Bạn: xin chào")

	outbound, err := orchestrator.HandleMessage(context.Background(), InboundMessage{
		UpdateID: 13,
		ChatID:   23,
		UserID:   33,
		Text:     "hãy kể những việc tôi vừa nói",
		Source:   "telegram",
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}

	if strings.Contains(outbound.Text, "nãy tôi vừa nói gì") {
		t.Fatalf("expected recall queries to be filtered out, got: %q", outbound.Text)
	}
	if !strings.Contains(outbound.Text, "xin chào") {
		t.Fatalf("expected meaningful history to remain, got: %q", outbound.Text)
	}

	orchestrator.FinalizeAudit(InboundMessage{UpdateID: 13, ChatID: 23, UserID: 33, Text: "hãy kể những việc tôi vừa nói"}, nil)
}

func TestHandleMessageReturnsEmptyHistoryMessage(t *testing.T) {
	dir := t.TempDir()
	orchestrator := NewOrchestrator(memory.NewStore(), intent.NewClassifier(), audit.NewLogger(filepath.Join(dir, "audit.jsonl")))

	outbound, err := orchestrator.HandleMessage(context.Background(), InboundMessage{
		UpdateID: 12,
		ChatID:   22,
		UserID:   32,
		Text:     "tôi vừa nói gì",
		Source:   "telegram",
	})
	if err != nil {
		t.Fatalf("HandleMessage() returned error: %v", err)
	}
	if outbound.Text != "Tôi chưa có lịch sử hội thoại nào trong phiên này." {
		t.Fatalf("unexpected empty-history reply: %q", outbound.Text)
	}

	orchestrator.FinalizeAudit(InboundMessage{UpdateID: 12, ChatID: 22, UserID: 32, Text: "tôi vừa nói gì"}, nil)
}
