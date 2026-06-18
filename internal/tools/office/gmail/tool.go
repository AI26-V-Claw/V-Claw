package gmail

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	googleconnector "vclaw/internal/connectors/google"
	driveconnector "vclaw/internal/connectors/google/drive"
	gmailconnector "vclaw/internal/connectors/google/gmail"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameListEmails          = "gmail.listEmails"
	ToolNameGetEmail            = "gmail.getEmail"
	ToolNameListLabels          = "gmail.listLabels"
	ToolNameGetProfile          = "gmail.getProfile"
	ToolNameListThreads         = "gmail.listThreads"
	ToolNameGetThread           = "gmail.getThread"
	ToolNameListDrafts          = "gmail.listDrafts"
	ToolNameGetDraft            = "gmail.getDraft"
	ToolNameCreateDraft         = "gmail.createDraft"
	ToolNameUpdateDraft         = "gmail.updateDraft"
	ToolNameSendDraft           = "gmail.sendDraft"
	ToolNameDeleteDraft         = "gmail.deleteDraft"
	ToolNameReplyDraft          = "gmail.replyDraft"
	ToolNameForwardDraft        = "gmail.forwardDraft"
	ToolNameDownloadAttachments = "gmail.downloadAttachments"
	ToolNameModifyMessage       = "gmail.modifyMessage"
	ToolNameBatchModifyMessages = "gmail.batchModifyMessages"
	ToolNameTrashMessage        = "gmail.trashMessage"
	ToolNameUntrashMessage      = "gmail.untrashMessage"
)

const (
	RenderModeText    = "text"
	RenderModeRawHTML = "raw-html"
)

const (
	defaultMaxResults     = int64(10)
	maxAllowedResults     = int64(50)
	maxDraftAttachments   = 10
	maxDraftAttachmentRaw = int64(20 * 1024 * 1024)

	// Auto-pagination: when the caller makes a bare list request (no explicit
	// maxResults and no pageToken), the Service fetches successive pages until
	// the result set is exhausted or these safety caps are reached, so a
	// complete list is not silently truncated to one page.
	autoPaginatePageSize = int64(50)  // per-connector-call page size while auto-paginating
	maxAutoPaginateTotal = int64(300) // hard cap on total accumulated results
	maxAutoPaginatePages = 25         // hard cap on round trips (guards against a stuck pageToken)
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	Description      string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{Name: ToolNameListEmails, Owner: "integration", Description: "List emails by search criteria. after/before must be date-only YYYY-MM-DD strings (not RFC3339). To list sent mail to a recipient, use query \"in:sent to:<email>\" with labelIds=[\"SENT\"]. Returns message summaries only; call gmail.getEmail to inspect attachments.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameGetEmail, Owner: "integration", Description: "Read one email in detail, including attachment metadata and message content. This is a sensitive read and requires approval.", DefaultRiskLevel: "sensitive_read", RequiresApproval: true},
	{Name: ToolNameListLabels, Owner: "integration", Description: "List Gmail labels.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameGetProfile, Owner: "integration", Description: "Read the Gmail account profile.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameListThreads, Owner: "integration", Description: "List Gmail threads by search criteria. after/before must be date-only YYYY-MM-DD strings (not RFC3339).", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameGetThread, Owner: "integration", Description: "Read a Gmail thread in detail.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameListDrafts, Owner: "integration", Description: "List Gmail drafts.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameGetDraft, Owner: "integration", Description: "Read one Gmail draft in detail.", DefaultRiskLevel: "safe_read", RequiresApproval: false},
	{Name: ToolNameCreateDraft, Owner: "integration", Description: "Create a Gmail draft. To attach a Google Drive file, use the driveAttachments field with the Drive file ID from drive.listFiles — do not construct a file path from a Drive filename.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameUpdateDraft, Owner: "integration", Description: "Update a Gmail draft.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameSendDraft, Owner: "integration", Description: "Send an existing Gmail draft. Requires a draftId from gmail.createDraft. The email is not delivered until this tool succeeds — createDraft alone does not send.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameDeleteDraft, Owner: "integration", Description: "Delete an existing Gmail draft.", DefaultRiskLevel: "destructive", RequiresApproval: true},
	{Name: ToolNameReplyDraft, Owner: "integration", Description: "Create a Gmail reply draft.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameForwardDraft, Owner: "integration", Description: "Create a Gmail forward draft.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameDownloadAttachments, Owner: "integration", Description: "Download Gmail attachments into the local sandbox workspace. Call gmail.getEmail immediately before gmail.downloadAttachments so the agent can use fresh attachment filenames from the current message. If filenames are provided, only matching attachments are downloaded. outputDir is optional and defaults to the workspace root.", DefaultRiskLevel: "local_write", RequiresApproval: true},
	{Name: ToolNameModifyMessage, Owner: "integration", Description: "Modify Gmail message labels such as read, unread, starred, archive, or inbox.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameBatchModifyMessages, Owner: "integration", Description: "Modify Gmail labels for multiple messages.", DefaultRiskLevel: "external_write", RequiresApproval: true},
	{Name: ToolNameTrashMessage, Owner: "integration", Description: "Move a Gmail message to trash.", DefaultRiskLevel: "destructive", RequiresApproval: true},
	{Name: ToolNameUntrashMessage, Owner: "integration", Description: "Restore a Gmail message from trash.", DefaultRiskLevel: "external_write", RequiresApproval: true},
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

type Connector interface {
	ListLabels(ctx context.Context, userID string) ([]gmailconnector.Label, error)
	GetProfile(ctx context.Context, userID string) (gmailconnector.Profile, error)
	ListMessages(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error)
	GetMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error)
	ListThreads(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.ThreadSummary, string, error)
	GetThread(ctx context.Context, userID string, threadID string) (gmailconnector.ThreadDetail, error)
	ListDrafts(ctx context.Context, userID string, maxResults int64, pageToken string) (gmailconnector.ListDraftsOutput, error)
	GetDraft(ctx context.Context, userID string, draftID string) (gmailconnector.DraftDetail, error)
	CreateDraft(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error)
	UpdateDraft(ctx context.Context, userID string, draftID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error)
	SendDraft(ctx context.Context, userID string, draftID string) (gmailconnector.MessageSummary, error)
	DeleteDraft(ctx context.Context, userID string, draftID string) error
	DownloadAttachment(ctx context.Context, userID string, messageID string, attachment gmailconnector.Attachment) (gmailconnector.AttachmentData, error)
	ModifyMessage(ctx context.Context, userID string, messageID string, input gmailconnector.ModifyMessageInput) (gmailconnector.ModifyMessageOutput, error)
	BatchModifyMessages(ctx context.Context, userID string, messageIDs []string, input gmailconnector.ModifyMessageInput) (gmailconnector.BatchModifyMessagesOutput, error)
	TrashMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error)
	UntrashMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error)
}

// DriveFileSource is a minimal interface for downloading Drive files to attach to Gmail drafts.
type DriveFileSource interface {
	GetFile(ctx context.Context, fileID string) (driveconnector.FileSummary, error)
	DownloadFile(ctx context.Context, fileID string, maxBytes int64) (driveconnector.FileContentOutput, error)
	ExportFile(ctx context.Context, fileID string, mimeType string, maxBytes int64) (driveconnector.FileContentOutput, error)
}

// PathGuard confines attachment downloads to the sandbox workspace.
// filesystem.PathGuard satisfies this interface.
type PathGuard interface {
	Resolve(path string) (string, error)
}

type Service struct {
	connector   Connector
	driveSource DriveFileSource
	// location is the user's timezone, used to derive a local send time from each
	// message's InternalDate (epoch ms) for display. Defaults to time.Local.
	location *time.Location
	// downloadGuard confines gmail.downloadAttachments output to the sandbox
	// workspace. When set, outputDir is optional and defaults to the workspace root.
	downloadGuard PathGuard
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
}

func (s *Service) WithDriveSource(source DriveFileSource) *Service {
	s.driveSource = source
	return s
}

// WithLocation sets the timezone used to localize message send times.
func (s *Service) WithLocation(loc *time.Location) *Service {
	s.location = loc
	return s
}

// WithDownloadGuard confines attachment downloads to the sandbox workspace and
// makes outputDir optional (defaulting to the workspace root).
func (s *Service) WithDownloadGuard(guard PathGuard) *Service {
	s.downloadGuard = guard
	return s
}

func (s *Service) localLocation() *time.Location {
	if s != nil && s.location != nil {
		return s.location
	}
	return time.Local
}

type ListEmailsInput struct {
	UserID     string
	Query      string
	From       string
	Subject    string
	After      string
	Before     string
	LabelIDs   []string
	MaxResults int64
	PageToken  string
}

type ListEmailsOutput struct {
	Query         string
	Messages      []gmailconnector.MessageSummary
	NextPageToken string
}

type ListLabelsInput struct {
	UserID string
}

type ListLabelsOutput struct {
	Labels []gmailconnector.Label
}

type GetProfileInput struct {
	UserID string
}

type GetProfileOutput struct {
	Profile gmailconnector.Profile
}

type GetEmailInput struct {
	UserID       string
	MessageID    string
	RenderMode   string
	Full         bool
	PreviewChars int
}

type GetEmailOutput struct {
	Message gmailconnector.MessageDetail
	Display DisplayOutput
}

type ListThreadsInput struct {
	UserID     string
	Query      string
	From       string
	Subject    string
	After      string
	Before     string
	LabelIDs   []string
	MaxResults int64
	PageToken  string
}

type ListThreadsOutput struct {
	Query         string
	Threads       []gmailconnector.ThreadSummary
	NextPageToken string
}

type GetThreadInput struct {
	UserID       string
	ThreadID     string
	RenderMode   string
	Full         bool
	PreviewChars int
}

type ThreadMessageDisplay struct {
	Message gmailconnector.MessageDetail
	Display DisplayOutput
}

type GetThreadOutput struct {
	Thread   gmailconnector.ThreadDetail
	Messages []ThreadMessageDisplay
}

type ListDraftsInput struct {
	UserID     string
	MaxResults int64
	PageToken  string
}

type ListDraftsOutput struct {
	Drafts        []gmailconnector.DraftSummary
	NextPageToken string
}

type GetDraftInput struct {
	UserID       string
	DraftID      string
	RenderMode   string
	Full         bool
	PreviewChars int
}

type GetDraftOutput struct {
	Draft   gmailconnector.DraftDetail
	Display DisplayOutput
}

type DraftInput struct {
	UserID           string
	To               []string
	Cc               []string
	Bcc              []string
	Subject          string
	TextBody         string
	HTMLBody         string
	ThreadID         string
	Attachments      []string
	DriveAttachments []string
}

type CreateDraftOutput struct {
	Draft gmailconnector.DraftSummary
}

type UpdateDraftInput struct {
	DraftInput
	DraftID string
}

type SendDraftInput struct {
	UserID  string
	DraftID string
}

type SendDraftOutput struct {
	Message gmailconnector.MessageSummary
}

type DeleteDraftInput struct {
	UserID  string
	DraftID string
}

type DeleteDraftOutput struct {
	DraftID string
}

type ReplyDraftInput struct {
	DraftInput
	MessageID string
}

type ForwardDraftInput struct {
	DraftInput
	MessageID string
}

type DownloadAttachmentsInput struct {
	UserID    string
	MessageID string
	Filenames []string
	OutputDir string
}

type DownloadedAttachment struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

type DownloadAttachmentsOutput struct {
	Files []DownloadedAttachment `json:"files"`
}

type ModifyMessageInput struct {
	UserID    string
	MessageID string
	Action    string
	LabelIDs  []string
}

type ModifyMessageOutput struct {
	Message gmailconnector.ModifyMessageOutput
}

type BatchModifyMessagesInput struct {
	UserID     string
	MessageIDs []string
	Action     string
	LabelIDs   []string
}

type BatchModifyMessagesOutput struct {
	Result gmailconnector.BatchModifyMessagesOutput
}

type TrashMessageInput struct {
	UserID    string
	MessageID string
}

type TrashMessageOutput struct {
	Message gmailconnector.MessageSummary
}

type UntrashMessageInput struct {
	UserID    string
	MessageID string
}

type UntrashMessageOutput struct {
	Message gmailconnector.MessageSummary
}

type DisplayOutput struct {
	Mode         string
	Source       string
	Text         string
	Truncated    bool
	PreviewChars int
}

func (s *Service) ListEmails(ctx context.Context, input ListEmailsInput) (ListEmailsOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return ListEmailsOutput{}, errShape
	}
	query, err := BuildSearchQuery(input)
	if err != nil {
		return ListEmailsOutput{}, invalidInput(err.Error())
	}
	userID := normalizeUserID(input.UserID)
	// A bare list request (no explicit count, no page cursor) means "list everything":
	// auto-paginate up to a safe cap so results are not silently truncated to one page.
	if input.PageToken == "" && input.MaxResults <= 0 {
		messages, nextPageToken, err := s.listAllMessages(ctx, userID, query, input.LabelIDs)
		if err != nil {
			return ListEmailsOutput{}, MapError(err)
		}
		return ListEmailsOutput{Query: query, Messages: messages, NextPageToken: nextPageToken}, nil
	}
	// Explicit count or manual pagination: return a single page.
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListEmailsOutput{}, errShape
	}
	messages, nextPageToken, err := s.connector.ListMessages(ctx, userID, query, input.LabelIDs, maxResults, input.PageToken)
	if err != nil {
		return ListEmailsOutput{}, MapError(err)
	}
	return ListEmailsOutput{Query: query, Messages: messages, NextPageToken: nextPageToken}, nil
}

// listAllMessages fetches successive pages until the result set is exhausted or
// a safety cap is reached. The returned token is non-empty only when a cap cut
// the listing short, signalling there are more results to fetch manually.
func (s *Service) listAllMessages(ctx context.Context, userID, query string, labelIDs []string) ([]gmailconnector.MessageSummary, string, error) {
	var all []gmailconnector.MessageSummary
	pageToken := ""
	for page := 0; page < maxAutoPaginatePages; page++ {
		messages, next, err := s.connector.ListMessages(ctx, userID, query, labelIDs, autoPaginatePageSize, pageToken)
		if err != nil {
			return nil, "", err
		}
		all = append(all, messages...)
		if next == "" || int64(len(all)) >= maxAutoPaginateTotal {
			return all, next, nil
		}
		pageToken = next
	}
	return all, pageToken, nil
}

func (s *Service) ListLabels(ctx context.Context, input ListLabelsInput) (ListLabelsOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return ListLabelsOutput{}, errShape
	}
	labels, err := s.connector.ListLabels(ctx, normalizeUserID(input.UserID))
	if err != nil {
		return ListLabelsOutput{}, MapError(err)
	}
	return ListLabelsOutput{Labels: labels}, nil
}

func (s *Service) GetProfile(ctx context.Context, input GetProfileInput) (GetProfileOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return GetProfileOutput{}, errShape
	}
	profile, err := s.connector.GetProfile(ctx, normalizeUserID(input.UserID))
	if err != nil {
		return GetProfileOutput{}, MapError(err)
	}
	return GetProfileOutput{Profile: profile}, nil
}

func (s *Service) GetEmail(ctx context.Context, input GetEmailInput) (GetEmailOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return GetEmailOutput{}, errShape
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return GetEmailOutput{}, invalidInput("messageId is required")
	}
	message, err := s.connector.GetMessage(ctx, normalizeUserID(input.UserID), input.MessageID)
	if err != nil {
		return GetEmailOutput{}, MapError(err)
	}
	display, errShape := buildDisplay(message, input)
	if errShape != nil {
		return GetEmailOutput{}, errShape
	}
	return GetEmailOutput{Message: message, Display: display}, nil
}

func (s *Service) ListThreads(ctx context.Context, input ListThreadsInput) (ListThreadsOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return ListThreadsOutput{}, errShape
	}
	query, err := BuildSearchQuery(ListEmailsInput{Query: input.Query, From: input.From, Subject: input.Subject, After: input.After, Before: input.Before})
	if err != nil {
		return ListThreadsOutput{}, invalidInput(err.Error())
	}
	userID := normalizeUserID(input.UserID)
	// A bare list request (no explicit count, no page cursor) means "list everything":
	// auto-paginate up to a safe cap so results are not silently truncated to one page.
	if input.PageToken == "" && input.MaxResults <= 0 {
		threads, nextPageToken, err := s.listAllThreads(ctx, userID, query, input.LabelIDs)
		if err != nil {
			return ListThreadsOutput{}, MapError(err)
		}
		return ListThreadsOutput{Query: query, Threads: threads, NextPageToken: nextPageToken}, nil
	}
	// Explicit count or manual pagination: return a single page.
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListThreadsOutput{}, errShape
	}
	threads, nextPageToken, err := s.connector.ListThreads(ctx, userID, query, input.LabelIDs, maxResults, input.PageToken)
	if err != nil {
		return ListThreadsOutput{}, MapError(err)
	}
	return ListThreadsOutput{Query: query, Threads: threads, NextPageToken: nextPageToken}, nil
}

// listAllThreads fetches successive pages until the result set is exhausted or
// a safety cap is reached, mirroring listAllMessages.
func (s *Service) listAllThreads(ctx context.Context, userID, query string, labelIDs []string) ([]gmailconnector.ThreadSummary, string, error) {
	var all []gmailconnector.ThreadSummary
	pageToken := ""
	for page := 0; page < maxAutoPaginatePages; page++ {
		threads, next, err := s.connector.ListThreads(ctx, userID, query, labelIDs, autoPaginatePageSize, pageToken)
		if err != nil {
			return nil, "", err
		}
		all = append(all, threads...)
		if next == "" || int64(len(all)) >= maxAutoPaginateTotal {
			return all, next, nil
		}
		pageToken = next
	}
	return all, pageToken, nil
}

func (s *Service) GetThread(ctx context.Context, input GetThreadInput) (GetThreadOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return GetThreadOutput{}, errShape
	}
	if strings.TrimSpace(input.ThreadID) == "" {
		return GetThreadOutput{}, invalidInput("threadId is required")
	}
	thread, err := s.connector.GetThread(ctx, normalizeUserID(input.UserID), input.ThreadID)
	if err != nil {
		return GetThreadOutput{}, MapError(err)
	}
	output := GetThreadOutput{Thread: thread}
	for _, message := range thread.Messages {
		display, errShape := buildDisplay(message, GetEmailInput{RenderMode: input.RenderMode, Full: input.Full, PreviewChars: input.PreviewChars})
		if errShape != nil {
			return GetThreadOutput{}, errShape
		}
		output.Messages = append(output.Messages, ThreadMessageDisplay{Message: message, Display: display})
	}
	return output, nil
}

func (s *Service) ListDrafts(ctx context.Context, input ListDraftsInput) (ListDraftsOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return ListDraftsOutput{}, errShape
	}
	maxResults, errShape := normalizeMaxResults(input.MaxResults)
	if errShape != nil {
		return ListDraftsOutput{}, errShape
	}
	output, err := s.connector.ListDrafts(ctx, normalizeUserID(input.UserID), maxResults, input.PageToken)
	if err != nil {
		return ListDraftsOutput{}, MapError(err)
	}
	return ListDraftsOutput{Drafts: output.Drafts, NextPageToken: output.NextPageToken}, nil
}

func (s *Service) GetDraft(ctx context.Context, input GetDraftInput) (GetDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return GetDraftOutput{}, errShape
	}
	if strings.TrimSpace(input.DraftID) == "" {
		return GetDraftOutput{}, invalidInput("draftId is required")
	}
	draft, err := s.connector.GetDraft(ctx, normalizeUserID(input.UserID), input.DraftID)
	if err != nil {
		return GetDraftOutput{}, MapError(err)
	}
	display, errShape := buildDisplay(draft.Message, GetEmailInput{RenderMode: input.RenderMode, Full: input.Full, PreviewChars: input.PreviewChars})
	if errShape != nil {
		return GetDraftOutput{}, errShape
	}
	return GetDraftOutput{Draft: draft, Display: display}, nil
}

func (s *Service) CreateDraft(ctx context.Context, input DraftInput) (CreateDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	draftInput, errShape := buildDraftMessageInput(input, true, true)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	driveAttachments, errShape := s.loadDriveAttachments(ctx, input.DriveAttachments)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	draftInput.Attachments = append(draftInput.Attachments, driveAttachments...)
	draft, err := s.connector.CreateDraft(ctx, normalizeUserID(input.UserID), draftInput)
	if err != nil {
		return CreateDraftOutput{}, MapError(err)
	}
	return CreateDraftOutput{Draft: draft}, nil
}

func (s *Service) UpdateDraft(ctx context.Context, input UpdateDraftInput) (CreateDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	if strings.TrimSpace(input.DraftID) == "" {
		return CreateDraftOutput{}, invalidInput("draftId is required")
	}
	draftInput, errShape := buildDraftMessageInput(input.DraftInput, true, true)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	driveAttachments, errShape := s.loadDriveAttachments(ctx, input.DraftInput.DriveAttachments)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	draftInput.Attachments = append(draftInput.Attachments, driveAttachments...)
	draft, err := s.connector.UpdateDraft(ctx, normalizeUserID(input.UserID), input.DraftID, draftInput)
	if err != nil {
		return CreateDraftOutput{}, MapError(err)
	}
	return CreateDraftOutput{Draft: draft}, nil
}

func (s *Service) SendDraft(ctx context.Context, input SendDraftInput) (SendDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return SendDraftOutput{}, errShape
	}
	if strings.TrimSpace(input.DraftID) == "" {
		return SendDraftOutput{}, invalidInput("draftId is required")
	}
	message, err := s.connector.SendDraft(ctx, normalizeUserID(input.UserID), input.DraftID)
	if err != nil {
		return SendDraftOutput{}, MapError(err)
	}
	return SendDraftOutput{Message: message}, nil
}

func (s *Service) DeleteDraft(ctx context.Context, input DeleteDraftInput) (DeleteDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return DeleteDraftOutput{}, errShape
	}
	if strings.TrimSpace(input.DraftID) == "" {
		return DeleteDraftOutput{}, invalidInput("draftId is required")
	}
	if err := s.connector.DeleteDraft(ctx, normalizeUserID(input.UserID), input.DraftID); err != nil {
		return DeleteDraftOutput{}, MapError(err)
	}
	return DeleteDraftOutput{DraftID: input.DraftID}, nil
}

func (s *Service) ReplyDraft(ctx context.Context, input ReplyDraftInput) (CreateDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	requireSubject := strings.TrimSpace(input.MessageID) == ""
	draftInput, errShape := buildDraftMessageInput(input.DraftInput, true, requireSubject)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	driveAttachments, errShape := s.loadDriveAttachments(ctx, input.DraftInput.DriveAttachments)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	draftInput.Attachments = append(draftInput.Attachments, driveAttachments...)
	var draft gmailconnector.DraftSummary
	var err error
	if strings.TrimSpace(input.MessageID) != "" {
		original, getErr := s.connector.GetMessage(ctx, normalizeUserID(input.UserID), input.MessageID)
		if getErr != nil {
			return CreateDraftOutput{}, MapError(getErr)
		}
		draftInput = replyDraftMessageInput(draftInput, original)
		draft, err = s.connector.CreateDraft(ctx, normalizeUserID(input.UserID), draftInput)
	} else if strings.TrimSpace(input.ThreadID) != "" {
		draftInput.ThreadID = input.ThreadID
		draft, err = s.connector.CreateDraft(ctx, normalizeUserID(input.UserID), draftInput)
	} else {
		return CreateDraftOutput{}, invalidInput("messageId or threadId is required")
	}
	if err != nil {
		return CreateDraftOutput{}, MapError(err)
	}
	return CreateDraftOutput{Draft: draft}, nil
}

func (s *Service) ForwardDraft(ctx context.Context, input ForwardDraftInput) (CreateDraftOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return CreateDraftOutput{}, invalidInput("messageId is required")
	}
	draftInput, errShape := buildDraftMessageInput(input.DraftInput, true, false)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	driveAttachments, errShape := s.loadDriveAttachments(ctx, input.DraftInput.DriveAttachments)
	if errShape != nil {
		return CreateDraftOutput{}, errShape
	}
	draftInput.Attachments = append(draftInput.Attachments, driveAttachments...)
	original, err := s.connector.GetMessage(ctx, normalizeUserID(input.UserID), input.MessageID)
	if err != nil {
		return CreateDraftOutput{}, MapError(err)
	}
	draftInput = forwardDraftMessageInput(draftInput, original)
	draft, err := s.connector.CreateDraft(ctx, normalizeUserID(input.UserID), draftInput)
	if err != nil {
		return CreateDraftOutput{}, MapError(err)
	}
	return CreateDraftOutput{Draft: draft}, nil
}

func (s *Service) DownloadAttachments(ctx context.Context, input DownloadAttachmentsInput) (DownloadAttachmentsOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return DownloadAttachmentsOutput{}, errShape
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return DownloadAttachmentsOutput{}, invalidInput("messageId is required")
	}
	outputDir, errShape := s.resolveDownloadDir(input.OutputDir)
	if errShape != nil {
		return DownloadAttachmentsOutput{}, errShape
	}
	message, err := s.connector.GetMessage(ctx, normalizeUserID(input.UserID), input.MessageID)
	if err != nil {
		return DownloadAttachmentsOutput{}, MapError(err)
	}
	attachments := selectAttachmentsByFilename(message.Attachments, input.Filenames)
	if len(attachments) == 0 {
		return DownloadAttachmentsOutput{}, invalidInput("no matching attachments found")
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return DownloadAttachmentsOutput{}, internalError("create outputDir: " + err.Error())
	}

	output := DownloadAttachmentsOutput{}
	for _, attachment := range attachments {
		data, err := s.connector.DownloadAttachment(ctx, normalizeUserID(input.UserID), input.MessageID, attachment)
		if err != nil {
			return DownloadAttachmentsOutput{}, MapError(err)
		}
		filename := safeAttachmentFilename(attachment)
		path := filepath.Join(outputDir, filename)
		if err := os.WriteFile(path, data.Data, 0600); err != nil {
			return DownloadAttachmentsOutput{}, internalError("write attachment: " + err.Error())
		}
		output.Files = append(output.Files, DownloadedAttachment{
			Filename: filename,
			Path:     path,
			MimeType: attachment.MimeType,
			Size:     int64(len(data.Data)),
		})
	}
	return output, nil
}

// resolveDownloadDir confines the attachment output directory to the sandbox
// workspace when a guard is configured. With a guard, outputDir is optional and
// defaults to the workspace root; a path outside the workspace is rejected.
// Without a guard (e.g. unit tests), outputDir is required and used verbatim.
func (s *Service) resolveDownloadDir(outputDir string) (string, *ErrorShape) {
	outputDir = strings.TrimSpace(outputDir)
	if s.downloadGuard == nil {
		if outputDir == "" {
			return "", invalidInput("outputDir is required")
		}
		return outputDir, nil
	}
	if outputDir == "" {
		outputDir = "."
	}
	resolved, err := s.downloadGuard.Resolve(outputDir)
	if err != nil {
		return "", invalidInput("outputDir is outside the allowed workspace: " + err.Error())
	}
	return resolved, nil
}

func (s *Service) ModifyMessage(ctx context.Context, input ModifyMessageInput) (ModifyMessageOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return ModifyMessageOutput{}, errShape
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return ModifyMessageOutput{}, invalidInput("messageId is required")
	}
	modifyInput, errShape := buildModifyMessageInput(input)
	if errShape != nil {
		return ModifyMessageOutput{}, errShape
	}
	message, err := s.connector.ModifyMessage(ctx, normalizeUserID(input.UserID), input.MessageID, modifyInput)
	if err != nil {
		return ModifyMessageOutput{}, MapError(err)
	}
	return ModifyMessageOutput{Message: message}, nil
}

func (s *Service) BatchModifyMessages(ctx context.Context, input BatchModifyMessagesInput) (BatchModifyMessagesOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return BatchModifyMessagesOutput{}, errShape
	}
	messageIDs := cleanStringSlice(input.MessageIDs)
	if len(messageIDs) == 0 {
		return BatchModifyMessagesOutput{}, invalidInput("messageIds is required")
	}
	if len(messageIDs) > int(maxAllowedResults) {
		return BatchModifyMessagesOutput{}, invalidInput(fmt.Sprintf("messageIds must contain between 1 and %d ids", maxAllowedResults))
	}
	modifyInput, errShape := buildModifyMessageInput(ModifyMessageInput{Action: input.Action, LabelIDs: input.LabelIDs})
	if errShape != nil {
		return BatchModifyMessagesOutput{}, errShape
	}
	result, err := s.connector.BatchModifyMessages(ctx, normalizeUserID(input.UserID), messageIDs, modifyInput)
	if err != nil {
		return BatchModifyMessagesOutput{}, MapError(err)
	}
	return BatchModifyMessagesOutput{Result: result}, nil
}

func (s *Service) TrashMessage(ctx context.Context, input TrashMessageInput) (TrashMessageOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return TrashMessageOutput{}, errShape
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return TrashMessageOutput{}, invalidInput("messageId is required")
	}
	message, err := s.connector.TrashMessage(ctx, normalizeUserID(input.UserID), input.MessageID)
	if err != nil {
		return TrashMessageOutput{}, MapError(err)
	}
	return TrashMessageOutput{Message: message}, nil
}

func (s *Service) UntrashMessage(ctx context.Context, input UntrashMessageInput) (UntrashMessageOutput, *ErrorShape) {
	if errShape := s.validateConnector(); errShape != nil {
		return UntrashMessageOutput{}, errShape
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return UntrashMessageOutput{}, invalidInput("messageId is required")
	}
	message, err := s.connector.UntrashMessage(ctx, normalizeUserID(input.UserID), input.MessageID)
	if err != nil {
		return UntrashMessageOutput{}, MapError(err)
	}
	return UntrashMessageOutput{Message: message}, nil
}

func (s *Service) validateConnector() *ErrorShape {
	if s == nil || s.connector == nil {
		return internalError("gmail connector is not configured")
	}
	return nil
}

func BuildSearchQuery(input ListEmailsInput) (string, error) {
	parts := []string{}
	base := strings.TrimSpace(input.Query)
	if base != "" {
		parts = append(parts, base)
	}
	if value := strings.TrimSpace(input.From); value != "" {
		parts = append(parts, "from:"+quoteIfNeeded(value))
	}
	if value := strings.TrimSpace(input.Subject); value != "" {
		parts = append(parts, "subject:"+quoteIfNeeded(value))
	}
	if value := strings.TrimSpace(input.After); value != "" {
		date, err := normalizeDate(value)
		if err != nil {
			return "", fmt.Errorf("after must be in YYYY-MM-DD format")
		}
		parts = append(parts, "after:"+date)
	}
	if value := strings.TrimSpace(input.Before); value != "" {
		date, err := normalizeDate(value)
		if err != nil {
			return "", fmt.Errorf("before must be in YYYY-MM-DD format")
		}
		parts = append(parts, "before:"+date)
	}
	return strings.Join(parts, " "), nil
}

func MapError(err error) *ErrorShape {
	if googleconnector.IsNetworkError(err) {
		return &ErrorShape{Code: "PROVIDER_TIMEOUT", Message: "network error contacting Gmail API: " + err.Error(), Retryable: true}
	}
	var gerr *googleapi.Error
	if !asGoogleError(err, &gerr) {
		return internalError(err.Error())
	}
	switch {
	case gerr.Code == http.StatusUnauthorized:
		return &ErrorShape{Code: "AUTH_EXPIRED", Message: gerr.Message, Retryable: true}
	case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
		return &ErrorShape{Code: "AUTH_MISSING_SCOPE", Message: gerr.Message, Retryable: false}
	case gerr.Code == http.StatusNotFound:
		return &ErrorShape{Code: "RESOURCE_NOT_FOUND", Message: gerr.Message, Retryable: false}
	case gerr.Code == http.StatusTooManyRequests:
		return &ErrorShape{Code: "RATE_LIMITED", Message: gerr.Message, Retryable: true}
	case gerr.Code >= 500:
		return &ErrorShape{Code: "PROVIDER_UNAVAILABLE", Message: gerr.Message, Retryable: true}
	default:
		return internalError(gerr.Message)
	}
}

func asGoogleError(err error, target **googleapi.Error) bool {
	if err == nil {
		return false
	}
	typed, ok := err.(*googleapi.Error)
	if !ok {
		return false
	}
	*target = typed
	return true
}

func hasMissingScopeReason(err *googleapi.Error) bool {
	text := strings.ToLower(err.Message)
	return strings.Contains(text, "insufficient authentication scopes") || strings.Contains(text, "insufficient permissions")
}

func normalizeMaxResults(value int64) (int64, *ErrorShape) {
	if value == 0 {
		return defaultMaxResults, nil
	}
	if value < 1 || value > maxAllowedResults {
		return 0, invalidInput(fmt.Sprintf("maxResults must be between 1 and %d", maxAllowedResults))
	}
	return value, nil
}

// listMaxResultsArg reads the maxResults argument for a list call. When the key
// is absent it returns 0, signalling the Service to auto-paginate the full
// result set. When present it is clamped into [1, maxAllowedResults] so an
// out-of-range request is bounded to a single page rather than rejected.
func listMaxResultsArg(args map[string]any) int64 {
	if args == nil {
		return 0
	}
	if _, ok := args["maxResults"]; !ok {
		return 0
	}
	return boundedInt64Arg(args, "maxResults", defaultMaxResults, maxAllowedResults)
}

func buildDraftMessageInput(input DraftInput, requireRecipient bool, requireSubject bool) (gmailconnector.DraftMessageInput, *ErrorShape) {
	if requireRecipient && len(cleanStringSlice(append(append(input.To, input.Cc...), input.Bcc...))) == 0 {
		return gmailconnector.DraftMessageInput{}, invalidInput("at least one recipient is required")
	}
	if requireSubject && strings.TrimSpace(input.Subject) == "" {
		return gmailconnector.DraftMessageInput{}, invalidInput("subject is required")
	}
	if strings.TrimSpace(input.TextBody) == "" && strings.TrimSpace(input.HTMLBody) == "" {
		return gmailconnector.DraftMessageInput{}, invalidInput("textBody or htmlBody is required")
	}
	attachments, errShape := loadDraftAttachments(input.Attachments)
	if errShape != nil {
		return gmailconnector.DraftMessageInput{}, errShape
	}
	return gmailconnector.DraftMessageInput{
		To:          cleanStringSlice(input.To),
		Cc:          cleanStringSlice(input.Cc),
		Bcc:         cleanStringSlice(input.Bcc),
		Subject:     input.Subject,
		TextBody:    input.TextBody,
		HTMLBody:    input.HTMLBody,
		ThreadID:    input.ThreadID,
		Attachments: attachments,
	}, nil
}

func loadDraftAttachments(paths []string) ([]gmailconnector.DraftAttachmentInput, *ErrorShape) {
	cleaned := cleanStringSlice(paths)
	if len(cleaned) == 0 {
		return nil, nil
	}
	if len(cleaned) > maxDraftAttachments {
		return nil, invalidInput(fmt.Sprintf("attachments must contain at most %d files", maxDraftAttachments))
	}

	totalSize := int64(0)
	attachments := make([]gmailconnector.DraftAttachmentInput, 0, len(cleaned))
	for _, path := range cleaned {
		info, err := os.Stat(path)
		if err != nil {
			return nil, invalidInput("attachment not found: " + path)
		}
		if info.IsDir() {
			return nil, invalidInput("attachment must be a file: " + path)
		}
		totalSize += info.Size()
		if totalSize > maxDraftAttachmentRaw {
			return nil, invalidInput(fmt.Sprintf("total attachment size must be at most %d bytes", maxDraftAttachmentRaw))
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, internalError("read attachment: " + err.Error())
		}
		filename := safeDraftAttachmentFilename(path)
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		attachments = append(attachments, gmailconnector.DraftAttachmentInput{
			Filename: filename,
			MimeType: mimeType,
			Data:     data,
		})
	}
	return attachments, nil
}

func (s *Service) loadDriveAttachments(ctx context.Context, fileIDs []string) ([]gmailconnector.DraftAttachmentInput, *ErrorShape) {
	cleaned := cleanStringSlice(fileIDs)
	if len(cleaned) == 0 || s.driveSource == nil {
		return nil, nil
	}
	if len(cleaned) > maxDraftAttachments {
		return nil, invalidInput(fmt.Sprintf("driveAttachments must contain at most %d files", maxDraftAttachments))
	}
	attachments := make([]gmailconnector.DraftAttachmentInput, 0, len(cleaned))
	totalSize := int64(0)
	for _, fileID := range cleaned {
		meta, err := s.driveSource.GetFile(ctx, fileID)
		if err != nil {
			return nil, MapError(err)
		}
		var output driveconnector.FileContentOutput
		if isGoogleAppsFile(meta.MimeType) {
			exportMIME, ext := googleAppsExportFormat(meta.MimeType)
			output, err = s.driveSource.ExportFile(ctx, fileID, exportMIME, maxDraftAttachmentRaw)
			if err != nil {
				return nil, MapError(err)
			}
			if output.File.Name != "" && !strings.Contains(output.File.Name, ".") {
				output.File.Name = output.File.Name + ext
			}
			output.MimeType = exportMIME
		} else {
			output, err = s.driveSource.DownloadFile(ctx, fileID, maxDraftAttachmentRaw)
			if err != nil {
				return nil, MapError(err)
			}
		}
		totalSize += output.Size
		if totalSize > maxDraftAttachmentRaw {
			return nil, invalidInput(fmt.Sprintf("total attachment size must be at most %d bytes", maxDraftAttachmentRaw))
		}
		mimeType := output.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		attachments = append(attachments, gmailconnector.DraftAttachmentInput{
			Filename: output.File.Name,
			MimeType: mimeType,
			Data:     []byte(output.Content),
		})
	}
	return attachments, nil
}

func replyDraftMessageInput(input gmailconnector.DraftMessageInput, original gmailconnector.MessageDetail) gmailconnector.DraftMessageInput {
	if strings.TrimSpace(input.ThreadID) == "" {
		input.ThreadID = original.ThreadID
	}
	if strings.TrimSpace(input.Subject) == "" {
		input.Subject = replySubject(original.Subject)
	}
	if strings.TrimSpace(input.InReplyTo) == "" {
		input.InReplyTo = original.MessageIDHeader
	}
	if strings.TrimSpace(input.References) == "" {
		input.References = strings.TrimSpace(strings.TrimSpace(original.References) + " " + strings.TrimSpace(original.MessageIDHeader))
	}
	return input
}

func forwardDraftMessageInput(input gmailconnector.DraftMessageInput, original gmailconnector.MessageDetail) gmailconnector.DraftMessageInput {
	if strings.TrimSpace(input.Subject) == "" {
		input.Subject = forwardSubject(original.Subject)
	}
	if strings.TrimSpace(input.TextBody) != "" {
		input.TextBody += "\n\n"
	}
	input.TextBody += forwardedText(original)
	return input
}

func replySubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func forwardSubject(subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "Fwd:"
	}
	lower := strings.ToLower(subject)
	if strings.HasPrefix(lower, "fwd:") || strings.HasPrefix(lower, "fw:") {
		return subject
	}
	return "Fwd: " + subject
}

func forwardedText(message gmailconnector.MessageDetail) string {
	var b strings.Builder
	b.WriteString("---------- Forwarded message ---------\n")
	if strings.TrimSpace(message.From) != "" {
		b.WriteString("From: " + message.From + "\n")
	}
	if strings.TrimSpace(message.Date) != "" {
		b.WriteString("Date: " + message.Date + "\n")
	}
	if strings.TrimSpace(message.Subject) != "" {
		b.WriteString("Subject: " + message.Subject + "\n")
	}
	if strings.TrimSpace(message.To) != "" {
		b.WriteString("To: " + message.To + "\n")
	}
	b.WriteString("\n")
	if strings.TrimSpace(message.BodyPlain) != "" {
		b.WriteString(message.BodyPlain)
	} else {
		b.WriteString(stripHTMLForForward(message.BodyHTML))
	}
	return b.String()
}

func stripHTMLForForward(value string) string {
	value = strings.ReplaceAll(value, "<br>", "\n")
	value = strings.ReplaceAll(value, "<br/>", "\n")
	value = strings.ReplaceAll(value, "<br />", "\n")
	return strings.TrimSpace(value)
}

func buildModifyMessageInput(input ModifyMessageInput) (gmailconnector.ModifyMessageInput, *ErrorShape) {
	action := strings.TrimSpace(input.Action)
	switch action {
	case "markRead":
		return gmailconnector.ModifyMessageInput{RemoveLabelIDs: []string{"UNREAD"}}, nil
	case "markUnread":
		return gmailconnector.ModifyMessageInput{AddLabelIDs: []string{"UNREAD"}}, nil
	case "star":
		return gmailconnector.ModifyMessageInput{AddLabelIDs: []string{"STARRED"}}, nil
	case "unstar":
		return gmailconnector.ModifyMessageInput{RemoveLabelIDs: []string{"STARRED"}}, nil
	case "archive":
		return gmailconnector.ModifyMessageInput{RemoveLabelIDs: []string{"INBOX"}}, nil
	case "moveToInbox":
		return gmailconnector.ModifyMessageInput{AddLabelIDs: []string{"INBOX"}}, nil
	case "addLabels":
		labels := cleanStringSlice(input.LabelIDs)
		if len(labels) == 0 {
			return gmailconnector.ModifyMessageInput{}, invalidInput("labelIds is required for addLabels")
		}
		return gmailconnector.ModifyMessageInput{AddLabelIDs: labels}, nil
	case "removeLabels":
		labels := cleanStringSlice(input.LabelIDs)
		if len(labels) == 0 {
			return gmailconnector.ModifyMessageInput{}, invalidInput("labelIds is required for removeLabels")
		}
		return gmailconnector.ModifyMessageInput{RemoveLabelIDs: labels}, nil
	default:
		return gmailconnector.ModifyMessageInput{}, invalidInput("action must be one of markRead, markUnread, star, unstar, archive, moveToInbox, addLabels, removeLabels")
	}
}

func selectAttachmentsByFilename(attachments []gmailconnector.Attachment, filenames []string) []gmailconnector.Attachment {
	wanted := map[string]struct{}{}
	for _, name := range filenames {
		name = strings.TrimSpace(name)
		if name != "" {
			wanted[name] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return attachments
	}
	out := []gmailconnector.Attachment{}
	for _, attachment := range attachments {
		if _, ok := wanted[strings.TrimSpace(attachment.Filename)]; ok {
			out = append(out, attachment)
		}
	}
	return out
}

func safeAttachmentFilename(attachment gmailconnector.Attachment) string {
	filename := filepath.Base(strings.TrimSpace(attachment.Filename))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = strings.TrimSpace(attachment.AttachmentID) + ".dat"
	}
	filename = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, filename)
	if strings.Trim(filename, "._ ") == "" {
		return "attachment.dat"
	}
	return filename
}

func safeDraftAttachmentFilename(path string) string {
	filename := filepath.Base(strings.TrimSpace(path))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = "attachment.dat"
	}
	filename = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, filename)
	if strings.Trim(filename, "._ ") == "" {
		return "attachment.dat"
	}
	return filename
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func invalidInput(message string) *ErrorShape {
	return &ErrorShape{Code: "INVALID_INPUT", Message: message, Retryable: false}
}

// isGoogleAppsFile returns true for Google Workspace native formats that require
// files.export instead of files.get?alt=media.
func isGoogleAppsFile(mimeType string) bool {
	return strings.HasPrefix(mimeType, "application/vnd.google-apps.")
}

// googleAppsExportFormat returns the export MIME type and file extension for a
// Google Workspace native file. Defaults to PDF for all editor types.
func googleAppsExportFormat(mimeType string) (exportMIME string, ext string) {
	switch mimeType {
	case "application/vnd.google-apps.spreadsheet":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"
	case "application/vnd.google-apps.presentation":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx"
	case "application/vnd.google-apps.drawing":
		return "image/png", ".png"
	default:
		return "application/pdf", ".pdf"
	}
}

func internalError(message string) *ErrorShape {
	return &ErrorShape{Code: "INTERNAL_ERROR", Message: message, Retryable: false}
}

func normalizeDate(value string) (string, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", err
	}
	return parsed.Format("2006/01/02"), nil
}

func quoteIfNeeded(value string) string {
	if strings.ContainsAny(value, " \t") {
		escaped := strings.ReplaceAll(value, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return value
}

func normalizeUserID(userID string) string {
	value := strings.TrimSpace(userID)
	if value == "" {
		return "me"
	}
	return value
}

type GmailTool struct {
	name    string
	service *Service
}

func NewTool(name string, service *Service) GmailTool {
	return GmailTool{name: name, service: service}
}

func (t GmailTool) Name() string { return t.name }

func (t GmailTool) Description() string {
	switch t.name {
	case ToolNameListEmails:
		return "List Gmail messages by search criteria. This returns message summaries only and does not include attachment information; call gmail.getEmail to inspect attachments."
	case ToolNameGetEmail:
		return "Read one Gmail message in detail, including attachment metadata and message content. This is a sensitive read and requires approval."
	case ToolNameListLabels:
		return "List Gmail labels."
	case ToolNameGetProfile:
		return "Read the Gmail account profile."
	case ToolNameListThreads:
		return "List Gmail threads by search criteria."
	case ToolNameGetThread:
		return "Read one Gmail thread in detail."
	case ToolNameListDrafts:
		return "List Gmail drafts."
	case ToolNameGetDraft:
		return "Read one Gmail draft in detail."
	case ToolNameCreateDraft:
		return "Create a Gmail draft WITHOUT sending it. The email is NOT delivered until gmail.sendDraft is called with the returned Draft.ID value passed as draftId. If the user asked to send an email, you MUST call gmail.sendDraft after this tool succeeds."
	case ToolNameUpdateDraft:
		return "Update a Gmail draft. This external write requires approval."
	case ToolNameSendDraft:
		return "Send a Gmail draft, delivering it to recipients. Call this after gmail.createDraft succeeds, using Draft.ID from the createDraft result as the draftId argument. This is the step that actually sends the email."
	case ToolNameDeleteDraft:
		return "Delete an existing Gmail draft. This destructive action requires approval."
	case ToolNameReplyDraft:
		return "Create a Gmail reply draft. This external write requires approval."
	case ToolNameForwardDraft:
		return "Create a Gmail forward draft. This external write requires approval."
	case ToolNameDownloadAttachments:
		return "Download Gmail attachments to a local directory. Call gmail.getEmail immediately before gmail.downloadAttachments so the agent can use fresh attachment filenames from the current message. If filenames are provided, only matching attachments are downloaded. This local write requires approval."
	case ToolNameModifyMessage:
		return "Modify Gmail message labels such as read, unread, starred, archive, or inbox. This external write requires approval."
	case ToolNameBatchModifyMessages:
		return "Modify Gmail labels for multiple messages. This external write requires approval."
	case ToolNameTrashMessage:
		return "Move a Gmail message to trash. This destructive action requires approval."
	case ToolNameUntrashMessage:
		return "Restore a Gmail message from trash. This external write requires approval."
	default:
		return "Gmail tool."
	}
}

func (t GmailTool) Parameters() tools.ToolSchema {
	switch t.name {
	case ToolNameListEmails, ToolNameListThreads:
		return listSchema()
	case ToolNameListLabels, ToolNameGetProfile:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	case ToolNameGetEmail:
		return getEmailSchema()
	case ToolNameGetThread:
		return getThreadSchema()
	case ToolNameListDrafts:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"maxResults": maxResultsSchema(), "pageToken": map[string]any{"type": "string"}}, "additionalProperties": false}
	case ToolNameGetDraft:
		return getDraftSchema()
	case ToolNameCreateDraft:
		return draftSchema([]string{"to", "subject"})
	case ToolNameUpdateDraft:
		schema := draftSchema([]string{"draftId", "to", "subject"})
		props := schema["properties"].(map[string]any)
		props["draftId"] = map[string]any{"type": "string"}
		return schema
	case ToolNameSendDraft:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"draftId": map[string]any{"type": "string"}}, "required": []string{"draftId"}, "additionalProperties": false}
	case ToolNameDeleteDraft:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"draftId": map[string]any{"type": "string"}}, "required": []string{"draftId"}, "additionalProperties": false}
	case ToolNameReplyDraft:
		return replyDraftSchema()
	case ToolNameForwardDraft:
		return forwardDraftSchema()
	case ToolNameDownloadAttachments:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"messageId": map[string]any{"type": "string"}, "filenames": arrayStringSchema(), "outputDir": map[string]any{"type": "string", "description": "Optional workspace-relative directory. Omit to save into the workspace root. Paths outside the workspace are rejected."}}, "required": []string{"messageId"}, "additionalProperties": false}
	case ToolNameModifyMessage:
		return tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"messageId": map[string]any{"type": "string"},
				"action":    map[string]any{"type": "string", "enum": []string{"markRead", "markUnread", "star", "unstar", "archive", "moveToInbox", "addLabels", "removeLabels"}},
				"labelIds":  arrayStringSchema(),
			},
			"required":             []string{"messageId", "action"},
			"additionalProperties": false,
		}
	case ToolNameBatchModifyMessages:
		return tools.ToolSchema{
			"type": "object",
			"properties": map[string]any{
				"messageIds": arrayStringSchema(),
				"action":     map[string]any{"type": "string", "enum": []string{"markRead", "markUnread", "star", "unstar", "archive", "moveToInbox", "addLabels", "removeLabels"}},
				"labelIds":   arrayStringSchema(),
			},
			"required":             []string{"messageIds", "action"},
			"additionalProperties": false,
		}
	case ToolNameTrashMessage, ToolNameUntrashMessage:
		return tools.ToolSchema{"type": "object", "properties": map[string]any{"messageId": map[string]any{"type": "string"}}, "required": []string{"messageId"}, "additionalProperties": false}
	default:
		return tools.ToolSchema{"type": "object"}
	}
}

func (t GmailTool) Capability() tools.Capability {
	switch t.name {
	case ToolNameListEmails, ToolNameGetEmail, ToolNameListLabels, ToolNameGetProfile, ToolNameListThreads, ToolNameGetThread, ToolNameListDrafts, ToolNameGetDraft:
		return tools.CapabilityReadOnly
	default:
		return tools.CapabilityMutating
	}
}

func (t GmailTool) RiskLevel() tools.RiskLevel {
	switch t.name {
	case ToolNameGetEmail:
		// Reading a full email (body + attachment metadata) is a sensitive read:
		// it stays read-only but requires approval and is redacted from LLM context.
		return tools.RiskLevelSensitiveRead
	case ToolNameListEmails, ToolNameListLabels, ToolNameGetProfile, ToolNameListThreads, ToolNameGetThread, ToolNameListDrafts, ToolNameGetDraft:
		return tools.RiskLevelSafeRead
	case ToolNameDownloadAttachments:
		return tools.RiskLevelLocalWrite
	case ToolNameDeleteDraft, ToolNameTrashMessage:
		return tools.RiskLevelDestructive
	default:
		return tools.RiskLevelExternalWrite
	}
}

func (t GmailTool) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	switch t.name {
	case ToolNameListEmails:
		output, errShape := t.service.ListEmails(ctx, ListEmailsInput{Query: stringArg(call.Arguments, "query"), From: stringArg(call.Arguments, "from"), Subject: stringArg(call.Arguments, "subject"), After: stringArg(call.Arguments, "after"), Before: stringArg(call.Arguments, "before"), LabelIDs: stringSliceArg(call.Arguments, "labelIds"), MaxResults: listMaxResultsArg(call.Arguments), PageToken: stringArg(call.Arguments, "pageToken")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameListLabels:
		output, errShape := t.service.ListLabels(ctx, ListLabelsInput{})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameGetProfile:
		output, errShape := t.service.GetProfile(ctx, GetProfileInput{})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameGetEmail:
		output, errShape := t.service.GetEmail(ctx, GetEmailInput{MessageID: stringArg(call.Arguments, "messageId"), RenderMode: stringArg(call.Arguments, "renderMode"), Full: boolArg(call.Arguments, "full"), PreviewChars: intArg(call.Arguments, "previewChars")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameListThreads:
		output, errShape := t.service.ListThreads(ctx, ListThreadsInput{Query: stringArg(call.Arguments, "query"), From: stringArg(call.Arguments, "from"), Subject: stringArg(call.Arguments, "subject"), After: stringArg(call.Arguments, "after"), Before: stringArg(call.Arguments, "before"), LabelIDs: stringSliceArg(call.Arguments, "labelIds"), MaxResults: listMaxResultsArg(call.Arguments), PageToken: stringArg(call.Arguments, "pageToken")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameGetThread:
		output, errShape := t.service.GetThread(ctx, GetThreadInput{ThreadID: stringArg(call.Arguments, "threadId"), RenderMode: stringArg(call.Arguments, "renderMode"), Full: boolArg(call.Arguments, "full"), PreviewChars: intArg(call.Arguments, "previewChars")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameListDrafts:
		output, errShape := t.service.ListDrafts(ctx, ListDraftsInput{MaxResults: boundedInt64Arg(call.Arguments, "maxResults", defaultMaxResults, maxAllowedResults), PageToken: stringArg(call.Arguments, "pageToken")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameGetDraft:
		output, errShape := t.service.GetDraft(ctx, GetDraftInput{DraftID: stringArg(call.Arguments, "draftId"), RenderMode: stringArg(call.Arguments, "renderMode"), Full: boolArg(call.Arguments, "full"), PreviewChars: intArg(call.Arguments, "previewChars")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameCreateDraft:
		output, errShape := t.service.CreateDraft(ctx, draftInputFromArgs(call.Arguments))
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameUpdateDraft:
		input := UpdateDraftInput{DraftInput: draftInputFromArgs(call.Arguments), DraftID: stringArg(call.Arguments, "draftId")}
		output, errShape := t.service.UpdateDraft(ctx, input)
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameSendDraft:
		output, errShape := t.service.SendDraft(ctx, SendDraftInput{DraftID: stringArg(call.Arguments, "draftId")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameDeleteDraft:
		output, errShape := t.service.DeleteDraft(ctx, DeleteDraftInput{DraftID: stringArg(call.Arguments, "draftId")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameReplyDraft:
		input := ReplyDraftInput{DraftInput: draftInputFromArgs(call.Arguments), MessageID: stringArg(call.Arguments, "messageId")}
		output, errShape := t.service.ReplyDraft(ctx, input)
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameForwardDraft:
		input := ForwardDraftInput{DraftInput: draftInputFromArgs(call.Arguments), MessageID: stringArg(call.Arguments, "messageId")}
		output, errShape := t.service.ForwardDraft(ctx, input)
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameDownloadAttachments:
		output, errShape := t.service.DownloadAttachments(ctx, DownloadAttachmentsInput{MessageID: stringArg(call.Arguments, "messageId"), Filenames: stringSliceArg(call.Arguments, "filenames"), OutputDir: stringArg(call.Arguments, "outputDir")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameModifyMessage:
		output, errShape := t.service.ModifyMessage(ctx, ModifyMessageInput{MessageID: stringArg(call.Arguments, "messageId"), Action: stringArg(call.Arguments, "action"), LabelIDs: stringSliceArg(call.Arguments, "labelIds")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameBatchModifyMessages:
		output, errShape := t.service.BatchModifyMessages(ctx, BatchModifyMessagesInput{MessageIDs: stringSliceArg(call.Arguments, "messageIds"), Action: stringArg(call.Arguments, "action"), LabelIDs: stringSliceArg(call.Arguments, "labelIds")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameTrashMessage:
		output, errShape := t.service.TrashMessage(ctx, TrashMessageInput{MessageID: stringArg(call.Arguments, "messageId")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	case ToolNameUntrashMessage:
		output, errShape := t.service.UntrashMessage(ctx, UntrashMessageInput{MessageID: stringArg(call.Arguments, "messageId")})
		return outputToolResult(call, output, errShape, t.service.localLocation())
	default:
		return tools.ToolNotFoundResult(call)
	}
}

func RegisterTools(registry *tools.ToolRegistry, service *Service) error {
	for _, entry := range RegistryEntries {
		if err := registry.RegisterWithEntry(NewTool(entry.Name, service), tools.ToolRegistryEntry{Owner: "integration", Group: "google_workspace"}); err != nil {
			return err
		}
	}
	return nil
}

func outputToolResult(call tools.ToolCall, output any, errShape *ErrorShape, location *time.Location) tools.ToolResult {
	if errShape != nil {
		return toolErrorResult(call, errShape)
	}
	userContent := gmailUserSummary(call.Name, output)
	if call.Name == ToolNameGetEmail {
		userContent = formatJSON(output)
	} else if call.Name == ToolNameListEmails {
		if out, ok := output.(ListEmailsOutput); ok && len(out.Messages) == 1 {
			userContent = formatJSON(output)
		}
	}
	llmContent := formatJSON(compactOutputForLLM(call.Name, output, location))
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: llmContent, ContentForUser: userContent, ArtifactRef: gmailArtifactRef(call.Name, output)}
}

func gmailUserSummary(toolName string, output any) string {
	switch toolName {
	case ToolNameListEmails:
		if out, ok := output.(ListEmailsOutput); ok {
			return fmt.Sprintf("Đã tìm thấy %d email", len(out.Messages))
		}
	case ToolNameGetEmail:
		if out, ok := output.(GetEmailOutput); ok {
			from := strings.TrimSpace(out.Message.From)
			subject := strings.TrimSpace(out.Message.Subject)
			if from != "" && subject != "" {
				return fmt.Sprintf("Đã đọc email từ %s với chủ đề: %s", from, subject)
			}
			if subject != "" {
				return fmt.Sprintf("Đã đọc email với chủ đề: %s", subject)
			}
			return "Đã đọc email"
		}
	case ToolNameCreateDraft, ToolNameUpdateDraft, ToolNameReplyDraft, ToolNameForwardDraft:
		return "Đã tạo bản nháp email"
	case ToolNameSendDraft:
		if out, ok := output.(SendDraftOutput); ok {
			if recipient := strings.TrimSpace(out.Message.To); recipient != "" {
				return fmt.Sprintf("Đã gửi email tới %s", recipient)
			}
		}
		return "Đã gửi email"
	case ToolNameDeleteDraft:
		return "Đã xóa bản nháp"
	case ToolNameTrashMessage:
		return "Đã chuyển email vào thùng rác"
	case ToolNameUntrashMessage:
		return "Đã khôi phục email từ thùng rác"
	case ToolNameListLabels:
		if out, ok := output.(ListLabelsOutput); ok {
			return fmt.Sprintf("Đã tìm thấy %d nhãn Gmail", len(out.Labels))
		}
	case ToolNameGetProfile:
		if out, ok := output.(GetProfileOutput); ok {
			if email := strings.TrimSpace(out.Profile.EmailAddress); email != "" {
				return fmt.Sprintf("Đã đọc hồ sơ Gmail của %s", email)
			}
			return "Đã đọc hồ sơ Gmail"
		}
	case ToolNameListThreads:
		if out, ok := output.(ListThreadsOutput); ok {
			return fmt.Sprintf("Đã tìm thấy %d chuỗi email", len(out.Threads))
		}
	case ToolNameGetThread:
		if out, ok := output.(GetThreadOutput); ok {
			return fmt.Sprintf("Đã đọc chuỗi email gồm %d tin nhắn", len(out.Messages))
		}
	case ToolNameListDrafts:
		if out, ok := output.(ListDraftsOutput); ok {
			return fmt.Sprintf("Đã tìm thấy %d bản nháp", len(out.Drafts))
		}
	case ToolNameGetDraft:
		if out, ok := output.(GetDraftOutput); ok {
			if subject := strings.TrimSpace(out.Draft.Message.Subject); subject != "" {
				return fmt.Sprintf("Đã đọc bản nháp với chủ đề: %s", subject)
			}
			return "Đã đọc bản nháp"
		}
	case ToolNameDownloadAttachments:
		if out, ok := output.(DownloadAttachmentsOutput); ok {
			return fmt.Sprintf("Đã tải xuống %d tệp đính kèm", len(out.Files))
		}
	case ToolNameModifyMessage:
		return "Đã cập nhật nhãn email"
	case ToolNameBatchModifyMessages:
		if out, ok := output.(BatchModifyMessagesOutput); ok {
			return fmt.Sprintf("Đã cập nhật nhãn cho %d email", len(out.Result.MessageIDs))
		}
		return "Đã cập nhật nhãn cho nhiều email"
	}
	return formatJSON(output)
}

// gmailArtifactRef returns a typed reference to the message produced by a Gmail
// write tool, so the messenger no longer has to parse it back out of the result
// text. Returns nil when the tool produces no referenceable artifact.
func gmailArtifactRef(toolName string, output any) *tools.ToolArtifactRef {
	if toolName != ToolNameSendDraft {
		return nil
	}
	out, ok := output.(SendDraftOutput)
	if !ok {
		return nil
	}
	id := strings.TrimSpace(out.Message.ID)
	if id == "" {
		return nil
	}
	return &tools.ToolArtifactRef{
		Kind:  "gmail.message",
		Label: "Gmail message",
		ID:    id,
		URI:   "https://mail.google.com/mail/u/0/#sent/" + id,
	}
}

func toolErrorResult(call tools.ToolCall, errShape *ErrorShape) tools.ToolResult {
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: false, ContentForLLM: errShape.Code + ": " + errShape.Message, ContentForUser: errShape.Message, Error: &tools.ToolError{Code: errShape.Code, Message: errShape.Message}}
}

func formatJSON(output any) string {
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%#v", output)
	}
	return string(data)
}

func compactOutputForLLM(toolName string, output any, location *time.Location) any {
	switch toolName {
	case ToolNameListEmails:
		if list, ok := output.(ListEmailsOutput); ok {
			return compactListEmailsOutput(list, location)
		}
	case ToolNameListThreads:
		if list, ok := output.(ListThreadsOutput); ok {
			return compactListThreadsOutput(list)
		}
	case ToolNameListDrafts:
		if list, ok := output.(ListDraftsOutput); ok {
			return compactListDraftsOutput(list)
		}
	case ToolNameGetEmail:
		if msg, ok := output.(GetEmailOutput); ok {
			return compactGetEmailOutput(msg, location)
		}
	case ToolNameGetThread:
		if thread, ok := output.(GetThreadOutput); ok {
			return compactGetThreadOutput(thread, location)
		}
	case ToolNameGetDraft:
		if draft, ok := output.(GetDraftOutput); ok {
			return compactGetDraftOutput(draft, location)
		}
	}
	return output
}

// compactEmailDetail mirrors a single message for the LLM, replacing the raw
// "Date" header (sender timezone) with LocalDate/LocalDateTime in the user's
// timezone. See compactEmailMessage for why the raw header is omitted.
type compactEmailDetail struct {
	ID            string                      `json:"ID"`
	ThreadID      string                      `json:"ThreadID"`
	From          string                      `json:"From"`
	To            string                      `json:"To"`
	Subject       string                      `json:"Subject"`
	LocalDate     string                      `json:"LocalDate,omitempty"`
	LocalDateTime string                      `json:"LocalDateTime,omitempty"`
	LabelIDs      []string                    `json:"LabelIDs,omitempty"`
	InternalDate  int64                       `json:"InternalDate"`
	Attachments   []gmailconnector.Attachment `json:"Attachments,omitempty"`
}

func compactMessageDetail(m gmailconnector.MessageDetail, location *time.Location) compactEmailDetail {
	if location == nil {
		location = time.Local
	}
	detail := compactEmailDetail{
		ID:           m.ID,
		ThreadID:     m.ThreadID,
		From:         m.From,
		To:           m.To,
		Subject:      m.Subject,
		LabelIDs:     m.LabelIDs,
		InternalDate: m.InternalDate,
		Attachments:  m.Attachments,
	}
	if m.InternalDate > 0 {
		localTime := time.UnixMilli(m.InternalDate).In(location)
		detail.LocalDate = localTime.Format("2006-01-02")
		detail.LocalDateTime = localTime.Format(time.RFC3339)
	}
	return detail
}

type compactGetEmail struct {
	Message compactEmailDetail `json:"Message"`
	Display DisplayOutput      `json:"Display"`
}

func compactGetEmailOutput(output GetEmailOutput, location *time.Location) compactGetEmail {
	return compactGetEmail{Message: compactMessageDetail(output.Message, location), Display: output.Display}
}

type compactThreadMessage struct {
	Message compactEmailDetail `json:"Message"`
	Display DisplayOutput      `json:"Display"`
}

type compactGetThread struct {
	ThreadID string                 `json:"ThreadID"`
	Snippet  string                 `json:"Snippet,omitempty"`
	Messages []compactThreadMessage `json:"Messages"`
}

func compactGetThreadOutput(output GetThreadOutput, location *time.Location) compactGetThread {
	messages := make([]compactThreadMessage, 0, len(output.Messages))
	for _, m := range output.Messages {
		messages = append(messages, compactThreadMessage{
			Message: compactMessageDetail(m.Message, location),
			Display: m.Display,
		})
	}
	return compactGetThread{ThreadID: output.Thread.ID, Snippet: output.Thread.Snippet, Messages: messages}
}

type compactGetDraft struct {
	DraftID   string             `json:"DraftID"`
	MessageID string             `json:"MessageID,omitempty"`
	ThreadID  string             `json:"ThreadID,omitempty"`
	Message   compactEmailDetail `json:"Message"`
	Display   DisplayOutput      `json:"Display"`
}

func compactGetDraftOutput(output GetDraftOutput, location *time.Location) compactGetDraft {
	return compactGetDraft{
		DraftID:   output.Draft.ID,
		MessageID: output.Draft.MessageID,
		ThreadID:  output.Draft.ThreadID,
		Message:   compactMessageDetail(output.Draft.Message, location),
		Display:   output.Display,
	}
}

type compactListEmails struct {
	Query         string                `json:"Query"`
	Messages      []compactEmailMessage `json:"Messages"`
	NextPageToken string                `json:"NextPageToken,omitempty"`
}

// compactEmailMessage intentionally omits the raw "Date" header. That header
// carries the sender's own timezone offset (e.g. -0700) and the model would
// otherwise echo it verbatim, reporting the wrong local time. LocalDate and
// LocalDateTime are derived from InternalDate in the user's timezone and are
// the only date fields the model should display.
type compactEmailMessage struct {
	ID            string   `json:"ID"`
	ThreadID      string   `json:"ThreadID"`
	From          string   `json:"From"`
	To            string   `json:"To"`
	Subject       string   `json:"Subject"`
	LocalDate     string   `json:"LocalDate,omitempty"`
	LocalDateTime string   `json:"LocalDateTime,omitempty"`
	LabelIDs      []string `json:"LabelIDs,omitempty"`
	InternalDate  int64    `json:"InternalDate"`
}

func compactListEmailsOutput(output ListEmailsOutput, location *time.Location) compactListEmails {
	if location == nil {
		location = time.Local
	}
	messages := make([]compactEmailMessage, 0, len(output.Messages))
	for _, message := range output.Messages {
		compact := compactEmailMessage{
			ID:           message.ID,
			ThreadID:     message.ThreadID,
			From:         message.From,
			To:           message.To,
			Subject:      message.Subject,
			LabelIDs:     message.LabelIDs,
			InternalDate: message.InternalDate,
		}
		if message.InternalDate > 0 {
			localTime := time.UnixMilli(message.InternalDate).In(location)
			compact.LocalDate = localTime.Format("2006-01-02")
			compact.LocalDateTime = localTime.Format(time.RFC3339)
		}
		messages = append(messages, compact)
	}
	return compactListEmails{Query: output.Query, Messages: messages, NextPageToken: output.NextPageToken}
}

type compactListThreads struct {
	Query         string          `json:"Query"`
	Threads       []compactThread `json:"Threads"`
	NextPageToken string          `json:"NextPageToken,omitempty"`
}

type compactThread struct {
	ID        string `json:"ID"`
	HistoryID uint64 `json:"HistoryID"`
}

func compactListThreadsOutput(output ListThreadsOutput) compactListThreads {
	threads := make([]compactThread, 0, len(output.Threads))
	for _, thread := range output.Threads {
		threads = append(threads, compactThread{ID: thread.ID, HistoryID: thread.HistoryID})
	}
	return compactListThreads{Query: output.Query, Threads: threads, NextPageToken: output.NextPageToken}
}

type compactListDrafts struct {
	Drafts        []gmailconnector.DraftSummary `json:"Drafts"`
	NextPageToken string                        `json:"NextPageToken,omitempty"`
}

func compactListDraftsOutput(output ListDraftsOutput) compactListDrafts {
	return compactListDrafts{Drafts: output.Drafts, NextPageToken: output.NextPageToken}
}

func draftInputFromArgs(args map[string]any) DraftInput {
	return DraftInput{To: stringSliceArg(args, "to"), Cc: stringSliceArg(args, "cc"), Bcc: stringSliceArg(args, "bcc"), Subject: stringArg(args, "subject"), TextBody: multilineStringArg(args, "textBody"), HTMLBody: multilineStringArg(args, "htmlBody"), ThreadID: stringArg(args, "threadId"), Attachments: stringSliceArg(args, "attachments"), DriveAttachments: stringSliceArg(args, "driveAttachments")}
}

// multilineStringArg is like stringArg but joins array values with newlines.
// LLMs occasionally send textBody/htmlBody as a string array instead of a
// single string; joining preserves all paragraphs instead of dropping them.
func multilineStringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	switch value := args[name].(type) {
	case string:
		return value
	case []string:
		return strings.Join(value, "\n")
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func listSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "from": map[string]any{"type": "string"}, "subject": map[string]any{"type": "string"}, "after": map[string]any{"type": "string"}, "before": map[string]any{"type": "string"}, "labelIds": arrayStringSchema(), "maxResults": maxResultsSchema(), "pageToken": map[string]any{"type": "string"}}, "additionalProperties": false}
}

func maxResultsSchema() map[string]any {
	return map[string]any{
		"type":    "number",
		"minimum": 1,
		"maximum": maxAllowedResults,
		"description": "OMIT this for normal listing. When omitted, the tool returns ALL matching results " +
			"by paginating automatically. Set it ONLY when the user explicitly asks for a specific number " +
			"(e.g. \"my 5 latest emails\"); a set value returns just that many from a single page and may truncate the list.",
	}
}

func getEmailSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{"messageId": map[string]any{"type": "string"}, "renderMode": map[string]any{"type": "string", "enum": []string{RenderModeText, RenderModeRawHTML}}, "full": map[string]any{"type": "boolean"}, "previewChars": map[string]any{"type": "number"}}, "required": []string{"messageId"}, "additionalProperties": false}
}

func getThreadSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{"threadId": map[string]any{"type": "string"}, "renderMode": map[string]any{"type": "string", "enum": []string{RenderModeText, RenderModeRawHTML}}, "full": map[string]any{"type": "boolean"}, "previewChars": map[string]any{"type": "number"}}, "required": []string{"threadId"}, "additionalProperties": false}
}

func getDraftSchema() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{"draftId": map[string]any{"type": "string"}, "renderMode": map[string]any{"type": "string", "enum": []string{RenderModeText, RenderModeRawHTML}}, "full": map[string]any{"type": "boolean"}, "previewChars": map[string]any{"type": "number"}}, "required": []string{"draftId"}, "additionalProperties": false}
}

func draftSchema(required []string) tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{
		"to":          emailArraySchema("Recipient email addresses. Only include valid non-empty email addresses. Do not include empty strings."),
		"cc":          emailArraySchema("CC email addresses. Only include valid non-empty email addresses."),
		"bcc":         emailArraySchema("BCC email addresses. Only include valid non-empty email addresses."),
		"subject":     map[string]any{"type": "string"},
		"textBody":    map[string]any{"type": "string"},
		"htmlBody":    map[string]any{"type": "string"},
		"threadId":    map[string]any{"type": "string"},
		"attachments":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Local file system paths to attach (e.g. /tmp/file.pdf). Do NOT use this for Google Drive files — use driveAttachments instead."},
		"driveAttachments": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Google Drive file IDs to attach (not filenames or URLs — the Drive file ID string such as '1abc...'). Use this whenever the file is on Google Drive."},
	}, "required": required, "additionalProperties": false}
}

func emailArraySchema(description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string", "minLength": 1},
		"description": description,
	}
}

func replyDraftSchema() tools.ToolSchema {
	schema := draftSchema([]string{"to", "threadId"})
	props := schema["properties"].(map[string]any)
	props["messageId"] = map[string]any{"type": "string", "description": "ID of the specific message to reply to. Provides proper Re: headers and quoted text. Preferred over threadId when available."}
	props["threadId"] = map[string]any{"type": "string", "description": "Thread ID to reply in. Required. Always include this from the email listing result."}
	return schema
}

func forwardDraftSchema() tools.ToolSchema {
	schema := draftSchema([]string{"messageId", "to"})
	props := schema["properties"].(map[string]any)
	props["messageId"] = map[string]any{"type": "string"}
	return schema
}

func arrayStringSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	switch value := args[name].(type) {
	case string:
		return value
	case []string:
		for _, item := range value {
			if text := strings.TrimSpace(item); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				return text
			}
		}
	}
	return ""
}

func boolArg(args map[string]any, name string) bool {
	if args == nil {
		return false
	}
	value, _ := args[name].(bool)
	return value
}

func intArg(args map[string]any, name string) int {
	return int(int64Arg(args, name))
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

func stringSliceArg(args map[string]any, name string) []string {
	if args == nil {
		return nil
	}
	switch value := args[name].(type) {
	case []string:
		return cleanStringSlice(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return cleanStringSlice(out)
	case string:
		return cleanStringSlice(strings.Split(value, ","))
	default:
		return nil
	}
}
