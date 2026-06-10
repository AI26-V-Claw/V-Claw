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
	listLabels         func(ctx context.Context, userID string) ([]gmailconnector.Label, error)
	getProfile         func(ctx context.Context, userID string) (gmailconnector.Profile, error)
	listMessages       func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error)
	getMessage         func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error)
	listThreads        func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.ThreadSummary, string, error)
	getThread          func(ctx context.Context, userID string, threadID string) (gmailconnector.ThreadDetail, error)
	listDrafts         func(ctx context.Context, userID string, maxResults int64, pageToken string) (gmailconnector.ListDraftsOutput, error)
	getDraft           func(ctx context.Context, userID string, draftID string) (gmailconnector.DraftDetail, error)
	createDraft        func(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error)
	updateDraft        func(ctx context.Context, userID string, draftID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error)
	sendDraft          func(ctx context.Context, userID string, draftID string) (gmailconnector.MessageSummary, error)
	deleteDraft        func(ctx context.Context, userID string, draftID string) error
	downloadAttachment func(ctx context.Context, userID string, messageID string, attachment gmailconnector.Attachment) (gmailconnector.AttachmentData, error)
	modifyMessage      func(ctx context.Context, userID string, messageID string, input gmailconnector.ModifyMessageInput) (gmailconnector.ModifyMessageOutput, error)
	batchModify        func(ctx context.Context, userID string, messageIDs []string, input gmailconnector.ModifyMessageInput) (gmailconnector.BatchModifyMessagesOutput, error)
	trashMessage       func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error)
	untrashMessage     func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error)
}

func (m *mockConnector) ListLabels(ctx context.Context, userID string) ([]gmailconnector.Label, error) {
	if m.listLabels == nil {
		return nil, nil
	}
	return m.listLabels(ctx, userID)
}

func (m *mockConnector) GetProfile(ctx context.Context, userID string) (gmailconnector.Profile, error) {
	if m.getProfile == nil {
		return gmailconnector.Profile{}, nil
	}
	return m.getProfile(ctx, userID)
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

func (m *mockConnector) ListDrafts(ctx context.Context, userID string, maxResults int64, pageToken string) (gmailconnector.ListDraftsOutput, error) {
	if m.listDrafts == nil {
		return gmailconnector.ListDraftsOutput{}, nil
	}
	return m.listDrafts(ctx, userID, maxResults, pageToken)
}

func (m *mockConnector) GetDraft(ctx context.Context, userID string, draftID string) (gmailconnector.DraftDetail, error) {
	if m.getDraft == nil {
		return gmailconnector.DraftDetail{}, nil
	}
	return m.getDraft(ctx, userID, draftID)
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

func (m *mockConnector) DeleteDraft(ctx context.Context, userID string, draftID string) error {
	if m.deleteDraft == nil {
		return nil
	}
	return m.deleteDraft(ctx, userID, draftID)
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

func (m *mockConnector) BatchModifyMessages(ctx context.Context, userID string, messageIDs []string, input gmailconnector.ModifyMessageInput) (gmailconnector.BatchModifyMessagesOutput, error) {
	if m.batchModify == nil {
		return gmailconnector.BatchModifyMessagesOutput{}, nil
	}
	return m.batchModify(ctx, userID, messageIDs, input)
}

func (m *mockConnector) TrashMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error) {
	if m.trashMessage == nil {
		return gmailconnector.MessageSummary{}, nil
	}
	return m.trashMessage(ctx, userID, messageID)
}

func (m *mockConnector) UntrashMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error) {
	if m.untrashMessage == nil {
		return gmailconnector.MessageSummary{}, nil
	}
	return m.untrashMessage(ctx, userID, messageID)
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

func TestListLabelsAndProfile(t *testing.T) {
	service := NewService(&mockConnector{
		listLabels: func(ctx context.Context, userID string) ([]gmailconnector.Label, error) {
			return []gmailconnector.Label{{ID: "INBOX", Name: "Inbox"}}, nil
		},
		getProfile: func(ctx context.Context, userID string) (gmailconnector.Profile, error) {
			return gmailconnector.Profile{EmailAddress: "me@example.com", MessagesTotal: 10, ThreadsTotal: 5, HistoryID: 123}, nil
		},
	})

	labels, errShape := service.ListLabels(context.Background(), ListLabelsInput{})
	if errShape != nil || len(labels.Labels) != 1 || labels.Labels[0].ID != "INBOX" {
		t.Fatalf("ListLabels() = %#v, %#v", labels, errShape)
	}
	profile, errShape := service.GetProfile(context.Background(), GetProfileInput{})
	if errShape != nil || profile.Profile.EmailAddress != "me@example.com" {
		t.Fatalf("GetProfile() = %#v, %#v", profile, errShape)
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

func TestDraftManagementService(t *testing.T) {
	service := NewService(&mockConnector{
		listDrafts: func(ctx context.Context, userID string, maxResults int64, pageToken string) (gmailconnector.ListDraftsOutput, error) {
			if maxResults != 5 || pageToken != "next" {
				t.Fatalf("unexpected list draft args: %d %q", maxResults, pageToken)
			}
			return gmailconnector.ListDraftsOutput{Drafts: []gmailconnector.DraftSummary{{ID: "draft-1"}}, NextPageToken: "next-2"}, nil
		},
		getDraft: func(ctx context.Context, userID string, draftID string) (gmailconnector.DraftDetail, error) {
			return gmailconnector.DraftDetail{
				DraftSummary: gmailconnector.DraftSummary{ID: draftID},
				Message: gmailconnector.MessageDetail{
					MessageSummary: gmailconnector.MessageSummary{ID: "msg-1"},
					BodyPlain:      "draft body",
				},
			}, nil
		},
		deleteDraft: func(ctx context.Context, userID string, draftID string) error {
			if draftID != "draft-1" {
				t.Fatalf("unexpected draft id: %q", draftID)
			}
			return nil
		},
	})

	listed, errShape := service.ListDrafts(context.Background(), ListDraftsInput{MaxResults: 5, PageToken: "next"})
	if errShape != nil || listed.NextPageToken != "next-2" {
		t.Fatalf("ListDrafts() = %#v, %#v", listed, errShape)
	}
	got, errShape := service.GetDraft(context.Background(), GetDraftInput{DraftID: "draft-1", Full: true})
	if errShape != nil || got.Draft.ID != "draft-1" || got.Display.Text != "draft body" {
		t.Fatalf("GetDraft() = %#v, %#v", got, errShape)
	}
	deleted, errShape := service.DeleteDraft(context.Background(), DeleteDraftInput{DraftID: "draft-1"})
	if errShape != nil || deleted.DraftID != "draft-1" {
		t.Fatalf("DeleteDraft() = %#v, %#v", deleted, errShape)
	}
}

func TestCreateDraftValidatesRecipientSubjectAndBody(t *testing.T) {
	service := NewService(&mockConnector{})

	_, errShape := service.CreateDraft(context.Background(), DraftInput{TextBody: "hello"})
	if errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected recipient validation error, got %#v", errShape)
	}

	_, errShape = service.CreateDraft(context.Background(), DraftInput{To: []string{"a@example.com"}, TextBody: "hello"})
	if errShape == nil || errShape.Code != "INVALID_INPUT" || !strings.Contains(errShape.Message, "subject") {
		t.Fatalf("expected subject validation error, got %#v", errShape)
	}

	_, errShape = service.CreateDraft(context.Background(), DraftInput{To: []string{"a@example.com"}, Subject: "Hello"})
	if errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected body validation error, got %#v", errShape)
	}
}

func TestCreateDraftLoadsAttachments(t *testing.T) {
	var captured gmailconnector.DraftMessageInput
	service := NewService(&mockConnector{
		createDraft: func(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error) {
			captured = input
			return gmailconnector.DraftSummary{ID: "draft-1"}, nil
		},
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(path, []byte("attachment"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	output, errShape := service.CreateDraft(context.Background(), DraftInput{
		To:          []string{"alice@example.com"},
		Subject:     "Report",
		TextBody:    "hello",
		Attachments: []string{path},
	})
	if errShape != nil {
		t.Fatalf("CreateDraft() errShape = %v", errShape)
	}
	if output.Draft.ID != "draft-1" || len(captured.Attachments) != 1 {
		t.Fatalf("unexpected draft output/input: %#v %#v", output, captured)
	}
	if captured.Attachments[0].Filename != "report.txt" || !strings.HasPrefix(captured.Attachments[0].MimeType, "text/plain") {
		t.Fatalf("unexpected attachment metadata: %#v", captured.Attachments[0])
	}
	if string(captured.Attachments[0].Data) != "attachment" {
		t.Fatalf("unexpected attachment data: %q", captured.Attachments[0].Data)
	}
}

func TestCreateDraftToolAcceptsSingleItemStringArrays(t *testing.T) {
	var captured gmailconnector.DraftMessageInput
	service := NewService(&mockConnector{
		createDraft: func(ctx context.Context, userID string, input gmailconnector.DraftMessageInput) (gmailconnector.DraftSummary, error) {
			captured = input
			return gmailconnector.DraftSummary{ID: "draft-1"}, nil
		},
	})
	tool := NewTool(ToolNameCreateDraft, service)

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_1",
		Name: ToolNameCreateDraft,
		Arguments: map[string]any{
			"to":       []any{"alice@example.com"},
			"subject":  []any{"Chúc mừng sinh nhật"},
			"textBody": []any{"Chúc mừng sinh nhật bạn!"},
		},
	})

	if !result.Success {
		t.Fatalf("expected successful draft creation, got %#v", result)
	}
	if captured.Subject != "Chúc mừng sinh nhật" || captured.TextBody != "Chúc mừng sinh nhật bạn!" {
		t.Fatalf("expected string arrays to normalize into draft fields, got %#v", captured)
	}
}

func TestCreateDraftRejectsMissingAttachment(t *testing.T) {
	service := NewService(&mockConnector{})

	_, errShape := service.CreateDraft(context.Background(), DraftInput{
		To:          []string{"alice@example.com"},
		Subject:     "Report",
		TextBody:    "hello",
		Attachments: []string{filepath.Join(t.TempDir(), "missing.txt")},
	})
	if errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected missing attachment validation error, got %#v", errShape)
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

func TestBatchModifyMessagesMapsActionsAndValidatesLimit(t *testing.T) {
	var capturedIDs []string
	var captured gmailconnector.ModifyMessageInput
	service := NewService(&mockConnector{
		batchModify: func(ctx context.Context, userID string, messageIDs []string, input gmailconnector.ModifyMessageInput) (gmailconnector.BatchModifyMessagesOutput, error) {
			capturedIDs = messageIDs
			captured = input
			return gmailconnector.BatchModifyMessagesOutput{MessageIDs: messageIDs, RemoveLabelIDs: input.RemoveLabelIDs}, nil
		},
	})

	output, errShape := service.BatchModifyMessages(context.Background(), BatchModifyMessagesInput{
		MessageIDs: []string{"m1", "m2"},
		Action:     "archive",
	})
	if errShape != nil {
		t.Fatalf("BatchModifyMessages() errShape = %v", errShape)
	}
	if strings.Join(capturedIDs, ",") != "m1,m2" || len(captured.RemoveLabelIDs) != 1 || captured.RemoveLabelIDs[0] != "INBOX" {
		t.Fatalf("unexpected batch modify input: %#v %#v", capturedIDs, captured)
	}
	if len(output.Result.MessageIDs) != 2 {
		t.Fatalf("unexpected output: %#v", output)
	}

	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "m"
	}
	_, errShape = service.BatchModifyMessages(context.Background(), BatchModifyMessagesInput{MessageIDs: tooMany, Action: "archive"})
	if errShape == nil || errShape.Code != "INVALID_INPUT" {
		t.Fatalf("expected too many message ids validation error, got %#v", errShape)
	}
}

func TestTrashAndUntrashMessage(t *testing.T) {
	service := NewService(&mockConnector{
		trashMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error) {
			return gmailconnector.MessageSummary{ID: messageID, Subject: "Trashed"}, nil
		},
		untrashMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageSummary, error) {
			return gmailconnector.MessageSummary{ID: messageID, Subject: "Restored"}, nil
		},
	})

	trashed, errShape := service.TrashMessage(context.Background(), TrashMessageInput{MessageID: "m1"})
	if errShape != nil || trashed.Message.Subject != "Trashed" {
		t.Fatalf("TrashMessage() = %#v, %#v", trashed, errShape)
	}
	restored, errShape := service.UntrashMessage(context.Background(), UntrashMessageInput{MessageID: "m1"})
	if errShape != nil || restored.Message.Subject != "Restored" {
		t.Fatalf("UntrashMessage() = %#v, %#v", restored, errShape)
	}
}

func TestModifyMessageSchemaIncludesLabelIDsProperty(t *testing.T) {
	schema := NewTool(ToolNameModifyMessage, nil).Parameters()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("modify message schema properties missing or invalid: %#v", schema["properties"])
	}

	for _, key := range []string{"messageId", "action", "labelIds"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("modify message schema missing properties.%s: %#v", key, properties)
		}
	}

	if _, ok := schema["labelIds"]; ok {
		t.Fatalf("modify message schema should not expose top-level labelIds: %#v", schema)
	}
}

func schemaRequires(schema tools.ToolSchema, name string) bool {
	required, ok := schema["required"].([]string)
	if !ok {
		return false
	}
	for _, item := range required {
		if item == name {
			return true
		}
	}
	return false
}

func TestCreateAndUpdateDraftSchemasRequireSubject(t *testing.T) {
	createSchema := NewTool(ToolNameCreateDraft, nil).Parameters()
	if !schemaRequires(createSchema, "to") || !schemaRequires(createSchema, "subject") {
		t.Fatalf("create draft schema should require to and subject: %#v", createSchema["required"])
	}

	updateSchema := NewTool(ToolNameUpdateDraft, nil).Parameters()
	if !schemaRequires(updateSchema, "draftId") || !schemaRequires(updateSchema, "to") || !schemaRequires(updateSchema, "subject") {
		t.Fatalf("update draft schema should require draftId, to, and subject: %#v", updateSchema["required"])
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

func TestDownloadAttachmentsFiltersByFilename(t *testing.T) {
	service := NewService(&mockConnector{
		getMessage: func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error) {
			return gmailconnector.MessageDetail{
				MessageSummary: gmailconnector.MessageSummary{ID: messageID},
				Attachments: []gmailconnector.Attachment{
					{Filename: "one.txt", MimeType: "text/plain", AttachmentID: "att-1"},
					{Filename: "two.txt", MimeType: "text/plain", AttachmentID: "att-2"},
				},
			}, nil
		},
		downloadAttachment: func(ctx context.Context, userID string, messageID string, attachment gmailconnector.Attachment) (gmailconnector.AttachmentData, error) {
			return gmailconnector.AttachmentData{
				Attachment: attachment,
				Data:       []byte(attachment.Filename),
			}, nil
		},
	})

	outputDir := t.TempDir()
	output, errShape := service.DownloadAttachments(context.Background(), DownloadAttachmentsInput{
		MessageID: "m1",
		Filenames: []string{"two.txt"},
		OutputDir: outputDir,
	})
	if errShape != nil {
		t.Fatalf("DownloadAttachments() errShape = %v", errShape)
	}
	if len(output.Files) != 1 {
		t.Fatalf("expected one file, got %#v", output)
	}
	if output.Files[0].Filename != "two.txt" {
		t.Fatalf("expected two.txt to be selected, got %#v", output.Files[0])
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

	labels, ok := registry.GetTool(ToolNameListLabels)
	if !ok {
		t.Fatal("expected list labels tool")
	}
	if labels.Capability() != tools.CapabilityReadOnly || labels.RiskLevel() != tools.RiskLevelSafeRead {
		t.Fatalf("list labels metadata mismatch: %s %s", labels.Capability(), labels.RiskLevel())
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
	schema := download.Parameters()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("download schema properties missing: %#v", schema)
	}
	if _, ok := properties["filenames"]; !ok {
		t.Fatalf("expected filenames in download schema: %#v", schema)
	}
	if _, ok := properties["attachmentIds"]; ok {
		t.Fatalf("attachmentIds should not remain in download schema: %#v", schema)
	}

	deleteDraft, ok := registry.GetTool(ToolNameDeleteDraft)
	if !ok {
		t.Fatal("expected delete draft tool")
	}
	if deleteDraft.Capability() != tools.CapabilityMutating || deleteDraft.RiskLevel() != tools.RiskLevelDestructive {
		t.Fatalf("delete draft metadata mismatch: %s %s", deleteDraft.Capability(), deleteDraft.RiskLevel())
	}

	batch, ok := registry.GetTool(ToolNameBatchModifyMessages)
	if !ok {
		t.Fatal("expected batch modify tool")
	}
	if batch.Capability() != tools.CapabilityMutating || batch.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("batch modify metadata mismatch: %s %s", batch.Capability(), batch.RiskLevel())
	}

	trash, ok := registry.GetTool(ToolNameTrashMessage)
	if !ok {
		t.Fatal("expected trash message tool")
	}
	if trash.Capability() != tools.CapabilityMutating || trash.RiskLevel() != tools.RiskLevelDestructive {
		t.Fatalf("trash metadata mismatch: %s %s", trash.Capability(), trash.RiskLevel())
	}

	untrash, ok := registry.GetTool(ToolNameUntrashMessage)
	if !ok {
		t.Fatal("expected untrash message tool")
	}
	if untrash.Capability() != tools.CapabilityMutating || untrash.RiskLevel() != tools.RiskLevelExternalWrite {
		t.Fatalf("untrash metadata mismatch: %s %s", untrash.Capability(), untrash.RiskLevel())
	}
}
