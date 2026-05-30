package calendar

import (
	"context"
	"testing"
	"time"

	gcal "vclaw/internal/connectors/google/calendar"
	"vclaw/internal/connectors/google/common"
)

type mockClient struct {
	listEventsFunc  func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error)
	getEventFunc    func(ctx context.Context, eventID string) (gcal.Event, error)
	createEventFunc func(ctx context.Context, e gcal.Event) (gcal.Event, error)
	updateEventFunc func(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error)
	deleteEventFunc func(ctx context.Context, eventID string) error

	callCount int
}

func (m *mockClient) ListEvents(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
	m.callCount++
	if m.listEventsFunc != nil {
		return m.listEventsFunc(ctx, timeMin, timeMax, query)
	}
	return nil, nil
}

func (m *mockClient) GetEvent(ctx context.Context, eventID string) (gcal.Event, error) {
	m.callCount++
	if m.getEventFunc != nil {
		return m.getEventFunc(ctx, eventID)
	}
	return gcal.Event{}, nil
}

func (m *mockClient) CreateEvent(ctx context.Context, e gcal.Event) (gcal.Event, error) {
	m.callCount++
	if m.createEventFunc != nil {
		return m.createEventFunc(ctx, e)
	}
	return gcal.Event{}, nil
}

func (m *mockClient) UpdateEvent(ctx context.Context, eventID string, e gcal.Event) (gcal.Event, error) {
	m.callCount++
	if m.updateEventFunc != nil {
		return m.updateEventFunc(ctx, eventID, e)
	}
	return gcal.Event{}, nil
}

func (m *mockClient) DeleteEvent(ctx context.Context, eventID string) error {
	m.callCount++
	if m.deleteEventFunc != nil {
		return m.deleteEventFunc(ctx, eventID)
	}
	return nil
}

func TestRetryLogic(t *testing.T) {
	// 5xx should retry once (takes ~1s)
	m5xx := &mockClient{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return nil, common.ErrAPI
		},
	}
	tool5xx := NewTool(m5xx)
	tool5xx.ListEvents(context.Background(), time.Now(), time.Now())
	if m5xx.callCount != 2 {
		t.Errorf("Expected 2 calls for 5xx retry, got %d", m5xx.callCount)
	}

	// 429 should NOT retry
	m429 := &mockClient{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return nil, common.ErrRateLimit
		},
	}
	tool429 := NewTool(m429)
	tool429.ListEvents(context.Background(), time.Now(), time.Now())
	if m429.callCount != 1 {
		t.Errorf("Expected 1 call for 429 (no retry), got %d", m429.callCount)
	}
}

func TestCheckConflict(t *testing.T) {
	ev := gcal.Event{
		Attendees: []gcal.Attendee{{Email: "test@example.com"}},
	}

	m := &mockClient{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{ev}, nil
		},
	}
	tool := NewTool(m)

	res := tool.CheckConflict(context.Background(), time.Now(), time.Now(), []string{"test@example.com"})
	if res.Status != "conflict" {
		t.Errorf("Expected conflict, got %s", res.Status)
	}

	res2 := tool.CheckConflict(context.Background(), time.Now(), time.Now(), []string{"other@example.com"})
	if res2.Status != "success" {
		t.Errorf("Expected success, got %s", res2.Status)
	}
}

func TestSearchEvents(t *testing.T) {
	// Not Found
	m0 := &mockClient{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{}, nil
		},
	}
	tool0 := NewTool(m0)
	if res := tool0.SearchEvents(context.Background(), "test", time.Now()); res.Status != "not_found" {
		t.Errorf("Expected not_found, got %s", res.Status)
	}

	// Success
	m1 := &mockClient{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{{ID: "1"}}, nil
		},
	}
	tool1 := NewTool(m1)
	if res := tool1.SearchEvents(context.Background(), "test", time.Now()); res.Status != "success" {
		t.Errorf("Expected success, got %s", res.Status)
	}

	// Disambiguation
	m2 := &mockClient{
		listEventsFunc: func(ctx context.Context, timeMin, timeMax time.Time, query string) ([]gcal.Event, error) {
			return []gcal.Event{{ID: "1"}, {ID: "2"}}, nil
		},
	}
	tool2 := NewTool(m2)
	if res := tool2.SearchEvents(context.Background(), "test", time.Now()); res.Status != "disambiguation_needed" {
		t.Errorf("Expected disambiguation_needed, got %s", res.Status)
	}
}

func TestRecurringOptions(t *testing.T) {
	m := &mockClient{
		getEventFunc: func(ctx context.Context, eventID string) (gcal.Event, error) {
			return gcal.Event{IsRecurring: true}, nil
		},
	}
	tool := NewTool(m)

	res := tool.UpdateEvent(context.Background(), "1", gcal.Event{}, "")
	if res.Status != "recurring_options" {
		t.Errorf("Expected recurring_options, got %s", res.Status)
	}

	res2 := tool.DeleteEvent(context.Background(), "1", "")
	if res2.Status != "recurring_options" {
		t.Errorf("Expected recurring_options, got %s", res2.Status)
	}
}
