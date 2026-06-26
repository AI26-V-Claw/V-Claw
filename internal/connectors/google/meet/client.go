package meet

import (
	"net/http"
	"strings"
)

const defaultBaseURL = "https://meet.googleapis.com/v2"

// Client calls the Google Meet REST API. It only performs API transport and
// maps responses into small domain structs.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a Google Meet API client using the provided OAuth HTTP client.
func NewClient(client *http.Client) *Client {
	return NewClientWithBaseURL(client, defaultBaseURL)
}

// NewClientWithBaseURL creates a client with an override base URL for tests.
func NewClientWithBaseURL(client *http.Client, baseURL string) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{httpClient: client, baseURL: baseURL}
}
