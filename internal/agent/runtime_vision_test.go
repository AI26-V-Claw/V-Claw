package agent

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/sessions"
	"vclaw/internal/tools"
	sandboxtool "vclaw/internal/tools/system/sandbox"
)

type visionFakeProvider struct {
	fakeProvider
}

func (p *visionFakeProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{ImageInput: true}
}

func TestRuntimeAttachesTelegramImageToProviderOnly(t *testing.T) {
	workspaceBase := t.TempDir()
	t.Setenv("VCLAW_SANDBOX_WORKSPACE_DIR", workspaceBase)
	imagePath := writeVisionTestPNG(t, workspaceBase, "photo.png")
	store := sessions.NewInMemoryStore()
	provider := &visionFakeProvider{fakeProvider: fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Anh co mot hinh vuong mau do."},
	}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     tools.NewToolRegistry(),
		SessionStore: store,
	})

	message := runtimeTestMessage()
	message.Text = "Mo ta anh nay"
	message.Metadata = map[string]any{
		"attachments": []map[string]any{{
			"path":       imagePath,
			"filename":   "photo.png",
			"mimeType":   "image/png",
			"source":     "telegram",
			"fileSafety": allowedFileSafety(),
		}},
	}

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	user := lastUserProviderMessage(provider.calls[0].Messages)
	if len(user.Parts) != 2 {
		t.Fatalf("expected text + image parts, got %#v", user.Parts)
	}
	if user.Parts[1].Image == nil || user.Parts[1].Image.MIMEType != "image/png" || len(user.Parts[1].Image.Data) == 0 {
		t.Fatalf("expected loaded image part, got %#v", user.Parts[1])
	}

	transcript, err := store.LoadTranscript(context.Background(), message.SessionID)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	storedUser := lastUserProviderMessage(transcript)
	if len(storedUser.Parts) != 0 {
		t.Fatalf("transcript must not store raw image parts, got %#v", storedUser.Parts)
	}
	memory, err := store.LoadMemory(context.Background(), message.SessionID)
	if err != nil {
		t.Fatalf("load memory: %v", err)
	}
	if len(memory.ImageRefs) != 1 || memory.ImageRefs[0].Path == "" || memory.ImageRefs[0].MIMEType != "image/png" {
		t.Fatalf("expected image reference metadata in session memory, got %#v", memory.ImageRefs)
	}
}

func TestRuntimeFailsSoftWhenProviderDoesNotSupportVision(t *testing.T) {
	workspaceBase := t.TempDir()
	t.Setenv("VCLAW_SANDBOX_WORKSPACE_DIR", workspaceBase)
	imagePath := writeVisionTestPNG(t, workspaceBase, "photo.png")
	provider := &fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "fallback"},
	}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     tools.NewToolRegistry(),
		SessionStore: sessions.NewInMemoryStore(),
	})

	message := runtimeTestMessage()
	message.Text = "Mo ta anh nay"
	message.Metadata = map[string]any{
		"attachments": []map[string]any{{
			"path":       imagePath,
			"filename":   "photo.png",
			"mimeType":   "image/png",
			"fileSafety": allowedFileSafety(),
		}},
	}

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusFailed {
		t.Fatalf("expected failed response, got %#v", response)
	}
	if response.Error == nil || response.Error.Code != contracts.ErrorProviderUnavailable {
		t.Fatalf("expected PROVIDER_UNAVAILABLE, got %#v", response.Error)
	}
	if response.Message != "model không hỗ trợ input với ảnh" {
		t.Fatalf("unexpected response message: %q", response.Message)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider should not be called without vision support, got %d calls", len(provider.calls))
	}
}

func TestRuntimeDoesNotAttachUnscannedImageToProvider(t *testing.T) {
	workspaceBase := t.TempDir()
	t.Setenv("VCLAW_SANDBOX_WORKSPACE_DIR", workspaceBase)
	imagePath := writeVisionTestPNG(t, workspaceBase, "photo.png")
	provider := &visionFakeProvider{fakeProvider: fakeProvider{responses: []providers.ChatResponse{{
		Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "Khong co anh hop le."},
	}}}}
	runtime := NewRuntime(RuntimeConfig{
		Provider:     provider,
		Registry:     tools.NewToolRegistry(),
		SessionStore: sessions.NewInMemoryStore(),
	})

	message := runtimeTestMessage()
	message.Text = "Mo ta anh nay"
	message.Metadata = map[string]any{
		"attachments": []map[string]any{{
			"path":     imagePath,
			"filename": "photo.png",
			"mimeType": "image/png",
		}},
	}

	response, err := runtime.Run(context.Background(), message)
	if err != nil {
		t.Fatalf("run runtime: %v", err)
	}
	if response.Status != contracts.AgentStatusCompleted {
		t.Fatalf("expected completed response, got %#v", response)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("expected one provider call, got %d", len(provider.calls))
	}
	user := lastUserProviderMessage(provider.calls[0].Messages)
	if len(user.Parts) != 0 {
		t.Fatalf("unscanned image must not be sent as provider image parts, got %#v", user.Parts)
	}
}

func allowedFileSafety() map[string]any {
	return map[string]any{
		"decision":      "allow",
		"flags":         []any{"type_match"},
		"detected_type": "png",
	}
}

func TestResolveVisionAttachmentPathRejectsWorkspaceEscape(t *testing.T) {
	workspaceBase := t.TempDir()
	t.Setenv("VCLAW_SANDBOX_WORKSPACE_DIR", workspaceBase)
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("not really an image"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	_, errShape := resolveVisionAttachmentPath(outside)
	if errShape == nil || errShape.Code != contracts.ErrorFileAccessDenied {
		t.Fatalf("expected FILE_ACCESS_DENIED, got %#v", errShape)
	}
}

func writeVisionTestPNG(t *testing.T, workspaceBase string, filename string) string {
	t.Helper()
	dir := filepath.Join(workspaceBase, sandboxtool.DefaultSessionID, "workspace", "data", "telegram_attachments", "chat", "1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir image dir: %v", err)
	}
	path := filepath.Join(dir, filename)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	defer file.Close()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return path
}

func lastUserProviderMessage(messages []providers.Message) providers.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == providers.MessageRoleUser {
			return messages[i]
		}
	}
	return providers.Message{}
}
