package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type Label struct {
	ID   string
	Name string
}

type MessageSummary struct {
	ID           string
	ThreadID     string
	From         string
	To           string
	Subject      string
	Date         string
	Snippet      string
	LabelIDs     []string
	InternalDate int64
}

type Attachment struct {
	Filename     string
	MimeType     string
	AttachmentID string
	Size         int64
}

type MessageDetail struct {
	MessageSummary
	BodyPlain   string
	BodyHTML    string
	Attachments []Attachment
}

func (c *Client) ListLabels(ctx context.Context, userID string) ([]Label, error) {
	return ListLabels(ctx, c.httpClient, userID)
}

func (c *Client) ListMessages(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]MessageSummary, string, error) {
	return ListMessages(ctx, c.httpClient, userID, query, labelIDs, maxResults, pageToken)
}

func (c *Client) GetMessage(ctx context.Context, userID string, messageID string) (MessageDetail, error) {
	return GetMessage(ctx, c.httpClient, userID, messageID)
}

func ListLabels(ctx context.Context, client *http.Client, userID string) ([]Label, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return nil, err
	}

	response, err := service.Users.Labels.List(userID).Do()
	if err != nil {
		return nil, err
	}

	labels := make([]Label, 0, len(response.Labels))
	for _, label := range response.Labels {
		labels = append(labels, Label{
			ID:   label.Id,
			Name: label.Name,
		})
	}
	return labels, nil
}

func ListMessages(ctx context.Context, client *http.Client, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]MessageSummary, string, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return nil, "", err
	}

	call := service.Users.Messages.List(userID).MaxResults(maxResults)
	if strings.TrimSpace(query) != "" {
		call = call.Q(query)
	}
	if len(labelIDs) > 0 {
		call = call.LabelIds(labelIDs...)
	}
	if strings.TrimSpace(pageToken) != "" {
		call = call.PageToken(pageToken)
	}

	response, err := call.Do()
	if err != nil {
		return nil, "", err
	}

	summaries := make([]MessageSummary, 0, len(response.Messages))
	for _, msg := range response.Messages {
		full, err := service.Users.Messages.Get(userID, msg.Id).
			Format("metadata").
			MetadataHeaders("From", "To", "Subject", "Date").
			Do()
		if err != nil {
			return nil, "", err
		}
		summaries = append(summaries, messageSummaryFromAPI(full))
	}

	return summaries, response.NextPageToken, nil
}

func GetMessage(ctx context.Context, client *http.Client, userID string, messageID string) (MessageDetail, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return MessageDetail{}, err
	}

	message, err := service.Users.Messages.Get(userID, messageID).Format("full").Do()
	if err != nil {
		return MessageDetail{}, err
	}

	detail := MessageDetail{
		MessageSummary: messageSummaryFromAPI(message),
	}
	detail.BodyPlain, detail.BodyHTML, detail.Attachments = extractBodiesAndAttachments(message.Payload)
	return detail, nil
}

func serviceFromClient(ctx context.Context, client *http.Client) (*gmail.Service, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}

	service, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}

	return service, nil
}

func messageSummaryFromAPI(msg *gmail.Message) MessageSummary {
	if msg == nil {
		return MessageSummary{}
	}

	var payload *gmail.MessagePart
	payload = msg.Payload
	var headers map[string]string
	if payload != nil {
		headers = headerMap(payload.Headers)
	} else {
		headers = map[string]string{}
	}
	return MessageSummary{
		ID:           msg.Id,
		ThreadID:     msg.ThreadId,
		From:         headers["from"],
		To:           headers["to"],
		Subject:      headers["subject"],
		Date:         headers["date"],
		Snippet:      msg.Snippet,
		LabelIDs:     append([]string(nil), msg.LabelIds...),
		InternalDate: msg.InternalDate,
	}
}

func headerMap(headers []*gmail.MessagePartHeader) map[string]string {
	values := map[string]string{}
	for _, h := range headers {
		name := strings.ToLower(strings.TrimSpace(h.Name))
		if name == "" {
			continue
		}
		if _, exists := values[name]; exists {
			continue
		}
		values[name] = h.Value
	}
	return values
}

func extractBodiesAndAttachments(part *gmail.MessagePart) (string, string, []Attachment) {
	var bodyPlain string
	var bodyHTML string
	attachments := []Attachment{}

	var walk func(p *gmail.MessagePart)
	walk = func(p *gmail.MessagePart) {
		if p == nil {
			return
		}

		if p.Body != nil && strings.TrimSpace(p.Body.AttachmentId) != "" {
			attachments = append(attachments, Attachment{
				Filename:     p.Filename,
				MimeType:     p.MimeType,
				AttachmentID: p.Body.AttachmentId,
				Size:         p.Body.Size,
			})
		}

		if p.Body != nil && strings.TrimSpace(p.Body.Data) != "" {
			content := decodeMessageBodyData(p.Body.Data)
			switch strings.ToLower(strings.TrimSpace(p.MimeType)) {
			case "text/plain":
				if bodyPlain == "" {
					bodyPlain = content
				}
			case "text/html":
				if bodyHTML == "" {
					bodyHTML = content
				}
			}
		}

		for _, child := range p.Parts {
			walk(child)
		}
	}

	walk(part)
	return bodyPlain, bodyHTML, attachments
}

func decodeMessageBodyData(data string) string {
	reader := base64.NewDecoder(base64.RawURLEncoding, strings.NewReader(data))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		reader = base64.NewDecoder(base64.URLEncoding, strings.NewReader(data))
		decoded, err = io.ReadAll(reader)
		if err != nil {
			return ""
		}
	}
	return string(decoded)
}
