package docs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vclaw/internal/connectors/google/common"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/option"
)

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{httpClient: httpClient}
}

type Document struct {
	ID       string
	Title    string
	Revision string
	BodyText string
}

type AppendTextOutput struct {
	DocumentID string
	Title      string
}

type EditTextOutput struct {
	DocumentID string
	Title      string
}

func (c *Client) GetDocument(ctx context.Context, documentID string) (Document, error) {
	return GetDocument(ctx, c.httpClient, documentID)
}

func (c *Client) CreateDocument(ctx context.Context, title string) (Document, error) {
	return CreateDocument(ctx, c.httpClient, title)
}

func (c *Client) AppendText(ctx context.Context, documentID string, text string) (AppendTextOutput, error) {
	return AppendText(ctx, c.httpClient, documentID, text)
}

func (c *Client) ReplaceText(ctx context.Context, documentID string, oldText string, newText string, matchCase bool) (EditTextOutput, error) {
	return ReplaceText(ctx, c.httpClient, documentID, oldText, newText, matchCase)
}

func (c *Client) InsertText(ctx context.Context, documentID string, index int64, text string) (EditTextOutput, error) {
	return InsertText(ctx, c.httpClient, documentID, index, text)
}

func (c *Client) DeleteContent(ctx context.Context, documentID string, startIndex int64, endIndex int64) (EditTextOutput, error) {
	return DeleteContent(ctx, c.httpClient, documentID, startIndex, endIndex)
}

func GetDocument(ctx context.Context, client *http.Client, documentID string) (Document, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return Document{}, err
	}
	doc, err := service.Documents.Get(documentID).Do()
	if err != nil {
		return Document{}, common.MapError(err)
	}
	return documentFromAPI(doc), nil
}

func CreateDocument(ctx context.Context, client *http.Client, title string) (Document, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return Document{}, err
	}
	doc, err := service.Documents.Create(&docs.Document{Title: strings.TrimSpace(title)}).Do()
	if err != nil {
		return Document{}, common.MapError(err)
	}
	return documentFromAPI(doc), nil
}

func AppendText(ctx context.Context, client *http.Client, documentID string, text string) (AppendTextOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return AppendTextOutput{}, err
	}
	doc, err := service.Documents.Get(documentID).Fields("title,body").Do()
	if err != nil {
		return AppendTextOutput{}, common.MapError(err)
	}
	index := int64(1)
	if doc.Body != nil && len(doc.Body.Content) > 0 {
		last := doc.Body.Content[len(doc.Body.Content)-1]
		if last.EndIndex > 1 {
			index = last.EndIndex - 1
		}
	}
	value := text
	if value != "" && !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	_, err = service.Documents.BatchUpdate(documentID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			InsertText: &docs.InsertTextRequest{
				Location: &docs.Location{Index: index},
				Text:     value,
			},
		}},
	}).Do()
	if err != nil {
		return AppendTextOutput{}, common.MapError(err)
	}
	return AppendTextOutput{DocumentID: documentID, Title: doc.Title}, nil
}

func ReplaceText(ctx context.Context, client *http.Client, documentID string, oldText string, newText string, matchCase bool) (EditTextOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return EditTextOutput{}, err
	}
	doc, err := service.Documents.Get(documentID).Fields("title").Do()
	if err != nil {
		return EditTextOutput{}, common.MapError(err)
	}
	_, err = service.Documents.BatchUpdate(documentID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			ReplaceAllText: &docs.ReplaceAllTextRequest{
				ContainsText: &docs.SubstringMatchCriteria{
					Text:      oldText,
					MatchCase: matchCase,
				},
				ReplaceText: newText,
			},
		}},
	}).Do()
	if err != nil {
		return EditTextOutput{}, common.MapError(err)
	}
	return EditTextOutput{DocumentID: documentID, Title: doc.Title}, nil
}

func InsertText(ctx context.Context, client *http.Client, documentID string, index int64, text string) (EditTextOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return EditTextOutput{}, err
	}
	doc, err := service.Documents.Get(documentID).Fields("title").Do()
	if err != nil {
		return EditTextOutput{}, common.MapError(err)
	}
	_, err = service.Documents.BatchUpdate(documentID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			InsertText: &docs.InsertTextRequest{
				Location: &docs.Location{Index: index},
				Text:     text,
			},
		}},
	}).Do()
	if err != nil {
		return EditTextOutput{}, common.MapError(err)
	}
	return EditTextOutput{DocumentID: documentID, Title: doc.Title}, nil
}

func DeleteContent(ctx context.Context, client *http.Client, documentID string, startIndex int64, endIndex int64) (EditTextOutput, error) {
	service, err := serviceFromClient(ctx, client)
	if err != nil {
		return EditTextOutput{}, err
	}
	doc, err := service.Documents.Get(documentID).Fields("title").Do()
	if err != nil {
		return EditTextOutput{}, common.MapError(err)
	}
	_, err = service.Documents.BatchUpdate(documentID, &docs.BatchUpdateDocumentRequest{
		Requests: []*docs.Request{{
			DeleteContentRange: &docs.DeleteContentRangeRequest{
				Range: &docs.Range{StartIndex: startIndex, EndIndex: endIndex},
			},
		}},
	}).Do()
	if err != nil {
		return EditTextOutput{}, common.MapError(err)
	}
	return EditTextOutput{DocumentID: documentID, Title: doc.Title}, nil
}

func serviceFromClient(ctx context.Context, client *http.Client) (*docs.Service, error) {
	if client == nil {
		return nil, errors.New("http client is required")
	}
	service, err := docs.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create docs service: %w", err)
	}
	return service, nil
}

func documentFromAPI(doc *docs.Document) Document {
	if doc == nil {
		return Document{}
	}
	return Document{
		ID:       doc.DocumentId,
		Title:    doc.Title,
		Revision: doc.RevisionId,
		BodyText: extractBodyText(doc.Body),
	}
}

func extractBodyText(body *docs.Body) string {
	if body == nil {
		return ""
	}
	var b strings.Builder
	for _, element := range body.Content {
		if element == nil || element.Paragraph == nil {
			continue
		}
		for _, pe := range element.Paragraph.Elements {
			if pe != nil && pe.TextRun != nil {
				b.WriteString(pe.TextRun.Content)
			}
		}
	}
	return b.String()
}
