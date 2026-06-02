package calendar

import (
	"time"

	gcal "google.golang.org/api/calendar/v3"
)

// Event represents a domain calendar event used by the tools layer.
type Event struct {
	ID          string
	Title       string
	Description string
	Location    string
	StartTime   time.Time
	EndTime     time.Time
	Attendees   []Attendee
	MeetLink    string
	IsRecurring bool
}

// Attendee represents a participant in a calendar event.
type Attendee struct {
	Email          string
	ResponseStatus string // e.g., "needsAction", "declined", "tentative", "accepted"
}

// toDomainEvent maps a Google Calendar API event to our domain Event type.
func toDomainEvent(gEvent *gcal.Event) Event {
	if gEvent == nil {
		return Event{}
	}

	var startTime, endTime time.Time

	if gEvent.Start != nil {
		if gEvent.Start.DateTime != "" {
			startTime, _ = time.Parse(time.RFC3339, gEvent.Start.DateTime)
		} else if gEvent.Start.Date != "" {
			startTime, _ = time.Parse("2006-01-02", gEvent.Start.Date)
		}
	}

	if gEvent.End != nil {
		if gEvent.End.DateTime != "" {
			endTime, _ = time.Parse(time.RFC3339, gEvent.End.DateTime)
		} else if gEvent.End.Date != "" {
			endTime, _ = time.Parse("2006-01-02", gEvent.End.Date)
		}
	}

	var attendees []Attendee
	for _, a := range gEvent.Attendees {
		if a != nil {
			attendees = append(attendees, Attendee{
				Email:          a.Email,
				ResponseStatus: a.ResponseStatus,
			})
		}
	}

	return Event{
		ID:          gEvent.Id,
		Title:       gEvent.Summary,
		Description: gEvent.Description,
		Location:    gEvent.Location,
		StartTime:   startTime,
		EndTime:     endTime,
		Attendees:   attendees,
		MeetLink:    gEvent.HangoutLink,
		IsRecurring: len(gEvent.Recurrence) > 0 || gEvent.RecurringEventId != "",
	}
}

// toGoogleEvent maps our domain Event type to a Google Calendar API event.
func toGoogleEvent(e Event) *gcal.Event {
	gEvent := &gcal.Event{
		Id:          e.ID,
		Summary:     e.Title,
		Description: e.Description,
		Location:    e.Location,
	}

	if !e.StartTime.IsZero() {
		gEvent.Start = &gcal.EventDateTime{
			DateTime: e.StartTime.Format(time.RFC3339),
		}
	}
	if !e.EndTime.IsZero() {
		gEvent.End = &gcal.EventDateTime{
			DateTime: e.EndTime.Format(time.RFC3339),
		}
	}

	for _, a := range e.Attendees {
		gEvent.Attendees = append(gEvent.Attendees, &gcal.EventAttendee{
			Email:          a.Email,
			ResponseStatus: a.ResponseStatus,
		})
	}

	return gEvent
}
