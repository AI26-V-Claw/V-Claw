package agent

import (
	"context"
	"errors"
	"time"

	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

type ParallelConfig struct {
	MaxWorkers         int
	ToolTimeoutDefault time.Duration
	OnStart            func(index int)
}

type parallelToolCall struct {
	call       providers.ToolCall
	definition tools.ToolDefinition
	tool       tools.Tool
}

type parallelToolResult struct {
	result   tools.ToolResult
	duration time.Duration
}

type parallelEvent struct {
	index   int
	started bool
	result  parallelToolResult
}

func executeParallelBatch(ctx context.Context, calls []parallelToolCall, cfg ParallelConfig) []parallelToolResult {
	results := make([]parallelToolResult, len(calls))
	if len(calls) == 0 {
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
	events := make(chan parallelEvent, len(calls)*2)
	for index, call := range calls {
		go func() {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				events <- parallelEvent{
					index: index,
					result: parallelToolResult{
						result: timeoutResult(call.call, ctx.Err()),
					},
				}
				return
			}

			events <- parallelEvent{index: index, started: true}
			events <- parallelEvent{
				index:  index,
				result: executeParallelToolCall(ctx, call, cfg.ToolTimeoutDefault),
			}
		}()
	}

	completed := 0
	for completed < len(calls) {
		event := <-events
		if event.started {
			if cfg.OnStart != nil {
				cfg.OnStart(event.index)
			}
			continue
		}
		results[event.index] = event.result
		completed++
	}
	return results
}

func executeParallelToolCall(ctx context.Context, call parallelToolCall, defaultTimeout time.Duration) parallelToolResult {
	startedAt := time.Now()
	toolCall := providerToolCallToToolCall(call.call)
	if call.tool == nil {
		return parallelToolResult{
			result:   tools.ExecutionErrorResult(toolCall, errors.New("parallel tool is nil")),
			duration: time.Since(startedAt),
		}
	}

	timeout := call.definition.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if timeout <= 0 {
		timeout = DefaultToolTimeout
	}

	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resultCh := make(chan tools.ToolResult, 1)
	go func() {
		resultCh <- executeToolSafely(toolCtx, call.tool, toolCall)
	}()

	var result tools.ToolResult
	select {
	case result = <-resultCh:
	case <-toolCtx.Done():
		result = timeoutResult(call.call, toolCtx.Err())
	}
	return parallelToolResult{
		result:   result,
		duration: time.Since(startedAt),
	}
}

func isBatchSystemError(results []parallelToolResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, outcome := range results {
		result := outcome.result
		if result.Success || result.Error == nil {
			return false
		}
		if result.Error.Code != tools.ErrorExecutionFailed && result.Error.Code != tools.ErrorTimeout {
			return false
		}
	}
	return true
}

func timeoutResult(call providers.ToolCall, err error) tools.ToolResult {
	message := "tool execution timed out"
	if err != nil {
		message = err.Error()
	}
	return tools.ToolResult{
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
