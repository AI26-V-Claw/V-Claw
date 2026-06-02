package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
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
	ID              string
	ThreadID        string
	From            string
	To              string
	Subject         string
	Date            string
	Snippet         string
	LabelIDs        []string
	InternalDate    int64
	MessageIDHeader string
	References      string
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

type ThreadSummary struct {
	ID        string
	HistoryID uint64
	Snippet   string
}

type ThreadDetail struct {
	ThreadSummary
	Messages []MessageDetail
}

type DraftMessageInput struct {
	To         []string
	Cc         []string
	Bcc        []string
	Subject    string
	TextBody   string
	HTMLBody   string
	ThreadID   string
	InReplyTo  string
	References string
}

type DraftSummary struct {
	ID        string
	MessageID string
	ThreadID  string
}

type AttachmentData struct {
	Attachment
	Data []byte
}

type ModifyMessageInput struct {
	AddLabelIDs    []string
	RemoveLabelIDs []string
}

type ModifyMessageOutput struct {
	ID       string
	ThreadID string
	LabelIDs []string
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

func (c *Client) ListThreads(ctx context.Context, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]ThreadSummary, string, error) {
	return ListThreads(ctx, c.httpClient, userID, query, labelIDs, maxResults, pageToken)
}

func (c *Client) GetThread(ctx context.Context, userID string, threadID string) (ThreadDetail, error) {
	return GetThread(ctx, c.httpClient, userID, threadID)
}

func (c *Client) CreateDraft(ctx context.Context, userID string, input DraftMessageInput) (DraftSummary, error) {
	return CreateDraft(ctx, c.httpClient, userID, input)
}

func (c *Client) UpdateDraft(ctx context.Context, userID string, draftID string, input DraftMessageInput) (DraftSummary, error) {
	return UpdateDraft(ctx, c.httpClient, userID, draftID, input)
}

func (c *Client) SendDraft(ctx context.Context, userID string, draftID string) (MessageSummary, error) {
	return SendDraft(ctx, c.httpClient, userID, draftID)
}

func (c *Client) DownloadAttachment(ctx context.Context, userID string, messageID string, attachment Attachment) (AttachmentData, error) {
	return DownloadAttachment(ctx, c.httpClient, userID, messageID, attachment)
}

func (c *Client) ModifyMessage(ctx context.Context, userID string, messageID string, input ModifyMessageInput) (ModifyMessageOutput, error) {
	return ModifyMessage(ctx, c.httpClient, userID, messageID, input)
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
			MetadataHeaders("From", "To", "Subject", "Date", "Message-ID", "References").
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

	return messageDetailFromAPI(message), nil
}

func ListThreads(ctx context.Context, client *http.Client, userID string, query string, labelIDs []string, maxResults int64, pageToken string) ([]ThreadSummary, string, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return nil, "", err
	}

	call := service.Users.Threads.List(userID).MaxResults(maxResults)
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

	threads := make([]ThreadSummary, 0, len(response.Threads))
	for _, thread := range response.Threads {
		threads = append(threads, threadSummaryFromAPI(thread))
	}
	return threads, response.NextPageToken, nil
}

func GetThread(ctx context.Context, client *http.Client, userID string, threadID string) (ThreadDetail, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ThreadDetail{}, err
	}

	thread, err := service.Users.Threads.Get(userID, threadID).Format("full").Do()
	if err != nil {
		return ThreadDetail{}, err
	}

	detail := ThreadDetail{ThreadSummary: threadSummaryFromAPI(thread)}
	for _, message := range thread.Messages {
		detail.Messages = append(detail.Messages, messageDetailFromAPI(message))
	}
	return detail, nil
}

func CreateDraft(ctx context.Context, client *http.Client, userID string, input DraftMessageInput) (DraftSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return DraftSummary{}, err
	}

	draft, err := service.Users.Drafts.Create(userID, draftFromInput(input)).Do()
	if err != nil {
		return DraftSummary{}, err
	}
	return draftSummaryFromAPI(draft), nil
}

func UpdateDraft(ctx context.Context, client *http.Client, userID string, draftID string, input DraftMessageInput) (DraftSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return DraftSummary{}, err
	}

	draft := draftFromInput(input)
	draft.Id = draftID
	updated, err := service.Users.Drafts.Update(userID, draftID, draft).Do()
	if err != nil {
		return DraftSummary{}, err
	}
	return draftSummaryFromAPI(updated), nil
}

func SendDraft(ctx context.Context, client *http.Client, userID string, draftID string) (MessageSummary, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return MessageSummary{}, err
	}

	message, err := service.Users.Drafts.Send(userID, &gmail.Draft{Id: draftID}).Do()
	if err != nil {
		return MessageSummary{}, err
	}
	return messageSummaryFromAPI(message), nil
}

func DownloadAttachment(ctx context.Context, client *http.Client, userID string, messageID string, attachment Attachment) (AttachmentData, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return AttachmentData{}, err
	}

	response, err := service.Users.Messages.Attachments.Get(userID, messageID, attachment.AttachmentID).Do()
	if err != nil {
		return AttachmentData{}, err
	}

	data, err := decodeMessageBodyBytes(response.Data)
	if err != nil {
		return AttachmentData{}, err
	}
	return AttachmentData{Attachment: attachment, Data: data}, nil
}

func ModifyMessage(ctx context.Context, client *http.Client, userID string, messageID string, input ModifyMessageInput) (ModifyMessageOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return ModifyMessageOutput{}, err
	}

	message, err := service.Users.Messages.Modify(userID, messageID, &gmail.ModifyMessageRequest{
		AddLabelIds:    input.AddLabelIDs,
		RemoveLabelIds: input.RemoveLabelIDs,
	}).Do()
	if err != nil {
		return ModifyMessageOutput{}, err
	}
	return ModifyMessageOutput{
		ID:       message.Id,
		ThreadID: message.ThreadId,
		LabelIDs: append([]string(nil), message.LabelIds...),
	}, nil
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

func threadSummaryFromAPI(thread *gmail.Thread) ThreadSummary {
	if thread == nil {
		return ThreadSummary{}
	}
	return ThreadSummary{
		ID:        thread.Id,
		HistoryID: thread.HistoryId,
		Snippet:   thread.Snippet,
	}
}

func draftSummaryFromAPI(draft *gmail.Draft) DraftSummary {
	if draft == nil {
		return DraftSummary{}
	}
	summary := DraftSummary{ID: draft.Id}
	if draft.Message != nil {
		summary.MessageID = draft.Message.Id
		summary.ThreadID = draft.Message.ThreadId
	}
	return summary
}

func messageDetailFromAPI(message *gmail.Message) MessageDetail {
	detail := MessageDetail{MessageSummary: messageSummaryFromAPI(message)}
	if message != nil {
		detail.BodyPlain, detail.BodyHTML, detail.Attachments = extractBodiesAndAttachments(message.Payload)
	}
	return detail
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
		ID:              msg.Id,
		ThreadID:        msg.ThreadId,
		From:            headers["from"],
		To:              headers["to"],
		Subject:         headers["subject"],
		Date:            headers["date"],
		Snippet:         msg.Snippet,
		LabelIDs:        append([]string(nil), msg.LabelIds...),
		InternalDate:    msg.InternalDate,
		MessageIDHeader: headers["message-id"],
		References:      headers["references"],
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

func draftFromInput(input DraftMessageInput) *gmail.Draft {
	return &gmail.Draft{Message: &gmail.Message{
		Raw:      rawMessageFromInput(input),
		ThreadId: input.ThreadID,
	}}
}

func rawMessageFromInput(input DraftMessageInput) string {
	raw := mimeMessageFromInput(input)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func mimeMessageFromInput(input DraftMessageInput) string {
	var b strings.Builder
	writeAddressHeader(&b, "To", input.To)
	writeAddressHeader(&b, "Cc", input.Cc)
	writeAddressHeader(&b, "Bcc", input.Bcc)
	writeHeader(&b, "Subject", mime.QEncoding.Encode("utf-8", sanitizeHeader(input.Subject)))
	writeHeader(&b, "In-Reply-To", input.InReplyTo)
	writeHeader(&b, "References", input.References)

	if strings.TrimSpace(input.HTMLBody) != "" {
		boundary := "vclaw-gmail-alt"
		writeHeader(&b, "MIME-Version", "1.0")
		writeHeader(&b, "Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		b.WriteString("\r\n")
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(normalizeBody(input.TextBody) + "\r\n")
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(normalizeBody(input.HTMLBody) + "\r\n")
		b.WriteString("--" + boundary + "--\r\n")
		return b.String()
	}

	writeHeader(&b, "MIME-Version", "1.0")
	writeHeader(&b, "Content-Type", "text/plain; charset=UTF-8")
	b.WriteString("\r\n")
	b.WriteString(normalizeBody(input.TextBody))
	return b.String()
}

func writeAddressHeader(b *strings.Builder, name string, values []string) {
	cleaned := cleanAddresses(values)
	if len(cleaned) == 0 {
		return
	}
	writeHeader(b, name, strings.Join(cleaned, ", "))
}

func writeHeader(b *strings.Builder, name string, value string) {
	value = sanitizeHeader(value)
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString(name)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

func cleanAddresses(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := sanitizeHeader(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func sanitizeHeader(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.TrimSpace(value)
}

func normalizeBody(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func decodeMessageBodyData(data string) string {
	decoded, err := decodeMessageBodyBytes(data)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func decodeMessageBodyBytes(data string) ([]byte, error) {
	reader := base64.NewDecoder(base64.RawURLEncoding, strings.NewReader(data))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		reader = base64.NewDecoder(base64.URLEncoding, strings.NewReader(data))
		decoded, err = io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
	}
	return bytes.TrimPrefix(decoded, []byte("\ufeff")), nil
}
