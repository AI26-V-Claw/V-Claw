package calendar

import (
	"context"
	"net/http"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Client is the Google Calendar API connector client.
// Nó chỉ gọi API, không chứa bất kỳ business logic nào.
type Client struct {
	srv *gcal.Service
}

// NewClient creates a new Google Calendar API client using the provided HTTP client.
func NewClient(ctx context.Context, client *http.Client) (*Client, error) {
	srv, err := gcal.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	return &Client{srv: srv}, nil
}
