package common

import (
	"errors"
	"fmt"
	"net/http"

	"google.golang.org/api/googleapi"
)

// Domain errors mapped from Google API responses.
var (
	ErrAuth      = errors.New("auth_error")
	ErrNotFound  = errors.New("not_found")
	ErrRateLimit = errors.New("rate_limit")
	ErrAPI       = errors.New("api_error")
)

// MapError takes an error returned by Google API and maps it to a domain error.
// It wraps the original error so that details are preserved.
func MapError(err error) error {
	if err == nil {
		return nil
	}

	var gErr *googleapi.Error
	if errors.As(err, &gErr) {
		switch gErr.Code {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w: %v", ErrAuth, err)
		case http.StatusNotFound:
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		case http.StatusTooManyRequests:
			return fmt.Errorf("%w: %v", ErrRateLimit, err)
		default:
			// Treat all other Google API errors as ErrAPI, including 5xx and other 4xx.
			return fmt.Errorf("%w: %v", ErrAPI, err)
		}
	}

	// For network errors (timeout, connection reset) and other non-googleapi errors,
	// wrap them as ErrAPI to be uniformly treated as API errors.
	return fmt.Errorf("%w: %v", ErrAPI, err)
}
