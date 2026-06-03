package app

import (
	"context"
	"testing"
)

func TestNewAgentToolRegistryRegistersBuiltInsByDefault(t *testing.T) {
	registry, err := NewAgentToolRegistry(context.Background(), AgentRuntimeConfig{})
	if err != nil {
		t.Fatalf("NewAgentToolRegistry() error = %v", err)
	}

	if _, ok := registry.GetTool("calculator"); !ok {
		t.Fatal("expected calculator tool")
	}
	if _, ok := registry.GetTool("get_current_time"); !ok {
		t.Fatal("expected get_current_time tool")
	}
	if _, ok := registry.GetTool("calendar.listEvents"); ok {
		t.Fatal("google tools should not be registered by default")
	}
}
