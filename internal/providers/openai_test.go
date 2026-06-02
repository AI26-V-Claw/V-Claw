package providers

import "testing"

func TestOpenAIToolNameMapPreservesContractNames(t *testing.T) {
	nameMap := newOpenAIToolNameMap([]ToolDefinition{
		{Name: "gmail.listEmails"},
		{Name: "calendar.createEvent"},
	})

	if got := nameMap.safeName("gmail.listEmails"); got != "gmail__dot__listEmails" {
		t.Fatalf("unexpected safe name: %q", got)
	}
	if got := nameMap.contractName("gmail__dot__listEmails"); got != "gmail.listEmails" {
		t.Fatalf("unexpected contract name: %q", got)
	}
	if got := nameMap.contractName("calendar__dot__createEvent"); got != "calendar.createEvent" {
		t.Fatalf("unexpected calendar contract name: %q", got)
	}
}

func TestOpenAIMessageMappingUsesSafeNamesAndRestoresContractNames(t *testing.T) {
	nameMap := newOpenAIToolNameMap([]ToolDefinition{{Name: "gmail.listEmails"}})

	wire := openAIMessageFromProvider(Message{
		Role: MessageRoleAssistant,
		ToolCalls: []ToolCall{{
			ID:        "call_1",
			Name:      "gmail.listEmails",
			Arguments: map[string]any{"query": "newer_than:1d"},
		}},
	}, nameMap.safeName)

	if len(wire.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(wire.ToolCalls))
	}
	if got := wire.ToolCalls[0].Function.Name; got != "gmail__dot__listEmails" {
		t.Fatalf("unexpected safe function name: %q", got)
	}

	providerMessage := providerMessageFromOpenAI(wire, nameMap.contractName)
	if len(providerMessage.ToolCalls) != 1 {
		t.Fatalf("expected one provider tool call, got %d", len(providerMessage.ToolCalls))
	}
	if got := providerMessage.ToolCalls[0].Name; got != "gmail.listEmails" {
		t.Fatalf("unexpected restored tool name: %q", got)
	}
}
