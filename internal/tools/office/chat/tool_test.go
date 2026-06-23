package chat

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	updated         chatconnector.Message
	createdSpace    chatconnector.Space
	addedMember     chatconnector.Membership
	err             error
	seenParent      string
	uploadRefs      []string
	uploadedFiles   []string
}

func (f fakeConnector) ListSpacesPage(context.Context, int64, string) (chatconnector.ListSpacesOutput, error) {
	if f.err != nil {
		return chatconnector.ListSpacesOutput{}, f.err
	}
	return f.spacesOutput, nil
}

func (f fakeConnector) ListSpacesPageFiltered(ctx context.Context, pageSize int64, pageToken string, _ string) (chatconnector.ListSpacesOutput, error) {
	return f.ListSpacesPage(ctx, pageSize, pageToken)
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

func (f *fakeConnector) CreateTextMessage(_ context.Context, _ string, _ string, options chatconnector.MessageCreateOptions) (chatconnector.Message, error) {
	if f.err != nil {
		return chatconnector.Message{}, f.err
	}
	f.uploadRefs = append([]string(nil), options.AttachmentUploadRefs...)
	return f.sent, nil
}

func (f *fakeConnector) UpdateTextMessage(context.Context, string, string) (chatconnector.Message, error) {
	if f.err != nil {
		return chatconnector.Message{}, f.err
	}
	return f.updated, nil
}

func (f *fakeConnector) DeleteMessage(context.Context, string, bool) error {
	return f.err
}

func (f *fakeConnector) CreateSpace(context.Context, chatconnector.CreateSpaceInput) (chatconnector.Space, error) {
	if f.err != nil {
		return chatconnector.Space{}, f.err
	}
	return f.createdSpace, nil
}

func (f *fakeConnector) AddMember(context.Context, string, string) (chatconnector.Membership, error) {
	if f.err != nil {
		return chatconnector.Membership{}, f.err
	}
	return f.addedMember, nil
}

func (f *fakeConnector) RemoveMember(context.Context, string) error {
	return f.err
}

func (f *fakeConnector) UploadAttachment(_ context.Context, _ string, filename string, _ string, reader io.Reader) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if _, err := io.ReadAll(reader); err != nil {
		return "", err
	}
	f.uploadedFiles = append(f.uploadedFiles, filename)
	return "upload-token-" + filename, nil
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

func TestListMessagesRejectsPlaceholderSpace(t *testing.T) {
	connector := &fakeConnector{}
	service := NewService(connector)

	_, errShape := service.ListMessages(context.Background(), ListMessagesInput{Space: "spaces/UNKNOWN"})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
	if connector.seenParent != "" {
		t.Fatalf("connector should not be called for placeholder space, got %q", connector.seenParent)
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

func TestSendMessageRequiresTextOrAttachment(t *testing.T) {
	service := NewService(&fakeConnector{})

	_, errShape := service.SendMessage(context.Background(), SendMessageInput{Space: "spaces/A"})
	if errShape == nil {
		t.Fatal("expected validation error")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q", errShape.Code)
	}
}

func TestSendMessageAllowsAttachmentOnly(t *testing.T) {
	connector := &fakeConnector{sent: chatconnector.Message{Name: "spaces/A/messages/B"}}
	service := NewService(connector)
	path := filepath.Join(t.TempDir(), "diagram.png")
	if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
		t.Fatalf("write attachment: %v", err)
	}

	output, errShape := service.SendMessage(context.Background(), SendMessageInput{
		Space:       "spaces/A",
		Attachments: []string{path},
	})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if output.Message.Name != "spaces/A/messages/B" {
		t.Fatalf("unexpected message: %#v", output.Message)
	}
	if len(connector.uploadRefs) != 1 || connector.uploadRefs[0] != "upload-token-diagram.png" {
		t.Fatalf("unexpected upload refs: %#v", connector.uploadRefs)
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

func TestSendMessageToolUsesVietnameseUserOutput(t *testing.T) {
	service := NewService(&fakeConnector{sent: chatconnector.Message{Name: "spaces/A/messages/B"}})
	result := NewSendMessageTool(service).Execute(context.Background(), tools.ToolCall{
		ID:   "call_send",
		Name: ToolNameSendMessage,
		Arguments: map[string]any{
			"space": "spaces/A",
			"text":  "hello",
		},
	})
	if !result.Success {
		t.Fatalf("expected successful result, got %#v", result)
	}
	if result.ContentForUser != "Đã gửi tin nhắn Google Chat." {
		t.Fatalf("expected Vietnamese user output, got %q", result.ContentForUser)
	}
	if !strings.Contains(result.ContentForLLM, "Sent Google Chat message: spaces/A/messages/B") {
		t.Fatalf("expected LLM output to keep message resource, got %q", result.ContentForLLM)
	}
}

func TestSendMessageUploadsAttachments(t *testing.T) {
	connector := &fakeConnector{sent: chatconnector.Message{Name: "spaces/A/messages/B"}}
	service := NewService(connector)
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("write attachment: %v", err)
	}

	output, errShape := service.SendMessage(context.Background(), SendMessageInput{
		Space:       "spaces/A",
		Text:        "hello",
		Attachments: []string{path},
	})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Message)
	}
	if output.Message.Name != "spaces/A/messages/B" {
		t.Fatalf("unexpected message: %#v", output.Message)
	}
	if len(connector.uploadedFiles) != 1 || connector.uploadedFiles[0] != "report.txt" {
		t.Fatalf("unexpected uploaded files: %#v", connector.uploadedFiles)
	}
	if len(connector.uploadRefs) != 1 || connector.uploadRefs[0] != "upload-token-report.txt" {
		t.Fatalf("unexpected upload refs: %#v", connector.uploadRefs)
	}
}

func TestUpdateDeleteMessageValidateInput(t *testing.T) {
	service := NewService(&fakeConnector{})
	if _, errShape := service.UpdateMessage(context.Background(), UpdateMessageInput{Text: "hello"}); errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected update name validation error, got %#v", errShape)
	}
	if _, errShape := service.DeleteMessage(context.Background(), DeleteMessageInput{}); errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected delete name validation error, got %#v", errShape)
	}
}

func TestCreateSpaceAndAddMemberRequireAllowedWorkspaceDomain(t *testing.T) {
	service := NewServiceWithWorkspaceDomains(&fakeConnector{}, nil)
	if _, errShape := service.CreateSpace(context.Background(), CreateSpaceInput{
		SpaceType:   "DIRECT_MESSAGE",
		MemberUsers: []string{"bao@vclaw.site"},
	}); errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected missing domain config error, got %#v", errShape)
	}

	service = NewServiceWithWorkspaceDomains(&fakeConnector{}, []string{"vclaw.site"})
	if _, errShape := service.AddMember(context.Background(), AddMemberInput{
		Space: "spaces/A",
		User:  "bao@example.com",
	}); errShape == nil || errShape.Code != "ACTION_BLOCKED_BY_POLICY" {
		t.Fatalf("expected blocked domain error, got %#v", errShape)
	}
}

func TestToolRiskMetadata(t *testing.T) {
	service := NewService(&fakeConnector{})
	spacesTool := NewListSpacesTool(service)
	membersTool := NewListMembersTool(service)
	findTool := NewFindSpacesByMembersTool(service)
	listTool := NewListMessagesTool(service)
	sendTool := NewSendMessageTool(service)
	updateTool := NewUpdateMessageTool(service)
	deleteTool := NewDeleteMessageTool(service)
	createSpaceTool := NewCreateSpaceTool(service)
	addMemberTool := NewAddMemberTool(service)
	removeMemberTool := NewRemoveMemberTool(service)

	if spacesTool.Capability() != tools.CapabilityReadOnly || spacesTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list spaces tool should be safe read")
	}
	if membersTool.Capability() != tools.CapabilityReadOnly || membersTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list members tool should be safe read")
	}
	if findTool.Capability() != tools.CapabilityReadOnly || findTool.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("find spaces by members tool should be safe read")
	}
	if listTool.Capability() != tools.CapabilityReadOnly || listTool.RiskLevel() != tools.RiskLevelSensitiveRead {
		t.Fatalf("list tool should be sensitive read")
	}
	if sendTool.Capability() != tools.CapabilityMutating || sendTool.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("send tool should be external write")
	}
	if updateTool.Capability() != tools.CapabilityMutating || updateTool.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("update tool should be external write")
	}
	if deleteTool.Capability() != tools.CapabilityMutating || deleteTool.RiskLevel() != tools.RiskLevelDestructive {
		t.Fatalf("delete tool should be destructive")
	}
	if createSpaceTool.Capability() != tools.CapabilityMutating || createSpaceTool.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("create space tool should be external write")
	}
	if addMemberTool.Capability() != tools.CapabilityMutating || addMemberTool.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("add member tool should be external write")
	}
	if removeMemberTool.Capability() != tools.CapabilityMutating || removeMemberTool.RiskLevel() != tools.RiskLevelDestructive {
		t.Fatalf("remove member tool should be destructive")
	}
}

func TestRegisterToolsMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(&fakeConnector{})); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	assertToolMetadata(t, registry, ToolNameListSpaces, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameListMembers, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameFindSpacesByMembers, tools.CapabilityReadOnly, tools.RiskLevelSafeRead, false)
	assertToolMetadata(t, registry, ToolNameListMessages, tools.CapabilityReadOnly, tools.RiskLevelSensitiveRead, true)
	assertToolMetadata(t, registry, ToolNameSendMessage, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameUpdateMessage, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameDeleteMessage, tools.CapabilityMutating, tools.RiskLevelDestructive, true)
	assertToolMetadata(t, registry, ToolNameCreateSpace, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameAddMember, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameRemoveMember, tools.CapabilityMutating, tools.RiskLevelDestructive, true)
}

func assertToolMetadata(t *testing.T, registry *tools.ToolRegistry, name string, capability tools.Capability, risk tools.RiskLevel, approval bool) {
	t.Helper()
	definition, ok := registry.GetDefinition(name)
	if !ok {
		t.Fatalf("expected %s definition", name)
	}
	if definition.Capability != capability {
		t.Fatalf("%s capability = %s, want %s", name, definition.Capability, capability)
	}
	if definition.RiskLevel != risk {
		t.Fatalf("%s risk = %s, want %s", name, definition.RiskLevel, risk)
	}
	if definition.RequiresApproval != approval {
		t.Fatalf("%s approval = %t, want %t", name, definition.RequiresApproval, approval)
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
	for _, name := range []string{ToolNameUpdateMessage, ToolNameDeleteMessage, ToolNameCreateSpace, ToolNameAddMember, ToolNameRemoveMember} {
		if _, ok := registry.GetTool(name); !ok {
			t.Fatalf("expected %s to be registered", name)
		}
	}
}
