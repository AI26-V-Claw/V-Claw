package drive

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestUploadFileInfersMimeTypeFromExtension(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "Google Workspace Message-2026-05-29-024245.png")
	if err := os.WriteFile(localPath, []byte("png"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", request.Method)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(request.Header.Get("Content-Type"), "multipart/related") {
			t.Fatalf("content-type = %q, want multipart/related", request.Header.Get("Content-Type"))
		}
		if !strings.Contains(string(body), "image/png") {
			t.Fatalf("request body does not contain inferred mime type: %s", string(body))
		}
		if !strings.Contains(string(body), "Google Workspace Message-2026-05-29-024245.png") {
			t.Fatalf("request body does not contain file name: %s", string(body))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"file_123","name":"Google Workspace Message-2026-05-29-024245.png","mimeType":"image/png","webViewLink":"https://drive.google.com/file/d/file_123/view"}`)),
		}, nil
	})}

	file, err := UploadFile(context.Background(), client, localPath, "", "", nil)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if file.MimeType != "image/png" {
		t.Fatalf("file mimeType = %q, want image/png", file.MimeType)
	}
}

func TestUploadFileInfersDocxMimeTypeFromExtension(t *testing.T) {
	dir := t.TempDir()
	localPath := filepath.Join(dir, "11.docx")
	if err := os.WriteFile(localPath, []byte("docx"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), "application/vnd.openxmlformats-officedocument.wordprocessingml.document") {
			t.Fatalf("request body does not contain docx mime type: %s", string(body))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"file_456","name":"11.docx","mimeType":"application/vnd.openxmlformats-officedocument.wordprocessingml.document","webViewLink":"https://drive.google.com/file/d/file_456/view"}`)),
		}, nil
	})}

	file, err := UploadFile(context.Background(), client, localPath, "", "", nil)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if file.MimeType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("file mimeType = %q, want docx mime type", file.MimeType)
	}
}
