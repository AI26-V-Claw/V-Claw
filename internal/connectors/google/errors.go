package google

import (
	"errors"
	"io"
	"net"
	"strings"
)

// IsNetworkError reports whether err is a transient network-level failure
// (TCP reset, broken pipe, unexpected EOF) that should be retried.
// These errors are not *googleapi.Error and would otherwise be misclassified
// as INTERNAL_ERROR with Retryable=false.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "eof")
}
