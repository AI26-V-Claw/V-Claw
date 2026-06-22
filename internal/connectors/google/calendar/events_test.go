package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vclaw/internal/connectors/google/common"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// mockServer creates an httptest.Server and a corresponding Client.
func mockServer(handler http.HandlerFunc) (*Client, *httptest.Server) {
	ts := httptest.NewServer(handler)
	srv, _ := gcal.NewService(context.Background(), option.WithEndpoint(ts.URL), option.WithoutAuthentication())
	client := &Client{srv: srv}
	return client, ts
}

func googleErrorResponse(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
}

func TestEvents_ErrorBranches(t *testing.T) {
	tests := []struct {
		name        string
		handlerCode int
		expectedErr error
	}{
		{"Auth Error 401", http.StatusUnauthorized, common.ErrAuth},
		{"Auth Error 403", http.StatusForbidden, common.ErrAuth},
		{"Not Found 404", http.StatusNotFound, common.ErrNotFound},
		{"Rate Limit 429", http.StatusTooManyRequests, common.ErrRateLimit},
		{"API Error 500", http.StatusInternalServerError, common.ErrAPI},
	}

	for _, tt := range tests {
		t.Run("ListEvents_"+tt.name, func(t *testing.T) {
			client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
				googleErrorResponse(w, tt.handlerCode, "mock error")
			})
			defer ts.Close()

			_, err := client.ListEvents(context.Background(), time.Now(), time.Now().Add(time.Hour), "test")
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
		})

		t.Run("CreateEvent_"+tt.name, func(t *testing.T) {
			client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
				googleErrorResponse(w, tt.handlerCode, "mock error")
			})
			defer ts.Close()

			_, err := client.CreateEvent(context.Background(), Event{Title: "Test"})
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
		})

		t.Run("GetEvent_"+tt.name, func(t *testing.T) {
			client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
				googleErrorResponse(w, tt.handlerCode, "mock error")
			})
			defer ts.Close()

			_, err := client.GetEvent(context.Background(), "id1")
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
		})

		t.Run("UpdateEvent_"+tt.name, func(t *testing.T) {
			client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
				googleErrorResponse(w, tt.handlerCode, "mock error")
			})
			defer ts.Close()

			_, err := client.UpdateEvent(context.Background(), "id1", Event{Title: "Test"})
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
		})

		t.Run("DeleteEvent_"+tt.name, func(t *testing.T) {
			client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
				googleErrorResponse(w, tt.handlerCode, "mock error")
			})
			defer ts.Close()

			err := client.DeleteEvent(context.Background(), "id1")
			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected error %v, got %v", tt.expectedErr, err)
			}
		})
	}
}

func TestGetEvent_Success(t *testing.T) {
	client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(gcal.Event{
			Id:       "event_detail",
			Summary:  "Project review",
			HtmlLink: "https://calendar.google.com/calendar/event?eid=event_detail",
			Organizer: &gcal.EventOrganizer{
				Email:       "organizer@example.com",
				DisplayName: "Organizer Name",
			},
			Creator: &gcal.EventCreator{
				Email:       "creator@example.com",
				DisplayName: "Creator Name",
			},
			Attendees: []*gcal.EventAttendee{
				{Email: "alice@example.com", DisplayName: "Alice", ResponseStatus: "accepted"},
			},
		})
	})
	defer ts.Close()

	event, err := client.GetEvent(context.Background(), "event_detail")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.ID != "event_detail" || event.Title != "Project review" {
		t.Fatalf("unexpected event returned: %+v", event)
	}
	if event.Organizer.Email != "organizer@example.com" || event.Organizer.DisplayName != "Organizer Name" {
		t.Fatalf("organizer was not mapped: %+v", event.Organizer)
	}
	if event.Creator.Email != "creator@example.com" || event.Creator.DisplayName != "Creator Name" {
		t.Fatalf("creator was not mapped: %+v", event.Creator)
	}
	if len(event.Attendees) != 1 || event.Attendees[0].DisplayName != "Alice" {
		t.Fatalf("attendees were not mapped: %+v", event.Attendees)
	}
}

func TestListEvents_Success(t *testing.T) {
	client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(gcal.Events{
			Items: []*gcal.Event{
				{Id: "1", Summary: "Event 1", HtmlLink: "https://calendar.google.com/calendar/event?eid=event_1"},
				{Id: "2", Summary: "Event 2", HtmlLink: "https://calendar.google.com/calendar/event?eid=event_2"},
			},
		})
	})
	defer ts.Close()

	events, err := client.ListEvents(context.Background(), time.Now(), time.Now().Add(time.Hour), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != "1" || events[1].ID != "2" {
		t.Errorf("unexpected event IDs")
	}
	if events[0].EventLink != "https://calendar.google.com/calendar/event?eid=event_1" {
		t.Errorf("unexpected event link: %q", events[0].EventLink)
	}
}

func TestCreateEvent_Success(t *testing.T) {
	client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(gcal.Event{
			Id:       "new_id",
			Summary:  "Created Event",
			HtmlLink: "https://calendar.google.com/calendar/event?eid=created_event",
		})
	})
	defer ts.Close()

	e, err := client.CreateEvent(context.Background(), Event{Title: "Created Event"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.ID != "new_id" || e.Title != "Created Event" {
		t.Errorf("unexpected event returned: %+v", e)
	}
	if e.EventLink != "https://calendar.google.com/calendar/event?eid=created_event" {
		t.Errorf("unexpected event link: %q", e.EventLink)
	}
}

func TestUpdateEvent_Success(t *testing.T) {
	client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(gcal.Event{
			Id:       "updated_id",
			Summary:  "Updated Event",
			HtmlLink: "https://calendar.google.com/calendar/event?eid=updated_event",
		})
	})
	defer ts.Close()

	e, err := client.UpdateEvent(context.Background(), "updated_id", Event{Title: "Updated Event"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if e.ID != "updated_id" || e.Title != "Updated Event" {
		t.Errorf("unexpected event returned: %+v", e)
	}
	if e.EventLink != "https://calendar.google.com/calendar/event?eid=updated_event" {
		t.Errorf("unexpected event link: %q", e.EventLink)
	}
}

func TestRespondEvent_SuccessUsesSelfWhenEmailEmpty(t *testing.T) {
	patchCalled := false
	client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(gcal.Event{
				Id:      "respond_id",
				Summary: "RSVP Event",
				Attendees: []*gcal.EventAttendee{
					{Email: "other@example.com", ResponseStatus: "needsAction"},
					{Email: "quanghtd@vclaw.site", Self: true, ResponseStatus: "needsAction"},
				},
			})
		case http.MethodPatch:
			patchCalled = true
			var payload gcal.Event
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode patch payload: %v", err)
			}
			if len(payload.Attendees) != 2 || payload.Attendees[1].ResponseStatus != "accepted" {
				t.Fatalf("unexpected patch attendees: %+v", payload.Attendees)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(gcal.Event{
				Id:      "respond_id",
				Summary: "RSVP Event",
				Attendees: []*gcal.EventAttendee{
					{Email: "quanghtd@vclaw.site", Self: true, ResponseStatus: "accepted"},
				},
			})
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	})
	defer ts.Close()

	event, err := client.RespondEvent(context.Background(), "respond_id", "", "accepted")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !patchCalled {
		t.Fatal("expected PATCH to be called")
	}
	if len(event.Attendees) != 1 || event.Attendees[0].ResponseStatus != "accepted" {
		t.Fatalf("unexpected responded event: %+v", event)
	}
}

func TestDeleteEvent_Success(t *testing.T) {
	client, ts := mockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer ts.Close()

	err := client.DeleteEvent(context.Background(), "delete_id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
