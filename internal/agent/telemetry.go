package agent

import (
	"context"
	"time"

	"vclaw/internal/contracts"
	"vclaw/internal/providers"
	"vclaw/internal/tools"
)

type ApprovalTelemetryEvent struct {
	Status     ActionStatus
	ApprovalID string
	RequestID  string
	SessionID  string
	ToolCallID string
	ToolName   string
	RiskLevel  contracts.RiskLevel
	Comment    string
	ExpiresAt  time.Time
}

type RuntimeTelemetry interface {
	StartRequest(ctx context.Context, message contracts.UserMessage) (context.Context, func(contracts.AgentResponse, error))
	WrapProvider(provider providers.Provider) providers.Provider
	RecordToolCall(ctx context.Context, toolCall providers.ToolCall, result tools.ToolResult, latency time.Duration)
	RecordApproval(ctx context.Context, event ApprovalTelemetryEvent)
}
