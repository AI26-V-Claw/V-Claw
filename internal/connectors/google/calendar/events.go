package calendar

import (
	"context"
	"fmt"
	"time"

	"vclaw/internal/connectors/google/common"
)

// ListEvents retrieves events from the primary calendar within a time range.
// It also applies a search query if one is provided.
func (c *Client) ListEvents(ctx context.Context, timeMin, timeMax time.Time, query string) ([]Event, error) {
	req := c.srv.Events.List("primary").
		TimeMin(timeMin.Format(time.RFC3339)).
		TimeMax(timeMax.Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime")

	if query != "" {
		req = req.Q(query)
	}

	res, err := req.Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar: list events: %w", common.MapError(err))
	}

	var events []Event
	for _, item := range res.Items {
		events = append(events, toDomainEvent(item))
	}
	return events, nil
}

// GetEvent retrieves a single event by ID.
func (c *Client) GetEvent(ctx context.Context, eventID string) (Event, error) {
	res, err := c.srv.Events.Get("primary", eventID).Context(ctx).Do()
	if err != nil {
		return Event{}, fmt.Errorf("calendar: get event: %w", common.MapError(err))
	}
	return toDomainEvent(res), nil
}

// CreateEvent inserts a new event into the primary calendar.
func (c *Client) CreateEvent(ctx context.Context, e Event) (Event, error) {
	gEvent := toGoogleEvent(e)
	res, err := c.srv.Events.Insert("primary", gEvent).Context(ctx).Do()
	if err != nil {
		return Event{}, fmt.Errorf("calendar: create event: %w", common.MapError(err))
	}
	return toDomainEvent(res), nil
}

// UpdateEvent updates an existing event using PATCH semantic.
func (c *Client) UpdateEvent(ctx context.Context, eventID string, e Event) (Event, error) {
	gEvent := toGoogleEvent(e)
	// UpdateEvent dùng PATCH không phải PUT theo RULES
	res, err := c.srv.Events.Patch("primary", eventID, gEvent).Context(ctx).Do()
	if err != nil {
		return Event{}, fmt.Errorf("calendar: update event: %w", common.MapError(err))
	}
	return toDomainEvent(res), nil
}

// DeleteEvent removes an event from the primary calendar.
// It accepts either a master event ID (deletes all) or an instance ID (deletes single).
// Future recurring scope handling is typically done by updating the event via UpdateEvent,
// while the tool decides the appropriate API call.
func (c *Client) DeleteEvent(ctx context.Context, eventID string) error {
	err := c.srv.Events.Delete("primary", eventID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("calendar: delete event: %w", common.MapError(err))
	}
	return nil
}
