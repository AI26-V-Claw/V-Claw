package agent

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"vclaw/internal/tools"
)

type ToolResult = tools.ToolResult

type ToolExecutor interface {
	Execute(ctx context.Context, call tools.ToolCall) ToolResult
}

type ParallelPolicy interface {
	CanRunInParallel(tool tools.Tool) bool
}

type ParallelConfig struct {
	MaxWorkers         int
	ToolTimeoutDefault time.Duration
}

type BatchError struct {
	SystemError bool
	ToolErrors  []ToolResult
}

func ExecuteParallelBatch(
	ctx context.Context,
	calls []tools.ToolCall,
	executor ToolExecutor,
	policy ParallelPolicy,
	cfg ParallelConfig,
	toolRegistry map[string]tools.Tool,
) []ToolResult {
	results := make([]ToolResult, len(calls))
	if len(calls) == 0 {
		return results
	}
	if executor == nil {
		for i, call := range calls {
			results[i] = executionErrorResult(call, "executor is nil")
		}
		return results
	}
	if ctx == nil {
		ctx = context.Background()
	}

	workerCap := cfg.MaxWorkers
	if workerCap < 1 {
		workerCap = 1
	}
	if workerCap > len(calls) {
		workerCap = len(calls)
	}

	sem := make(chan struct{}, workerCap)
	var wg sync.WaitGroup
	for i, call := range calls {
		i, call := i, call
		tool, ok := toolRegistry[call.Name]
		if !ok || tool == nil {
			results[i] = executionErrorResult(call, "tool not found in registry")
			continue
		}
		if policy != nil && !policy.CanRunInParallel(tool) {
			results[i] = executionErrorResult(call, "tool is not parallel-safe")
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			timeout := cfg.ToolTimeoutDefault
			if timeout <= 0 {
				timeout = 15 * time.Second
			}
			start := time.Now()
			toolCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			resultCh := make(chan ToolResult, 1)
			go func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						stack := debug.Stack()
						slog.Error("parallel tool panic",
							"tool_call_id", call.ID,
							"tool_name", call.Name,
							"error", recovered,
							"stacktrace", string(stack),
						)
						resultCh <- executionErrorResult(call, "panic: "+toString(recovered))
					}
				}()
				resultCh <- executor.Execute(toolCtx, call)
			}()

			select {
			case <-toolCtx.Done():
				slog.Warn("parallel tool timeout",
					"tool_call_id", call.ID,
					"tool_name", call.Name,
					"duration", time.Since(start),
					"timeout_limit", timeout,
				)
				results[i] = timeoutResult(call, toolCtx.Err())
			case result := <-resultCh:
				results[i] = result
			}
		}()
	}
	wg.Wait()
	return results
}

func IsBatchSystemError(results []ToolResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if result.Success {
			return false
		}
		if result.Error == nil {
			return false
		}
		if result.Error.Code != tools.ErrorExecutionFailed && result.Error.Code != tools.ErrorTimeout {
			return false
		}
	}

	return true
}

func NewBatchError(results []ToolResult) *BatchError {
	return &BatchError{
		SystemError: IsBatchSystemError(results),
		ToolErrors:  results,
	}
}

func executionErrorResult(call tools.ToolCall, message string) ToolResult {
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Tool execution error for " + call.Name + ": " + message,
		ContentForUser: "Tool execution error: " + call.Name,
		Error: &tools.ToolError{
			Code:    tools.ErrorExecutionFailed,
			Message: message,
		},
	}
}

func timeoutResult(call tools.ToolCall, err error) ToolResult {
	message := "tool execution timed out"
	if err != nil {
		message = err.Error()
	}
	return ToolResult{
		ToolCallID:     call.ID,
		ToolName:       call.Name,
		Success:        false,
		ContentForLLM:  "Tool execution timed out for " + call.Name,
		ContentForUser: "Tool execution timed out: " + call.Name,
		Error: &tools.ToolError{
			Code:    tools.ErrorTimeout,
			Message: message,
		},
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	default:
		return "unknown panic"
	}
}
