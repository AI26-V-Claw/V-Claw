package docs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gdocs "vclaw/internal/connectors/google/docs"
	"vclaw/internal/contracts"
	"vclaw/internal/tools"
	fstool "vclaw/internal/tools/os/filesystem"
)

type fakeDocsConnector struct{}

func (fakeDocsConnector) GetDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{}, nil
}
func (fakeDocsConnector) CreateDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{}, nil
}
func (fakeDocsConnector) AppendText(context.Context, string, string) (gdocs.AppendTextOutput, error) {
	return gdocs.AppendTextOutput{}, nil
}
func (fakeDocsConnector) AppendRichText(context.Context, string, gdocs.RichTextContent) (gdocs.AppendTextOutput, error) {
	return gdocs.AppendTextOutput{}, nil
}
func (fakeDocsConnector) ReplaceText(context.Context, string, string, string, bool) (gdocs.EditTextOutput, error) {
	return gdocs.EditTextOutput{}, nil
}
func (fakeDocsConnector) InsertText(context.Context, string, int64, string) (gdocs.EditTextOutput, error) {
	return gdocs.EditTextOutput{}, nil
}
func (fakeDocsConnector) DeleteContent(context.Context, string, int64, int64) (gdocs.EditTextOutput, error) {
	return gdocs.EditTextOutput{}, nil
}

type artifactDocsConnector struct {
	fakeDocsConnector
}

type recordingDocsConnector struct {
	fakeDocsConnector
	appendCalls int
	appended    string
	richCalls   int
	richContent gdocs.RichTextContent
}

func (c *recordingDocsConnector) AppendText(_ context.Context, _ string, text string) (gdocs.AppendTextOutput, error) {
	c.appendCalls++
	c.appended = text
	return gdocs.AppendTextOutput{DocumentID: "doc_123", Title: "Probability Cheat Sheet"}, nil
}

func (c *recordingDocsConnector) AppendRichText(_ context.Context, _ string, content gdocs.RichTextContent) (gdocs.AppendTextOutput, error) {
	c.richCalls++
	c.richContent = content
	return gdocs.AppendTextOutput{DocumentID: "doc_123", Title: "C program Cheat Sheet"}, nil
}

func (artifactDocsConnector) GetDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{
		ID:       "doc_123",
		Title:    "Plan",
		Revision: "rev_1",
		BodyText: "abcdef",
	}, nil
}

func (artifactDocsConnector) AppendText(context.Context, string, string) (gdocs.AppendTextOutput, error) {
	return gdocs.AppendTextOutput{DocumentID: "doc_123", Title: "Plan"}, nil
}

type partialFailureDocsConnector struct {
	fakeDocsConnector
}

func (partialFailureDocsConnector) CreateDocument(context.Context, string) (gdocs.Document, error) {
	return gdocs.Document{ID: "doc_partial", Title: "Plan"}, nil
}

func (partialFailureDocsConnector) AppendText(context.Context, string, string) (gdocs.AppendTextOutput, error) {
	return gdocs.AppendTextOutput{}, errors.New("append failed")
}

func TestRegisterToolsMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(fakeDocsConnector{})); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	assertToolMetadata(t, registry, ToolNameGetDocument, tools.CapabilityReadOnly, tools.RiskLevelSensitiveRead, true)
	assertToolMetadata(t, registry, ToolNameCreateDocument, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameAppendText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameAppendMarkdown, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameReplaceText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameInsertText, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
	assertToolMetadata(t, registry, ToolNameDeleteContent, tools.CapabilityMutating, tools.RiskLevelExternalWrite, true)
}

func TestDocsToolResultIncludesArtifactMetadataAndTruncation(t *testing.T) {
	get := NewTool(ToolNameGetDocument, NewService(artifactDocsConnector{}))
	result := get.Execute(context.Background(), tools.ToolCall{
		ID:   "call_get_doc",
		Name: ToolNameGetDocument,
		Arguments: map[string]any{
			"documentId":   "doc_123",
			"previewChars": float64(3),
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	if result.ArtifactRef == nil {
		t.Fatal("expected artifact ref")
	}
	if result.ArtifactRef.Kind != "google.docs.document" || result.ArtifactRef.ID != "doc_123" {
		t.Fatalf("unexpected artifact ref: %#v", result.ArtifactRef)
	}
	if result.Metadata["revision"] != "rev_1" {
		t.Fatalf("expected revision metadata, got %#v", result.Metadata)
	}
	if result.Metadata["preview_chars"] != 3 {
		t.Fatalf("expected preview_chars metadata, got %#v", result.Metadata)
	}
	if !result.Truncated {
		t.Fatal("expected truncated document preview")
	}
}

func TestCreateDocumentPartialAppendFailureUsesContractErrorAndArtifact(t *testing.T) {
	create := NewTool(ToolNameCreateDocument, NewService(partialFailureDocsConnector{}))
	result := create.Execute(context.Background(), tools.ToolCall{
		ID:   "call_create_doc",
		Name: ToolNameCreateDocument,
		Arguments: map[string]any{
			"title":   "Plan",
			"content": "Initial content",
		},
	})

	if result.Success {
		t.Fatalf("expected partial append failure, got success: %#v", result)
	}
	if result.Error == nil || result.Error.Code != contracts.ErrorInternal {
		t.Fatalf("expected INTERNAL_ERROR, got %#v", result.Error)
	}
	if result.ArtifactRef == nil {
		t.Fatal("expected artifact ref for the created document")
	}
	if result.ArtifactRef.ID != "doc_partial" {
		t.Fatalf("unexpected artifact ref: %#v", result.ArtifactRef)
	}
	if !strings.Contains(result.ContentForLLM, "doc_partial") {
		t.Fatalf("expected created document ID in LLM content, got %q", result.ContentForLLM)
	}
}

func TestAppendTextReadsCompleteUTF8WorkspaceFile(t *testing.T) {
	workspace := t.TempDir()
	localPath := filepath.Join(workspace, "extracted", "probability_cheatsheet.txt")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("create extraction directory: %v", err)
	}
	want := strings.Repeat("Probability content\n", 4000)
	if err := os.WriteFile(localPath, []byte(want), 0o600); err != nil {
		t.Fatalf("write extraction: %v", err)
	}
	connector := &recordingDocsConnector{}
	tool := NewTool(ToolNameAppendText, NewService(connector), fstool.NewPathGuard([]string{workspace}))

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_append_file",
		Name: ToolNameAppendText,
		Arguments: map[string]any{
			"documentId": "doc_123",
			"localPath":  localPath,
		},
	})

	if !result.Success {
		t.Fatalf("append local file failed: %#v", result.Error)
	}
	if connector.appendCalls != 1 || connector.appended != want {
		t.Fatalf("connector received incomplete content: calls=%d chars=%d want=%d", connector.appendCalls, len(connector.appended), len(want))
	}
	if result.Metadata["local_file_bytes"] != len([]byte(want)) {
		t.Fatalf("unexpected local file metadata: %#v", result.Metadata)
	}
}

func TestAppendTextLocalPathRequiresWorkspaceGuard(t *testing.T) {
	localPath := filepath.Join(t.TempDir(), "content.txt")
	if err := os.WriteFile(localPath, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	connector := &recordingDocsConnector{}
	tool := NewTool(ToolNameAppendText, NewService(connector))

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_append_unguarded",
		Name: ToolNameAppendText,
		Arguments: map[string]any{
			"documentId": "doc_123",
			"localPath":  localPath,
		},
	})

	if result.Success || result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected unguarded localPath to fail safely, got %#v", result)
	}
	if connector.appendCalls != 0 {
		t.Fatalf("connector called before localPath validation: %d", connector.appendCalls)
	}
}

func TestAppendTextRejectsFileOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	connector := &recordingDocsConnector{}
	tool := NewTool(ToolNameAppendText, NewService(connector), fstool.NewPathGuard([]string{workspace}))

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_append_outside",
		Name: ToolNameAppendText,
		Arguments: map[string]any{
			"documentId": "doc_123",
			"localPath":  outside,
		},
	})

	if result.Success || result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected out-of-workspace localPath to fail, got %#v", result)
	}
	if connector.appendCalls != 0 {
		t.Fatalf("connector called for outside file: %d", connector.appendCalls)
	}
}

func TestAppendTextRejectsAmbiguousContentSources(t *testing.T) {
	connector := &recordingDocsConnector{}
	tool := NewTool(ToolNameAppendText, NewService(connector), fstool.NewPathGuard([]string{t.TempDir()}))
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_append_ambiguous",
		Name: ToolNameAppendText,
		Arguments: map[string]any{
			"documentId": "doc_123",
			"text":       "inline",
			"localPath":  "content.txt",
		},
	})
	if result.Success || result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected ambiguous sources to fail, got %#v", result)
	}
}

func TestParseMarkdownRichTextPreservesStructureAndUTF16Offsets(t *testing.T) {
	markdown := "# C 😀 Cheat Sheet\n\n## Console Input/Output\n\n| Entry | Description |\n| --- | --- |\n| getchar() | Read one character |\n\n### Notes\n\n- Safe input\n\n~~~c\n#include <stdio.h>\n~~~\n"
	content := parseMarkdownRichText(markdown)

	for _, unwanted := range []string{"# ", "| ---", "~~~", "- Safe"} {
		if strings.Contains(content.Text, unwanted) {
			t.Fatalf("rendered text still contains Markdown syntax %q:\n%s", unwanted, content.Text)
		}
	}
	for _, want := range []string{"C 😀 Cheat Sheet", "Console Input/Output", "Entry\tDescription", "getchar()\tRead one character", "#include <stdio.h>"} {
		if !strings.Contains(content.Text, want) {
			t.Fatalf("rendered text missing %q:\n%s", want, content.Text)
		}
	}
	if len(content.ParagraphStyles) != 3 {
		t.Fatalf("paragraph styles = %d, want 3: %#v", len(content.ParagraphStyles), content.ParagraphStyles)
	}
	if content.ParagraphStyles[0].End != utf16Length("C 😀 Cheat Sheet\n") {
		t.Fatalf("title range does not use UTF-16 indices: %#v", content.ParagraphStyles[0])
	}
	if len(content.BulletRanges) != 1 || len(content.TextStyles) < 3 {
		t.Fatalf("expected bullets and table/code styles, bullets=%#v styles=%#v", content.BulletRanges, content.TextStyles)
	}
}

func TestAppendMarkdownReadsWorkspaceFileAndSendsRichContent(t *testing.T) {
	workspace := t.TempDir()
	localPath := filepath.Join(workspace, "c_cheatsheet_structured.md")
	markdown := "# C Cheat Sheet\n\n## Console Input/Output\n\n| Entry | Description |\n| --- | --- |\n| getchar() | Reads one character |\n"
	if err := os.WriteFile(localPath, []byte(markdown), 0o600); err != nil {
		t.Fatalf("write Markdown fixture: %v", err)
	}
	connector := &recordingDocsConnector{}
	tool := NewTool(ToolNameAppendMarkdown, NewService(connector), fstool.NewPathGuard([]string{workspace}))

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "call_append_markdown",
		Name: ToolNameAppendMarkdown,
		Arguments: map[string]any{
			"documentId": "doc_123",
			"localPath":  localPath,
		},
	})

	if !result.Success {
		t.Fatalf("append Markdown failed: %#v", result.Error)
	}
	if connector.richCalls != 1 || !strings.Contains(connector.richContent.Text, "Entry\tDescription") {
		t.Fatalf("connector did not receive parsed rich content: calls=%d content=%#v", connector.richCalls, connector.richContent)
	}
	if result.Metadata["local_file_bytes"] != len([]byte(markdown)) || result.Metadata["paragraph_styles"] != 2 {
		t.Fatalf("unexpected Markdown metadata: %#v", result.Metadata)
	}
}

func assertToolMetadata(t *testing.T, registry *tools.ToolRegistry, name string, capability tools.Capability, risk tools.RiskLevel, approval bool) {
	t.Helper()
	definition, ok := registry.GetDefinition(name)
	if !ok {
		t.Fatalf("expected %s definition", name)
	}
	if definition.Capability != capability {
		t.Fatalf("%s capability = %s, want %s", name, definition.Capability, capability)
	}
	if definition.RiskLevel != risk {
		t.Fatalf("%s risk = %s, want %s", name, definition.RiskLevel, risk)
	}
	if definition.RequiresApproval != approval {
		t.Fatalf("%s approval = %t, want %t", name, definition.RequiresApproval, approval)
	}
}
