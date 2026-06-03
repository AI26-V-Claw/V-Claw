package agent

import "context"

type ProgressStage string

const (
	ProgressStageStarted       ProgressStage = "started"
	ProgressStageThinking      ProgressStage = "thinking"
	ProgressStageToolStarted   ProgressStage = "tool_started"
	ProgressStageToolCompleted ProgressStage = "tool_completed"
	ProgressStageToolFailed    ProgressStage = "tool_failed"
	ProgressStageFinalizing    ProgressStage = "finalizing"
)

type ProgressEvent struct {
	Stage      ProgressStage
	ToolName   string
	ToolCallID string
	Message    string
}

type ProgressSink func(context.Context, ProgressEvent)

type progressSinkKey struct{}

func WithProgressSink(ctx context.Context, sink ProgressSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, progressSinkKey{}, sink)
}

func emitProgress(ctx context.Context, event ProgressEvent) {
	sink, ok := ctx.Value(progressSinkKey{}).(ProgressSink)
	if !ok || sink == nil {
		return
	}
	sink(ctx, event)
}

func ReportProgress(ctx context.Context, event ProgressEvent) {
	emitProgress(ctx, event)
}
