package reference

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/sessions"
)

type fakeProvider struct {
	text string
	err  error
	reqs []*providers.GenerateRequest
}

func (p *fakeProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (p *fakeProvider) Generate(_ context.Context, req *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	p.reqs = append(p.reqs, req)
	if p.err != nil {
		return nil, p.err
	}
	return &providers.GenerateResponse{Text: p.text, Model: req.Model}, nil
}

func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) Close() error { return nil }

func TestHeuristicResolverResolvesRecentCalendarEvent(t *testing.T) {
	resolver := NewHeuristicResolver()
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "Lịch này có gửi mail thông báo cho người tham gia không?",
		Memory: sessions.SessionMemory{LastActionResults: []sessions.ActionResult{{
			ToolName:  "calendar.createEvent",
			Content:   `Event created: {"id":"evt_1","title":"Demo"}`,
			CreatedAt: time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC),
		}}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeCalendarEvent {
		t.Fatalf("expected calendar reference, got %#v", result)
	}
	if result.ReferenceID != "evt_1" {
		t.Fatalf("expected extracted event id, got %q", result.ReferenceID)
	}
	if result.NeedsClarification {
		t.Fatalf("expected resolved reference, got clarification: %#v", result)
	}
}

func TestHeuristicResolverResolvesMeetingAboveFromCalendarList(t *testing.T) {
	resolver := NewHeuristicResolver()
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "viết email mời tham dự cuộc họp trên",
		Memory: sessions.SessionMemory{LastActionResults: []sessions.ActionResult{{
			ToolName: "calendar.listEvents",
			Content:  "Upcoming meeting: Test HITL, 2026-06-05 09:30-10:30, attendee baolnc@vclaw.site",
		}}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeCalendarEvent {
		t.Fatalf("expected calendar reference for meeting above, got %#v", result)
	}
	if result.Source != SourceLastActionResult {
		t.Fatalf("expected last action result source, got %q", result.Source)
	}
	if result.NeedsClarification {
		t.Fatalf("expected resolved reference, got clarification: %#v", result)
	}
}

func TestHeuristicResolverResolvesConversationTopic(t *testing.T) {
	resolver := NewHeuristicResolver()
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "Note lại chủ đề đó giúp tôi",
		Memory: sessions.SessionMemory{
			Summary: "User and assistant discussed HITL design with approval buttons and revise comments.",
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeConversationTopic {
		t.Fatalf("expected conversation topic reference, got %#v", result)
	}
	if result.Source != SourceMemorySummary {
		t.Fatalf("expected memory summary source, got %q", result.Source)
	}
}

func TestHeuristicResolverDoesNotResolveNewRequest(t *testing.T) {
	resolver := NewHeuristicResolver()
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "Tạo lịch họp cho tôi",
		Memory: sessions.SessionMemory{LastActionResults: []sessions.ActionResult{{
			ToolName: "calendar.createEvent",
			Content:  "Event created: old",
		}}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if result.HasReference {
		t.Fatalf("new request should not resolve old memory, got %#v", result)
	}
}

func TestHeuristicResolverResolvesRecentGmailDraft(t *testing.T) {
	resolver := NewHeuristicResolver()
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "gui ban nhap vua roi luon",
		Memory: sessions.SessionMemory{LastActionResults: []sessions.ActionResult{{
			ToolName: "gmail.createDraft",
			Content:  `{"Draft":{"ID":"draft_1","MessageID":"msg_1"}}`,
		}}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeGmailEmail {
		t.Fatalf("expected gmail draft reference, got %#v", result)
	}
}

func TestLLMResolverParsesJSONAndBuildsXMLPrompt(t *testing.T) {
	provider := &fakeProvider{text: `{"hasReference":true,"referenceType":"gmail_email","source":"recent_history","confidence":0.84,"resolvedContext":{"subject":"Welcome"},"reasoning":"Tin nhắn nhắc email vừa rồi."}`}
	resolver := NewLLMResolver(provider, "test-model")
	result, err := resolver.Resolve(context.Background(), Input{CurrentMessage: "Email vừa rồi nói gì?"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeGmailEmail || result.Confidence != 0.84 {
		t.Fatalf("unexpected resolution: %#v", result)
	}
	if len(provider.reqs) != 1 {
		t.Fatalf("expected provider request")
	}
	if !strings.Contains(provider.reqs[0].SystemPrompt, "<reference_resolver_system_prompt>") {
		t.Fatalf("expected XML system prompt, got %s", provider.reqs[0].SystemPrompt)
	}
}

func TestFallbackResolverUsesHeuristicWhenLLMFails(t *testing.T) {
	resolver := NewFallbackResolver(
		NewLLMResolver(&fakeProvider{err: errors.New("provider down")}, "test-model"),
		NewHeuristicResolver(),
	)
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "Lịch này có gửi mail không?",
		Memory: sessions.SessionMemory{LastActionResults: []sessions.ActionResult{{
			ToolName: "calendar.createEvent",
			Content:  `Event created: {"id":"evt_1"}`,
		}}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeCalendarEvent {
		t.Fatalf("expected fallback calendar reference, got %#v", result)
	}
}

func TestFallbackResolverUsesHeuristicWhenLLMNeedsClarification(t *testing.T) {
	provider := &fakeProvider{text: `{
		"hasReference": true,
		"referenceType": "calendar_event",
		"source": "recent_history",
		"confidence": 0.45,
		"needsClarification": true,
		"clarificationQuestion": "Ban muon noi toi calendar event nao gan day?",
		"reasoning": "LLM thay co tham chieu nhung chua tu tin."
	}`}
	resolver := NewFallbackResolver(
		NewLLMResolver(provider, "test-model"),
		NewHeuristicResolver(),
	)
	result, err := resolver.Resolve(context.Background(), Input{
		CurrentMessage: "viet email cho baolnc@vclaw.site moi tham du cuoc hop tren",
		Memory: sessions.SessionMemory{LastActionResults: []sessions.ActionResult{{
			ToolName: "calendar.listEvents",
			Content:  "Upcoming meeting: Test HITL, 2026-06-05 09:30-10:30, attendee baolnc@vclaw.site",
		}}},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !result.HasReference || result.ReferenceType != TypeCalendarEvent {
		t.Fatalf("expected heuristic calendar reference, got %#v", result)
	}
	if result.Source != SourceLastActionResult {
		t.Fatalf("expected last action result source, got %q", result.Source)
	}
	if result.NeedsClarification {
		t.Fatalf("expected fallback to avoid clarification, got %#v", result)
	}
}
