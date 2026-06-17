package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vclaw/internal/monitoring"
)

func runLogs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vclaw logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	limit := fs.Int("limit", 50, "maximum number of log events to show")
	sinceRaw := fs.String("since", "1h", sinceHelpText)
	level := fs.String("level", "", "optional level filter: error or info")
	tool := fs.String("tool", "", "optional tool name filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if value := strings.TrimSpace(*level); value != "" && value != "error" && value != "info" {
		return fmt.Errorf("invalid level %q", value)
	}
	since, err := parseSince(*sinceRaw, time.Now())
	if err != nil {
		return err
	}

	events, err := monitoring.QueryLogs(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), monitoring.LogQuery{
		Limit: *limit,
		Since: since,
		Level: strings.TrimSpace(*level),
		Tool:  strings.TrimSpace(*tool),
	})
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Println("No audit log events found.")
		return nil
	}
	for _, event := range events {
		fmt.Println(formatLogEvent(event))
	}
	return nil
}

func formatLogEvent(event monitoring.LogEvent) string {
	parts := []string{
		event.Timestamp.Format(time.RFC3339),
		"[" + event.Level + "]",
		event.EventType,
	}
	if value := strings.TrimSpace(event.Status); value != "" {
		parts = append(parts, "status="+value)
	}
	if value := strings.TrimSpace(event.ToolName); value != "" {
		parts = append(parts, "tool="+value)
	}
	if value := strings.TrimSpace(event.ApprovalID); value != "" {
		parts = append(parts, "approvalId="+value)
	}
	if value := strings.TrimSpace(event.RequestID); value != "" {
		parts = append(parts, "requestId="+value)
	}
	if value := strings.TrimSpace(event.SessionID); value != "" {
		parts = append(parts, "sessionId="+value)
	}
	if value := strings.TrimSpace(event.TraceURL); value != "" {
		parts = append(parts, "traceUrl="+value)
	}
	if value := strings.TrimSpace(event.Message); value != "" {
		parts = append(parts, "message="+compactField(value))
	}
	if value := strings.TrimSpace(event.Error); value != "" {
		parts = append(parts, "error="+compactField(value))
	}
	return strings.Join(parts, " ")
}

func compactField(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 120 {
		return value[:117] + "..."
	}
	return value
}
