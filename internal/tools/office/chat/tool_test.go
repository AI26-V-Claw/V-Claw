package chat

import (
	"context"
	"errors"
	"testing"

	chatconnector "vclaw/internal/connectors/google/chat"
	"vclaw/internal/tools"
)

type fakeConnector struct {
	spacesOutput    chatconnector.ListSpacesOutput
	membersOutput   chatconnector.ListMembersOutput
	membersByParent map[string]chatconnector.ListMembersOutput
	listOutput      chatconnector.ListMessagesOutput
	sent            chatconnector.Message
	err             error
	seenParent      string
}

func (f fakeConnector) ListSpacesPage(context.Context, int64, string) (chatconnector.ListSpacesOutput, error) {
	if f.err != nil {
		return chatconnector.ListSpacesOutput{}, f.err
	}
	return f.spacesOutput, nil
}

func (f *fakeConnector) ListMembers(_ context.Context, parent string, _ int64, _ string) (chatconnector.ListMembersOutput, error) {
	f.seenParent = parent
	if f.err != nil {
		return chatconnector.ListMembersOutput{}, f.err
	}
	if f.membersByParent != nil {
		if output, ok := f.membersByParent[parent]; ok {
			return output, nil
		}
	}
	return f.membersOutput, nil
}

func (f *fakeConnector) ListMessages(_ context.Context, parent string, _ int64, _ string, _ bool) (chatconnector.ListMessagesOutput, error) {
	f.seenParent = parent
	if f.err != nil {
		return chatconnector.ListMessagesOutput{}, f.err
	}
	return f.listOutput, nil
}

func (f fakeConnector) CreateTextMessage(context.Context, string, string, chatconnector.MessageCreateOptions) (chatconnector.Message, error) {
	if f.err != nil {
		return chatconnector.Message{}, f.err
	}
	return f.sent, nil
}

func (f fakeConnector) CreateCardMessage(context.Context, string, chatconnector.CardMessage, chatconnector.MessageCreateOptions) (chatconnector.Message, error) {
	if f.err != nil {
		return chatconnector.Message{}, f.err
	}
	return f.sent, nil
}

func TestListSpacesReturnsSpaces(t *testing.T) {
	service := NewService(&fakeConnector{
		spacesOutput: chatconnector.ListSpacesOutput{
			Spaces: []chatconnector.Space{{Name: "spaces/A", SpaceType: "GROUP_CHAT"}},
		},
	})

	output, errShape := service.ListSpaces(context.Background(), ListSpacesInput{})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if len(output.Spaces) != 1 {
		t.Fatalf("expected 1 space, got %d", len(output.Spaces))
	}
}

func TestListMembersRequiresSpace(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.ListMembers(context.Background(), ListMembersInput{})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestListMembersReturnsMembers(t *testing.T) {
	connector := &fakeConnector{
		membersOutput: chatconnector.ListMembersOutput{
			Members: []chatconnector.Membership{{Name: "spaces/A/members/B", DisplayName: "Bao"}},
		},
	}
	service := NewService(connector)

	output, errShape := service.ListMembers(context.Background(), ListMembersInput{Space: "- spaces/A | (no display name) | GROUP_CHAT"})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if len(output.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(output.Members))
	}
	if connector.seenParent != "spaces/A" {
		t.Fatalf("expected normalized parent spaces/A, got %q", connector.seenParent)
	}
}

func TestFindSpacesByMembersReturnsMatchingSpace(t *testing.T) {
	service := NewService(&fakeConnector{
		spacesOutput: chatconnector.ListSpacesOutput{
			Spaces: []chatconnector.Space{
				{Name: "spaces/A", SpaceType: "DIRECT_MESSAGE"},
				{Name: "spaces/B", SpaceType: "GROUP_CHAT"},
			},
		},
		membersByParent: map[string]chatconnector.ListMembersOutput{
			"spaces/A": {
				Members: []chatconnector.Membership{
					{MemberName: "users/me"},
					{MemberName: "users/bao"},
				},
			},
			"spaces/B": {
				Members: []chatconnector.Membership{
					{MemberName: "users/me"},
					{MemberName: "users/tung"},
				},
			},
		},
	})

	output, errShape := service.FindSpacesByMembers(context.Background(), FindSpacesByMembersInput{
		MemberUserNames: []string{"users/bao"},
		SpaceType:       "DIRECT_MESSAGE",
	})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if len(output.Spaces) != 1 {
		t.Fatalf("expected 1 matching space, got %d", len(output.Spaces))
	}
	if output.Spaces[0].Space.Name != "spaces/A" {
		t.Fatalf("expected spaces/A, got %q", output.Spaces[0].Space.Name)
	}
}

func TestFindSpacesByMembersRequiresMembers(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.FindSpacesByMembers(context.Background(), FindSpacesByMembersInput{})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestListMessagesRequiresSpace(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.ListMessages(context.Background(), ListMessagesInput{})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestListMessagesUsesDefaultLimit(t *testing.T) {
	connector := &fakeConnector{
		listOutput: chatconnector.ListMessagesOutput{
			Messages: []chatconnector.Message{{Name: "spaces/A/messages/B", Text: "hello"}},
		},
	}
	service := NewService(connector)

	output, errShape := service.ListMessages(context.Background(), ListMessagesInput{Space: "spaces/A | (no display name)"})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if len(output.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(output.Messages))
	}
	if connector.seenParent != "spaces/A" {
		t.Fatalf("expected normalized parent spaces/A, got %q", connector.seenParent)
	}
}

func TestBoundedInt64ArgClampsAgentProvidedLimit(t *testing.T) {
	args := map[string]any{"tooHigh": float64(100), "tooLow": float64(-1), "valid": float64(20)}

	if got := boundedInt64Arg(args, "tooHigh", defaultMaxResults, maxAllowedResults); got != maxAllowedResults {
		t.Fatalf("expected max clamp %d, got %d", maxAllowedResults, got)
	}
	if got := boundedInt64Arg(args, "tooLow", defaultMaxResults, maxAllowedResults); got != defaultMaxResults {
		t.Fatalf("expected default %d, got %d", defaultMaxResults, got)
	}
	if got := boundedInt64Arg(args, "valid", defaultMaxResults, maxAllowedResults); got != 20 {
		t.Fatalf("expected valid value 20, got %d", got)
	}
}

func TestSendMessageRequiresSpace(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Text: "hello"})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageRequiresText(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Space: "spaces/A"})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageRejectsCardInput(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{
		Space:     "spaces/A",
		CardTitle: "Meeting Reminder",
		CardText:  "Team sync starts at 10:00.",
	})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageMapsConnectorError(t *testing.T) {
	service := NewService(&fakeConnector{err: errors.New("boom")})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Space: "spaces/A", Text: "hello"})
	if errShape == nil {
		t.Fatal("expected connector error")
	}
	if errShape.Code != "INTERNAL_ERROR" {
		t.Fatalf("expected INTERNAL_ERROR, got %q", errShape.Code)
	}
}

func TestToolRiskMetadata(t *testing.T) {
	service := NewService(&fakeConnector{})
	spacesTool := NewListSpacesTool(service)
	membersTool := NewListMembersTool(service)
	findTool := NewFindSpacesByMembersTool(service)
	listTool := NewListMessagesTool(service)
	sendTool := NewSendMessageTool(service)

	if spacesTool.Capability() != tools.CapabilityReadOnly || spacesTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list spaces tool should be safe read")
	}
	if membersTool.Capability() != tools.CapabilityReadOnly || membersTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list members tool should be safe read")
	}
	if findTool.Capability() != tools.CapabilityReadOnly || findTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("find spaces by members tool should be safe read")
	}
	if listTool.Capability() != tools.CapabilityReadOnly || listTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list tool should be safe read")
	}
	if sendTool.Capability() != tools.CapabilityMutating || sendTool.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("send tool should be external write")
	}
}

func TestRegisterToolsIncludesFindSpacesByMembers(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(&fakeConnector{})); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}
	if _, ok := registry.GetTool(ToolNameFindSpacesByMembers); !ok {
		t.Fatalf("expected %s to be registered", ToolNameFindSpacesByMembers)
	}
}
