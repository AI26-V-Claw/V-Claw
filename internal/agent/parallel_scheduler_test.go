package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vclaw/internal/tools"
)

type testParallelTool struct {
	name     string
	cap      tools.Capability
	risk     tools.RiskLevel
	started  chan string
	release  chan struct{}
	delay    time.Duration
	panicNow bool
	result   string
}

func (t testParallelTool) Name() string                 { return t.name }
func (t testParallelTool) Description() string          { return "test tool" }
func (t testParallelTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (t testParallelTool) Capability() tools.Capability { return t.cap }
func (t testParallelTool) RiskLevel() tools.RiskLevel   { return t.risk }
func (t testParallelTool) Execute(_ context.Context, call tools.ToolCall) tools.ToolResult {
	if t.started != nil {
		t.started <- t.name
	}
	if t.panicNow {
		panic("boom")
	}
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	if t.release != nil {
		<-t.release
	}
	if t.result == "" {
		t.result = t.name + " result"
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  t.result,
		ContentForUser: t.result,
	}
}

type testParallelExecutor struct {
	mu       sync.Mutex
	executed []string
	byName   map[string]tools.Tool
}

func (e *testParallelExecutor) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	e.mu.Lock()
	e.executed = append(e.executed, call.Name)
	e.mu.Unlock()

	tool := e.byName[call.Name]
	if tool == nil {
		return tools.ToolNotFoundResult(call)
	}
	return tool.Execute(ctx, call)
}

type allowAllParallelPolicy struct{}

func (allowAllParallelPolicy) CanRunInParallel(tool tools.Tool) bool {
	return tool != nil
}

type onlySafeParallelPolicy struct{}

func (onlySafeParallelPolicy) CanRunInParallel(tool tools.Tool) bool {
	if tool == nil {
		return false
	}
	return tool.Capability() == tools.CapabilityReadOnly &&
		(tool.RiskLevel() == tools.RiskLevelSafeRead || tool.RiskLevel() == tools.RiskLevelSafeCompute)
}

func TestExecuteParallelBatchAllSafeRunsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	registry := map[string]tools.Tool{
		"safe.a": testParallelTool{
			name:    "safe.a",
			cap:     tools.CapabilityReadOnly,
			risk:    tools.RiskLevelSafeRead,
			started: started,
			release: release,
		},
		"safe.b": testParallelTool{
			name:    "safe.b",
			cap:     tools.CapabilityReadOnly,
			risk:    tools.RiskLevelSafeCompute,
			started: started,
			release: release,
		},
	}
	exec := &testParallelExecutor{byName: registry}

	done := make(chan []ToolResult, 1)
	go func() {
		done <- ExecuteParallelBatch(
			context.Background(),
			[]tools.ToolCall{{ID: "call_a", Name: "safe.a"}, {ID: "call_b", Name: "safe.b"}},
			exec,
			onlySafeParallelPolicy{},
			ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: 2 * time.Second},
			registry,
		)
	}()

	first := <-started
	second := <-started
	if first == second {
		t.Fatalf("expected two different tools to start, got %q twice", first)
	}
	close(release)

	results := <-done
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success || !results[1].Success {
		t.Fatalf("expected both tools to succeed, got %#v", results)
	}
	if results[0].ToolCallID != "call_a" || results[1].ToolCallID != "call_b" {
		t.Fatalf("expected preserved order, got %#v", results)
	}
}

func TestExecuteParallelBatchMixedSensitiveNotParallel(t *testing.T) {
	registry := map[string]tools.Tool{
		"safe.a": testParallelTool{
			name:   "safe.a",
			cap:    tools.CapabilityReadOnly,
			risk:   tools.RiskLevelSafeRead,
			result: "safe a result",
		},
		"sensitive.b": testParallelTool{
			name:   "sensitive.b",
			cap:    tools.CapabilityMutating,
			risk:   tools.RiskLevelExternalWrite,
			result: "sensitive b result",
		},
	}
	exec := &testParallelExecutor{byName: registry}

	results := ExecuteParallelBatch(
		context.Background(),
		[]tools.ToolCall{{ID: "call_safe", Name: "safe.a"}, {ID: "call_sensitive", Name: "sensitive.b"}},
		exec,
		onlySafeParallelPolicy{},
		ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: 2 * time.Second},
		registry,
	)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("expected safe tool to run, got %#v", results[0])
	}
	if results[1].Success {
		t.Fatalf("expected sensitive tool to be rejected, got %#v", results[1])
	}
	if results[1].Error == nil || results[1].Error.Message != "tool is not parallel-safe" {
		t.Fatalf("expected not parallel-safe error, got %#v", results[1].Error)
	}
}

func TestExecuteParallelBatchTimeoutDoesNotBlockBatch(t *testing.T) {
	registry := map[string]tools.Tool{
		"slow.a": testParallelTool{
			name:   "slow.a",
			cap:    tools.CapabilityReadOnly,
			risk:   tools.RiskLevelSafeRead,
			delay:  300 * time.Millisecond,
			result: "slow a result",
		},
		"fast.b": testParallelTool{
			name:   "fast.b",
			cap:    tools.CapabilityReadOnly,
			risk:   tools.RiskLevelSafeCompute,
			result: "fast b result",
		},
	}
	exec := &testParallelExecutor{byName: registry}

	start := time.Now()
	results := ExecuteParallelBatch(
		context.Background(),
		[]tools.ToolCall{{ID: "call_slow", Name: "slow.a"}, {ID: "call_fast", Name: "fast.b"}},
		exec,
		onlySafeParallelPolicy{},
		ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: 50 * time.Millisecond},
		registry,
	)
	elapsed := time.Since(start)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("expected batch to finish quickly, took %s", elapsed)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Success {
		t.Fatalf("expected slow tool to timeout, got %#v", results[0])
	}
	if results[0].Error == nil || results[0].Error.Code != tools.ErrorTimeout {
		t.Fatalf("expected timeout error, got %#v", results[0].Error)
	}
	if !results[1].Success || results[1].ContentForLLM != "fast b result" {
		t.Fatalf("expected fast tool result, got %#v", results[1])
	}
}

func TestExecuteParallelBatchPanicRecovery(t *testing.T) {
	registry := map[string]tools.Tool{
		"panic.a": testParallelTool{
			name:     "panic.a",
			cap:      tools.CapabilityReadOnly,
			risk:     tools.RiskLevelSafeRead,
			panicNow: true,
		},
		"ok.b": testParallelTool{
			name:   "ok.b",
			cap:    tools.CapabilityReadOnly,
			risk:   tools.RiskLevelSafeCompute,
			result: "ok b result",
		},
	}
	exec := &testParallelExecutor{byName: registry}

	results := ExecuteParallelBatch(
		context.Background(),
		[]tools.ToolCall{{ID: "call_panic", Name: "panic.a"}, {ID: "call_ok", Name: "ok.b"}},
		exec,
		onlySafeParallelPolicy{},
		ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: 2 * time.Second},
		registry,
	)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Success {
		t.Fatalf("expected panic slot to fail, got %#v", results[0])
	}
	if results[0].Error == nil {
		t.Fatalf("expected panic error, got nil")
	}
	if !results[1].Success || results[1].ContentForLLM != "ok b result" {
		t.Fatalf("expected unaffected second tool, got %#v", results[1])
	}
}

func TestExecuteParallelBatchOrderPreserved(t *testing.T) {
	registry := map[string]tools.Tool{
		"slow.a": testParallelTool{
			name:   "slow.a",
			cap:    tools.CapabilityReadOnly,
			risk:   tools.RiskLevelSafeRead,
			delay:  150 * time.Millisecond,
			result: "slow a result",
		},
		"fast.b": testParallelTool{
			name:   "fast.b",
			cap:    tools.CapabilityReadOnly,
			risk:   tools.RiskLevelSafeCompute,
			result: "fast b result",
		},
	}
	exec := &testParallelExecutor{byName: registry}

	results := ExecuteParallelBatch(
		context.Background(),
		[]tools.ToolCall{{ID: "call_a", Name: "slow.a"}, {ID: "call_b", Name: "fast.b"}},
		exec,
		onlySafeParallelPolicy{},
		ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: 2 * time.Second},
		registry,
	)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ToolCallID != "call_a" || results[0].ContentForLLM != "slow a result" {
		t.Fatalf("expected first result to belong to slow A, got %#v", results[0])
	}
	if results[1].ToolCallID != "call_b" || results[1].ContentForLLM != "fast b result" {
		t.Fatalf("expected second result to belong to fast B, got %#v", results[1])
	}
}

var _ = errors.New
