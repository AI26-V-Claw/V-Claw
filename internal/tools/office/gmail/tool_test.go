package gmail

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gmailconnector "vclaw/internal/connectors/google/gmail"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

type mockConnector struct {
	listMessages       func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error)
	getMessage         func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error)
	listThreads        func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.ThreadSummary, string, error)
	getThread          func(ctx context.Context, userID string, threadID string) (gmailconnector.ThreadDetail, error)
	createDraft        func(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error)
	updateDraft        func(ctx context.Context, userID string, draftID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error)
	sendDraft          func(ctx context.Context, userID string, draftID string) (gmailconnector.MessageSummary, error)
	downloadAttachment func(ctx context.Context, userID string, messageID string, attachment gmailconnector.Attachment) (gmailconnector.AttachmentData, error)
	modifyMessage      func(ctx context.Context, userID string, messageID string, input gmailconnector.ModifyMessageInput) (gmailconnector.ModifyMessageOutput, error)
}

func (m *mockConnector) ListMessages(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error) {
	if m.listMessages == nil {
		return nil, "", nil
	}
	return m.listMessages(ctx, userID, query, labelIDs, maxResults, pageToken)
}

func (m *mockConnector) GetMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
	if m.getMessage == nil {
		return gmailconnector.MessageDetail{}, nil
	}
	return m.getMessage(ctx, userID, messageID)
}

func (m *mockConnector) ListThreads(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.ThreadSummary, string, error) {
	if m.listThreads == nil {
		return nil, "", nil
	}
	return m.listThreads(ctx, userID, query, labelIDs, maxResults, pageToken)
}

func (m *mockConnector) GetThread(ctx context.Context, userID string, threadID string) (gmailconnector.ThreadDetail, error) {
	if m.getThread == nil {
		return gmailconnector.ThreadDetail{}, nil
	}
	return m.getThread(ctx, userID, threadID)
}

func (m *mockConnector) CreateDraft(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error) {
	if m.createDraft == nil {
		return gmailconnector.DraftSummary{}, nil
	}
	return m.createDraft(ctx, userID, input)
}

func (m *mockConnector) UpdateDraft(ctx context.Context, userID string, draftID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error) {
	if m.updateDraft == nil {
		return gmailconnector.DraftSummary{}, nil
	}
	return m.updateDraft(ctx, userID, draftID, input)
}

func (m *mockConnector) SendDraft(ctx context.Context, userID string, draftID string) (gmailconnector.MessageSummary, error) {
	if m.sendDraft == nil {
		return gmailconnector.MessageSummary{}, nil
	}
	return m.sendDraft(ctx, userID, draftID)
}

func (m *mockConnector) DownloadAttachment(ctx context.Context, userID string, messageID string, attachment gmailconnector.Attachment) (gmailconnector.AttachmentData, error) {
	if m.downloadAttachment == nil {
		return gmailconnector.AttachmentData{}, nil
	}
	return m.downloadAttachment(ctx, userID, messageID, attachment)
}

func (m *mockConnector) ModifyMessage(ctx context.Context, userID string, messageID string, input gmailconnector.ModifyMessageInput) (gmailconnector.ModifyMessageOutput, error) {
	if m.modifyMessage == nil {
		return gmailconnector.ModifyMessageOutput{}, nil
	}
	return m.modifyMessage(ctx, userID, messageID, input)
}

func TestBuildSearchQuery(t *testing.T) {
	query, err := BuildSearchQuery(ListEmailsInput{
		Query:   "is:unread",
		From:    "alice@example.com",
		Subject: "weekly report",
		After:   "2026-06-01",
		Before:  "2026-06-30",
	})
	if err != nil {
		t.Fatalf("BuildSearchQuery() error = %v", err)
	}

	want := `is:unread from:alice@example.com subject:"weekly report" after:2026/06/01 before:2026/06/30`
	if query != want {
		t.Fatalf("BuildSearchQuery() = %q, want %q", query, want)
	}
}

func TestBuildSearchQueryRejectsInvalidDate(t *testing.T) {
	_, err := BuildSearchQuery(ListEmailsInput{After: "2026/06/01"})
	if err == nil {
		t.Fatal("BuildSearchQuery() error = nil, want non-nil")
	}
}

func TestListEmailsValidatesMaxResults(t *testing.T) {
	service := NewService(&mockConnector{})

	_, errShape := service.ListEmails(context.Background(), ListEmailsInput{
		MaxResults: 99,
	})
	if errShape == nil {
		t.Fatal("ListEmails() errShape = nil, want non-nil")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("ListEmails() errShape.Code = %q, want INVALID_INPUT", errShape.Code)
	}
}

func TestGetEmailRequiresMessageID(t *testing.T) {
	service := NewService(&mockConnector{})

	_, errShape := service.GetEmail(context.Background(), GetEmailInput{})
	if errShape == nil {
		t.Fatal("GetEmail() errShape = nil, want non-nil")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("GetEmail() errShape.Code = %q, want INVALID_INPUT", errShape.Code)
	}
}

func TestGetEmailBuildsDisplayText(t *testing.T) {
	service := NewService(&mockConnector{
		getMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
			return gmailconnector.MessageDetail{
				MessageSummary: gmailconnector.MessageSummary{ID: "m1"},
				BodyPlain:      "hello plain",
				BodyHTML:       "<p>hello html</p>",
			}, nil
		},
	})

	output, errShape := service.GetEmail(context.Background(), GetEmailInput{
		MessageID: "m1",
	})
	if errShape != nil {
		t.Fatalf("GetEmail() errShape = %v", errShape)
	}
	if output.Display.Mode != RenderModeText {
		t.Fatalf("GetEmail() Display.Mode = %q, want %q", output.Display.Mode, RenderModeText)
	}
	if output.Display.Source != displaySourcePlain {
		t.Fatalf("GetEmail() Display.Source = %q, want %q", output.Display.Source, displaySourcePlain)
	}
	if !strings.Contains(output.Display.Text, "hello plain") {
		t.Fatalf("GetEmail() Display.Text = %q", output.Display.Text)
	}
}

func TestGetEmailRejectsInvalidRenderMode(t *testing.T) {
	service := NewService(&mockConnector{
		getMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
			return gmailconnector.MessageDetail{
				MessageSummary: gmailconnector.MessageSummary{ID: "m1"},
			}, nil
		},
	})

	_, errShape := service.GetEmail(context.Background(), GetEmailInput{
		MessageID:  "m1",
		RenderMode: "xml",
	})
	if errShape == nil {
		t.Fatal("GetEmail() errShape = nil, want non-nil")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Fatalf("GetEmail() errShape.Code = %q, want INVALID_INPUT", errShape.Code)
	}
}

func TestListEmailsMapsConnectorError(t *testing.T) {
	service := NewService(&mockConnector{
		listMessages: func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error) {
			return nil, "", &googleapi.Error{Code: 401, Message: "expired"}
		},
	})

	_, errShape := service.ListEmails(context.Background(), ListEmailsInput{})
	if errShape == nil {
		t.Fatal("ListEmails() errShape = nil, want non-nil")
	}
	if errShape.Code != "AUTH_EXPIRED" {
		t.Fatalf("ListEmails() errShape.Code = %q, want AUTH_EXPIRED", errShape.Code)
	}
}

func TestMapErrorFallback(t *testing.T) {
	errShape := MapError(errors.New("boom"))
	if errShape.Code != "INTERNAL_ERROR" {
		t.Fatalf("MapError() = %q, want INTERNAL_ERROR", errShape.Code)
	}
}

func TestListThreadsBuildsQuery(t *testing.T) {
	var capturedQuery string
	service := NewService(&mockConnector{
		listThreads: func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.ThreadSummary, string, error) {
			capturedQuery = query
			return []gmailconnector.ThreadSummary{{ID: "t1"}}, "next", nil
		},
	})

	output, errShape := service.ListThreads(context.Background(), ListThreadsInput{
		From:       "alice@example.com",
		Subject:    "weekly report",
		MaxResults: 5,
	})
	if errShape != nil {
		t.Fatalf("ListThreads() errShape = %v", errShape)
	}
	if capturedQuery != `from:alice@example.com subject:"weekly report"` {
		t.Fatalf("unexpected query: %q", capturedQuery)
	}
	if len(output.Threads) != 1 || output.NextPageToken != "next" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestCreateDraftValidatesRecipientAndBody(t *testing.T) {
	service := NewService(&mockConnector{})

	_, errShape := service.CreateDraft(context.Background(), DraftInput{TextBody: "hello"})
	if errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected recipient validation error, got %#v", errShape)
	}

	_, errShape = service.CreateDraft(context.Background(), DraftInput{To: []string{"a@example.com"}})
	if errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected body validation error, got %#v", errShape)
	}
}

func TestReplyDraftComposesFromOriginalMessageInServiceLayer(t *testing.T) {
	var created gmailconnector.DraftMessageInput
	service := NewService(&mockConnector{
		getMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
			return gmailconnector.MessageDetail{
				MessageSummary: gmailconnector.MessageSummary{
					ID:              messageID,
					ThreadID:        "thread-1",
					Subject:         "Question",
					MessageIDHeader: "<msg-1@example.com>",
					References:      "<root@example.com>",
				},
			}, nil
		},
		createDraft: func(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error) {
			created = input
			return gmailconnector.DraftSummary{ID: "draft-reply"}, nil
		},
	})

	output, errShape := service.ReplyDraft(context.Background(), ReplyDraftInput{
		MessageID: "m1",
		DraftInput: DraftInput{
			To:       []string{"alice@example.com"},
			TextBody: "reply",
		},
	})
	if errShape != nil {
		t.Fatalf("ReplyDraft() errShape = %v", errShape)
	}
	if output.Draft.ID != "draft-reply" {
		t.Fatalf("unexpected output: %#v", output)
	}
	if created.ThreadID != "thread-1" || created.Subject != "Re: Question" || created.InReplyTo != "<msg-1@example.com>" {
		t.Fatalf("reply draft was not composed from original: %#v", created)
	}
	if !strings.Contains(created.References, "<root@example.com>") || !strings.Contains(created.References, "<msg-1@example.com>") {
		t.Fatalf("reply references missing original ids: %q", created.References)
	}
}

func TestForwardDraftComposesFromOriginalMessageInServiceLayer(t *testing.T) {
	var created gmailconnector.DraftMessageInput
	service := NewService(&mockConnector{
		getMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
			return gmailconnector.MessageDetail{
				MessageSummary: gmailconnector.MessageSummary{
					ID:      messageID,
					From:    "alice@example.com",
					To:      "bob@example.com",
					Subject: "Report",
					Date:    "Mon, 01 Jun 2026 10:00:00 +0700",
				},
				BodyPlain: "original body",
			}, nil
		},
		createDraft: func(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error) {
			created = input
			return gmailconnector.DraftSummary{ID: "draft-forward"}, nil
		},
	})

	_, errShape := service.ForwardDraft(context.Background(), ForwardDraftInput{
		MessageID: "m1",
		DraftInput: DraftInput{
			To:       []string{"carol@example.com"},
			TextBody: "see below",
		},
	})
	if errShape != nil {
		t.Fatalf("ForwardDraft() errShape = %v", errShape)
	}
	if created.Subject != "Fwd: Report" {
		t.Fatalf("unexpected forward subject: %q", created.Subject)
	}
	if !strings.Contains(created.TextBody, "Forwarded message") || !strings.Contains(created.TextBody, "original body") {
		t.Fatalf("forward body missing original content: %q", created.TextBody)
	}
}

func TestModifyMessageMapsActions(t *testing.T) {
	var captured gmailconnector.ModifyMessageInput
	service := NewService(&mockConnector{
		modifyMessage: func(ctx context.Context, userID string, messageID string, input gmailconnector.ModifyMessageInput) (gmailconnector.ModifyMessageOutput, error) {
			captured = input
			return gmailconnector.ModifyMessageOutput{ID: messageID, LabelIDs: []string{"INBOX"}}, nil
		},
	})

	_, errShape := service.ModifyMessage(context.Background(), ModifyMessageInput{
		MessageID: "m1",
		Action:    "archive",
	})
	if errShape != nil {
		t.Fatalf("ModifyMessage() errShape = %v", errShape)
	}
	if len(captured.RemoveLabelIDs) != 1 || captured.RemoveLabelIDs[0] != "INBOX" {
		t.Fatalf("archive should remove INBOX, got %#v", captured)
	}
}

func TestDownloadAttachmentsWritesFiles(t *testing.T) {
	service := NewService(&mockConnector{
		getMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
			return gmailconnector.MessageDetail{
				MessageSummary: gmailconnector.MessageSummary{ID: messageID},
				Attachments: []gmailconnector.Attachment{{
					Filename:     "../report?.txt",
					MimeType:     "text/plain",
					AttachmentID: "att-1",
				}},
			}, nil
		},
		downloadAttachment: func(ctx context.Context, userID string, messageID string, attachment gmailconnector.Attachment) (gmailconnector.AttachmentData, error) {
			return gmailconnector.AttachmentData{
				Attachment: attachment,
				Data:       []byte("hello attachment"),
			}, nil
		},
	})

	outputDir := t.TempDir()
	output, errShape := service.DownloadAttachments(context.Background(), DownloadAttachmentsInput{
		MessageID: "m1",
		OutputDir: outputDir,
	})
	if errShape != nil {
		t.Fatalf("DownloadAttachments() errShape = %v", errShape)
	}
	if len(output.Files) != 1 {
		t.Fatalf("expected one file, got %#v", output)
	}
	if filepath.Dir(output.Files[0].Path) != outputDir {
		t.Fatalf("attachment should be written inside output dir, got %q", output.Files[0].Path)
	}
	data, err := os.ReadFile(output.Files[0].Path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "hello attachment" {
		t.Fatalf("unexpected file content: %q", data)
	}
}

func TestGmailToolRiskMetadataAndRegistration(t *testing.T) {
	service := NewService(&mockConnector{})
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, service); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}

	list, ok := registry.GetTool(ToolNameListThreads)
	if !ok {
		t.Fatal("expected list threads tool")
	}
	if list.Capability() != tools.CapabilityReadOnly || list.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list threads metadata mismatch: %s %s", list.Capability(), list.RiskLevel())
	}

	draft, ok := registry.GetTool(ToolNameCreateDraft)
	if !ok {
		t.Fatal("expected create draft tool")
	}
	if draft.Capability() != tools.CapabilityMutating || draft.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("create draft metadata mismatch: %s %s", draft.Capability(), draft.RiskLevel())
	}

	download, ok := registry.GetTool(ToolNameDownloadAttachments)
	if !ok {
		t.Fatal("expected download attachments tool")
	}
	if download.Capability() != tools.CapabilityMutating || download.RiskLevel() != tools.RiskLevelLocalWrite {
		t.Fatalf("download metadata mismatch: %s %s", download.Capability(), download.RiskLevel())
	}
}
