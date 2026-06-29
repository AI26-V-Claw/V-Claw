package docs

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"vclaw/internal/connectors/google/common"

	googledocs "google.golang.org/api/docs/v1"
)

type RichTextContent struct {
	Text            string
	ParagraphStyles []ParagraphStyleRange
	TextStyles      []TextStyleRange
	BulletRanges    []TextRange
}

type TextRange struct {
	Start int64
	End   int64
}

type ParagraphStyleRange struct {
	TextRange
	NamedStyleType string
}

type TextStyleRange struct {
	TextRange
	Bold      bool
	Italic    bool
	Monospace bool
}

func (c *Client) AppendRichText(ctx context.Context, documentID string, content RichTextContent) (AppendTextOutput, error) {
	return AppendRichText(ctx, c.httpClient, documentID, content)
}

func AppendRichText(ctx context.Context, client *http.Client, documentID string, content RichTextContent) (AppendTextOutput, error) {
	if strings.TrimSpace(content.Text) == "" {
		return AppendTextOutput{}, fmt.Errorf("rich text content is empty")
	}
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
	value := content.Text
	if !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	requests := []*googledocs.Request{{
		InsertText: &googledocs.InsertTextRequest{
			Location: &googledocs.Location{Index: index},
			Text:     value,
		},
	}}
	for _, style := range content.ParagraphStyles {
		if !validLocalRange(style.TextRange) || strings.TrimSpace(style.NamedStyleType) == "" {
			continue
		}
		requests = append(requests, &googledocs.Request{UpdateParagraphStyle: &googledocs.UpdateParagraphStyleRequest{
			Range: &googledocs.Range{StartIndex: index + style.Start, EndIndex: index + style.End},
			ParagraphStyle: &googledocs.ParagraphStyle{
				NamedStyleType: style.NamedStyleType,
			},
			Fields: "namedStyleType",
		}})
	}
	for _, style := range content.TextStyles {
		if !validLocalRange(style.TextRange) {
			continue
		}
		textStyle := &googledocs.TextStyle{}
		fields := make([]string, 0, 3)
		if style.Bold {
			textStyle.Bold = true
			fields = append(fields, "bold")
		}
		if style.Italic {
			textStyle.Italic = true
			fields = append(fields, "italic")
		}
		if style.Monospace {
			textStyle.WeightedFontFamily = &googledocs.WeightedFontFamily{FontFamily: "Roboto Mono", Weight: 400}
			fields = append(fields, "weightedFontFamily")
		}
		if len(fields) == 0 {
			continue
		}
		requests = append(requests, &googledocs.Request{UpdateTextStyle: &googledocs.UpdateTextStyleRequest{
			Range:     &googledocs.Range{StartIndex: index + style.Start, EndIndex: index + style.End},
			TextStyle: textStyle,
			Fields:    strings.Join(fields, ","),
		}})
	}
	for _, bulletRange := range content.BulletRanges {
		if !validLocalRange(bulletRange) {
			continue
		}
		requests = append(requests, &googledocs.Request{CreateParagraphBullets: &googledocs.CreateParagraphBulletsRequest{
			Range:        &googledocs.Range{StartIndex: index + bulletRange.Start, EndIndex: index + bulletRange.End},
			BulletPreset: "BULLET_DISC_CIRCLE_SQUARE",
		}})
	}
	if _, err := service.Documents.BatchUpdate(documentID, &googledocs.BatchUpdateDocumentRequest{Requests: requests}).Do(); err != nil {
		return AppendTextOutput{}, common.MapError(err)
	}
	return AppendTextOutput{DocumentID: documentID, Title: doc.Title}, nil
}

func validLocalRange(value TextRange) bool {
	return value.Start >= 0 && value.End > value.Start
}
