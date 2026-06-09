package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gdocs "google.golang.org/api/docs/v1"
	"google.golang.org/api/option"
)

type Client struct {
	srv *gdocs.Service
}

func NewClient(ctx context.Context, client *http.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	srv, err := gdocs.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create docs service: %w", err)
	}
	return &Client{srv: srv}, nil
}

type Document struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	RevisionID string `json:"revisionId,omitempty"`
	Text       string `json:"text,omitempty"`
}

func (c *Client) GetDocument(ctx context.Context, documentID string) (Document, error) {
	if c == nil || c.srv == nil {
		return Document{}, errors.New("docs service is not configured")
	}
	doc, err := c.srv.Documents.Get(documentID).Context(ctx).Do()
	if err != nil {
		return Document{}, err
	}
	return documentFromAPI(doc), nil
}

func (c *Client) CreateDocument(ctx context.Context, title string) (Document, error) {
	if c == nil || c.srv == nil {
		return Document{}, errors.New("docs service is not configured")
	}
	doc, err := c.srv.Documents.Create(&gdocs.Document{Title: title}).Context(ctx).Do()
	if err != nil {
		return Document{}, err
	}
	return documentFromAPI(doc), nil
}

func (c *Client) AppendText(ctx context.Context, documentID string, text string) (Document, error) {
	if c == nil || c.srv == nil {
		return Document{}, errors.New("docs service is not configured")
	}
	doc, err := c.srv.Documents.Get(documentID).Context(ctx).Do()
	if err != nil {
		return Document{}, err
	}
	index := appendIndex(doc)
	_, err = c.srv.Documents.BatchUpdate(documentID, &gdocs.BatchUpdateDocumentRequest{
		Requests: []*gdocs.Request{
			{
				InsertText: &gdocs.InsertTextRequest{
					Location: &gdocs.Location{Index: index},
					Text:     text,
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return Document{}, err
	}
	return c.GetDocument(ctx, documentID)
}

func documentFromAPI(doc *gdocs.Document) Document {
	if doc == nil {
		return Document{}
	}
	return Document{
		ID:         doc.DocumentId,
		Title:      doc.Title,
		RevisionID: doc.RevisionId,
		Text:       extractText(doc),
	}
}

func appendIndex(doc *gdocs.Document) int64 {
	if doc == nil || doc.Body == nil || len(doc.Body.Content) == 0 {
		return 1
	}
	last := doc.Body.Content[len(doc.Body.Content)-1]
	if last.EndIndex > 1 {
		return last.EndIndex - 1
	}
	return 1
}

func extractText(doc *gdocs.Document) string {
	if doc == nil || doc.Body == nil {
		return ""
	}
	var b strings.Builder
	for _, item := range doc.Body.Content {
		if item == nil || item.Paragraph == nil {
			continue
		}
		for _, element := range item.Paragraph.Elements {
			if element == nil || element.TextRun == nil {
				continue
			}
			b.WriteString(element.TextRun.Content)
		}
	}
	return strings.TrimSpace(b.String())
}
