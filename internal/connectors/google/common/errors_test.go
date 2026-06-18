package common

import (
	"errors"
	"net/http"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestMapError(t *testing.T) {
	tests := []struct {
		name     string
		input    error
		expected error
	}{
		{
			name:     "nil error",
			input:    nil,
			expected: nil,
		},
		{
			name: "auth error 401",
			input: &googleapi.Error{
				Code:    http.StatusUnauthorized,
				Message: "Unauthorized",
			},
			expected: ErrAuth,
		},
		{
			name: "auth error 403",
			input: &googleapi.Error{
				Code:    http.StatusForbidden,
				Message: "Forbidden",
			},
			expected: ErrAuth,
		},
		{
			name: "403 fileNotDownloadable is a usage error, not auth (reason)",
			input: &googleapi.Error{
				Code:    http.StatusForbidden,
				Message: "Only files with binary content can be downloaded.",
				Errors:  []googleapi.ErrorItem{{Reason: "fileNotDownloadable"}},
			},
			expected: ErrAPI,
		},
		{
			name: "403 fileNotDownloadable is a usage error, not auth (message)",
			input: &googleapi.Error{
				Code:    http.StatusForbidden,
				Message: "Error 403: Only files with binary content can be downloaded. Use Export with Docs Editors files., fileNotDownloadable",
			},
			expected: ErrAPI,
		},
		{
			name: "not found 404",
			input: &googleapi.Error{
				Code:    http.StatusNotFound,
				Message: "Not Found",
			},
			expected: ErrNotFound,
		},
		{
			name: "rate limit 429",
			input: &googleapi.Error{
				Code:    http.StatusTooManyRequests,
				Message: "Too Many Requests",
			},
			expected: ErrRateLimit,
		},
		{
			name: "api error 500",
			input: &googleapi.Error{
				Code:    http.StatusInternalServerError,
				Message: "Internal Server Error",
			},
			expected: ErrAPI,
		},
		{
			name: "api error 503",
			input: &googleapi.Error{
				Code:    http.StatusServiceUnavailable,
				Message: "Service Unavailable",
			},
			expected: ErrAPI,
		},
		{
			name: "generic network error",
			input: errors.New("connection timeout"),
			expected: ErrAPI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MapError(tt.input)
			if !errors.Is(err, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, err)
			}
		})
	}
}
