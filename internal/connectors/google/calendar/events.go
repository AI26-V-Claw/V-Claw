package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vclaw/internal/connectors/google/common"
)

// maxListEventsPages caps the number of result pages fetched in one ListEvents
// call. It guards against an unbounded loop while still covering far more events
// than any normal time-range query returns.
const maxListEventsPages = 20

const conferenceWaitTimeout = 15 * time.Second

// ListEvents retrieves events from the primary calendar within a time range.
// It also applies a search query if one is provided. The Calendar API paginates
// large result sets, so this follows NextPageToken across pages to avoid
// silently dropping events beyond the first page.
func (c *Client) ListEvents(ctx context.Context, timeMin, timeMax time.Time, query string) ([]Event, error) {
	var events []Event
	pageToken := ""
	for page := 0; page < maxListEventsPages; page++ {
		req := c.srv.Events.List("primary").
			TimeMin(timeMin.Format(time.RFC3339)).
			TimeMax(timeMax.Format(time.RFC3339)).
			SingleEvents(true).
			OrderBy("startTime")
		if query != "" {
			req = req.Q(query)
		}
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}

		res, err := req.Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("calendar: list events: %w", common.MapError(err))
		}

		for _, item := range res.Items {
			events = append(events, toDomainEvent(item))
		}

		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
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
	req := c.srv.Events.Insert("primary", gEvent).Context(ctx)
	if e.CreateConference {
		req = req.ConferenceDataVersion(1)
	}
	res, err := req.Do()
	if err != nil {
		return Event{}, fmt.Errorf("calendar: create event: %w", common.MapError(err))
	}
	event := toDomainEvent(res)
	if e.CreateConference {
		return c.waitForConference(ctx, event)
	}
	return event, nil
}

// UpdateEvent updates an existing event using PATCH semantic.
func (c *Client) UpdateEvent(ctx context.Context, eventID string, e Event) (Event, error) {
	gEvent := toGoogleEvent(e)
	req := c.srv.Events.Patch("primary", eventID, gEvent).Context(ctx)
	if e.CreateConference {
		req = req.ConferenceDataVersion(1)
	}
	res, err := req.Do()
	if err != nil {
		return Event{}, fmt.Errorf("calendar: update event: %w", common.MapError(err))
	}
	event := toDomainEvent(res)
	if e.CreateConference {
		return c.waitForConference(ctx, event)
	}
	return event, nil
}

func (c *Client) waitForConference(ctx context.Context, event Event) (Event, error) {
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.MeetLink) != "" {
		return event, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, conferenceWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	latest := event
	for {
		select {
		case <-waitCtx.Done():
			return latest, nil
		case <-ticker.C:
			refreshed, err := c.GetEvent(waitCtx, event.ID)
			if err != nil {
				return latest, err
			}
			latest = refreshed
			if strings.TrimSpace(refreshed.MeetLink) != "" {
				return refreshed, nil
			}
			if strings.EqualFold(strings.TrimSpace(refreshed.ConferenceStatus), "failure") {
				return refreshed, fmt.Errorf("calendar: create meet conference: %w", common.ErrAPI)
			}
		}
	}
}

// RespondEvent updates the response status for an attendee on an event.
// If email is empty, it tries to update the attendee marked as self.
func (c *Client) RespondEvent(ctx context.Context, eventID string, email string, responseStatus string) (Event, error) {
	current, err := c.GetEvent(ctx, eventID)
	if err != nil {
		return Event{}, err
	}

	email = strings.ToLower(strings.TrimSpace(email))
	target := -1
	for i, attendee := range current.Attendees {
		if email != "" && strings.EqualFold(strings.TrimSpace(attendee.Email), email) {
			target = i
			break
		}
		if email == "" && attendee.Self {
			target = i
			break
		}
	}
	if target < 0 {
		return Event{}, fmt.Errorf("calendar: respond event: %w", common.ErrNotFound)
	}

	current.Attendees[target].ResponseStatus = responseStatus
	return c.UpdateEvent(ctx, eventID, Event{Attendees: current.Attendees})
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
