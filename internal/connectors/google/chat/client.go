package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type Space struct {
	Name        string
	DisplayName string
	Type        string
	SpaceType   string
	SpaceURI    string
}

type Message struct {
	Name           string
	Text           string
	FormattedText  string
	Sender         string
	CreateTime     string
	ThreadName     string
	ThreadKey      string
	ThreadReply    bool
	AttachmentName string
}

type Membership struct {
	Name        string
	MemberName  string
	MemberType  string
	DisplayName string
	Email       string
	State       string
	Role        string
}

type MessageCreateOptions struct {
	ThreadName           string
	ThreadKey            string
	MessageReplyOption   string
	MessageID            string
	RequestID            string
	AttachmentUploadRefs []string
}

type CardMessage struct {
	Title    string
	Subtitle string
	Text     string
}

type CreateSpaceInput struct {
	DisplayName string
	SpaceType   string
	MemberUsers []string
	RequestID   string
}

type ListMessagesOutput struct {
	Messages      []Message
	NextPageToken string
}

type ListSpacesOutput struct {
	Spaces        []Space
	NextPageToken string
}

type ListMembersOutput struct {
	Members       []Membership
	NextPageToken string
}

func (c *Client) ListSpaces(ctx context.Context, pageSize int64) ([]Space, error) {
	output, err := ListSpaces(ctx, c.httpClient, pageSize, "", "")
	return output.Spaces, err
}

func (c *Client) ListSpacesPage(ctx context.Context, pageSize int64, pageToken string) (ListSpacesOutput, error) {
	return ListSpaces(ctx, c.httpClient, pageSize, pageToken, "")
}

func (c *Client) ListSpacesPageFiltered(ctx context.Context, pageSize int64, pageToken string, spaceTypeFilter string) (ListSpacesOutput, error) {
	return ListSpaces(ctx, c.httpClient, pageSize, pageToken, spaceTypeFilter)
}

func (c *Client) ListMessages(ctx context.Context, parent string, pageSize int64, pageToken string, showDeleted bool) (ListMessagesOutput, error) {
	return ListMessages(ctx, c.httpClient, parent, pageSize, pageToken, showDeleted)
}

func (c *Client) ListMembers(ctx context.Context, parent string, pageSize int64, pageToken string) (ListMembersOutput, error) {
	return ListMembers(ctx, c.httpClient, parent, pageSize, pageToken)
}

func (c *Client) CreateTextMessage(ctx context.Context, parent string, text string, options MessageCreateOptions) (Message, error) {
	return CreateTextMessage(ctx, c.httpClient, parent, text, options)
}

func (c *Client) CreateCardMessage(ctx context.Context, parent string, card CardMessage, options MessageCreateOptions) (Message, error) {
	return CreateCardMessage(ctx, c.httpClient, parent, card, options)
}

func (c *Client) UpdateTextMessage(ctx context.Context, name string, text string) (Message, error) {
	return UpdateTextMessage(ctx, c.httpClient, name, text)
}

func (c *Client) DeleteMessage(ctx context.Context, name string, force bool) error {
	return DeleteMessage(ctx, c.httpClient, name, force)
}

func (c *Client) CreateSpace(ctx context.Context, input CreateSpaceInput) (Space, error) {
	return CreateSpace(ctx, c.httpClient, input)
}

func (c *Client) AddMember(ctx context.Context, parent string, user string) (Membership, error) {
	return AddMember(ctx, c.httpClient, parent, user)
}

func (c *Client) RemoveMember(ctx context.Context, name string) error {
	return RemoveMember(ctx, c.httpClient, name)
}

func (c *Client) UploadAttachment(ctx context.Context, parent string, filename string, mediaType string, reader io.Reader) (string, error) {
	return UploadAttachment(ctx, c.httpClient, parent, filename, mediaType, reader)
}

func ListSpaces(ctx context.Context, client *http.Client, pageSize int64, pageToken string, spaceTypeFilter string) (ListSpacesOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ListSpacesOutput{}, err
	}

	call := service.Spaces.List().PageSize(pageSize)
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(pageToken)
	}
	if f := strings.TrimSpace(spaceTypeFilter); f != "" {
		call = call.Filter(`spaceType = "` + f + `"`)
	}
	response, err := call.Do()
	if err != nil {
		return ListSpacesOutput{}, err
	}

	spaces := make([]Space, 0, len(response.Spaces))
	for _, space := range response.Spaces {
		spaces = append(spaces, spaceFromAPI(space))
	}
	return ListSpacesOutput{Spaces: spaces, NextPageToken: response.NextPageToken}, nil
}

func ListMessages(ctx context.Context, client *http.Client, parent string, pageSize int64, pageToken string, showDeleted bool) (ListMessagesOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ListMessagesOutput{}, err
	}

	// Google Chat API returns messages in ascending createTime order (oldest first).
	// When the caller wants the most recent N messages (no pagination token), fetch
	// a larger batch and take the tail so recent messages are returned.
	fetchSize := pageSize
	isPaged := strings.TrimSpace(pageToken) != ""
	if !isPaged && pageSize <= 25 {
		fetchSize = 50
	}

	call := service.Spaces.Messages.List(parent).PageSize(fetchSize).ShowDeleted(showDeleted)
	if isPaged {
		call = call.PageToken(pageToken)
	}
	response, err := call.Do()
	if err != nil {
		return ListMessagesOutput{}, err
	}

	raw := response.Messages
	if !isPaged && int64(len(raw)) > pageSize {
		raw = raw[int64(len(raw))-pageSize:]
	}

	messages := make([]Message, len(raw))
	for i, message := range raw {
		messages[i] = messageFromAPI(message)
	}
	// Reverse so newest message appears first.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return ListMessagesOutput{
		Messages:      messages,
		NextPageToken: response.NextPageToken,
	}, nil
}

func ListMembers(ctx context.Context, client *http.Client, parent string, pageSize int64, pageToken string) (ListMembersOutput, error) {
	if strings.TrimSpace(parent) == "" {
		return ListMembersOutput{}, errors.New("space name is required")
	}
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ListMembersOutput{}, err
	}

	call := service.Spaces.Members.List(parent).PageSize(pageSize)
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(pageToken)
	}
	response, err := call.Do()
	if err != nil {
		return ListMembersOutput{}, err
	}

	members := make([]Membership, 0, len(response.Memberships))
	for _, membership := range response.Memberships {
		members = append(members, membershipFromAPI(membership))
	}
	return ListMembersOutput{Members: members, NextPageToken: response.NextPageToken}, nil
}

func CreateTextMessage(ctx context.Context, client *http.Client, parent string, text string, options MessageCreateOptions) (Message, error) {
	attachments := attachmentsFromUploadRefs(options.AttachmentUploadRefs)
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return Message{}, errors.New("text or attachments is required")
	}

	message := &chatapi.Message{
		Text:       text,
		Attachment: attachments,
	}
	if strings.TrimSpace(options.ThreadName) != "" {
		message.Thread = &chatapi.Thread{Name: options.ThreadName}
	}

	return createMessage(ctx, client, parent, message, options)
}

func CreateCardMessage(ctx context.Context, client *http.Client, parent string, card CardMessage, options MessageCreateOptions) (Message, error) {
	if strings.TrimSpace(card.Title) == "" {
		return Message{}, errors.New("card title is required")
	}
	if strings.TrimSpace(card.Text) == "" {
		return Message{}, errors.New("card text is required")
	}

	message := &chatapi.Message{
		Text: card.Title,
		CardsV2: []*chatapi.CardWithId{
			{
				CardId: "vclaw-card",
				Card: &chatapi.GoogleAppsCardV1Card{
					Header: &chatapi.GoogleAppsCardV1CardHeader{
						Title:    card.Title,
						Subtitle: card.Subtitle,
					},
					Sections: []*chatapi.GoogleAppsCardV1Section{
						{
							Widgets: []*chatapi.GoogleAppsCardV1Widget{
								{
									TextParagraph: &chatapi.GoogleAppsCardV1TextParagraph{
										Text: card.Text,
									},
								},
							},
						},
					},
				},
			},
		},
		Attachment: attachmentsFromUploadRefs(options.AttachmentUploadRefs),
	}
	if strings.TrimSpace(options.ThreadName) != "" {
		message.Thread = &chatapi.Thread{Name: options.ThreadName}
	}

	return createMessage(ctx, client, parent, message, options)
}

func UpdateTextMessage(ctx context.Context, client *http.Client, name string, text string) (Message, error) {
	if strings.TrimSpace(name) == "" {
		return Message{}, errors.New("message name is required")
	}
	if strings.TrimSpace(text) == "" {
		return Message{}, errors.New("text is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return Message{}, err
	}

	response, err := service.Spaces.Messages.Patch(name, &chatapi.Message{
		Name: name,
		Text: text,
	}).UpdateMask("text").Do()
	if err != nil {
		return Message{}, err
	}
	return messageFromAPI(response), nil
}

func DeleteMessage(ctx context.Context, client *http.Client, name string, force bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("message name is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return err
	}

	_, err = service.Spaces.Messages.Delete(name).Force(force).Do()
	return err
}

func CreateSpace(ctx context.Context, client *http.Client, input CreateSpaceInput) (Space, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return Space{}, err
	}

	spaceType := strings.TrimSpace(input.SpaceType)
	if spaceType == "" {
		spaceType = "SPACE"
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if spaceType == "DIRECT_MESSAGE" {
		displayName = ""
	}
	request := &chatapi.SetUpSpaceRequest{
		RequestId: strings.TrimSpace(input.RequestID),
		Space: &chatapi.Space{
			SpaceType: spaceType,
		},
		Memberships: membershipsFromUsers(input.MemberUsers),
	}
	if displayName != "" {
		request.Space.DisplayName = displayName
	}
	if spaceType == "SPACE" && displayName == "" {
		return Space{}, errors.New("displayName is required for SPACE")
	}

	response, err := service.Spaces.Setup(request).Do()
	if err != nil {
		return Space{}, err
	}
	return spaceFromAPI(response), nil
}

func AddMember(ctx context.Context, client *http.Client, parent string, user string) (Membership, error) {
	if strings.TrimSpace(parent) == "" {
		return Membership{}, errors.New("space name is required")
	}
	if strings.TrimSpace(user) == "" {
		return Membership{}, errors.New("user is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return Membership{}, err
	}

	response, err := service.Spaces.Members.Create(parent, membershipFromUser(user)).Do()
	if err != nil {
		return Membership{}, err
	}
	return membershipFromAPI(response), nil
}

func RemoveMember(ctx context.Context, client *http.Client, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("membership name is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return err
	}

	_, err = service.Spaces.Members.Delete(name).Do()
	return err
}

func UploadAttachment(ctx context.Context, client *http.Client, parent string, filename string, mediaType string, reader io.Reader) (string, error) {
	if strings.TrimSpace(parent) == "" {
		return "", errors.New("space name is required")
	}
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("filename is required")
	}
	if reader == nil {
		return "", errors.New("attachment reader is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/octet-stream"
	}

	response, err := service.Media.Upload(parent, &chatapi.UploadAttachmentRequest{
		Filename: filename,
	}).Media(reader, googleapi.ContentType(mediaType)).Do()
	if err != nil {
		return "", err
	}
	if response.AttachmentDataRef == nil || strings.TrimSpace(response.AttachmentDataRef.AttachmentUploadToken) == "" {
		return "", errors.New("attachment upload token missing from response")
	}
	return response.AttachmentDataRef.AttachmentUploadToken, nil
}

func createMessage(ctx context.Context, client *http.Client, parent string, message *chatapi.Message, options MessageCreateOptions) (Message, error) {
	if strings.TrimSpace(parent) == "" {
		return Message{}, errors.New("space name is required")
	}

	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return Message{}, err
	}

	call := service.Spaces.Messages.Create(parent, message)
	if strings.TrimSpace(options.ThreadKey) != "" {
		call = call.ThreadKey(options.ThreadKey)
	}
	if strings.TrimSpace(options.MessageReplyOption) != "" {
		call = call.MessageReplyOption(options.MessageReplyOption)
	}
	if strings.TrimSpace(options.MessageID) != "" {
		call = call.MessageId(options.MessageID)
	}
	if strings.TrimSpace(options.RequestID) != "" {
		call = call.RequestId(options.RequestID)
	}

	response, err := call.Do()
	if err != nil {
		return Message{}, err
	}
	return messageFromAPI(response), nil
}

func serviceFromClient(ctx context.Context, client *http.Client) (*chatapi.Service, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}

	service, err := chatapi.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create chat service: %w", err)
	}
	return service, nil
}

func spaceFromAPI(space *chatapi.Space) Space {
	if space == nil {
		return Space{}
	}
	return Space{
		Name:        space.Name,
		DisplayName: space.DisplayName,
		Type:        space.Type,
		SpaceType:   space.SpaceType,
		SpaceURI:    space.SpaceUri,
	}
}

func messageFromAPI(message *chatapi.Message) Message {
	if message == nil {
		return Message{}
	}

	var sender string
	if message.Sender != nil {
		sender = message.Sender.Name
	}
	var threadName string
	var threadKey string
	if message.Thread != nil {
		threadName = message.Thread.Name
		threadKey = message.Thread.ThreadKey
	}
	var attachmentName string
	if len(message.Attachment) > 0 {
		attachmentName = message.Attachment[0].Name
	}

	return Message{
		Name:           message.Name,
		Text:           message.Text,
		FormattedText:  message.FormattedText,
		Sender:         sender,
		CreateTime:     message.CreateTime,
		ThreadName:     threadName,
		ThreadKey:      threadKey,
		ThreadReply:    message.ThreadReply,
		AttachmentName: attachmentName,
	}
}

func membershipFromAPI(membership *chatapi.Membership) Membership {
	if membership == nil {
		return Membership{}
	}

	var memberName string
	var memberType string
	var displayName string
	if membership.Member != nil {
		memberName = membership.Member.Name
		memberType = membership.Member.Type
		displayName = membership.Member.DisplayName
	}

	return Membership{
		Name:        membership.Name,
		MemberName:  memberName,
		MemberType:  memberType,
		DisplayName: displayName,
		Email:       emailFromUserName(memberName),
		State:       membership.State,
		Role:        membership.Role,
	}
}

func emailFromUserName(name string) string {
	value := strings.TrimSpace(name)
	if !strings.HasPrefix(value, "users/") {
		return ""
	}
	value = strings.TrimPrefix(value, "users/")
	if !strings.Contains(value, "@") {
		return ""
	}
	return value
}

func membershipsFromUsers(users []string) []*chatapi.Membership {
	memberships := make([]*chatapi.Membership, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user) == "" {
			continue
		}
		memberships = append(memberships, membershipFromUser(user))
	}
	return memberships
}

func membershipFromUser(user string) *chatapi.Membership {
	return &chatapi.Membership{
		Member: &chatapi.User{
			Name: normalizedUserName(user),
			Type: "HUMAN",
		},
	}
}

func normalizedUserName(user string) string {
	value := strings.TrimSpace(user)
	if strings.HasPrefix(value, "users/") {
		return value
	}
	return "users/" + value
}

func attachmentsFromUploadRefs(refs []string) []*chatapi.Attachment {
	attachments := make([]*chatapi.Attachment, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		attachments = append(attachments, &chatapi.Attachment{
			AttachmentDataRef: &chatapi.AttachmentDataRef{
				AttachmentUploadToken: ref,
			},
		})
	}
	return attachments
}
