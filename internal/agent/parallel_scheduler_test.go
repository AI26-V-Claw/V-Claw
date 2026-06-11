package agent

import (
	"context"
	"testing"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

type testParallelTool struct {
	name     string
	started  chan string
	release  chan struct{}
	delay    time.Duration
	panicNow bool
	result   string
}

func (t testParallelTool) Name() string                 { return t.name }
func (t testParallelTool) Description() string          { return "test tool" }
func (t testParallelTool) Parameters() tools.ToolSchema { return tools.ToolSchema{"type": "object"} }
func (t testParallelTool) Capability() tools.Capability { return tools.CapabilityReadOnly }
func (t testParallelTool) RiskLevel() tools.RiskLevel   { return tools.RiskLevelSafeRead }
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
	content := t.result
	if content == "" {
		content = t.name + " result"
	}
	return tools.ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        true,
		ContentForLLM:  content,
		ContentForUser: content,
	}
}

func parallelTestCall(id string, tool testParallelTool, timeout time.Duration) parallelToolCall {
	return parallelToolCall{
		call: providers.ToolCall{
			ID:   id,
			Name: tool.Name(),
		},
		definition: tools.ToolDefinition{
			Name:    tool.Name(),
			Timeout: timeout,
		},
		tool: tool,
	}
}

func TestExecuteParallelBatchRunsConcurrentlyAndPreservesOrder(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	first := testParallelTool{name: "safe.a", started: started, release: release}
	second := testParallelTool{name: "safe.b", started: started, release: release}

	done := make(chan []parallelToolResult, 1)
	go func() {
		done <- executeParallelBatch(context.Background(), []parallelToolCall{
			parallelTestCall("call_a", first, 0),
			parallelTestCall("call_b", second, 0),
		}, ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: time.Second})
	}()

	firstStarted := <-started
	secondStarted := <-started
	if firstStarted == secondStarted {
		t.Fatalf("expected both tools to start, got %q twice", firstStarted)
	}
	close(release)

	results := <-done
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].result.Success || !results[1].result.Success {
		t.Fatalf("expected both tools to succeed, got %#v", results)
	}
	if results[0].result.ToolCallID != "call_a" || results[1].result.ToolCallID != "call_b" {
		t.Fatalf("expected input order, got %#v", results)
	}
}

func TestExecuteParallelBatchEmitsStartAfterWorkerSlotAcquired(t *testing.T) {
	release := make(chan struct{})
	startEvents := make(chan int, 2)
	first := testParallelTool{name: "safe.a", release: release}
	second := testParallelTool{name: "safe.b", release: release}

	done := make(chan []parallelToolResult, 1)
	go func() {
		done <- executeParallelBatch(context.Background(), []parallelToolCall{
			parallelTestCall("call_a", first, 0),
			parallelTestCall("call_b", second, 0),
		}, ParallelConfig{
			MaxWorkers:         1,
			ToolTimeoutDefault: time.Second,
			OnStart: func(index int) {
				startEvents <- index
			},
		})
	}()

	<-startEvents
	select {
	case index := <-startEvents:
		t.Fatalf("queued call %d reported started before a worker slot was available", index)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	<-startEvents
	results := <-done
	if !results[0].result.Success || !results[1].result.Success {
		t.Fatalf("expected both calls to succeed, got %#v", results)
	}
}

func TestExecuteParallelBatchUsesParallelDefaultTimeout(t *testing.T) {
	slow := testParallelTool{name: "slow", delay: 250 * time.Millisecond}
	startedAt := time.Now()
	results := executeParallelBatch(context.Background(), []parallelToolCall{
		parallelTestCall("call_slow", slow, 0),
	}, ParallelConfig{MaxWorkers: 1, ToolTimeoutDefault: 20 * time.Millisecond})

	if elapsed := time.Since(startedAt); elapsed > 150*time.Millisecond {
		t.Fatalf("expected parallel default timeout, took %s", elapsed)
	}
	if results[0].result.Error == nil || results[0].result.Error.Code != tools.ErrorTimeout {
		t.Fatalf("expected timeout result, got %#v", results[0].result)
	}
}

func TestExecuteParallelBatchDefinitionTimeoutTakesPrecedence(t *testing.T) {
	slow := testParallelTool{name: "slow", delay: 80 * time.Millisecond}
	results := executeParallelBatch(context.Background(), []parallelToolCall{
		parallelTestCall("call_slow", slow, 200*time.Millisecond),
	}, ParallelConfig{MaxWorkers: 1, ToolTimeoutDefault: 10 * time.Millisecond})

	if !results[0].result.Success {
		t.Fatalf("expected definition timeout to override parallel default, got %#v", results[0].result)
	}
}

func TestExecuteParallelBatchRecoversPanicWithoutAffectingOtherCalls(t *testing.T) {
	panicTool := testParallelTool{name: "panic", panicNow: true}
	okTool := testParallelTool{name: "ok", result: "ok result"}
	results := executeParallelBatch(context.Background(), []parallelToolCall{
		parallelTestCall("call_panic", panicTool, 0),
		parallelTestCall("call_ok", okTool, 0),
	}, ParallelConfig{MaxWorkers: 2, ToolTimeoutDefault: time.Second})

	if results[0].result.Success || results[0].result.Error == nil {
		t.Fatalf("expected panic call to fail, got %#v", results[0].result)
	}
	if !results[1].result.Success || results[1].result.ContentForLLM != "ok result" {
		t.Fatalf("expected second call to succeed, got %#v", results[1].result)
	}
}
