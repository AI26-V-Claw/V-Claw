package monitoring

import (
	"context"
	"strings"
	"testing"

	"vclaw/internal/providers"
)

type capabilityTestProvider struct{}

func (capabilityTestProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (capabilityTestProvider) Generate(context.Context, *providers.GenerateRequest) (*providers.GenerateResponse, error) {
	return nil, nil
}

func (capabilityTestProvider) Name() string { return "capability-test" }

func (capabilityTestProvider) Close() error { return nil }

func (capabilityTestProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{ImageInput: true}
}

func TestLangfuseProviderForwardsCapabilities(t *testing.T) {
	wrapped := (&Langfuse{}).WrapProvider(capabilityTestProvider{})
	if !providers.ProviderCapabilities(wrapped).ImageInput {
		t.Fatal("expected wrapped provider to preserve image input capability")
	}
}

func TestSanitizeProviderMessagesForTelemetryRemovesImageBytes(t *testing.T) {
	messages := []providers.Message{{
		Role:    providers.MessageRoleUser,
		Content: "look",
		Parts: []providers.ContentPart{
			{Type: "text", Text: "look"},
			{Type: "image", Image: &providers.ImageContent{
				MIMEType: "image/png",
				Data:     []byte{1, 2, 3},
				Filename: "photo.png",
			}},
		},
	}}

	payload := compactJSON(sanitizeProviderMessagesForTelemetry(messages))
	if strings.Contains(payload, "AQID") || strings.Contains(payload, "[1,2,3]") {
		t.Fatalf("telemetry payload must not contain image bytes: %s", payload)
	}
	if !strings.Contains(payload, "photo.png") || !strings.Contains(payload, "image/png") {
		t.Fatalf("expected safe image metadata, got %s", payload)
	}
}
