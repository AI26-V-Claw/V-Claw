package calendar

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	gcal "vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/common"
	"vclaw/internal/tools"

	"google.golang.org/api/googleapi"
)

// --- Mock connector ---

type mockConnector struct {
	listEventsFunc   func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error)
	getEventFunc     func(ctx context.Context, eventID string) (gcal.Event, error)
	createEventFunc  func(ctx context.Context, e gcal.Event) (gcal.Event, error)
	updateEventFunc  func(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error)
	respondEventFunc func(ctx context.Context, eventID string, email string, responseStatus string) (gcal.Event, error)
	deleteEventFunc  func(ctx context.Context, eventID string) error
}

func TestCreateEventToolDescribesExplicitStartAndEndRequirements(t *testing.T) {
	tool := &CreateEventTool{service: NewService(&mockConnector{})}
	if !strings.Contains(tool.Description(), "explicit start date+time") || !strings.Contains(tool.Description(), "date-only") {
		t.Fatalf("description should require explicit start time and reject date-only inference: %q", tool.Description())
	}
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties in schema, got %#v", params["properties"])
	}
	start, ok := props["start"].(map[string]any)
	if !ok {
		t.Fatalf("expected start schema, got %#v", props["start"])
	}
	end, ok := props["end"].(map[string]any)
	if !ok {
		t.Fatalf("expected end schema, got %#v", props["end"])
	}
	if !strings.Contains(fmt.Sprint(start["description"]), "explicit time of day") {
		t.Fatalf("start description should require explicit time of day, got %#v", start["description"])
	}
	if !strings.Contains(fmt.Sprint(end["description"]), "end time or duration") {
		t.Fatalf("end description should require explicit end/duration, got %#v", end["description"])
	}
}

func (m *mockConnector) ListEvents(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
	if m.listEventsFunc != nil {
		return m.listEventsFunc(ctx, timeMin, timeMax, query)
	}
	return nil, nil
}

func (m *mockConnector) GetEvent(ctx context.Context, eventID string) (gcal.Event, error) {
	if m.getEventFunc != nil {
		return m.getEventFunc(ctx, eventID)
	}
	return gcal.Event{}, nil
}

func (m *mockConnector) CreateEvent(ctx context.Context, e gcal.Event) (gcal.Event, error) {
	if m.createEventFunc != nil {
		return m.createEventFunc(ctx, e)
	}
	return gcal.Event{}, nil
}

func (m *mockConnector) UpdateEvent(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error) {
	if m.updateEventFunc != nil {
		return m.updateEventFunc(ctx, eventID, e)
	}
	return gcal.Event{}, nil
}

func (m *mockConnector) RespondEvent(ctx context.Context, eventID string, email string, responseStatus string) (gcal.Event, error) {
	if m.respondEventFunc != nil {
		return m.respondEventFunc(ctx, eventID, email, responseStatus)
	}
	return gcal.Event{}, nil
}

func (m *mockConnector) DeleteEvent(ctx context.Context, eventID string) error {
	if m.deleteEventFunc != nil {
		return m.deleteEventFunc(ctx, eventID)
	}
	return nil
}

// --- Service tests ---

func TestListEvents_Success(t *testing.T) {
	mock := &mockConnector{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{
				{ID: "1", Title: "Meeting 1"},
				{ID: "2", Title: "Meeting 2"},
			}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "2026-05-29T09:00:00+07:00",
		TimeMax: "2026-05-30T09:00:00+07:00",
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if len(output.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(output.Events))
	}
	if output.Events[0].ID != "1" || output.Events[1].Title != "Meeting 2" {
		t.Errorf("unexpected event data: %+v", output.Events)
	}
}

func TestListEvents_Empty(t *testing.T) {
	mock := &mockConnector{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "2026-05-29T09:00:00+07:00",
		TimeMax: "2026-05-30T09:00:00+07:00",
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if len(output.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(output.Events))
	}
}

func TestListEvents_InvalidTimeMin(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "invalid",
		TimeMax: "2026-05-30T09:00:00+07:00",
	})

	if errShape == nil {
		t.Fatal("expected error for invalid timeMin")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestListEvents_InvalidTimeMax(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "2026-05-29T09:00:00+07:00",
		TimeMax: "bad-time",
	})

	if errShape == nil {
		t.Fatal("expected error for invalid timeMax")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestListEvents_PassesQuery(t *testing.T) {
	var capturedQuery string
	mock := &mockConnector{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			capturedQuery = query
			return nil, nil
		},
	}
	svc := NewService(mock)

	svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "2026-05-29T09:00:00+07:00",
		TimeMax: "2026-05-30T09:00:00+07:00",
		Query:   "standup",
	})

	if capturedQuery != "standup" {
		t.Errorf("expected query 'standup', got %q", capturedQuery)
	}
}

func TestListEvents_DropsDateOnlyQuery(t *testing.T) {
	var capturedQuery string
	mock := &mockConnector{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			capturedQuery = query
			return nil, nil
		},
	}
	svc := NewService(mock)

	_, errShape := svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "2026-06-01T00:00:00+07:00",
		TimeMax: "2026-06-08T00:00:00+07:00",
		Query:   "trong tuần này tôi có lịch gì không",
	})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if capturedQuery != "" {
		t.Fatalf("expected date-only query to be dropped, got %q", capturedQuery)
	}
}

func TestListEvents_KeepsSpecificSearchQuery(t *testing.T) {
	var capturedQuery string
	mock := &mockConnector{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			capturedQuery = query
			return nil, nil
		},
	}
	svc := NewService(mock)

	_, errShape := svc.ListEvents(context.Background(), ListEventsInput{
		TimeMin: "2026-06-01T00:00:00+07:00",
		TimeMax: "2026-06-08T00:00:00+07:00",
		Query:   "standup",
	})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if capturedQuery != "standup" {
		t.Fatalf("expected search query to be kept, got %q", capturedQuery)
	}
}

func TestGetEvent_Success(t *testing.T) {
	mock := &mockConnector{
		getEventFunc: func(ctx context.Context, eventID string) (gcal.Event, error) {
			if eventID != "event_001" {
				t.Fatalf("unexpected eventID: %s", eventID)
			}
			return gcal.Event{
				ID:    "event_001",
				Title: "Project review",
				Organizer: gcal.Person{
					Email:       "organizer@example.com",
					DisplayName: "Organizer",
				},
				Creator: gcal.Person{
					Email:       "creator@example.com",
					DisplayName: "Creator",
				},
				Attendees: []gcal.Attendee{
					{Email: "alice@example.com", DisplayName: "Alice", ResponseStatus: "accepted"},
				},
			}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.GetEvent(context.Background(), GetEventInput{EventID: "event_001"})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if output.Event.Organizer.Email != "organizer@example.com" {
		t.Fatalf("organizer missing: %+v", output.Event.Organizer)
	}
	if len(output.Event.Attendees) != 1 || output.Event.Attendees[0].DisplayName != "Alice" {
		t.Fatalf("attendees missing display name: %+v", output.Event.Attendees)
	}
}

func TestGetEvent_MissingEventID(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.GetEvent(context.Background(), GetEventInput{})
	if errShape == nil {
		t.Fatal("expected error for missing eventId")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestCreateEvent_Success(t *testing.T) {
	mock := &mockConnector{
		createEventFunc: func(ctx context.Context, e gcal.Event) (gcal.Event, error) {
			return gcal.Event{
				ID:    "new_id",
				Title: e.Title,
				Attendees: []gcal.Attendee{
					{Email: "minh@example.com"},
				},
			}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.CreateEvent(context.Background(), CreateEventInput{
		Title:     "Họp với anh Minh",
		Start:     "2026-05-30T10:00:00+07:00",
		End:       "2026-05-30T11:00:00+07:00",
		Attendees: []string{"minh@example.com"},
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if output.EventID != "new_id" {
		t.Errorf("expected event ID 'new_id', got %q", output.EventID)
	}
	if output.Event.Title != "Họp với anh Minh" {
		t.Errorf("unexpected title: %s", output.Event.Title)
	}
}

func TestCreateEvent_MissingTitle(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.CreateEvent(context.Background(), CreateEventInput{
		Start: "2026-05-30T10:00:00+07:00",
		End:   "2026-05-30T11:00:00+07:00",
	})

	if errShape == nil {
		t.Fatal("expected error for missing title")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestCreateEvent_InvalidStart(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.CreateEvent(context.Background(), CreateEventInput{
		Title: "Test",
		Start: "not-a-date",
		End:   "2026-05-30T11:00:00+07:00",
	})

	if errShape == nil {
		t.Fatal("expected error for invalid start")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestUpdateEvent_Success(t *testing.T) {
	mock := &mockConnector{
		updateEventFunc: func(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error) {
			return gcal.Event{
				ID:    eventID,
				Title: e.Title,
			}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.UpdateEvent(context.Background(), UpdateEventInput{
		EventID: "event_001",
		Title:   "Updated Title",
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if output.Event.ID != "event_001" {
		t.Errorf("expected event ID 'event_001', got %q", output.Event.ID)
	}
	if output.Event.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", output.Event.Title)
	}
}

func TestUpdateEvent_AddsAttendeesPreservingExistingResponseStatus(t *testing.T) {
	getCalled := false
	updateCalled := false
	mock := &mockConnector{
		getEventFunc: func(ctx context.Context, eventID string) (gcal.Event, error) {
			getCalled = true
			if eventID != "event_001" {
				t.Fatalf("unexpected get eventID: %s", eventID)
			}
			return gcal.Event{
				ID: "event_001",
				Attendees: []gcal.Attendee{
					{Email: "quanghtd@vclaw.site", DisplayName: "Quang", ResponseStatus: "accepted", Self: true},
					{Email: "old@example.com", ResponseStatus: "needsAction"},
				},
			}, nil
		},
		updateEventFunc: func(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error) {
			updateCalled = true
			if len(e.Attendees) != 3 {
				t.Fatalf("expected 3 merged attendees, got %+v", e.Attendees)
			}
			if e.Attendees[0].Email != "quanghtd@vclaw.site" || e.Attendees[0].ResponseStatus != "accepted" {
				t.Fatalf("existing RSVP was not preserved: %+v", e.Attendees[0])
			}
			if e.Attendees[2].Email != "new@example.com" || e.Attendees[2].ResponseStatus != "" {
				t.Fatalf("new attendee should be appended without overwriting RSVP state: %+v", e.Attendees[2])
			}
			return gcal.Event{
				ID:        eventID,
				Attendees: e.Attendees,
			}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.UpdateEvent(context.Background(), UpdateEventInput{
		EventID:   "event_001",
		Attendees: []string{"new@example.com"},
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if !getCalled || !updateCalled {
		t.Fatalf("expected get and update to be called, get=%t update=%t", getCalled, updateCalled)
	}
	if len(output.Event.Attendees) != 3 || output.Event.Attendees[0].ResponseStatus != "accepted" {
		t.Fatalf("unexpected output attendees: %+v", output.Event.Attendees)
	}
}

func TestUpdateEvent_AddAttendeesSkipsExistingEmailCaseInsensitive(t *testing.T) {
	mock := &mockConnector{
		getEventFunc: func(ctx context.Context, eventID string) (gcal.Event, error) {
			return gcal.Event{
				ID: "event_001",
				Attendees: []gcal.Attendee{
					{Email: "New@Example.com", ResponseStatus: "accepted"},
				},
			}, nil
		},
		updateEventFunc: func(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error) {
			if len(e.Attendees) != 1 {
				t.Fatalf("duplicate attendee should not be appended: %+v", e.Attendees)
			}
			if e.Attendees[0].ResponseStatus != "accepted" {
				t.Fatalf("existing attendee state should be preserved: %+v", e.Attendees[0])
			}
			return gcal.Event{ID: eventID, Attendees: e.Attendees}, nil
		},
	}
	svc := NewService(mock)

	_, errShape := svc.UpdateEvent(context.Background(), UpdateEventInput{
		EventID:   "event_001",
		Attendees: []string{"new@example.com"},
	})
	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
}

func TestUpdateEvent_MissingEventID(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.UpdateEvent(context.Background(), UpdateEventInput{
		Title: "Updated",
	})

	if errShape == nil {
		t.Fatal("expected error for missing eventId")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestRespondEvent_Success(t *testing.T) {
	mock := &mockConnector{
		respondEventFunc: func(ctx context.Context, eventID string, email string, responseStatus string) (gcal.Event, error) {
			if eventID != "event_001" || email != "quanghtd@vclaw.site" || responseStatus != "accepted" {
				t.Fatalf("unexpected respond args: eventID=%q email=%q status=%q", eventID, email, responseStatus)
			}
			return gcal.Event{
				ID:    eventID,
				Title: "N1 Long-term Test",
				Attendees: []gcal.Attendee{
					{Email: email, ResponseStatus: responseStatus},
				},
			}, nil
		},
	}
	svc := NewService(mock)

	output, errShape := svc.RespondEvent(context.Background(), RespondEventInput{
		EventID:        "event_001",
		Email:          "quanghtd@vclaw.site",
		ResponseStatus: "ACCEPTED",
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if len(output.Event.Attendees) != 1 || output.Event.Attendees[0].ResponseStatus != "accepted" {
		t.Fatalf("unexpected RSVP output: %+v", output.Event.Attendees)
	}
}

func TestRespondEvent_InvalidStatus(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.RespondEvent(context.Background(), RespondEventInput{
		EventID:        "event_001",
		ResponseStatus: "yes",
	})
	if errShape == nil {
		t.Fatal("expected error for invalid responseStatus")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestRespondEvent_MissingEventID(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.RespondEvent(context.Background(), RespondEventInput{ResponseStatus: "accepted"})
	if errShape == nil {
		t.Fatal("expected error for missing eventId")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

func TestDeleteEvent_Success(t *testing.T) {
	deleted := false
	mock := &mockConnector{
		deleteEventFunc: func(ctx context.Context, eventID string) error {
			deleted = true
			return nil
		},
	}
	svc := NewService(mock)

	_, errShape := svc.DeleteEvent(context.Background(), DeleteEventInput{
		EventID: "event_001",
	})

	if errShape != nil {
		t.Fatalf("unexpected error: %s", errShape.Error())
	}
	if !deleted {
		t.Error("expected delete to be called")
	}
}

func TestDeleteEvent_MissingEventID(t *testing.T) {
	svc := NewService(&mockConnector{})

	_, errShape := svc.DeleteEvent(context.Background(), DeleteEventInput{})

	if errShape == nil {
		t.Fatal("expected error for missing eventId")
	}
	if errShape.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT, got %s", errShape.Code)
	}
}

// --- Error mapping tests ---

func TestMapConnectorError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode string
		retryable    bool
	}{
		{"Auth error", common.ErrAuth, "AUTH_EXPIRED", true},
		{"Not found", common.ErrNotFound, "RESOURCE_NOT_FOUND", false},
		{"Rate limit", common.ErrRateLimit, "RATE_LIMITED", true},
		{"API error", common.ErrAPI, "PROVIDER_UNAVAILABLE", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapConnectorError(tt.err)
			if result.Code != tt.expectedCode {
				t.Errorf("expected code %s, got %s", tt.expectedCode, result.Code)
			}
			if result.Retryable != tt.retryable {
				t.Errorf("expected retryable %v, got %v", tt.retryable, result.Retryable)
			}
		})
	}
}

func TestMapConnectorErrorWrappedGoogleError(t *testing.T) {
	errShape := mapConnectorError(fmt.Errorf("calendar list failed: %w", &googleapi.Error{
		Code:    403,
		Message: "Request had insufficient authentication scopes.",
	}))
	if errShape == nil {
		t.Fatal("expected error shape")
	}
	if errShape.Code != "AUTH_MISSING_SCOPE" {
		t.Fatalf("expected AUTH_MISSING_SCOPE, got %#v", errShape)
	}
	if errShape.Message != "Request had insufficient authentication scopes." {
		t.Fatalf("unexpected message: %q", errShape.Message)
	}
}

func TestNilService(t *testing.T) {
	var svc *Service

	_, errShape := svc.ListEvents(context.Background(), ListEventsInput{})
	if errShape == nil || errShape.Code != "INTERNAL_ERROR" {
		t.Error("expected INTERNAL_ERROR for nil service")
	}
}

// --- Adapter tests ---

func TestListEventsTool_Execute(t *testing.T) {
	mock := &mockConnector{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{{
				ID:        "1",
				Title:     "Test",
				EventLink: "https://calendar.google.com/calendar/event?eid=list_event_1",
			}}, nil
		},
	}
	svc := NewService(mock)
	tool := &ListEventsTool{service: svc}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_001",
		Name: ToolNameListEvents,
		Arguments: map[string]any{
			"timeMin": "2026-05-29T09:00:00+07:00",
			"timeMax": "2026-05-30T09:00:00+07:00",
		},
	})

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if result.ToolCallID != "tc_001" {
		t.Errorf("expected ToolCallID 'tc_001', got %q", result.ToolCallID)
	}
	if result.ToolName != ToolNameListEvents {
		t.Errorf("expected ToolName %q, got %q", ToolNameListEvents, result.ToolName)
	}
	if !strings.Contains(result.ContentForUser, "\"eventLink\":\"https://calendar.google.com/calendar/event?eid=list_event_1\"") {
		t.Fatalf("expected list events output to include event link, got %q", result.ContentForUser)
	}
}

func TestGetEventTool_Execute(t *testing.T) {
	mock := &mockConnector{
		getEventFunc: func(ctx context.Context, eventID string) (gcal.Event, error) {
			return gcal.Event{
				ID:        eventID,
				Title:     "Project review",
				EventLink: "https://calendar.google.com/calendar/event?eid=get_event_1",
				Organizer: gcal.Person{
					Email:       "organizer@example.com",
					DisplayName: "Organizer",
				},
				Attendees: []gcal.Attendee{
					{Email: "alice@example.com", DisplayName: "Alice", ResponseStatus: "accepted"},
				},
			}, nil
		},
	}
	svc := NewService(mock)
	tool := &GetEventTool{service: svc}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_get_event",
		Name: ToolNameGetEvent,
		Arguments: map[string]any{
			"eventId": "event_001",
		},
	})

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForUser, "\"organizer\":{\"email\":\"organizer@example.com\"") {
		t.Fatalf("expected organizer in output, got %q", result.ContentForUser)
	}
	if !strings.Contains(result.ContentForUser, "\"displayName\":\"Alice\"") {
		t.Fatalf("expected attendee display name in output, got %q", result.ContentForUser)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.URI != "https://calendar.google.com/calendar/event?eid=get_event_1" {
		t.Fatalf("expected artifact ref URI for event, got %#v", result.ArtifactRef)
	}
}

func TestCreateEventTool_Execute(t *testing.T) {
	mock := &mockConnector{
		createEventFunc: func(ctx context.Context, e gcal.Event) (gcal.Event, error) {
			return gcal.Event{
				ID:        "new",
				Title:     e.Title,
				EventLink: "https://calendar.google.com/calendar/event?eid=create_event_new",
			}, nil
		},
	}
	svc := NewService(mock)
	tool := &CreateEventTool{service: svc}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_002",
		Name: ToolNameCreateEvent,
		Arguments: map[string]any{
			"title":     "Team standup",
			"start":     "2026-05-30T10:00:00+07:00",
			"end":       "2026-05-30T10:30:00+07:00",
			"attendees": []any{"a@test.com", "b@test.com"},
			"location":  "Room A",
		},
	})

	if !result.Success {
		t.Errorf("expected success, got error: %v", result.Error)
	}
	if result.ToolCallID != "tc_002" {
		t.Errorf("expected ToolCallID 'tc_002', got %q", result.ToolCallID)
	}
	if !strings.Contains(result.ContentForUser, "\"eventLink\":\"https://calendar.google.com/calendar/event?eid=create_event_new\"") {
		t.Fatalf("expected create event output to include event link, got %q", result.ContentForUser)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.URI != "https://calendar.google.com/calendar/event?eid=create_event_new" {
		t.Fatalf("expected artifact ref URI for created event, got %#v", result.ArtifactRef)
	}
}

func TestCreateEventToolRejectsInvalidAttendeeEmail(t *testing.T) {
	called := false
	mock := &mockConnector{
		createEventFunc: func(ctx context.Context, e gcal.Event) (gcal.Event, error) {
			called = true
			return gcal.Event{}, nil
		},
	}
	svc := NewService(mock)
	tool := &CreateEventTool{service: svc}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_invalid_attendee",
		Name: ToolNameCreateEvent,
		Arguments: map[string]any{
			"title":     "Team standup",
			"start":     "2026-05-30T10:00:00+07:00",
			"end":       "2026-05-30T10:30:00+07:00",
			"attendees": []any{"Bao"},
		},
	})

	if result.Success {
		t.Fatal("expected invalid attendee to fail")
	}
	if result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %#v", result.Error)
	}
	if called {
		t.Fatal("connector should not be called for invalid attendee email")
	}
}

func TestRespondEventTool_Execute(t *testing.T) {
	mock := &mockConnector{
		respondEventFunc: func(ctx context.Context, eventID string, email string, responseStatus string) (gcal.Event, error) {
			return gcal.Event{
				ID:        eventID,
				Title:     "N1 Long-term Test",
				EventLink: "https://calendar.google.com/calendar/event?eid=respond_event_1",
				Attendees: []gcal.Attendee{
					{Email: email, ResponseStatus: responseStatus},
				},
			}, nil
		},
	}
	svc := NewService(mock)
	tool := &RespondEventTool{service: svc}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_respond_event",
		Name: ToolNameRespondEvent,
		Arguments: map[string]any{
			"eventId":        "event_001",
			"email":          "quanghtd@vclaw.site",
			"responseStatus": "accepted",
		},
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if !strings.Contains(result.ContentForUser, "\"responseStatus\":\"accepted\"") {
		t.Fatalf("expected responseStatus in output, got %q", result.ContentForUser)
	}
	if result.ArtifactRef == nil || result.ArtifactRef.URI != "https://calendar.google.com/calendar/event?eid=respond_event_1" {
		t.Fatalf("expected artifact ref URI for responded event, got %#v", result.ArtifactRef)
	}
}

func TestDeleteEventTool_Execute_Error(t *testing.T) {
	mock := &mockConnector{
		deleteEventFunc: func(ctx context.Context, eventID string) error {
			return common.ErrNotFound
		},
	}
	svc := NewService(mock)
	tool := &DeleteEventTool{service: svc}

	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:   "tc_003",
		Name: ToolNameDeleteEvent,
		Arguments: map[string]any{
			"eventId": "nonexistent",
		},
	})

	if result.Success {
		t.Error("expected failure for not found")
	}
	if result.Error == nil {
		t.Fatal("expected ToolError to be set")
	}
	if result.Error.Code != "RESOURCE_NOT_FOUND" {
		t.Errorf("expected RESOURCE_NOT_FOUND, got %s", result.Error.Code)
	}
}

func TestAdapter_MissingArguments(t *testing.T) {
	svc := NewService(&mockConnector{})
	tool := &CreateEventTool{service: svc}

	// Missing title → should return INVALID_INPUT
	result := tool.Execute(context.Background(), tools.ToolCall{
		ID:        "tc_004",
		Name:      ToolNameCreateEvent,
		Arguments: map[string]any{},
	})

	if result.Success {
		t.Error("expected failure for missing arguments")
	}
	if result.Error == nil || result.Error.Code != "INVALID_INPUT" {
		t.Errorf("expected INVALID_INPUT error, got %v", result.Error)
	}
}

// --- Registry integration test ---

func TestRegisterTools(t *testing.T) {
	registry := tools.NewToolRegistry()
	svc := NewService(&mockConnector{})

	err := RegisterTools(registry, svc)
	if err != nil {
		t.Fatalf("failed to register tools: %v", err)
	}

	expectedTools := []string{
		ToolNameListEvents,
		ToolNameGetEvent,
		ToolNameCreateEvent,
		ToolNameUpdateEvent,
		ToolNameRespondEvent,
		ToolNameDeleteEvent,
	}

	for _, name := range expectedTools {
		tool, ok := registry.GetTool(name)
		if !ok {
			t.Errorf("tool %q not found in registry", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("expected tool name %q, got %q", name, tool.Name())
		}
	}

	defs := registry.ListTools()
	if len(defs) != 6 {
		t.Errorf("expected 6 tools in registry, got %d", len(defs))
	}
}

func TestRegisterTools_DuplicateFails(t *testing.T) {
	registry := tools.NewToolRegistry()
	svc := NewService(&mockConnector{})

	if err := RegisterTools(registry, svc); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	err := RegisterTools(registry, svc)
	if err == nil {
		t.Error("expected error when registering duplicate tools")
	}
}

// --- Argument helper tests ---

func TestStringArg(t *testing.T) {
	args := map[string]any{"key": "value", "num": 42}

	if v := stringArg(args, "key"); v != "value" {
		t.Errorf("expected 'value', got %q", v)
	}
	if v := stringArg(args, "missing"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
	if v := stringArg(args, "num"); v != "" {
		t.Errorf("expected empty for non-string, got %q", v)
	}
}

func TestStringSliceArg(t *testing.T) {
	args := map[string]any{
		"emails": []any{"a@test.com", "b@test.com"},
		"mixed":  []any{"valid", 42},
		"str":    "not-a-slice",
	}

	emails := stringSliceArg(args, "emails")
	if len(emails) != 2 || emails[0] != "a@test.com" {
		t.Errorf("unexpected emails: %v", emails)
	}

	mixed := stringSliceArg(args, "mixed")
	if len(mixed) != 1 || mixed[0] != "valid" {
		t.Errorf("expected only string values, got: %v", mixed)
	}

	if v := stringSliceArg(args, "str"); v != nil {
		t.Errorf("expected nil for non-slice, got %v", v)
	}

	if v := stringSliceArg(args, "missing"); v != nil {
		t.Errorf("expected nil for missing key, got %v", v)
	}
}

// --- toEventSummary test ---

func TestToEventSummary(t *testing.T) {
	now := time.Date(2026, 5, 30, 10, 0, 0, 0, time.FixedZone("ICT", 7*3600))

	event := gcal.Event{
		ID:          "ev_001",
		Title:       "Team sync",
		Description: "Weekly standup",
		Location:    "Room B",
		StartTime:   now,
		EndTime:     now.Add(30 * time.Minute),
		Attendees: []gcal.Attendee{
			{Email: "alice@example.com", DisplayName: "Alice", ResponseStatus: "accepted"},
			{Email: "bob@example.com", ResponseStatus: "needsAction"},
		},
		Organizer:   gcal.Person{Email: "organizer@example.com", DisplayName: "Organizer"},
		Creator:     gcal.Person{Email: "creator@example.com", DisplayName: "Creator"},
		EventLink:   "https://calendar.google.com/calendar/event?eid=summary_event_1",
		MeetLink:    "https://meet.google.com/abc-def",
		IsRecurring: true,
	}

	summary := toEventSummary(event)

	if summary.ID != "ev_001" {
		t.Errorf("expected ID 'ev_001', got %q", summary.ID)
	}
	if summary.Title != "Team sync" {
		t.Errorf("expected title 'Team sync', got %q", summary.Title)
	}
	if len(summary.Attendees) != 2 {
		t.Errorf("expected 2 attendees, got %d", len(summary.Attendees))
	}
	if summary.Attendees[0].DisplayName != "Alice" {
		t.Errorf("expected attendee display name Alice, got %q", summary.Attendees[0].DisplayName)
	}
	if summary.Organizer.Email != "organizer@example.com" {
		t.Errorf("unexpected Organizer: %+v", summary.Organizer)
	}
	if summary.Creator.Email != "creator@example.com" {
		t.Errorf("unexpected Creator: %+v", summary.Creator)
	}
	if summary.Start == "" || summary.End == "" {
		t.Error("expected non-empty start/end times")
	}
	if !summary.IsRecurring {
		t.Error("expected IsRecurring to be true")
	}
	if summary.MeetLink != "https://meet.google.com/abc-def" {
		t.Errorf("unexpected MeetLink: %s", summary.MeetLink)
	}
	if summary.EventLink != "https://calendar.google.com/calendar/event?eid=summary_event_1" {
		t.Errorf("unexpected EventLink: %s", summary.EventLink)
	}
}
