package common

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

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
		case http.StatusForbidden:
			// A 403 is not always an auth/permission problem. Drive returns 403
			// with reason "fileNotDownloadable" for Google Docs Editors files,
			// which is a usage error (export instead), not expired credentials.
			if isNonAuthForbidden(gErr) {
				return fmt.Errorf("%w: %v", ErrAPI, err)
			}
			return fmt.Errorf("%w: %v", ErrAuth, err)
		case http.StatusUnauthorized:
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

// nonAuthForbiddenReasons are Google 403 reasons that indicate a usage error
// rather than an authentication/permission failure, so they must not surface as
// AUTH_EXPIRED (which would wrongly prompt re-authentication).
var nonAuthForbiddenReasons = []string{
	"filenotdownloadable",
}

// isNonAuthForbidden reports whether a 403 carries a reason/message that is a
// usage error rather than an auth failure.
func isNonAuthForbidden(gErr *googleapi.Error) bool {
	for _, item := range gErr.Errors {
		reason := strings.ToLower(item.Reason)
		for _, r := range nonAuthForbiddenReasons {
			if reason == r {
				return true
			}
		}
	}
	msg := strings.ToLower(gErr.Message)
	for _, r := range nonAuthForbiddenReasons {
		if strings.Contains(msg, r) {
			return true
		}
	}
	return false
}
