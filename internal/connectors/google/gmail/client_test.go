package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestListThreadsSuccess(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/gmail/v1/users/me/threads" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			if req.URL.Query().Get("q") != "from:alice@example.com" {
				t.Fatalf("unexpected query: %s", req.URL.RawQuery)
			}
			return jsonResponse(http.StatusOK, `{
				"threads":[{"id":"t1","historyId":"123","snippet":"hello"}],
				"nextPageToken":"next-thread"
			}`), nil
		}),
	}

	threads, nextToken, err := ListThreads(context.Background(), client, "me", "from:alice@example.com", nil, 10, "")
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if nextToken != "next-thread" || len(threads) != 1 || threads[0].ID != "t1" {
		t.Fatalf("unexpected threads: %#v next=%q", threads, nextToken)
	}
}

func TestGetThreadParsesMessages(t *testing.T) {
	plain := base64.RawURLEncoding.EncodeToString([]byte("thread body"))
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/gmail/v1/users/me/threads/thread-1" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{
				"id":"thread-1",
				"snippet":"thread snippet",
				"messages":[{
					"id":"m1",
					"threadId":"thread-1",
					"payload":{
						"mimeType":"text/plain",
						"body":{"data":"%s"},
						"headers":[{"name":"Subject","value":"Thread subject"}]
					}
				}]
			}`, plain)), nil
		}),
	}

	thread, err := GetThread(context.Background(), client, "me", "thread-1")
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if thread.ID != "thread-1" || len(thread.Messages) != 1 {
		t.Fatalf("unexpected thread: %#v", thread)
	}
	if thread.Messages[0].BodyPlain != "thread body" {
		t.Fatalf("unexpected message body: %q", thread.Messages[0].BodyPlain)
	}
}

func TestCreateUpdateAndSendDraft(t *testing.T) {
	seenCreate := false
	seenUpdate := false
	seenSend := false
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodPost && req.URL.Path == "/gmail/v1/users/me/drafts":
				seenCreate = true
				assertDraftRawContains(t, req, "Hello draft")
				return jsonResponse(http.StatusOK, `{"id":"draft-1","message":{"id":"msg-draft","threadId":"thread-1"}}`), nil
			case req.Method == http.MethodPut && req.URL.Path == "/gmail/v1/users/me/drafts/draft-1":
				seenUpdate = true
				assertDraftRawContains(t, req, "Updated draft")
				return jsonResponse(http.StatusOK, `{"id":"draft-1","message":{"id":"msg-updated","threadId":"thread-1"}}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/gmail/v1/users/me/drafts/send":
				seenSend = true
				return jsonResponse(http.StatusOK, `{"id":"sent-1","threadId":"thread-1","labelIds":["SENT"],"payload":{"headers":[{"name":"Subject","value":"Sent"}]}}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, nil
			}
		}),
	}

	created, err := CreateDraft(context.Background(), client, "me", DraftMessageInput{To: []string{"a@example.com"}, Subject: "Draft", TextBody: "Hello draft"})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if created.ID != "draft-1" || !seenCreate {
		t.Fatalf("unexpected created draft: %#v", created)
	}

	updated, err := UpdateDraft(context.Background(), client, "me", "draft-1", DraftMessageInput{To: []string{"a@example.com"}, Subject: "Draft", TextBody: "Updated draft"})
	if err != nil {
		t.Fatalf("UpdateDraft() error = %v", err)
	}
	if updated.MessageID != "msg-updated" || !seenUpdate {
		t.Fatalf("unexpected updated draft: %#v", updated)
	}

	sent, err := SendDraft(context.Background(), client, "me", "draft-1")
	if err != nil {
		t.Fatalf("SendDraft() error = %v", err)
	}
	if sent.ID != "sent-1" || sent.Subject != "Sent" || !seenSend {
		t.Fatalf("unexpected sent message: %#v", sent)
	}
}

func TestDownloadAttachmentAndModifyMessage(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("attachment bytes"))
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/gmail/v1/users/me/messages/m1/attachments/att-1":
				return jsonResponse(http.StatusOK, fmt.Sprintf(`{"data":"%s","size":16}`, encoded)), nil
			case req.Method == http.MethodPost && req.URL.Path == "/gmail/v1/users/me/messages/m1/modify":
				var body struct {
					AddLabelIds    []string `json:"addLabelIds"`
					RemoveLabelIds []string `json:"removeLabelIds"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode modify body: %v", err)
				}
				if len(body.AddLabelIds) != 1 || body.AddLabelIds[0] != "STARRED" {
					t.Fatalf("unexpected modify body: %#v", body)
				}
				return jsonResponse(http.StatusOK, `{"id":"m1","threadId":"t1","labelIds":["STARRED"]}`), nil
			default:
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				return nil, nil
			}
		}),
	}

	attachment, err := DownloadAttachment(context.Background(), client, "me", "m1", Attachment{Filename: "a.txt", AttachmentID: "att-1"})
	if err != nil {
		t.Fatalf("DownloadAttachment() error = %v", err)
	}
	if string(attachment.Data) != "attachment bytes" {
		t.Fatalf("unexpected attachment data: %q", attachment.Data)
	}

	modified, err := ModifyMessage(context.Background(), client, "me", "m1", ModifyMessageInput{AddLabelIDs: []string{"STARRED"}})
	if err != nil {
		t.Fatalf("ModifyMessage() error = %v", err)
	}
	if modified.ID != "m1" || len(modified.LabelIDs) != 1 || modified.LabelIDs[0] != "STARRED" {
		t.Fatalf("unexpected modified message: %#v", modified)
	}
}

func assertDraftRawContains(t *testing.T, req *http.Request, want string) {
	t.Helper()
	var body struct {
		Message struct {
			Raw string `json:"raw"`
		} `json:"message"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatalf("decode draft body: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(body.Message.Raw)
	if err != nil {
		t.Fatalf("decode raw message: %v", err)
	}
	if !strings.Contains(string(decoded), want) {
		t.Fatalf("raw draft missing %q: %s", want, decoded)
	}
}
