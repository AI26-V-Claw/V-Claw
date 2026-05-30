package calendar

import (
	"context"
	"time"

	gcal "vclaw/internal/connectors/google/calendar"
)

// CheckConflict searches for events in the given time frame and checks if any provided attendee is busy.
// It returns "conflict" status if an overlap is found, otherwise "success".
func (t *Tool) CheckConflict(ctx context.Context, timeMin, timeMax time.Time, attendees []string) ToolResult {
	events, err := executeWithRetry(func() ([]gcal.Event, error) {
		return t.client.ListEvents(ctx, timeMin, timeMax, "")
	})
	if err != nil {
		return mapErrorToResult(err)
	}

	if len(events) == 0 {
		return ToolResult{Status: "success"}
	}

	if len(attendees) == 0 {
		// If no specific attendees are provided, any overlapping event is a conflict for the main user.
		return ToolResult{Status: "conflict", Data: events[0]}
	}

	// Check if any overlapping event includes any of the requested attendees.
	for _, ev := range events {
		for _, a := range ev.Attendees {
			for _, reqA := range attendees {
				if a.Email == reqA {
					return ToolResult{Status: "conflict", Data: ev}
				}
			}
		}
	}

	return ToolResult{Status: "success"}
}
