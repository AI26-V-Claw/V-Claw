package agent

import (
	"testing"

	"vclaw/internal/tools"
)

func TestIsBatchSystemErrorAllFailed(t *testing.T) {
	results := []parallelToolResult{
		{result: failedParallelTestResult("call_1", tools.ErrorExecutionFailed)},
		{result: failedParallelTestResult("call_2", tools.ErrorTimeout)},
	}
	if !isBatchSystemError(results) {
		t.Fatal("expected all-failed execution batch to be a system error")
	}
}

func TestIsBatchSystemErrorPartialSuccess(t *testing.T) {
	results := []parallelToolResult{
		{result: tools.ToolResult{ToolCallID: "call_1", ToolName: "tool.a", Success: true}},
		{result: failedParallelTestResult("call_2", tools.ErrorExecutionFailed)},
	}
	if isBatchSystemError(results) {
		t.Fatal("expected partial success batch not to be a system error")
	}
}

func failedParallelTestResult(id string, code string) tools.ToolResult {
	return tools.ToolResult{
		ToolCallID: id,
		ToolName:   "test.tool",
		Error: &tools.ToolError{
			Code:    code,
			Message: "boom",
		},
	}
}
