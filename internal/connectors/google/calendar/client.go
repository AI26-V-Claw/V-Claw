package calendar

import (
	"context"
	"net/http"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type Event struct {
	ID      string
	Summary string
	Start   string
}

func ListUpcomingEvents(ctx context.Context, client *http.Client, calendarID string, maxResults int64) ([]Event, error) {
	service, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}

	response, err := service.Events.List(calendarID).
		ShowDeleted(false).
		SingleEvents(true).
		TimeMin(time.Now().Format(time.RFC3339)).
		MaxResults(maxResults).
		OrderBy("startTime").
		Do()
	if err != nil {
		return nil, err
	}

	events := make([]Event, 0, len(response.Items))
	for _, item := range response.Items {
		start := item.Start.DateTime
		if start == "" {
			start = item.Start.Date
		}
		events = append(events, Event{
			ID:      item.Id,
			Summary: item.Summary,
			Start:   start,
		})
	}
	return events, nil
}
