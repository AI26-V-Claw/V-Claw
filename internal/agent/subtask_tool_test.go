package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

func TestSubtaskToolParametersRequireCapabilityAllowlist(t *testing.T) {
	schema := (&SubtaskTool{}).Parameters()
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("expected required to be []string, got %T", schema["required"])
	}
	if !reflect.DeepEqual(required, []string{"task"}) {
		t.Fatalf("expected only task to be required unconditionally, got %v", required)
	}
	// anyOf is intentionally absent: OpenAI rejects top-level anyOf/oneOf in tool schemas.
	// The constraint (allowed_skills OR allowed_tool_groups required) is enforced at runtime
	// in parseSubtaskRequestWithLimits and documented in each property's description.
	if _, hasAnyOf := schema["anyOf"]; hasAnyOf {
		t.Fatalf("anyOf must not be present in schema: OpenAI rejects top-level anyOf")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties to be map[string]any, got %T", schema["properties"])
	}
	for _, name := range []string{"allowed_skills", "allowed_tool_groups"} {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Fatalf("expected %s property to be map[string]any, got %T", name, properties[name])
		}
		if property["minItems"] != 1 {
			t.Fatalf("expected %s minItems to be 1, got %v", name, property["minItems"])
		}
	}
}

func TestSubtaskToolRejectsUnknownSkill(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	tool := NewSubtaskTool(runtime)

	result := tool.Execute(withParentRunID(context.Background(), "run_parent"), tools.ToolCall{
		ID:   "call_1",
		Name: SubtaskToolName,
		Arguments: map[string]any{
			"task":           "inspect repo",
			"allowed_skills": []any{"not_a_skill"},
		},
	})

	if result.Success {
		t.Fatalf("expected unknown skill to fail, got %#v", result)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "unknown skill") {
		t.Fatalf("expected unknown skill error, got %#v", result.Error)
	}
}

func TestSubtaskToolRejectsKnownSkillWithEmptyEffectiveTools(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	tool := NewSubtaskTool(runtime)

	result := tool.Execute(withParentRunID(context.Background(), "run_parent"), tools.ToolCall{
		ID:   "call_1",
		Name: SubtaskToolName,
		Arguments: map[string]any{
			"task":           "research web",
			"allowed_skills": []any{"web_research"},
		},
	})

	if result.Success {
		t.Fatalf("expected empty effective tool set to fail, got %#v", result)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "no usable tools") {
		t.Fatalf("expected no usable tools error, got %#v", result.Error)
	}
}

func TestSubtaskRegistryLeafExcludesDelegationAndWriteTools(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	if err := runtime.registry.RegisterWithEntry(NewSubtaskTool(runtime), tools.ToolRegistryEntry{Group: "delegation"}); err != nil {
		t.Fatalf("register subtask tool: %v", err)
	}
	registry, effective, err := buildSubtaskRegistry(runtime, subtaskRequest{AllowedSkills: []string{"repo_audit"}, Role: subtaskRoleLeaf}, 1)
	if err != nil {
		t.Fatalf("buildSubtaskRegistry: %v", err)
	}
	for _, denied := range []string{SubtaskToolName, "filesystem.writeFile", "sandbox.runShell", "chat.sendMessage"} {
		if _, ok := registry.GetDefinition(denied); ok {
			t.Fatalf("leaf child should not expose %s", denied)
		}
	}
	for _, allowed := range []string{"filesystem.listDir", "filesystem.readFile", "filesystem.fileInfo"} {
		if _, ok := registry.GetDefinition(allowed); !ok {
			t.Fatalf("expected leaf child to expose %s; effective=%v", allowed, effective)
		}
	}
}

func TestSubtaskToolEnforcesMaxChildrenPerParentRun(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	tool := NewSubtaskTool(runtime)
	ctx := withParentRunID(context.Background(), "run_parent")
	args := map[string]any{
		"task":           "compute something",
		"allowed_skills": []any{"basic_compute"},
	}

	for i := 0; i < 4; i++ {
		result := tool.Execute(ctx, tools.ToolCall{ID: "call_ok", Name: SubtaskToolName, Arguments: args})
		if !result.Success {
			t.Fatalf("child %d should be accepted: %#v", i+1, result)
		}
	}
	result := tool.Execute(ctx, tools.ToolCall{ID: "call_over", Name: SubtaskToolName, Arguments: args})
	if result.Success {
		t.Fatalf("expected fifth child to fail, got %#v", result)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "max children") {
		t.Fatalf("expected max children error, got %#v", result.Error)
	}
}

func TestSubtaskToolRejectedCapabilitiesDoNotConsumeChildSlots(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	tool := NewSubtaskTool(runtime)
	ctx := withParentRunID(context.Background(), "run_parent")
	rejectedArgs := map[string]any{
		"task":           "research web",
		"allowed_skills": []any{"web_research"},
	}
	validArgs := map[string]any{
		"task":           "compute something",
		"allowed_skills": []any{"basic_compute"},
	}

	for i := 0; i < 4; i++ {
		result := tool.Execute(ctx, tools.ToolCall{ID: "call_rejected", Name: SubtaskToolName, Arguments: rejectedArgs})
		if result.Success {
			t.Fatalf("rejected capability %d should fail, got %#v", i+1, result)
		}
		if result.Error == nil || !strings.Contains(result.Error.Message, "no usable tools") {
			t.Fatalf("expected no usable tools error, got %#v", result.Error)
		}
	}

	result := tool.Execute(ctx, tools.ToolCall{ID: "call_valid", Name: SubtaskToolName, Arguments: validArgs})
	if !result.Success {
		t.Fatalf("valid child should still be accepted after rejected capabilities: %#v", result)
	}
}

func newSubtaskTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	registry := tools.NewToolRegistry()
	if err := tools.RegisterBuiltInTools(registry); err != nil {
		t.Fatalf("register builtins: %v", err)
	}
	for _, tool := range []tools.Tool{
		subtaskDummyTool{name: "filesystem.listDir", risk: tools.RiskLevelSafeRead},
		subtaskDummyTool{name: "filesystem.readFile", risk: tools.RiskLevelSafeRead},
		subtaskDummyTool{name: "filesystem.fileInfo", risk: tools.RiskLevelSafeRead},
		subtaskDummyTool{name: "safe.lookup", risk: tools.RiskLevelSafeRead},
		subtaskDummyTool{name: "filesystem.writeFile", capability: tools.CapabilityMutating, risk: tools.RiskLevelLocalWrite},
		subtaskDummyTool{name: "sandbox.runShell", capability: tools.CapabilityMutating, risk: tools.RiskLevelCodeExecution},
		subtaskDummyTool{name: "chat.sendMessage", capability: tools.CapabilityMutating, risk: tools.RiskLevelExternalWrite},
	} {
		group := "test"
		if tool.Name() == "safe.lookup" {
			group = "safe_group"
		}
		if err := registry.RegisterWithEntry(tool, tools.ToolRegistryEntry{Group: group}); err != nil {
			t.Fatalf("register %s: %v", tool.Name(), err)
		}
	}
	return NewRuntime(RuntimeConfig{
		Provider: &fakeProvider{responses: []providers.ChatResponse{{Message: providers.Message{Role: providers.MessageRoleAssistant, Content: "child done"}}}},
		Registry: registry,
		Now:      fixedTestTime,
	})
}

func TestSubtaskRegistryAllowsToolGroups(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	registry, effective, err := buildSubtaskRegistry(runtime, subtaskRequest{AllowedToolGroups: []string{"safe_group"}, Role: subtaskRoleLeaf}, 1)
	if err != nil {
		t.Fatalf("buildSubtaskRegistry: %v", err)
	}
	if _, ok := registry.GetDefinition("safe.lookup"); !ok {
		t.Fatalf("expected safe.lookup from group allowlist; effective=%v", effective)
	}
}

func TestSubtaskRegistryRejectsUnknownToolGroup(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	_, _, err := buildSubtaskRegistry(runtime, subtaskRequest{AllowedToolGroups: []string{"missing"}, Role: subtaskRoleLeaf}, 1)
	if err == nil || !strings.Contains(err.Error(), "unknown or empty tool group") {
		t.Fatalf("expected unknown group error, got %v", err)
	}
}

func TestSubtaskRegistryKeepsDelegationForOrchestratorWithinDepth(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	runtime.subtaskMaxDepth = 2
	if err := runtime.registry.RegisterWithEntry(NewSubtaskTool(runtime), tools.ToolRegistryEntry{Group: "delegation"}); err != nil {
		t.Fatalf("register subtask tool: %v", err)
	}
	registry, _, err := buildSubtaskRegistry(runtime, subtaskRequest{AllowedToolGroups: []string{"delegation"}, Role: subtaskRoleOrchestrator}, 1)
	if err != nil {
		t.Fatalf("buildSubtaskRegistry: %v", err)
	}
	if _, ok := registry.GetDefinition(SubtaskToolName); !ok {
		t.Fatalf("expected orchestrator to retain delegation tool within depth budget")
	}
}

func TestSubtaskRegistryStripsDelegationAtMaxDepth(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	runtime.subtaskMaxDepth = 1
	if err := runtime.registry.RegisterWithEntry(NewSubtaskTool(runtime), tools.ToolRegistryEntry{Group: "delegation"}); err != nil {
		t.Fatalf("register subtask tool: %v", err)
	}
	_, _, err := buildSubtaskRegistry(runtime, subtaskRequest{AllowedToolGroups: []string{"delegation"}, Role: subtaskRoleOrchestrator}, 1)
	if err == nil || !strings.Contains(err.Error(), "no usable tools") {
		t.Fatalf("expected delegation to be stripped at max depth, got %v", err)
	}
}

func TestSubtaskToolRejectsSpawnBeyondMaxDepth(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	runtime.subtaskMaxDepth = 1
	tool := NewSubtaskTool(runtime)
	ctx := withSubtaskDepth(withParentRunID(context.Background(), "run_parent"), 1)
	result := tool.Execute(ctx, tools.ToolCall{
		ID:   "call_depth",
		Name: SubtaskToolName,
		Arguments: map[string]any{
			"task":           "too deep",
			"allowed_skills": []any{"basic_compute"},
		},
	})
	if result.Success {
		t.Fatalf("expected max depth rejection, got %#v", result)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "max subtask depth") {
		t.Fatalf("expected max depth error, got %#v", result.Error)
	}
}

type subtaskDummyTool struct {
	name       string
	capability tools.Capability
	risk       tools.RiskLevel
}

func (t subtaskDummyTool) Name() string        { return t.name }
func (t subtaskDummyTool) Description() string { return "subtask dummy tool" }
func (t subtaskDummyTool) Parameters() tools.ToolSchema {
	return tools.ToolSchema{"type": "object", "properties": map[string]any{}}
}
func (t subtaskDummyTool) Capability() tools.Capability {
	if t.capability != "" {
		return t.capability
	}
	return tools.CapabilityReadOnly
}
func (t subtaskDummyTool) RiskLevel() tools.RiskLevel {
	if t.risk != "" {
		return t.risk
	}
	return tools.RiskLevelSafeRead
}
func (t subtaskDummyTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	return tools.ToolResult{ToolCallID: call.ID, ToolName: call.Name, Success: true, ContentForLLM: "ok", ContentForUser: "ok"}
}

func TestSubtaskToolSyncReturnsChildResult(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	tool := NewSubtaskTool(runtime)
	result := tool.Execute(withParentRunID(context.Background(), "run_parent_sync"), tools.ToolCall{
		ID:   "call_sync",
		Name: SubtaskToolName,
		Arguments: map[string]any{
			"task":           "summarize",
			"allowed_skills": []any{"basic_compute"},
			"mode":           "sync",
		},
	})
	if !result.Success {
		t.Fatalf("expected sync subtask success, got %#v", result)
	}
	if !strings.Contains(result.ContentForLLM, "child done") {
		t.Fatalf("expected child content in result, got %s", result.ContentForLLM)
	}
}

func TestSubtaskTimeoutSecondsIsCapped(t *testing.T) {
	request, err := parseSubtaskRequest(map[string]any{
		"task":            "slow task",
		"allowed_skills":  []any{"basic_compute"},
		"timeout_seconds": float64(9999),
	})
	if err != nil {
		t.Fatalf("parseSubtaskRequest: %v", err)
	}
	if request.Timeout != maxSubtaskTimeout {
		t.Fatalf("expected timeout cap %s, got %s", maxSubtaskTimeout, request.Timeout)
	}
	if request.Timeout > 10*time.Minute {
		t.Fatalf("timeout cap should not exceed hard cap")
	}
}

func TestSubtaskRequestRejectsAsyncMode(t *testing.T) {
	_, err := parseSubtaskRequest(map[string]any{
		"task":           "background task",
		"allowed_skills": []any{"basic_compute"},
		"mode":           "async",
	})
	if err == nil || !strings.Contains(err.Error(), "mode must be sync") {
		t.Fatalf("expected async rejection, got %v", err)
	}
}

func TestSubtaskRequestUsesConfigurableTimeoutLimits(t *testing.T) {
	request, err := parseSubtaskRequestWithLimits(map[string]any{
		"task":            "slow task",
		"allowed_skills":  []any{"basic_compute"},
		"timeout_seconds": float64(500),
	}, 10*time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("parseSubtaskRequestWithLimits: %v", err)
	}
	if request.Timeout != 30*time.Second {
		t.Fatalf("expected configurable timeout cap, got %s", request.Timeout)
	}
}

func TestSubtaskChildrenShareParentRunIDWithinRun(t *testing.T) {
	coordinator := newSubtaskCoordinator(4)
	if _, err := coordinator.reserve("run_same"); err != nil {
		t.Fatalf("reserve first: %v", err)
	}
	count, err := coordinator.reserve("run_same")
	if err != nil {
		t.Fatalf("reserve second: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected same parent run count to reach 2, got %d", count)
	}
	count, err = coordinator.reserve("run_new")
	if err != nil {
		t.Fatalf("reserve new: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected new parent run to start at 1, got %d", count)
	}
}

func TestSubtaskCoordinatorCompleteRemovesParentRunCount(t *testing.T) {
	coordinator := newSubtaskCoordinator(1)
	if _, err := coordinator.reserve("run_done"); err != nil {
		t.Fatalf("reserve first: %v", err)
	}
	if _, err := coordinator.reserve("run_done"); err == nil {
		t.Fatalf("expected max children before cleanup")
	}

	coordinator.complete("run_done")

	count, err := coordinator.reserve("run_done")
	if err != nil {
		t.Fatalf("reserve after cleanup: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected cleaned parent run to restart at 1, got %d", count)
	}
}

func TestRuntimeFinishRunStateCleansSubtaskParentRunCount(t *testing.T) {
	runtime := newSubtaskTestRuntime(t)
	if _, err := runtime.subtasks.reserve("run_done"); err != nil {
		t.Fatalf("reserve first: %v", err)
	}
	if _, err := runtime.subtasks.reserve("run_done"); err != nil {
		t.Fatalf("reserve second: %v", err)
	}
	state := RunState{RunID: "run_done", SessionID: "session", RequestID: "request", Status: RuntimeRunStatusRunning}
	if err := runtime.stateStore.CreateRun(context.Background(), state); err != nil {
		t.Fatalf("create run: %v", err)
	}

	if errShape := runtime.finishRunState(context.Background(), state, RuntimeRunStatusCompleted); errShape != nil {
		t.Fatalf("finish run: %v", errShape)
	}

	count, err := runtime.subtasks.reserve("run_done")
	if err != nil {
		t.Fatalf("reserve after finish cleanup: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected finished parent run to restart at 1, got %d", count)
	}
}

func TestSubtaskCoordinatorPrunesExpiredParentRunCounts(t *testing.T) {
	now := fixedTestTime()
	coordinator := newSubtaskCoordinator(1)
	coordinator.ttl = time.Minute
	coordinator.now = func() time.Time { return now }
	if _, err := coordinator.reserve("run_expired"); err != nil {
		t.Fatalf("reserve first: %v", err)
	}

	now = now.Add(2 * time.Minute)

	count, err := coordinator.reserve("run_expired")
	if err != nil {
		t.Fatalf("reserve after TTL prune: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected expired parent run to restart at 1, got %d", count)
	}
}
