package agent

import (
	"context"
	"fmt"
	"time"

	"vclaw/internal/policies"
	"vclaw/internal/tools"
)

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type RunStatus string

const (
	RunStatusCompleted            RunStatus = "completed"
	RunStatusFailed               RunStatus = "failed"
	RunStatusMaxIterationsReached RunStatus = "max_iterations_reached"
)

type RunRequest struct {
	UserMessage   string
	SessionID     string
	MaxIterations int
}

type RunResult struct {
	FinalContent   string
	Iterations     int
	ToolCallsCount int
	Status         RunStatus
	Messages       []Message
	Error          string
}

const maxToolContentForLLM = 4000

type Message struct {
	Role       MessageRole
	Content    string
	ToolCallID string
	ToolCalls  []tools.ToolCall
}

type LLMClient interface {
	Chat(ctx context.Context, messages []Message, availableTools []tools.ToolDefinition) (LLMResponse, error)
}

type LLMResponse struct {
	Content   string
	ToolCalls []tools.ToolCall
}

type AgentLoop struct {
	llm                        LLMClient
	registry                   *tools.ToolRegistry
	policy                     policies.ToolPolicy
	parallelExecutionEnabled   bool
	parallelMaxWorkers         int
	parallelToolTimeoutDefault time.Duration
}

func NewAgentLoop(llm LLMClient, registry *tools.ToolRegistry) *AgentLoop {
	return NewAgentLoopWithPolicy(llm, registry, policies.NewToolPolicy())
}

func NewAgentLoopWithPolicy(llm LLMClient, registry *tools.ToolRegistry, policy policies.ToolPolicy) *AgentLoop {
	return &AgentLoop{
		llm:                        llm,
		registry:                   registry,
		policy:                     policy,
		parallelMaxWorkers:         8,
		parallelToolTimeoutDefault: 15 * time.Second,
	}
}

func (l *AgentLoop) Run(ctx context.Context, request RunRequest) RunResult {
	if l.llm == nil {
		return RunResult{Status: RunStatusFailed, Error: "llm client is required"}
	}
	if l.registry == nil {
		return RunResult{Status: RunStatusFailed, Error: "tool registry is required"}
	}

	maxIterations := request.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 1
	}

	messages := []Message{
		{Role: MessageRoleUser, Content: request.UserMessage},
	}
	toolCallsCount := 0

	for iteration := 1; iteration <= maxIterations; iteration++ {
		availableTools := l.policy.FilterTools(l.registry.ListTools())
		response, err := l.llm.Chat(ctx, cloneMessages(messages), availableTools)
		if err != nil {
			return RunResult{
				Status:         RunStatusFailed,
				Iterations:     iteration,
				ToolCallsCount: toolCallsCount,
				Messages:       messages,
				Error:          fmt.Sprintf("llm chat failed: %v", err),
			}
		}

		if len(response.ToolCalls) == 0 {
			messages = append(messages, Message{Role: MessageRoleAssistant, Content: response.Content})
			return RunResult{
				Status:         RunStatusCompleted,
				FinalContent:   response.Content,
				Iterations:     iteration,
				ToolCallsCount: toolCallsCount,
				Messages:       messages,
			}
		}

		assistantMessage := Message{
			Role:      MessageRoleAssistant,
			Content:   response.Content,
			ToolCalls: cloneToolCalls(response.ToolCalls),
		}
		messages = append(messages, assistantMessage)

		toolResults := l.executeToolCalls(ctx, response.ToolCalls)
		toolCallsCount += len(response.ToolCalls)
		for _, toolResult := range toolResults {
			messages = append(messages, Message{
				Role:       MessageRoleTool,
				Content:    truncateToolContentForLLM(toolResult.ContentForLLM),
				ToolCallID: toolResult.ToolCallID,
			})
		}
	}

	return RunResult{
		Status:         RunStatusMaxIterationsReached,
		Iterations:     maxIterations,
		ToolCallsCount: toolCallsCount,
		Messages:       messages,
		Error:          tools.ErrorMaxIterationsReached,
	}
}

func (l *AgentLoop) executeToolCalls(ctx context.Context, toolCalls []tools.ToolCall) []tools.ToolResult {
	if len(toolCalls) == 0 {
		return nil
	}
	if len(toolCalls) == 1 || !l.parallelExecutionEnabled || !l.canExecuteAllInParallel(toolCalls) {
		return l.executeToolCallsSequentially(ctx, toolCalls)
	}
	return ExecuteParallelBatch(
		ctx,
		toolCalls,
		l,
		l.policy,
		ParallelConfig{
			MaxWorkers:         l.parallelMaxWorkers,
			ToolTimeoutDefault: l.parallelToolTimeoutDefault,
		},
		l.toolRegistryMap(),
	)
}

func (l *AgentLoop) canExecuteAllInParallel(toolCalls []tools.ToolCall) bool {
	for _, toolCall := range toolCalls {
		tool, ok := l.registry.GetTool(toolCall.Name)
		if !ok || !l.policy.CanRunInParallel(tool) {
			return false
		}
	}
	return true
}

func (l *AgentLoop) executeToolCallsSequentially(ctx context.Context, toolCalls []tools.ToolCall) []tools.ToolResult {
	results := make([]tools.ToolResult, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		results = append(results, l.executeAllowedTool(ctx, toolCall))
	}
	return results
}

func (l *AgentLoop) executeAllowedTool(ctx context.Context, toolCall tools.ToolCall) tools.ToolResult {
	tool, ok := l.registry.GetTool(toolCall.Name)
	if !ok {
		return tools.ToolNotFoundResult(toolCall)
	}
	if !l.policy.CanExecute(tool) {
		return tools.PermissionDeniedResult(toolCall)
	}
	return executeToolSafely(ctx, tool, toolCall)
}

func (l *AgentLoop) Execute(ctx context.Context, call tools.ToolCall) tools.ToolResult {
	return l.executeAllowedTool(ctx, call)
}

func executeToolSafely(ctx context.Context, tool tools.Tool, toolCall tools.ToolCall) (result tools.ToolResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = tools.ExecutionErrorResult(toolCall, fmt.Errorf("panic: %v", recovered))
		}
	}()

	return tool.Execute(ctx, toolCall)
}

func (l *AgentLoop) toolRegistryMap() map[string]tools.Tool {
	if l == nil || l.registry == nil {
		return nil
	}
	definitions := l.registry.ListTools()
	toolMap := make(map[string]tools.Tool, len(definitions))
	for _, definition := range definitions {
		tool, ok := l.registry.GetTool(definition.Name)
		if !ok || tool == nil {
			continue
		}
		toolMap[definition.Name] = tool
	}
	return toolMap
}

func truncateToolContentForLLM(content string) string {
	if len(content) <= maxToolContentForLLM {
		return content
	}

	return content[:maxToolContentForLLM] + fmt.Sprintf("\n...[truncated %d bytes]", len(content)-maxToolContentForLLM)
}

func cloneMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	copy(cloned, messages)
	for i := range cloned {
		cloned[i].ToolCalls = cloneToolCalls(messages[i].ToolCalls)
	}
	return cloned
}

func cloneToolCalls(toolCalls []tools.ToolCall) []tools.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	cloned := make([]tools.ToolCall, len(toolCalls))
	copy(cloned, toolCalls)
	for i := range cloned {
		if toolCalls[i].Arguments == nil {
			continue
		}
		cloned[i].Arguments = make(map[string]any, len(toolCalls[i].Arguments))
		for key, value := range toolCalls[i].Arguments {
			cloned[i].Arguments[key] = value
		}
	}
	return cloned
}
