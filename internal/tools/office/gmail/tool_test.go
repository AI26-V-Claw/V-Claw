package gmail

import (
	"context"
	"errors"
	"strings"
	"testing"

	gmailconnector "vclaw/internal/connectors/google/gmail"

	"google.golang.org/api/googleapi"
)

type mockConnector struct {
	listMessages func(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error)
	getMessage   func(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error)
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
