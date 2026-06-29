package docs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAppendRichTextBuildsInsertAndStyleRequests(t *testing.T) {
	var batch map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		if request.Method == http.MethodGet {
			body = `{"title":"Cheat Sheet","body":{"content":[{"endIndex":5}]}}`
		} else if request.Method == http.MethodPost && strings.Contains(request.URL.Path, ":batchUpdate") {
			data, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read batch body: %v", err)
			}
			if err := json.Unmarshal(data, &batch); err != nil {
				t.Fatalf("decode batch body: %v\n%s", err, data)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	output, err := AppendRichText(context.Background(), client, "doc_123", RichTextContent{
		Text: "Title\nItem\n",
		ParagraphStyles: []ParagraphStyleRange{{
			TextRange:      TextRange{Start: 0, End: 6},
			NamedStyleType: "TITLE",
		}},
		TextStyles: []TextStyleRange{{
			TextRange: TextRange{Start: 6, End: 11},
			Bold:      true,
			Monospace: true,
		}},
		BulletRanges: []TextRange{{Start: 6, End: 11}},
	})
	if err != nil {
		t.Fatalf("AppendRichText: %v", err)
	}
	if output.DocumentID != "doc_123" || output.Title != "Cheat Sheet" {
		t.Fatalf("unexpected output: %#v", output)
	}
	requests, ok := batch["requests"].([]any)
	if !ok || len(requests) != 4 {
		t.Fatalf("expected insert + paragraph + text + bullet requests, got %#v", batch)
	}
	insert := requests[0].(map[string]any)["insertText"].(map[string]any)
	location := insert["location"].(map[string]any)
	if location["index"] != float64(4) {
		t.Fatalf("insert index = %#v, want 4", location["index"])
	}
	paragraph := requests[1].(map[string]any)["updateParagraphStyle"].(map[string]any)
	rangeValue := paragraph["range"].(map[string]any)
	if rangeValue["startIndex"] != float64(4) || rangeValue["endIndex"] != float64(10) {
		t.Fatalf("paragraph range was not offset by insertion index: %#v", rangeValue)
	}
}
