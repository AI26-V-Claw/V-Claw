package calendar

import (
	"context"
	"errors"
	"time"

	gcal "vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/common"
)

// ToolResult represents the uniform result returned to the Agent Orchestrator.
type ToolResult struct {
	Status string // "success", "conflict", "not_found", "disambiguation_needed", "recurring_options", "auth_error", "api_error"
	Data   any
	Error  string
}

// ConnectorClient defines the interface for interacting with the Google Calendar API connector.
type ConnectorClient interface {
	ListEvents(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error)
	GetEvent(ctx context.Context, eventID string) (gcal.Event, error)
	CreateEvent(ctx context.Context, e gcal.Event) (gcal.Event, error)
	UpdateEvent(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error)
	DeleteEvent(ctx context.Context, eventID string) error
}

// Tool implements business logic for calendar operations.
type Tool struct {
	client ConnectorClient
}

// NewTool creates a new Calendar Tool.
func NewTool(client ConnectorClient) *Tool {
	return &Tool{client: client}
}

// executeWithRetry runs a function and retries exactly once if a 5xx API error occurs.
func executeWithRetry[T any](f func() (T, error)) (T, error) {
	res, err := f()
	if err != nil && errors.Is(err, common.ErrAPI) {
		time.Sleep(1 * time.Second)
		return f()
	}
	return res, err
}

// executeWithRetryVoid runs a function returning only error and retries exactly once if a 5xx API error occurs.
func executeWithRetryVoid(f func() error) error {
	err := f()
	if err != nil && errors.Is(err, common.ErrAPI) {
		time.Sleep(1 * time.Second)
		return f()
	}
	return err
}

// mapErrorToResult maps a connector error to a ToolResult.
func mapErrorToResult(err error) ToolResult {
	if errors.Is(err, common.ErrAuth) {
		return ToolResult{Status: "auth_error", Error: err.Error()}
	}
	if errors.Is(err, common.ErrNotFound) {
		return ToolResult{Status: "not_found", Error: err.Error()}
	}
	// ErrRateLimit (429) or ErrAPI (500) both map to api_error in the tool status.
	if errors.Is(err, common.ErrRateLimit) || errors.Is(err, common.ErrAPI) {
		return ToolResult{Status: "api_error", Error: err.Error()}
	}
	// Fallback for any unknown errors
	return ToolResult{Status: "api_error", Error: err.Error()}
}

// ListEvents returns a list of events for a given time range.
func (t *Tool) ListEvents(ctx context.Context, timeMin, timeMax time.Time) ToolResult {
	events, err := executeWithRetry(func() ([]gcal.Event, error) {
		return t.client.ListEvents(ctx, timeMin, timeMax, "")
	})
	if err != nil {
		return mapErrorToResult(err)
	}
	return ToolResult{Status: "success", Data: events}
}

// CreateEvent handles the creation of a new event, including retry logic.
func (t *Tool) CreateEvent(ctx context.Context, e gcal.Event) ToolResult {
	res, err := executeWithRetry(func() (gcal.Event, error) {
		return t.client.CreateEvent(ctx, e)
	})
	if err != nil {
		return mapErrorToResult(err)
	}
	return ToolResult{Status: "success", Data: res}
}

// UpdateEvent updates an event. If it's a recurring event and scope is missing, it returns recurring_options.
func (t *Tool) UpdateEvent(ctx context.Context, eventID string, e gcal.Event, scope string) ToolResult {
	if scope == "" {
		ev, err := executeWithRetry(func() (gcal.Event, error) {
			return t.client.GetEvent(ctx, eventID)
		})
		if err != nil {
			return mapErrorToResult(err)
		}
		if ev.IsRecurring {
			return ToolResult{Status: "recurring_options", Data: ev}
		}
	}

	res, err := executeWithRetry(func() (gcal.Event, error) {
		return t.client.UpdateEvent(ctx, eventID, e)
	})
	if err != nil {
		return mapErrorToResult(err)
	}
	return ToolResult{Status: "success", Data: res}
}

// DeleteEvent deletes an event. If it's a recurring event and scope is missing, it returns recurring_options.
func (t *Tool) DeleteEvent(ctx context.Context, eventID string, scope string) ToolResult {
	if scope == "" {
		ev, err := executeWithRetry(func() (gcal.Event, error) {
			return t.client.GetEvent(ctx, eventID)
		})
		if err != nil {
			return mapErrorToResult(err)
		}
		if ev.IsRecurring {
			return ToolResult{Status: "recurring_options", Data: ev}
		}
	}

	err := executeWithRetryVoid(func() error {
		return t.client.DeleteEvent(ctx, eventID)
	})
	if err != nil {
		return mapErrorToResult(err)
	}
	return ToolResult{Status: "success"}
}
