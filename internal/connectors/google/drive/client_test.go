package drive

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCreateFolderSendsFolderMimeTypeParentsAndSupportsAllDrives(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", request.Method)
		}
		if request.URL.Path != "/drive/v3/files" {
			t.Fatalf("path = %s, want /drive/v3/files", request.URL.Path)
		}
		if got := request.URL.Query().Get("supportsAllDrives"); got != "true" {
			t.Fatalf("supportsAllDrives = %q, want true", got)
		}

		var payload struct {
			Name     string   `json:"name"`
			MimeType string   `json:"mimeType"`
			Parents  []string `json:"parents"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request payload: %v", err)
		}
		if payload.Name != "Project Docs" {
			t.Fatalf("name = %q, want Project Docs", payload.Name)
		}
		if payload.MimeType != FolderMimeType {
			t.Fatalf("mimeType = %q, want %s", payload.MimeType, FolderMimeType)
		}
		if len(payload.Parents) != 1 || payload.Parents[0] != "parent_123" {
			t.Fatalf("parents = %#v, want [parent_123]", payload.Parents)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"folder_123","name":"Project Docs","mimeType":"application/vnd.google-apps.folder","webViewLink":"https://drive.google.com/drive/folders/folder_123","parents":["parent_123"]}`)),
		}, nil
	})}

	file, err := CreateFolder(context.Background(), client, " Project Docs ", []string{"parent_123", " "})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if file.ID != "folder_123" || file.Name != "Project Docs" || file.MimeType != FolderMimeType {
		t.Fatalf("unexpected created folder: %#v", file)
	}
	if len(file.Parents) != 1 || file.Parents[0] != "parent_123" {
		t.Fatalf("created parents = %#v, want [parent_123]", file.Parents)
	}
}

func TestListFilesTreatsPlainTextQueryAsNameSearch(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/drive/v3/files" {
			t.Fatalf("path = %s, want /drive/v3/files", request.URL.Path)
		}
		if got := request.URL.Query().Get("supportsAllDrives"); got != "true" {
			t.Fatalf("supportsAllDrives = %q, want true", got)
		}
		if got := request.URL.Query().Get("includeItemsFromAllDrives"); got != "true" {
			t.Fatalf("includeItemsFromAllDrives = %q, want true", got)
		}
		wantQuery := "name contains 'Nhập môn lập trình' and trashed = false"
		if got := request.URL.Query().Get("q"); got != wantQuery {
			t.Fatalf("q = %q, want %q", got, wantQuery)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"files":[{"id":"folder_1","name":"Nhập môn lập trình","mimeType":"application/vnd.google-apps.folder"}]}`)),
		}, nil
	})}

	output, err := ListFiles(context.Background(), client, "Nhập môn lập trình", "", 10, "")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(output.Files) != 1 || output.Files[0].ID != "folder_1" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestListFilesKeepsExplicitDriveQuery(t *testing.T) {
	explicitQuery := "name contains 'report' and trashed = false"
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.URL.Query().Get("q"); got != explicitQuery {
			t.Fatalf("q = %q, want %q", got, explicitQuery)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"files":[]}`)),
		}, nil
	})}

	if _, err := ListFiles(context.Background(), client, explicitQuery, "", 10, ""); err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
}
