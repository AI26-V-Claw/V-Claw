package agent

import (
	"context"
	"strings"
	"testing"

	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

type staticSensitiveTool struct{}

func (staticSensitiveTool) Name() string                 { return "test.sensitiveRead" }
func (staticSensitiveTool) Description() string          { return "Returns sensitive test content." }
func (staticSensitiveTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (staticSensitiveTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (staticSensitiveTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSensitiveRead }
func (staticSensitiveTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	content := "Secret report\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789\nprivate body"
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "file", URI: "/workspace/secret.txt", Label: "secret.txt"},
		Metadata:       map[string]any{"size_bytes": 80},
	}
}

func TestExecuteAllowedToolRedactsSensitiveResultBeforeBoundary(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := registry.Register(staticSensitiveTool{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	definition, ok := registry.GetDefinition("test.sensitiveRead")
	if !ok {
		t.Fatal("missing tool definition")
	}
	runtime := NewRuntime(RuntimeConfig{Registry: registry})

	result := runtime.executeAllowedTool(context.Background(), providers.ToolCall{
		ID:   "call_sensitive",
		Name: "test.sensitiveRead",
	}, definition)

	if strings.Contains(result.ContentForLLM, "Bearer abcdef") || strings.Contains(result.ContentForLLM, "private body") {
		t.Fatalf("ContentForLLM was not redacted before boundary: %q", result.ContentForLLM)
	}
	if !strings.Contains(result.ContentForLLM, "[sensitive content redacted from LLM context]") {
		t.Fatalf("expected redaction notice, got %q", result.ContentForLLM)
	}
	if !strings.Contains(result.ContentForUser, "private body") {
		t.Fatalf("ContentForUser should remain intact, got %q", result.ContentForUser)
	}
	if !result.Redacted {
		t.Fatalf("expected Redacted=true after sensitive redaction, got %#v", result)
	}
}

func TestContractToolResultPreservesArtifactMetadataAndFlags(t *testing.T) {
	result := tools.ToolResult{
		ToolCallID:     "call_contract",
		ToolName:       "filesystem.readFile",
		Success:        true,
		ContentForLLM:  "File: big.log [content truncated]",
		ContentForUser: "File: big.log [content truncated]",
		ArtifactRef:    &tools.ToolArtifactRef{Kind: "file", URI: "/workspace/big.log", Label: "big.log"},
		Metadata:       map[string]any{"total_lines": 1000},
		Truncated:      true,
		Redacted:       true,
	}

	contractResult := contractToolResult(result, nil)

	if contractResult.ArtifactRef == nil {
		t.Fatal("expected ArtifactRef to survive contract conversion")
	}
	if contractResult.ArtifactRef.Kind != "file" || contractResult.ArtifactRef.URI != "/workspace/big.log" {
		t.Fatalf("unexpected ArtifactRef: %#v", contractResult.ArtifactRef)
	}
	if contractResult.Metadata["total_lines"] != 1000 {
		t.Fatalf("expected metadata to survive, got %#v", contractResult.Metadata)
	}
	if !contractResult.Truncated {
		t.Fatal("expected Truncated=true to survive")
	}
	if !contractResult.Redacted {
		t.Fatal("expected Redacted=true to survive contract conversion")
	}
}
