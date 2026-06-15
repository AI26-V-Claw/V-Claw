package toolhooks

import (
	"context"
	"testing"
)

func TestNoopHooksAllowByDefault(t *testing.T) {
	hooks := NoopHooks{}
	result, err := hooks.BeforeTool(context.Background(), PreToolInput{})
	if err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}
	if result.Decision != DecisionAllow {
		t.Fatalf("BeforeTool() decision = %q, want %q", result.Decision, DecisionAllow)
	}
	if err := hooks.AfterTool(context.Background(), PostToolInput{}); err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
}
