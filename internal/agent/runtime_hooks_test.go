package agent

import (
	"context"
	"errors"
	"testing"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/toolhooks"
	"vclaw/internal/tools"
)

type stubToolHooks struct {
	preResult toolhooks.PreToolResult
	preErr    error
	postErr   error
	before    []toolhooks.PreToolInput
	after     []toolhooks.PostToolInput
}

func (s *stubToolHooks) BeforeTool(_ context.Context, input toolhooks.PreToolInput) (toolhooks.PreToolResult, error) {
	s.before = append(s.before, input)
	return s.preResult, s.preErr
}

func (s *stubToolHooks) AfterTool(_ context.Context, input toolhooks.PostToolInput) error {
	s.after = append(s.after, input)
	return s.postErr
}

type safeCountingTool struct {
	executions *int
}

func (safeCountingTool) Name() string                 { return "safe.count" }
func (safeCountingTool) Description() string          { return "Safe counting tool." }
func (safeCountingTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (safeCountingTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (safeCountingTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeCompute }
func (t safeCountingTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	(*t.executions)++
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  "safe executed",
		ContentForUser: "safe executed",
	}
}

func TestRuntimeExecuteInternalPolicyCheckedTool_HookBlockPreventsExecution(t *testing.T) {
	executions := 0
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	hooks := &stubToolHooks{
		preResult: toolhooks.PreToolResult{
			Decision: toolhooks.DecisionBlock,
			Reason:   "blocked by pre-hook",
		},
	}
	runtime := NewRuntime(RuntimeConfig{
		Registry:  registry,
		ToolHooks: hooks,
	})

	result := runtime.executeInternalPolicyCheckedTool(context.Background(), providers.ToolCall{
		ID:   "call_block",
		Name: "danger.count",
	})

	if result.Success {
		t.Fatal("expected blocked result")
	}
	if result.Error == nil || result.Error.Code != tools.ErrorBlockedByPolicy {
		t.Fatalf("expected blocked-by-policy error, got %#v", result.Error)
	}
	if executions != 0 {
		t.Fatalf("tool must not execute when pre-hook blocks, executions=%d", executions)
	}
	if len(hooks.before) != 1 {
		t.Fatalf("expected one pre-hook call, got %d", len(hooks.before))
	}
}

func TestRuntimeDecideToolCall_HookRequiresApprovalOverridesSafeTool(t *testing.T) {
	executions := 0
	registry := tools.NewToolRegistry()
	if err := registry.Register(safeCountingTool{executions: &executions}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	hooks := &stubToolHooks{
		preResult: toolhooks.PreToolResult{
			Decision: toolhooks.DecisionRequiresApproval,
			Reason:   "review first",
		},
	}
	runtime := NewRuntime(RuntimeConfig{
		Registry:  registry,
		ToolHooks: hooks,
	})
	definition, found := registry.GetDefinition("safe.count")
	if !found {
		t.Fatal("expected tool definition")
	}

	decision := runtime.decideToolCall(context.Background(), providers.ToolCall{
		ID:   "call_approval",
		Name: "safe.count",
	}, definition, true)

	if decision.Decision != contracts.RiskDecisionRequiresApproval {
		t.Fatalf("expected requires_approval, got %#v", decision)
	}
	if decision.Reason == "" {
		t.Fatal("expected decision reason")
	}
	if executions != 0 {
		t.Fatalf("decision stage must not execute tool, executions=%d", executions)
	}
}

func TestRuntimeExecuteInternalPolicyCheckedTool_HookErrorBlocksExecution(t *testing.T) {
	executions := 0
	registry := tools.NewToolRegistry()
	if err := registry.Register(countingDangerousTool{executions: &executions}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	runtime := NewRuntime(RuntimeConfig{
		Registry: registry,
		ToolHooks: &stubToolHooks{
			preErr: errors.New("hook storage unavailable"),
		},
	})

	result := runtime.executeInternalPolicyCheckedTool(context.Background(), providers.ToolCall{
		ID:   "call_error",
		Name: "danger.count",
	})

	if result.Success {
		t.Fatal("expected blocked result")
	}
	if executions != 0 {
		t.Fatalf("tool must not execute when pre-hook errors, executions=%d", executions)
	}
}

func TestRuntimeExecuteAllowedTool_CallsPostHook(t *testing.T) {
	executions := 0
	registry := tools.NewToolRegistry()
	if err := registry.Register(safeCountingTool{executions: &executions}); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	hooks := &stubToolHooks{
		preResult: toolhooks.PreToolResult{Decision: toolhooks.DecisionAllow},
	}
	runtime := NewRuntime(RuntimeConfig{
		Registry:  registry,
		ToolHooks: hooks,
	})
	definition, found := registry.GetDefinition("safe.count")
	if !found {
		t.Fatal("expected tool definition")
	}

	result := runtime.executeAllowedTool(context.Background(), providers.ToolCall{
		ID:   "call_post",
		Name: "safe.count",
	}, definition)

	if !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if executions != 1 {
		t.Fatalf("expected one execution, got %d", executions)
	}
	if len(hooks.after) != 1 {
		t.Fatalf("expected one post-hook call, got %d", len(hooks.after))
	}
	if hooks.after[0].ToolName != "safe.count" {
		t.Fatalf("unexpected post-hook tool name: %#v", hooks.after[0])
	}
}
