package calendar

import (
	"context"
	"time"

	gcal "vclaw/internal/connectors/google/calendar"
)

// SearchEvents searches for events by title on a specific date.
// It returns "not_found" (0 events), "success" (1 event), or "disambiguation_needed" (>1 event).
func (t *Tool) SearchEvents(ctx context.Context, title string, date time.Time) ToolResult {
	// Set the time bounds to cover the entire day specified by the date.
	timeMin := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	timeMax := timeMin.Add(24 * time.Hour)

	events, err := executeWithRetry(func() ([]gcal.Event, error) {
		return t.client.ListEvents(ctx, timeMin, timeMax, title)
	})
	if err != nil {
		return mapErrorToResult(err)
	}

	if len(events) == 0 {
		return ToolResult{Status: "not_found"}
	}
	if len(events) > 1 {
		return ToolResult{Status: "disambiguation_needed", Data: events}
	}

	return ToolResult{Status: "success", Data: events[0]}
}
