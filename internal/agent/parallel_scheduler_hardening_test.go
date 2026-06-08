package agent

import (
	"testing"

	"vclaw/internal/tools"
)

func TestIsBatchSystemErrorAllFailed(t *testing.T) {
	results := []ToolResult{
		{
			ToolCallID: "call_1",
			ToolName:   "tool.a",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
		{
			ToolCallID: "call_2",
			ToolName:   "tool.b",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
		{
			ToolCallID: "call_3",
			ToolName:   "tool.c",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
	}

	if !IsBatchSystemError(results) {
		t.Fatal("expected all-failed batch to be a system error")
	}
}

func TestIsBatchSystemErrorPartialSuccess(t *testing.T) {
	results := []ToolResult{
		{
			ToolCallID: "call_1",
			ToolName:   "tool.a",
			Success:    true,
		},
		{
			ToolCallID: "call_2",
			ToolName:   "tool.b",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
	}

	if IsBatchSystemError(results) {
		t.Fatal("expected partial success batch to not be a system error")
	}
}

func TestNewBatchErrorSystemErrorFlag(t *testing.T) {
	results := []ToolResult{
		{
			ToolCallID: "call_1",
			ToolName:   "tool.a",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
		{
			ToolCallID: "call_2",
			ToolName:   "tool.b",
			Success:    false,
			Error: &tools.ToolError{
				Code:    tools.ErrorExecutionFailed,
				Message: "boom",
			},
		},
	}

	batchErr := NewBatchError(results)
	if batchErr == nil {
		t.Fatal("expected batch error")
	}
	if !batchErr.SystemError {
		t.Fatal("expected system error flag to be true")
	}
	if len(batchErr.ToolErrors) != len(results) {
		t.Fatalf("expected %d tool errors, got %d", len(results), len(batchErr.ToolErrors))
	}
}
