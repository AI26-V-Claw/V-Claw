package gmail

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	gmailconnector "vclaw/internal/connectors/google/gmail"

	"google.golang.org/api/googleapi"
)

const (
	ToolNameListEmails = "gmail.listEmails"
	ToolNameGetEmail   = "gmail.getEmail"
)

const (
	RenderModeText    = "text"
	RenderModeRawHTML = "raw-html"
)

const (
	defaultMaxResults = int64(10)
	maxAllowedResults = int64(50)
)

type ToolRegistryEntry struct {
	Name             string
	Owner            string
	DescriptionVi    string
	DefaultRiskLevel string
	RequiresApproval bool
}

var RegistryEntries = []ToolRegistryEntry{
	{
		Name:             ToolNameListEmails,
		Owner:            "integration",
		DescriptionVi:    "Liệt kê email theo điều kiện tìm kiếm.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
	{
		Name:             ToolNameGetEmail,
		Owner:            "integration",
		DescriptionVi:    "Đọc chi tiết một email.",
		DefaultRiskLevel: "safe_read",
		RequiresApproval: false,
	},
}

type ErrorShape struct {
	Code      string
	Message   string
	Retryable bool
}

type Connector interface {
	ListMessages(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]gmailconnector.MessageSummary, string, error)
	GetMessage(ctx context.Context, userID string, messageID string) (gmailconnector.MessageDetail, error)
}

type Service struct {
	connector Connector
}

func NewService(connector Connector) *Service {
	return &Service{connector: connector}
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

type DisplayOutput struct {
	Mode         string
	Source       string
	Text         string
	Truncated    bool
	PreviewChars int
}

func (s *Service) ListEmails(ctx context.Context, input ListEmailsInput) (ListEmailsOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return ListEmailsOutput{}, &ErrorShape{
			Code:      "INTERNAL_ERROR",
			Message:   "gmail connector is not configured",
			Retryable: false,
		}
	}

	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = defaultMaxResults
	}
	if maxResults < 1 || maxResults > maxAllowedResults {
		return ListEmailsOutput{}, &ErrorShape{
			Code:      "INVALID_INPUT",
			Message:   fmt.Sprintf("maxResults must be between 1 and %d", maxAllowedResults),
			Retryable: false,
		}
	}

	query, err := BuildSearchQuery(input)
	if err != nil {
		return ListEmailsOutput{}, &ErrorShape{
			Code:      "INVALID_INPUT",
			Message:   err.Error(),
			Retryable: false,
		}
	}

	userID := normalizeUserID(input.UserID)
	messages, nextPageToken, err := s.connector.ListMessages(ctx, userID, query, input.LabelIDs, maxResults, input.PageToken)
	if err != nil {
		return ListEmailsOutput{}, MapError(err)
	}

	return ListEmailsOutput{
		Query:         query,
		Messages:      messages,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Service) GetEmail(ctx context.Context, input GetEmailInput) (GetEmailOutput, *ErrorShape) {
	if s == nil || s.connector == nil {
		return GetEmailOutput{}, &ErrorShape{
			Code:      "INTERNAL_ERROR",
			Message:   "gmail connector is not configured",
			Retryable: false,
		}
	}

	if strings.TrimSpace(input.MessageID) == "" {
		return GetEmailOutput{}, &ErrorShape{
			Code:      "INVALID_INPUT",
			Message:   "messageId is required",
			Retryable: false,
		}
	}

	userID := normalizeUserID(input.UserID)
	message, err := s.connector.GetMessage(ctx, userID, input.MessageID)
	if err != nil {
		return GetEmailOutput{}, MapError(err)
	}

	display, errShape := buildDisplay(message, input)
	if errShape != nil {
		return GetEmailOutput{}, errShape
	}

	return GetEmailOutput{
		Message: message,
		Display: display,
	}, nil
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
	var gerr *googleapi.Error
	if !asGoogleError(err, &gerr) {
		return &ErrorShape{
			Code:      "INTERNAL_ERROR",
			Message:   err.Error(),
			Retryable: false,
		}
	}

	switch {
	case gerr.Code == http.StatusUnauthorized:
		return &ErrorShape{
			Code:      "AUTH_EXPIRED",
			Message:   gerr.Message,
			Retryable: true,
		}
	case gerr.Code == http.StatusForbidden && hasMissingScopeReason(gerr):
		return &ErrorShape{
			Code:      "AUTH_MISSING_SCOPE",
			Message:   gerr.Message,
			Retryable: false,
		}
	case gerr.Code == http.StatusTooManyRequests:
		return &ErrorShape{
			Code:      "RATE_LIMITED",
			Message:   gerr.Message,
			Retryable: true,
		}
	case gerr.Code >= 500:
		return &ErrorShape{
			Code:      "PROVIDER_UNAVAILABLE",
			Message:   gerr.Message,
			Retryable: true,
		}
	default:
		return &ErrorShape{
			Code:      "INTERNAL_ERROR",
			Message:   gerr.Message,
			Retryable: false,
		}
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
	return strings.Contains(text, "insufficient authentication scopes") ||
		strings.Contains(text, "insufficient permissions")
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
