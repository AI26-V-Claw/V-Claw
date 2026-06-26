package meet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"vclaw/internal/connectors/google/common"

	"google.golang.org/api/googleapi"
)

// CreateSpace creates a Google Meet meeting space for later use.
func (c *Client) CreateSpace(ctx context.Context) (Space, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/spaces", bytes.NewReader([]byte("{}")))
	if err != nil {
		return Space{}, fmt.Errorf("meet: create space: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return Space{}, fmt.Errorf("meet: create space: %w", common.MapError(err))
	}
	defer res.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if readErr != nil {
		return Space{}, fmt.Errorf("meet: create space: %w", common.MapError(readErr))
	}
	if res.StatusCode >= 300 {
		return Space{}, fmt.Errorf("meet: create space: %w", googleAPIError(res.StatusCode, body))
	}

	var payload struct {
		Name        string `json:"name"`
		MeetingURI  string `json:"meetingUri"`
		MeetingCode string `json:"meetingCode"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Space{}, fmt.Errorf("meet: create space: %w", common.MapError(err))
	}
	return Space{
		Name:        payload.Name,
		MeetingURI:  payload.MeetingURI,
		MeetingCode: payload.MeetingCode,
	}, nil
}

func googleAPIError(statusCode int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		message = strings.TrimSpace(payload.Error.Message)
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	return &googleapi.Error{
		Code:    statusCode,
		Message: message,
		Body:    strings.TrimSpace(string(body)),
	}
}
