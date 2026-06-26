package meet

import (
	"context"
	"errors"
	"testing"

	gmeet "vclaw/internal/connectors/google/meet"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

type mockConnector struct {
	createSpaceFunc func(ctx context.Context) (gmeet.Space, error)
}

func (m *mockConnector) CreateSpace(ctx context.Context) (gmeet.Space, error) {
	if m.createSpaceFunc != nil {
		return m.createSpaceFunc(ctx)
	}
	return gmeet.Space{}, nil
}

func TestCreateMeetingToolMetadata(t *testing.T) {
	registry := tools.NewToolRegistry()
	if err := RegisterTools(registry, NewService(&mockConnector{})); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	var found *tools.ToolDefinition
	for _, definition := range registry.ListTools() {
		if definition.Name == ToolNameCreateMeeting {
			def := definition
			found = &def
			break
		}
	}
	if found == nil {
		t.Fatalf("tool %s not registered", ToolNameCreateMeeting)
	}
	if found.RiskLevel != tools.RiskLevelExternalWrite {
		t.Fatalf("risk = %s, want external_write", found.RiskLevel)
	}
	if !found.RequiresApproval {
		t.Fatal("meet.createMeeting must require approval")
	}
}

func TestCreateMeetingToolExecute(t *testing.T) {
	tool := &CreateMeetingTool{service: NewService(&mockConnector{
		createSpaceFunc: func(ctx context.Context) (gmeet.Space, error) {
			return gmeet.Space{Name: "spaces/abc", MeetingURI: "https://meet.google.com/aaa-bbbb-ccc", MeetingCode: "aaa-bbbb-ccc"}, nil
		},
	})}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_meet",
		Name: ToolNameCreateMeeting,
		Arguments: map[string]any{
			"mode": ModeForLater,
		},
	})
	if !result.Success {
		t.Fatalf("expected success, got %#v", result.Error)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.Kind != "google.meet.space" || result.ArtifactRef.URI != "https://meet.google.com/aaa-bbbb-ccc" {
		t.Fatalf("unexpected artifact: %#v", result.ArtifactRef)
	}
}

func TestCreateMeetingInvalidMode(t *testing.T) {
	called := false
	tool := &CreateMeetingTool{service: NewService(&mockConnector{
		createSpaceFunc: func(ctx context.Context) (gmeet.Space, error) {
			called = true
			return gmeet.Space{}, nil
		},
	})}
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:        "tc_bad",
		Name:      ToolNameCreateMeeting,
		Arguments: map[string]any{"mode": "calendar"},
	})
	if result.Success || result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected invalid input, got %#v", result)
	}
	if called {
		t.Fatal("connector should not be called for invalid mode")
	}
}

func TestCreateMeetingMissingScope(t *testing.T) {
	tool := &CreateMeetingTool{service: NewService(&mockConnector{
		createSpaceFunc: func(ctx context.Context) (gmeet.Space, error) {
			return gmeet.Space{}, &googleapi.Error{Code: 403, Message: "Request had insufficient authentication scopes."}
		},
	})}
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:        "tc_scope",
		Name:      ToolNameCreateMeeting,
		Arguments: map[string]any{"mode": ModeInstant},
	})
	if result.Success || result.Error == nil || result.Error.Code != "AUTH_MISSING_SCOPE" {
		t.Fatalf("expected AUTH_MISSING_SCOPE, got %#v", result.Error)
	}
}

func TestCreateMeetingProviderError(t *testing.T) {
	tool := &CreateMeetingTool{service: NewService(&mockConnector{
		createSpaceFunc: func(ctx context.Context) (gmeet.Space, error) {
			return gmeet.Space{}, errors.New("boom")
		},
	})}
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:        "tc_error",
		Name:      ToolNameCreateMeeting,
		Arguments: map[string]any{"mode": ModeForLater},
	})
	if result.Success || result.Error == nil {
		t.Fatalf("expected error, got %#v", result)
	}
}
