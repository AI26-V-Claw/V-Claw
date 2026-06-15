package toolhooks

import (
	"context"
	"errors"
	"testing"
)

type countingHook struct {
	beforeCalls int
	afterCalls  int
	preResult   PreToolResult
	preErr      error
	postErr     error
}

func (h *countingHook) BeforeTool(context.Context, PreToolInput) (PreToolResult, error) {
	h.beforeCalls++
	return h.preResult, h.preErr
}

func (h *countingHook) AfterTool(context.Context, PostToolInput) error {
	h.afterCalls++
	return h.postErr
}

func TestChainHooksBeforeToolRunsAllHooks(t *testing.T) {
	first := &countingHook{
		preResult: PreToolResult{
			Decision: DecisionBlock,
			Reason:   "blocked",
		},
	}
	second := &countingHook{
		preResult: PreToolResult{Decision: DecisionAllow},
	}

	result, err := ChainHooks{first, second}.BeforeTool(context.Background(), PreToolInput{})
	if err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}
	if result.Decision != DecisionBlock {
		t.Fatalf("BeforeTool() decision = %q, want %q", result.Decision, DecisionBlock)
	}
	if first.beforeCalls != 1 || second.beforeCalls != 1 {
		t.Fatalf("expected both hooks to run, got first=%d second=%d", first.beforeCalls, second.beforeCalls)
	}
}

func TestChainHooksBeforeToolContinuesAfterError(t *testing.T) {
	first := &countingHook{preErr: errors.New("boom")}
	second := &countingHook{
		preResult: PreToolResult{Decision: DecisionAllow},
	}

	_, err := ChainHooks{first, second}.BeforeTool(context.Background(), PreToolInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	if first.beforeCalls != 1 || second.beforeCalls != 1 {
		t.Fatalf("expected both hooks to run, got first=%d second=%d", first.beforeCalls, second.beforeCalls)
	}
}

func TestChainHooksAfterToolRunsAllHooks(t *testing.T) {
	first := &countingHook{postErr: errors.New("boom")}
	second := &countingHook{}

	err := ChainHooks{first, second}.AfterTool(context.Background(), PostToolInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	if first.afterCalls != 1 || second.afterCalls != 1 {
		t.Fatalf("expected both hooks to run, got first=%d second=%d", first.afterCalls, second.afterCalls)
	}
}
