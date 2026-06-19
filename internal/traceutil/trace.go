package traceutil

import (
	"context"
	"os"
	"strings"
)

type contextKey struct{}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	traceID = strings.TrimSpace(traceID)
	if ctx == nil || traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, traceID)
}

func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(contextKey{}).(string)
	return strings.TrimSpace(traceID)
}

func BuildTraceURL(traceID string) string {
	traceID = strings.TrimSpace(traceID)
	projectID := strings.TrimSpace(os.Getenv("LANGFUSE_PROJECT_ID"))
	if traceID == "" || projectID == "" {
		return ""
	}
	host := strings.TrimRight(strings.TrimSpace(os.Getenv("LANGFUSE_HOST")), "/")
	if host == "" {
		host = "https://cloud.langfuse.com"
	}
	return host + "/project/" + projectID + "/traces/" + traceID
}
