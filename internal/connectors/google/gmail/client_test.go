package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func TestListMessagesSuccessWithPaging(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/gmail/v1/users/me/messages":
				return jsonResponse(http.StatusOK, `{
					"messages":[{"id":"m1","threadId":"t1"}],
					"nextPageToken":"next-token",
					"resultSizeEstimate":1
				}`), nil
			case "/gmail/v1/users/me/messages/m1":
				return jsonResponse(http.StatusOK, `{
					"id":"m1",
					"threadId":"t1",
					"labelIds":["INBOX","UNREAD"],
					"snippet":"hello world",
					"internalDate":"1717228800000",
					"payload":{
						"headers":[
							{"name":"From","value":"alice@example.com"},
							{"name":"To","value":"bob@example.com"},
							{"name":"Subject","value":"Meeting"},
							{"name":"Date","value":"Mon, 01 Jun 2026 09:00:00 +0700"}
						]
					}
				}`), nil
			default:
				t.Fatalf("unexpected path: %s", req.URL.Path)
				return nil, nil
			}
		}),
	}

	messages, nextToken, err := ListMessages(context.Background(), client, "me", "from:alice@example.com", []string{"INBOX"}, 10, "")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if nextToken != "next-token" {
		t.Fatalf("ListMessages() next token = %q, want %q", nextToken, "next-token")
	}
	if len(messages) != 1 {
		t.Fatalf("ListMessages() length = %d, want 1", len(messages))
	}

	msg := messages[0]
	if msg.ID != "m1" || msg.ThreadID != "t1" {
		t.Fatalf("unexpected id/thread: %#v", msg)
	}
	if msg.From != "alice@example.com" || msg.Subject != "Meeting" {
		t.Fatalf("unexpected mapped headers: %#v", msg)
	}
	if msg.InternalDate != 1717228800000 {
		t.Fatalf("unexpected internal date: %d", msg.InternalDate)
	}
}

func TestGetMessageParsesBodiesAndAttachments(t *testing.T) {
	plain := base64.RawURLEncoding.EncodeToString([]byte("hello plain"))
	html := base64.RawURLEncoding.EncodeToString([]byte("<p>hello html</p>"))

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/gmail/v1/users/me/messages/msg-full" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{
				"id":"msg-full",
				"threadId":"thread-1",
				"labelIds":["INBOX"],
				"snippet":"preview",
				"internalDate":"1717228800001",
				"payload":{
					"mimeType":"multipart/mixed",
					"parts":[
						{
							"mimeType":"multipart/alternative",
							"parts":[
								{"mimeType":"text/plain","body":{"data":"%s"}},
								{"mimeType":"text/html","body":{"data":"%s"}}
							]
						},
						{
							"filename":"report.pdf",
							"mimeType":"application/pdf",
							"body":{"attachmentId":"att-1","size":1234}
						}
					],
					"headers":[
						{"name":"From","value":"alice@example.com"},
						{"name":"To","value":"bob@example.com"},
						{"name":"Subject","value":"Report"},
						{"name":"Date","value":"Mon, 01 Jun 2026 10:00:00 +0700"}
					]
				}
			}`, plain, html)), nil
		}),
	}

	message, err := GetMessage(context.Background(), client, "me", "msg-full")
	if err != nil {
		t.Fatalf("GetMessage() error = %v", err)
	}

	if message.BodyPlain != "hello plain" {
		t.Fatalf("GetMessage() BodyPlain = %q", message.BodyPlain)
	}
	if message.BodyHTML != "<p>hello html</p>" {
		t.Fatalf("GetMessage() BodyHTML = %q", message.BodyHTML)
	}
	if len(message.Attachments) != 1 {
		t.Fatalf("GetMessage() attachments = %d, want 1", len(message.Attachments))
	}
	if message.Attachments[0].AttachmentID != "att-1" {
		t.Fatalf("unexpected attachment: %#v", message.Attachments[0])
	}
}

func TestExtractBodiesAndAttachmentsEmptyPayload(t *testing.T) {
	plain, html, attachments := extractBodiesAndAttachments(nil)
	if plain != "" || html != "" || len(attachments) != 0 {
		t.Fatalf("extractBodiesAndAttachments(nil) = %q, %q, %d", plain, html, len(attachments))
	}
}
