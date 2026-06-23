package chat

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	googleconnector "vclaw/internal/connectors/google"
	chatconnector "vclaw/internal/connectors/google/chat"
	peopleconnector "vclaw/internal/connectors/google/people"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameListSpaces          = "chat.listSpaces"
	ToolNameListMembers         = "chat.listMembers"
	ToolNameFindSpacesByMembers = "chat.findSpacesByMembers"
	ToolNameListMessages        = "chat.listMessages"
	ToolNameSendMessage         = "chat.sendMessage"
	ToolNameUpdateMessage       = "chat.updateMessage"
	ToolNameDeleteMessage       = "chat.deleteMessage"
	ToolNameCreateSpace         = "chat.createSpace"
	ToolNameAddMember           = "chat.addMember"
	ToolNameRemoveMember        = "chat.removeMember"
)

const (
	defaultMaxResults = int64(10)
	maxAllowedResults = int64(50)
	maxAttachments    = 5
	maxAttachmentRaw  = int64(20 * 1024 * 1024)
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{
		Name:             ToolNameListSpaces,
		Owner:            "integration",
		Description:      "List Google Chat spaces available to the authenticated user.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameListMembers,
		Owner:            "integration",
		Description:      "List members in a Google Chat space. space must be a resource name like spaces/AAAA.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameFindSpacesByMembers,
		Owner:            "integration",
		Description:      "Find Google Chat spaces that contain the requested member user resource names.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameListMessages,
		Owner:            "integration",
		Description:      "List messages in a Google Chat space. space must be a resource name like spaces/AAAA — resolve a group name with chat.listSpaces first.",
		DefaultRiskLevel: "sensitive_read",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameSendMessage,
		Owner:            "integration",
		Description:      "Send a Google Chat message, thread reply, or file attachment. space must be a resource name like spaces/AAAA. For a named person, call people.searchDirectory then chat.findSpacesByMembers before this tool — never assume a spaces/... value from history.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameUpdateMessage,
		Owner:            "integration",
		Description:      "Update the text of a Google Chat message.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameDeleteMessage,
		Owner:            "integration",
		Description:      "Delete a Google Chat message.",
		DefaultRiskLevel: "destructive",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameCreateSpace,
		Owner:            "integration",
		Description:      "Create or set up a Google Chat space, group chat, or direct message.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameAddMember,
		Owner:            "integration",
		Description:      "Add a member to a Google Chat space. space must be a resource name like spaces/AAAA.",
		DefaultRiskLevel: "external_write",
		RequiresApproval: true,
	},
	{
		Name:             ToolNameRemoveMember,
		Owner:            "integration",
		Description:      "Remove a member from a Google Chat space.",
		DefaultRiskLevel: "destructive",
		RequiresApproval: true,
	},
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

type Connector interface {
	ListSpacesPage(ctx context.Context, pageSize int64, pageToken string) (chatconnector.ListSpacesOutput, error)
	ListSpacesPageFiltered(ctx context.Context, pageSize int64, pageToken string, spaceTypeFilter string) (chatconnector.ListSpacesOutput, error)
	ListMembers(ctx context.Context, parent string, pageSize int64, pageToken string) (chatconnector.ListMembersOutput, error)
	ListMessages(ctx context.Context, parent string, pageSize int64, pageToken string, showDeleted bool) (chatconnector.ListMessagesOutput, error)
	CreateTextMessage(ctx context.Context, parent string, text string, options chatconnector.MessageCreateOptions) (chatconnector.Message, error)
	UpdateTextMessage(ctx context.Context, name string, text string) (chatconnector.Message, error)
	DeleteMessage(ctx context.Context, name string, force bool) error
	CreateSpace(ctx context.Context, input chatconnector.CreateSpaceInput) (chatconnector.Space, error)
	AddMember(ctx context.Context, parent string, user string) (chatconnector.Membership, error)
	RemoveMember(ctx context.Context, name string) error
	UploadAttachment(ctx context.Context, parent string, filename string, mediaType string, reader io.Reader) (string, error)
}

// PeopleEnricher resolves workspace user identities to email addresses.
// Satisfied directly by *peopleconnector.Client.
type PeopleEnricher interface {
	// SearchDirectoryPeople searches by display name when a name is available.
	SearchDirectoryPeople(ctx context.Context, query string, pageSize int64, pageToken string) (peopleconnector.SearchDirectoryOutput, error)
	// GetPerson fetches a single person by People API resource name (e.g. "people/123456789").
	GetPerson(ctx context.Context, resourceName string) (peopleconnector.DirectoryPerson, error)
}

type Service struct {
	connector        Connector
	workspaceDomains []string
	peopleEnricher   PeopleEnricher // optional; enriches member emails via Directory API
}

func NewService(connector Connector) *Service {
	return &Service{
		connector:        connector,
		workspaceDomains: splitCSV(os.Getenv("VCLAW_GOOGLE_WORKSPACE_DOMAINS")),
	}
}

func NewServiceWithWorkspaceDomains(connector Connector, domains []string) *Service {
	return &Service{connector: connector, workspaceDomains: cleanStrings(domains)}
}

// NewServiceWithPeople creates a Service that automatically enriches member email addresses
// by querying the Google Workspace Directory via the provided PeopleEnricher.
func NewServiceWithPeople(connector Connector, people PeopleEnricher) *Service {
	svc := NewService(connector)
	svc.peopleEnricher = people
	return svc
}

// NewServiceWithPeopleAndDomains combines domain restrictions and people enrichment.
func NewServiceWithPeopleAndDomains(connector Connector, people PeopleEnricher, domains []string) *Service {
	svc := NewServiceWithWorkspaceDomains(connector, domains)
	svc.peopleEnricher = people
	return svc
}

type ListMessagesInput struct {
	Space       string
	MaxResults  int64
	PageToken   string
	ShowDeleted bool
}

type ListSpacesInput struct {
	MaxResults int64
	PageToken  string
}

type ListSpacesOutput struct {
	Spaces        []chatconnector.Space
	NextPageToken string
}

type ListMembersInput struct {
	Space      string
	MaxResults int64
	PageToken  string
}

type ListMembersOutput struct {
	Members       []chatconnector.Membership
	NextPageToken string
}

type FindSpacesByMembersInput struct {
	MemberUserNames []string
	MaxResults      int64
	PageToken       string
	SpaceType       string
}

type MatchedSpace struct {
	Space   chatconnector.Space
	Members []chatconnector.Membership
}

type FindSpacesByMembersOutput struct {
	Spaces        []MatchedSpace
	NextPageToken string
}

type ListMessagesOutput struct {
	Messages      []chatconnector.Message
	NextPageToken string
}

type SendMessageInput struct {
	Space              string
	RecipientEmail     string // DM flow: find-or-create DM space automatically; mutually exclusive with Space
	Text               string
	ThreadName         string
	ThreadKey          string
	MessageReplyOption string
	MessageID          string
	RequestID          string
	Attachments        []string
	CardTitle          string
	CardSubtitle       string
	CardText           string
}

type SendMessageOutput struct {
	Message chatconnector.Message
}

type UpdateMessageInput struct {
	Name string
	Text string
}

type UpdateMessageOutput struct {
	Message chatconnector.Message
}

type DeleteMessageInput struct {
	Name  string
	Force bool
}

type DeleteMessageOutput struct {
	Name string
}

type CreateSpaceInput struct {
	DisplayName string
	SpaceType   string
	MemberUsers []string
	RequestID   string
}

type CreateSpaceOutput struct {
	Space chatconnector.Space
}

type AddMemberInput struct {
	Space string
	User  string   // single user; combined with Users if both provided
	Users []string // multiple users; use this when adding more than one member
}

type AddMemberOutput struct {
	Membership  chatconnector.Membership
	Memberships []chatconnector.Membership
}

type RemoveMemberInput struct {
	Name string
}

type RemoveMemberOutput struct {
	Name string
}

func (s *Service) ListSpaces(ctx context.Context, input ListSpacesInput) (ListSpacesOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListSpacesOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListSpacesOutput{}, errShape
	}
	output, err := s.connector.ListSpacesPage(ctx, maxResults, input.PageToken)
	if err != nil {
		return ListSpacesOutput{}, MapError(err)
	}
	return ListSpacesOutput{Spaces: output.Spaces, NextPageToken: output.NextPageToken}, nil
}

func (s *Service) ListMembers(ctx context.Context, input ListMembersInput) (ListMembersOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListMembersOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Space) == "" {
		return ListMembersOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space is required"}
	}
	space := normalizeSpaceName(input.Space)
	if space == "" {
		return ListMembersOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space must contain a resource name like spaces/AAAA"}
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListMembersOutput{}, errShape
	}
	output, err := s.connector.ListMembers(ctx, space, maxResults, input.PageToken)
	if err != nil {
		return ListMembersOutput{}, MapError(err)
	}
	members := output.Members
	if s.peopleEnricher != nil {
		members = s.enrichMembersWithEmails(ctx, members)
	}
	return ListMembersOutput{Members: members, NextPageToken: output.NextPageToken}, nil
}

// enrichMembersWithEmails resolves email addresses for members that have none.
// Strategy (best-effort, falls through on any error):
//  1. If MemberName is "users/<numericID>": call GetPerson("people/<numericID>") directly.
//  2. If DisplayName is present: call SearchDirectoryPeople and match by CandidateUserName.
func (s *Service) enrichMembersWithEmails(ctx context.Context, members []chatconnector.Membership) []chatconnector.Membership {
	enriched := make([]chatconnector.Membership, len(members))
	copy(enriched, members)
	for i, member := range enriched {
		if member.Email != "" {
			continue
		}
		if email := s.resolveEmail(ctx, member); email != "" {
			enriched[i].Email = email
		}
	}
	return enriched
}

func (s *Service) resolveEmail(ctx context.Context, member chatconnector.Membership) string {
	// Strategy 1: numeric user ID → People resource name "people/<id>"
	if numericID, ok := strings.CutPrefix(member.MemberName, "users/"); ok {
		if numericID != "" && !strings.Contains(numericID, "@") {
			person, err := s.peopleEnricher.GetPerson(ctx, "people/"+numericID)
			if err == nil && len(person.EmailAddresses) > 0 {
				return person.EmailAddresses[0]
			}
		}
	}

	// Strategy 2: search by display name and match by CandidateUserName
	if displayName := strings.TrimSpace(member.DisplayName); displayName != "" && strings.TrimSpace(member.MemberName) != "" {
		result, err := s.peopleEnricher.SearchDirectoryPeople(ctx, displayName, 5, "")
		if err != nil {
			return ""
		}
		for _, person := range result.People {
			if len(person.EmailAddresses) > 0 && slices.Contains(person.CandidateUserNames, member.MemberName) {
				return person.EmailAddresses[0]
			}
		}
	}
	return ""
}

func (s *Service) FindSpacesByMembers(ctx context.Context, input FindSpacesByMembersInput) (FindSpacesByMembersOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return FindSpacesByMembersOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}

	memberNames := normalizeMemberUserNames(input.MemberUserNames)
	if len(memberNames) == 0 {
		return FindSpacesByMembersOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "memberUserNames is required"}
	}

	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return FindSpacesByMembersOutput{}, errShape
	}

	// Use unfiltered listing: the server-side spaceType filter for DIRECT_MESSAGE
	// does not return results under user OAuth. Local type filtering is applied below.
	spacesOutput, err := s.connector.ListSpacesPage(ctx, maxResults, input.PageToken)
	if err != nil {
		return FindSpacesByMembersOutput{}, MapError(err)
	}

	required := map[string]struct{}{}
	for _, memberName := range memberNames {
		required[strings.ToLower(memberName)] = struct{}{}
	}

	var matches []MatchedSpace
	for _, space := range spacesOutput.Spaces {
		if strings.TrimSpace(input.SpaceType) != "" && !spaceMatchesType(space, input.SpaceType) {
			continue
		}

		membersOutput, err := s.connector.ListMembers(ctx, space.Name, maxAllowedResults, "")
		if err != nil {
			return FindSpacesByMembersOutput{}, MapError(err)
		}
		if membershipsContainAll(membersOutput.Members, required) {
			matches = append(matches, MatchedSpace{
				Space:   space,
				Members: membersOutput.Members,
			})
		}
	}

	return FindSpacesByMembersOutput{Spaces: matches, NextPageToken: spacesOutput.NextPageToken}, nil
}

func (s *Service) ListMessages(ctx context.Context, input ListMessagesInput) (ListMessagesOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListMessagesOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Space) == "" {
		return ListMessagesOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space is required"}
	}
	space := normalizeSpaceName(input.Space)
	if space == "" {
		return ListMessagesOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space must contain a resource name like spaces/AAAA"}
	}

	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListMessagesOutput{}, errShape
	}

	output, err := s.connector.ListMessages(ctx, space, maxResults, input.PageToken, input.ShowDeleted)
	if err != nil {
		return ListMessagesOutput{}, MapError(err)
	}
	return ListMessagesOutput{
		Messages:      output.Messages,
		NextPageToken: output.NextPageToken,
	}, nil
}

// findOrCreateDMSpace finds an existing DM space with the recipient or creates one if none exists.
// recipientEmail can be a plain email or users/... resource name.
func (s *Service) findOrCreateDMSpace(ctx context.Context, recipientEmail string) (string, *ErrorShape) {
	memberUserNames := normalizeMemberUserNames([]string{recipientEmail})
	if len(memberUserNames) == 0 {
		return "", &ErrorShape{Code: "INVALID_INPUT", Message: "recipientEmail is invalid"}
	}
	found, errShape := s.FindSpacesByMembers(ctx, FindSpacesByMembersInput{
		MemberUserNames: memberUserNames,
		SpaceType:       "DIRECT_MESSAGE",
	})
	if errShape != nil {
		return "", errShape
	}
	if len(found.Spaces) > 0 {
		return found.Spaces[0].Space.Name, nil
	}
	// No existing DM — create one. CreateSpace validates workspace domain.
	created, errShape := s.CreateSpace(ctx, CreateSpaceInput{
		SpaceType:   "DIRECT_MESSAGE",
		MemberUsers: []string{recipientEmail},
	})
	if errShape != nil {
		return "", errShape
	}
	return created.Space.Name, nil
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (SendMessageOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return SendMessageOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	space := normalizeSpaceName(input.Space)
	if space == "" {
		recipientEmail := strings.TrimSpace(input.RecipientEmail)
		if recipientEmail == "" {
			return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space or recipientEmail is required"}
		}
		resolved, errShape := s.findOrCreateDMSpace(ctx, recipientEmail)
		if errShape != nil {
			return SendMessageOutput{}, errShape
		}
		space = resolved
	}
	if strings.TrimSpace(input.CardTitle) != "" || strings.TrimSpace(input.CardText) != "" || strings.TrimSpace(input.CardSubtitle) != "" {
		return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "Google Chat card messages are not supported by the current user OAuth flow; send a text message instead"}
	}
	if strings.TrimSpace(input.Text) == "" && len(cleanStrings(input.Attachments)) == 0 {
		return SendMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "text or attachments is required"}
	}

	options := chatconnector.MessageCreateOptions{
		ThreadName:         input.ThreadName,
		ThreadKey:          input.ThreadKey,
		MessageReplyOption: input.MessageReplyOption,
		MessageID:          input.MessageID,
		RequestID:          input.RequestID,
	}
	uploadRefs, errShape := s.uploadAttachments(ctx, space, input.Attachments)
	if errShape != nil {
		return SendMessageOutput{}, errShape
	}
	options.AttachmentUploadRefs = uploadRefs

	message, err := s.connector.CreateTextMessage(ctx, space, input.Text, options)
	if err != nil {
		return SendMessageOutput{}, MapError(err)
	}
	return SendMessageOutput{Message: message}, nil
}

func (s *Service) UpdateMessage(ctx context.Context, input UpdateMessageInput) (UpdateMessageOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return UpdateMessageOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Name) == "" {
		return UpdateMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "name is required"}
	}
	if strings.TrimSpace(input.Text) == "" {
		return UpdateMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "text is required"}
	}
	message, err := s.connector.UpdateTextMessage(ctx, input.Name, input.Text)
	if err != nil {
		return UpdateMessageOutput{}, MapError(err)
	}
	return UpdateMessageOutput{Message: message}, nil
}

func (s *Service) DeleteMessage(ctx context.Context, input DeleteMessageInput) (DeleteMessageOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return DeleteMessageOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Name) == "" {
		return DeleteMessageOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "name is required"}
	}
	if err := s.connector.DeleteMessage(ctx, input.Name, input.Force); err != nil {
		return DeleteMessageOutput{}, MapError(err)
	}
	return DeleteMessageOutput{Name: input.Name}, nil
}

func (s *Service) CreateSpace(ctx context.Context, input CreateSpaceInput) (CreateSpaceOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return CreateSpaceOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	spaceType := strings.ToUpper(strings.TrimSpace(input.SpaceType))
	if spaceType == "" {
		spaceType = "SPACE"
	}
	if spaceType != "SPACE" && spaceType != "GROUP_CHAT" && spaceType != "DIRECT_MESSAGE" {
		return CreateSpaceOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "spaceType must be SPACE, GROUP_CHAT, or DIRECT_MESSAGE"}
	}
	memberUsers := cleanStrings(input.MemberUsers)
	if errShape := validateCreateSpaceMembers(spaceType, memberUsers); errShape != nil {
		return CreateSpaceOutput{}, errShape
	}
	if errShape := s.validateWorkspaceMemberEmails(memberUsers); errShape != nil {
		return CreateSpaceOutput{}, errShape
	}

	space, err := s.connector.CreateSpace(ctx, chatconnector.CreateSpaceInput{
		DisplayName: strings.TrimSpace(input.DisplayName),
		SpaceType:   spaceType,
		MemberUsers: memberUsers,
		RequestID:   strings.TrimSpace(input.RequestID),
	})
	if err != nil {
		return CreateSpaceOutput{}, MapError(err)
	}
	return CreateSpaceOutput{Space: space}, nil
}

func (s *Service) AddMember(ctx context.Context, input AddMemberInput) (AddMemberOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return AddMemberOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	space := normalizeSpaceName(input.Space)
	if space == "" {
		return AddMemberOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "space must contain a resource name like spaces/AAAA"}
	}
	users := cleanStrings(append([]string{input.User}, input.Users...))
	if len(users) == 0 {
		return AddMemberOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "user or users is required"}
	}
	if errShape := s.validateWorkspaceMemberEmails(users); errShape != nil {
		return AddMemberOutput{}, errShape
	}
	memberships := make([]chatconnector.Membership, 0, len(users))
	for _, user := range users {
		membership, err := s.connector.AddMember(ctx, space, user)
		if err != nil {
			return AddMemberOutput{}, MapError(err)
		}
		memberships = append(memberships, membership)
	}
	first := chatconnector.Membership{}
	if len(memberships) > 0 {
		first = memberships[0]
	}
	return AddMemberOutput{Membership: first, Memberships: memberships}, nil
}

func (s *Service) RemoveMember(ctx context.Context, input RemoveMemberInput) (RemoveMemberOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return RemoveMemberOutput{}, &ErrorShape{Code: "INTERNAL_ERROR", Message: "chat connector is not configured"}
	}
	if strings.TrimSpace(input.Name) == "" {
		return RemoveMemberOutput{}, &ErrorShape{Code: "INVALID_INPUT", Message: "name is required"}
	}
	if err := s.connector.RemoveMember(ctx, input.Name); err != nil {
		return RemoveMemberOutput{}, MapError(err)
	}
	return RemoveMemberOutput{Name: input.Name}, nil
}

func (s *Service) uploadAttachments(ctx context.Context, space string, paths []string) ([]string, *ErrorShape) {
	cleaned := cleanStrings(paths)
	if len(cleaned) == 0 {
		return nil, nil
	}
	if len(cleaned) > maxAttachments {
		return nil, &ErrorShape{Code: "INVALID_INPUT", Message: fmt.Sprintf("attachments must contain at most %d files", maxAttachments)}
	}
	totalSize := int64(0)
	refs := make([]string, 0, len(cleaned))
	for _, path := range cleaned {
		info, err := os.Stat(path)
		if err != nil {
			return nil, &ErrorShape{Code: "INVALID_INPUT", Message: "attachment not found: " + path}
		}
		if info.IsDir() {
			return nil, &ErrorShape{Code: "INVALID_INPUT", Message: "attachment must be a file: " + path}
		}
		totalSize += info.Size()
		if totalSize > maxAttachmentRaw {
			return nil, &ErrorShape{Code: "INVALID_INPUT", Message: fmt.Sprintf("total attachment size must be at most %d bytes", maxAttachmentRaw)}
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, &ErrorShape{Code: "FILE_ACCESS_DENIED", Message: "read attachment: " + err.Error()}
		}
		filename := safeAttachmentFilename(path)
		mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		if strings.TrimSpace(mediaType) == "" {
			mediaType = "application/octet-stream"
		}
		token, uploadErr := s.connector.UploadAttachment(ctx, space, filename, mediaType, file)
		closeErr := file.Close()
		if uploadErr != nil {
			return nil, MapError(uploadErr)
		}
		if closeErr != nil {
			return nil, &ErrorShape{Code: "INTERNAL_ERROR", Message: "close attachment: " + closeErr.Error()}
		}
		refs = append(refs, token)
	}
	return refs, nil
}

func normalizeMaxResults(value int64) (int64, *ErrorShape) {
	if value == 0 {
		return defaultMaxResults, nil
	}
	if value < 1 || value > maxAllowedResults {
		return 0, &ErrorShape{
			Code:    "INVALID_INPUT",
			Message: fmt.Sprintf("maxResults must be between 1 and %d", maxAllowedResults),
		}
	}
	return value, nil
}

type ListSpacesTool struct {
	service *Service
}

func NewListSpacesTool(service *Service) ListSpacesTool {
	return ListSpacesTool{service: service}
}

func (ListSpacesTool) Name() string {
	return ToolNameListSpaces
}

func (ListSpacesTool) Description() string {
	return "List Google Chat spaces available to the authenticated user. Use this to resolve a user-provided group/space display name, for example VClaw, before calling chat.sendMessage, chat.listMessages, or chat.listMembers with a spaces/... resource."
}

func (ListSpacesTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
}

func (ListSpacesTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (ListSpacesTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t ListSpacesTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.ListSpaces(ctx, ListSpacesInput{
		MaxResults: boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
		PageToken:  stringArg(call.Arguments, "pageToken"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, space := range output.Spaces {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | %s", space.Name, emptyValue(space.DisplayName, "(no display name)"), emptyValue(space.SpaceType, emptyValue(space.Type, "(no type)")), space.SpaceURI))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Google Chat spaces found.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type ListMembersTool struct {
	service *Service
}

func NewListMembersTool(service *Service) ListMembersTool {
	return ListMembersTool{service: service}
}

func (ListMembersTool) Name() string {
	return ToolNameListMembers
}

func (ListMembersTool) Description() string {
	return "List members in a Google Chat space. Use this to identify which unnamed space contains people mentioned by the user."
}

func (ListMembersTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":      map[string]any{"type": "string"},
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
		},
		"required":             []string{"space"},
		"additionalProperties": false,
	}
}

func (ListMembersTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (ListMembersTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t ListMembersTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.ListMembers(ctx, ListMembersInput{
		Space:      stringArg(call.Arguments, "space"),
		MaxResults: boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
		PageToken:  stringArg(call.Arguments, "pageToken"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, member := range output.Members {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | %s | %s", member.Name, member.MemberName, emptyValue(member.DisplayName, "(no display name)"), emptyValue(member.Email, "(no email)"), member.MemberType))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Chat members found.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type FindSpacesByMembersTool struct {
	service *Service
}

func NewFindSpacesByMembersTool(service *Service) FindSpacesByMembersTool {
	return FindSpacesByMembersTool{service: service}
}

func (FindSpacesByMembersTool) Name() string {
	return ToolNameFindSpacesByMembers
}

func (FindSpacesByMembersTool) Description() string {
	return "Find Google Chat spaces that include specific members. Use this after people.searchDirectory to resolve a person name to users/... resource names, then call this tool to find the space. When the user wants to send a message or file directly to a person (not a group), pass spaceType=DIRECT_MESSAGE to find the DM space. If the result is empty (no existing DM), call chat.createSpace with spaceType=DIRECT_MESSAGE and the person's email to create it, then use the returned space for chat.sendMessage."
}

func (FindSpacesByMembersTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"memberUserNames": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			"maxResults": map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":  map[string]any{"type": "string"},
			"spaceType":  map[string]any{"type": "string", "enum": []string{"SPACE", "GROUP_CHAT", "DIRECT_MESSAGE"}},
		},
		"required":             []string{"memberUserNames"},
		"additionalProperties": false,
	}
}

func (FindSpacesByMembersTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (FindSpacesByMembersTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSafeRead
}

func (t FindSpacesByMembersTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.FindSpacesByMembers(ctx, FindSpacesByMembersInput{
		MemberUserNames: stringSliceArg(call.Arguments, "memberUserNames"),
		MaxResults:      boundedInt64Arg(call.Arguments, "maxResults", maxAllowedResults, maxAllowedResults),
		PageToken:       stringArg(call.Arguments, "pageToken"),
		SpaceType:       stringArg(call.Arguments, "spaceType"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, match := range output.Spaces {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s | members: %s | %s",
			match.Space.Name,
			emptyValue(match.Space.DisplayName, "(no display name)"),
			emptyValue(match.Space.SpaceType, emptyValue(match.Space.Type, "(no type)")),
			memberNamesForOutput(match.Members),
			match.Space.SpaceURI,
		))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Google Chat spaces matched the requested members.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type ListMessagesTool struct {
	service *Service
}

func NewListMessagesTool(service *Service) ListMessagesTool {
	return ListMessagesTool{service: service}
}

func (ListMessagesTool) Name() string {
	return ToolNameListMessages
}

func (ListMessagesTool) Description() string {
	return "List Google Chat messages from a space. The space input must be a spaces/... resource name; if the user names a person or group instead, call people.searchDirectory and chat.findSpacesByMembers first."
}

func (ListMessagesTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":       map[string]any{"type": "string"},
			"maxResults":  map[string]any{"type": "number", "minimum": 1, "maximum": maxAllowedResults},
			"pageToken":   map[string]any{"type": "string"},
			"showDeleted": map[string]any{"type": "boolean"},
		},
		"required":             []string{"space"},
		"additionalProperties": false,
	}
}

func (ListMessagesTool) Capability() tools.Capability {
	return tools.CapabilityReadOnly
}

func (ListMessagesTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelSensitiveRead
}

func (t ListMessagesTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.ListMessages(ctx, ListMessagesInput{
		Space:       stringArg(call.Arguments, "space"),
		MaxResults:  boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults),
		PageToken:   stringArg(call.Arguments, "pageToken"),
		ShowDeleted: boolArg(call.Arguments, "showDeleted"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	lines := []string{}
	for _, message := range output.Messages {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", message.Name, message.Sender, message.Text))
	}
	if len(lines) == 0 {
		lines = append(lines, "No Chat messages found.")
	}
	if strings.TrimSpace(output.NextPageToken) != "" {
		lines = append(lines, "Next page token: "+output.NextPageToken)
	}
	content := strings.Join(lines, "\n")
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

type SendMessageTool struct {
	service *Service
}

func NewSendMessageTool(service *Service) SendMessageTool {
	return SendMessageTool{service: service}
}

func (SendMessageTool) Name() string {
	return ToolNameSendMessage
}

func (SendMessageTool) Description() string {
	return "Send a Google Chat text message or attachment. " +
		"For DM to a specific person (e.g. Bao Le): provide recipientEmail with their email address — the tool automatically finds or creates the DM space. Do NOT call chat.findSpacesByMembers or chat.createSpace separately for DM. " +
		"For a group chat or named space (e.g. VClaw): resolve the space name with chat.listSpaces, then provide the spaces/... resource name in the space parameter. " +
		"Never reuse a spaces/... value from history for a different recipient. This external write requires approval."
}

func (SendMessageTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space":              map[string]any{"type": "string", "description": "spaces/... resource name — for group chats and named spaces"},
			"recipientEmail":     map[string]any{"type": "string", "description": "email of the recipient — for DM; the tool finds or creates the DM space automatically"},
			"text":               map[string]any{"type": "string"},
			"threadName":         map[string]any{"type": "string"},
			"threadKey":          map[string]any{"type": "string"},
			"messageReplyOption": map[string]any{"type": "string"},
			"messageId":          map[string]any{"type": "string"},
			"requestId":          map[string]any{"type": "string"},
			"attachments":        arrayStringSchema(),
		},
		"additionalProperties": false,
	}
}

func (SendMessageTool) Capability() tools.Capability {
	return tools.CapabilityMutating
}

func (SendMessageTool) RiskLevel() tools.RiskLevel {
	return tools.RiskLevelExternalWrite
}

func (t SendMessageTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	recipientEmail := stringArg(call.Arguments, "recipientEmail")
	output, errShape := t.service.SendMessage(ctx, SendMessageInput{
		Space:              stringArg(call.Arguments, "space"),
		RecipientEmail:     recipientEmail,
		Text:               stringArg(call.Arguments, "text"),
		ThreadName:         stringArg(call.Arguments, "threadName"),
		ThreadKey:          stringArg(call.Arguments, "threadKey"),
		MessageReplyOption: stringArg(call.Arguments, "messageReplyOption"),
		MessageID:          stringArg(call.Arguments, "messageId"),
		RequestID:          stringArg(call.Arguments, "requestId"),
		Attachments:        stringSliceArg(call.Arguments, "attachments"),
		CardTitle:          stringArg(call.Arguments, "cardTitle"),
		CardSubtitle:       stringArg(call.Arguments, "cardSubtitle"),
		CardText:           stringArg(call.Arguments, "cardText"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}

	content := "Sent Google Chat message: " + output.Message.Name
	hasAttachments := len(stringSliceArg(call.Arguments, "attachments")) > 0
	userContent := "Đã gửi tin nhắn Google Chat."
	if strings.TrimSpace(recipientEmail) != "" {
		userContent = "Đã gửi tin nhắn Google Chat đến " + strings.TrimSpace(recipientEmail) + "."
	}
	if hasAttachments {
		userContent = "Đã gửi tin nhắn kèm file lên Google Chat."
		if strings.TrimSpace(stringArg(call.Arguments, "text")) == "" {
			userContent = "Đã gửi file lên Google Chat."
		}
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: userContent,
		ArtifactRef:    chatMessageArtifactRef(output.Message.Name),
	}
}

// chatMessageArtifactRef returns a typed reference to a sent Google Chat message.
func chatMessageArtifactRef(name string) *tools.ToolArtifactRef {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return &tools.ToolArtifactRef{
		Kind:  "chat.message",
		Label: "Google Chat message",
		ID:    name,
	}
}

type UpdateMessageTool struct {
	service *Service
}

func NewUpdateMessageTool(service *Service) UpdateMessageTool {
	return UpdateMessageTool{service: service}
}

func (UpdateMessageTool) Name() string { return ToolNameUpdateMessage }

func (UpdateMessageTool) Description() string {
	return "Update the text of a Google Chat message. This external write requires approval."
}

func (UpdateMessageTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"text": map[string]any{"type": "string"},
		},
		"required":             []string{"name", "text"},
		"additionalProperties": false,
	}
}

func (UpdateMessageTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (UpdateMessageTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t UpdateMessageTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.UpdateMessage(ctx, UpdateMessageInput{
		Name: stringArg(call.Arguments, "name"),
		Text: stringArg(call.Arguments, "text"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	content := "Updated Google Chat message: " + output.Message.Name
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type DeleteMessageTool struct {
	service *Service
}

func NewDeleteMessageTool(service *Service) DeleteMessageTool {
	return DeleteMessageTool{service: service}
}

func (DeleteMessageTool) Name() string { return ToolNameDeleteMessage }

func (DeleteMessageTool) Description() string {
	return "Delete a Google Chat message. This destructive action requires approval."
}

func (DeleteMessageTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"force": map[string]any{"type": "boolean"},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func (DeleteMessageTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (DeleteMessageTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelDestructive }

func (t DeleteMessageTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.DeleteMessage(ctx, DeleteMessageInput{
		Name:  stringArg(call.Arguments, "name"),
		Force: boolArg(call.Arguments, "force"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	content := "Deleted Google Chat message: " + output.Name
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type CreateSpaceTool struct {
	service *Service
}

func NewCreateSpaceTool(service *Service) CreateSpaceTool {
	return CreateSpaceTool{service: service}
}

func (CreateSpaceTool) Name() string { return ToolNameCreateSpace }

func (CreateSpaceTool) Description() string {
	return "Create or set up a Google Chat space, group chat, or direct message. Use spaceType=DIRECT_MESSAGE with memberUsers=[email] to open a DM with a person when chat.findSpacesByMembers returns no result. This external write requires approval."
}

func (CreateSpaceTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"displayName": map[string]any{"type": "string"},
			"spaceType":   map[string]any{"type": "string", "enum": []string{"SPACE", "GROUP_CHAT", "DIRECT_MESSAGE"}},
			"memberUsers": arrayStringSchema(),
			"requestId":   map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
}

func (CreateSpaceTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (CreateSpaceTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t CreateSpaceTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.CreateSpace(ctx, CreateSpaceInput{
		DisplayName: stringArg(call.Arguments, "displayName"),
		SpaceType:   stringArg(call.Arguments, "spaceType"),
		MemberUsers: stringSliceArg(call.Arguments, "memberUsers"),
		RequestID:   stringArg(call.Arguments, "requestId"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	content := fmt.Sprintf("Created Google Chat space: %s | %s | %s", output.Space.Name, output.Space.DisplayName, output.Space.SpaceType)
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type AddMemberTool struct {
	service *Service
}

func NewAddMemberTool(service *Service) AddMemberTool {
	return AddMemberTool{service: service}
}

func (AddMemberTool) Name() string { return ToolNameAddMember }

func (AddMemberTool) Description() string {
	return "Add one or more human members to a Google Chat space. Always pass all target member emails in the users array — do not call this tool once per member. This external write requires approval."
}

func (AddMemberTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"space": map[string]any{"type": "string"},
			"users": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "minLength": 1},
				"minItems":    1,
				"description": "Email addresses of all members to add. Include every target member in one call.",
			},
		},
		"required":             []string{"space", "users"},
		"additionalProperties": false,
	}
}

func (AddMemberTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (AddMemberTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelExternalWrite }

func (t AddMemberTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.AddMember(ctx, AddMemberInput{
		Space: stringArg(call.Arguments, "space"),
		Users: stringSliceArg(call.Arguments, "users"),
	})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	names := make([]string, 0, len(output.Memberships))
	for _, m := range output.Memberships {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	content := "Added Google Chat member: " + strings.Join(names, ", ")
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

type RemoveMemberTool struct {
	service *Service
}

func NewRemoveMemberTool(service *Service) RemoveMemberTool {
	return RemoveMemberTool{service: service}
}

func (RemoveMemberTool) Name() string { return ToolNameRemoveMember }

func (RemoveMemberTool) Description() string {
	return "Remove a member from a Google Chat space. This destructive action requires approval."
}

func (RemoveMemberTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func (RemoveMemberTool) Capability() tools.Capability { return tools.CapabilityMutating }

func (RemoveMemberTool) RiskLevel() tools.RiskLevel { return tools.RiskLevelDestructive }

func (t RemoveMemberTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	output, errShape := t.service.RemoveMember(ctx, RemoveMemberInput{Name: stringArg(call.Arguments, "name")})
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	content := "Removed Google Chat member: " + output.Name
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: content, ContentForUser: content}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, tool := range []tools.Tool{
		NewListSpacesTool(service),
		NewListMembersTool(service),
		NewFindSpacesByMembersTool(service),
		NewListMessagesTool(service),
		NewSendMessageTool(service),
		NewUpdateMessageTool(service),
		NewDeleteMessageTool(service),
		NewCreateSpaceTool(service),
		NewAddMemberTool(service),
		NewRemoveMemberTool(service),
	} {
		if err := registry.RegisterWithEntry(tool, tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

func arrayStringSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	return cleanStrings(parts)
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func safeAttachmentFilename(path string) string {
	filename := filepath.Base(strings.TrimSpace(path))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "attachment.dat"
	}
	filename = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, filename)
	if strings.Trim(filename, "._ ") == "" {
		return "attachment.dat"
	}
	return filename
}

func validateCreateSpaceMembers(spaceType string, users []string) *ErrorShape {
	if strings.EqualFold(spaceType, "GROUP_CHAT") {
		uniqueMembers := map[string]struct{}{}
		for _, user := range users {
			value := strings.ToLower(strings.TrimSpace(user))
			if value == "" {
				continue
			}
			uniqueMembers[value] = struct{}{}
		}
		if len(uniqueMembers) < 2 {
			return &ErrorShape{Code: "INVALID_INPUT", Message: "GROUP_CHAT requires at least 2 unique members; use DIRECT_MESSAGE for one other person"}
		}
	}
	return nil
}

func (s *Service) validateWorkspaceMemberEmails(users []string) *ErrorShape {
	users = cleanStrings(users)
	if len(users) == 0 {
		return nil
	}
	allowed := normalizedDomains(s.workspaceDomains)
	if len(allowed) == 0 {
		return &ErrorShape{Code: "INVALID_INPUT", Message: "workspace domain restriction requires VCLAW_GOOGLE_WORKSPACE_DOMAINS when adding Chat members"}
	}
	for _, user := range users {
		email, errShape := memberEmailForDomainCheck(user)
		if errShape != nil {
			return errShape
		}
		domain := emailDomain(email)
		if _, ok := allowed[domain]; !ok {
			return &ErrorShape{Code: "ACTION_BLOCKED_BY_POLICY", Message: fmt.Sprintf("member %q is outside the allowed Workspace domains: %s", user, strings.Join(mapKeys(allowed), ","))}
		}
	}
	return nil
}

func normalizedDomains(domains []string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, domain := range domains {
		value := strings.ToLower(strings.TrimSpace(domain))
		value = strings.TrimPrefix(value, "@")
		if value != "" {
			allowed[value] = struct{}{}
		}
	}
	return allowed
}

func memberEmailForDomainCheck(user string) (string, *ErrorShape) {
	value := strings.TrimSpace(user)
	value = strings.TrimPrefix(value, "users/")
	if value == "" {
		return "", &ErrorShape{Code: "INVALID_INPUT", Message: "member email is required"}
	}
	if !strings.Contains(value, "@") || strings.ContainsAny(value, " \t\r\n") || emailDomain(value) == "" {
		return "", &ErrorShape{Code: "INVALID_INPUT", Message: fmt.Sprintf("member %q must be an email address so the Workspace domain can be verified", user)}
	}
	return strings.ToLower(value), nil
}

func emailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[1]))
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func emptyValue(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeSpaceName(value string) string {
	value = strings.TrimSpace(value)
	start := strings.Index(value, "spaces/")
	if start < 0 {
		return ""
	}
	value = value[start:]
	end := len(value)
	for index, r := range value {
		if index == 0 {
			continue
		}
		if r == '|' || r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			end = index
			break
		}
	}
	space := strings.TrimSpace(value[:end])
	if !isUsableSpaceName(space) {
		return ""
	}
	return space
}

func isUsableSpaceName(space string) bool {
	space = strings.TrimSpace(space)
	resourceID, ok := strings.CutPrefix(space, "spaces/")
	if !ok {
		return false
	}
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return false
	}
	if strings.ContainsAny(resourceID, "{}<>") {
		return false
	}
	switch strings.ToUpper(resourceID) {
	case "UNKNOWN", "PLACEHOLDER", "REPLACE_ME", "SPACE", "SPACE_ID", "SPACEID":
		return false
	default:
		return true
	}
}

func normalizeMemberUserNames(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, "users/") {
			value = "users/" + value
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

// spaceMatchesType checks both SpaceType (new API field) and Type (legacy field)
// so DM spaces returned with Type="DM" are matched by spaceType="DIRECT_MESSAGE".
func spaceMatchesType(space chatconnector.Space, spaceType string) bool {
	if strings.EqualFold(space.SpaceType, spaceType) {
		return true
	}
	// Legacy field mapping: DM → DIRECT_MESSAGE, ROOM → SPACE
	switch strings.ToUpper(spaceType) {
	case "DIRECT_MESSAGE":
		return strings.EqualFold(space.Type, "DM")
	case "SPACE":
		return strings.EqualFold(space.Type, "ROOM")
	}
	return false
}

func membershipsContainAll(members []chatconnector.Membership, required map[string]struct{}) bool {
	if len(required) == 0 {
		return false
	}
	found := map[string]struct{}{}
	for _, member := range members {
		value := strings.ToLower(strings.TrimSpace(member.MemberName))
		if value == "" {
			continue
		}
		if _, ok := required[value]; ok {
			found[value] = struct{}{}
		}
	}
	return len(found) == len(required)
}

func memberNamesForOutput(members []chatconnector.Membership) string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		name := strings.TrimSpace(member.MemberName)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "(no members)"
	}
	return strings.Join(names, ",")
}

func MapError(err error) *ErrorShape {
	if err == nil {
		return nil
	}
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting Chat API: " + err.Error(), Retryable: true}
	}
	gerr, ok := err.(*googleapi.Error)
	if !ok {
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: err.Error()}
	}
	message := googleAPIErrorMessage(gerr)

	switch {
	case gerr.Code == http.StatusUnauthorized:
		return &ErrorShape{Code: "AUTH_EXPIRED", Message: message, Retryable: true}
	case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
		return &ErrorShape{Code: "AUTH_MISSING_SCOPE", Message: message}
	case gerr.Code == http.StatusBadRequest || gerr.Code == http.StatusNotFound:
		return &ErrorShape{Code: "INVALID_INPUT", Message: message}
	case gerr.Code == http.StatusTooManyRequests:
		return &ErrorShape{Code: "RATE_LIMITED", Message: message, Retryable: true}
	case gerr.Code >= 500:
		return &ErrorShape{Code: "PROVIDER_UNAVAILABLE", Message: message, Retryable: true}
	default:
		return &ErrorShape{Code: "INTERNAL_ERROR", Message: message}
	}
}

func googleAPIErrorMessage(err *googleapi.Error) string {
	if err == nil {
		return "Google API error"
	}
	if strings.TrimSpace(err.Message) != "" {
		return err.Message
	}
	if strings.TrimSpace(err.Body) != "" {
		return err.Body
	}
	if strings.TrimSpace(err.Error()) != "" {
		return err.Error()
	}
	return fmt.Sprintf("Google API error status %d", err.Code)
}

func hasMissingScopeReason(err *googleapi.Error) bool {
	text := strings.ToLower(err.Message)
	return strings.Contains(text, "insufficient authentication scopes") ||
		strings.Contains(text, "insufficient permissions")
}

func toolErrorResult(call tools.ToolCall, errShape *ErrorShape) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  errShape.Code + ": " + errShape.Message,
		ContentForUser: errShape.Message,
		Error: &tools.ToolError{
			Code:    errShape.Code,
			Message: errShape.Message,
		},
	}
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	value, _ := args[name].(string)
	return value
}

func boolArg(args map[string]any, name string) bool {
	if args == nil {
		return false
	}
	value, _ := args[name].(bool)
	return value
}

func stringSliceArg(args map[string]any, name string) []string {
	if args == nil {
		return nil
	}
	switch value := args[name].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func int64Arg(args map[string]any, name string) int64 {
	if args == nil {
		return 0
	}
	switch value := args[name].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func boundedInt64Arg(args map[string]any, name string, fallback int64, max int64) int64 {
	value := int64Arg(args, name)
	if value < 1 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
