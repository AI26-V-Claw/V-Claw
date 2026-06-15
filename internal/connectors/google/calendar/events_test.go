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
