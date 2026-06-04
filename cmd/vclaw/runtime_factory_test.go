package main

import (
	"strings"
	"testing"

	"vclaw/internal/tools"
	webtools "vclaw/internal/tools/web"
)

func TestRegisterWebToolsAutoSkipsWhenAPIKeyMissing(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("TALIVY_API_KEY", "")
	t.Setenv("VCLAW_WEB_TOOLS_MODE", "")

	registry := tools.NewToolRegistry()
	if err := registerWebTools(registry, agentRuntimeOptions{WebToolsMode: webToolsAuto}); err != nil {
		t.Fatalf("registerWebTools: %v", err)
	}
	if _, ok := registry.GetDefinition(webtools.ToolNameSearch); ok {
		t.Fatalf("web.search should not be registered without API key in auto mode")
	}
}

func TestRegisterWebToolsRequiredFailsWhenAPIKeyMissing(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("TALIVY_API_KEY", "")

	err := registerWebTools(tools.NewToolRegistry(), agentRuntimeOptions{WebToolsMode: webToolsRequired})
	if err == nil || !strings.Contains(err.Error(), "TAVILY_API_KEY") {
		t.Fatalf("expected missing TAVILY_API_KEY error, got %v", err)
	}
}

func TestRegisterWebToolsRegistersWhenAPIKeyExists(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "tvly-test")
	t.Setenv("TALIVY_API_KEY", "")

	registry := tools.NewToolRegistry()
	if err := registerWebTools(registry, agentRuntimeOptions{WebToolsMode: webToolsAuto}); err != nil {
		t.Fatalf("registerWebTools: %v", err)
	}
	for _, name := range []string{webtools.ToolNameSearch, webtools.ToolNameFetch} {
		definition, ok := registry.GetDefinition(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if definition.RiskLevel != tools.RiskLevelSafeRead || definition.RequiresApproval {
			t.Fatalf("unexpected definition for %s: %#v", name, definition)
		}
	}
}

func TestRegisterWebToolsOffSkipsWhenAPIKeyExists(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "tvly-test")

	registry := tools.NewToolRegistry()
	if err := registerWebTools(registry, agentRuntimeOptions{WebToolsMode: webToolsOff}); err != nil {
		t.Fatalf("registerWebTools: %v", err)
	}
	if _, ok := registry.GetDefinition(webtools.ToolNameSearch); ok {
		t.Fatalf("web.search should not be registered in off mode")
	}
}
