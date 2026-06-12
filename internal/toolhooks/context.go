package toolhooks

import "context"

type requestContext struct {
	requestID string
	sessionID string
}

type requestContextKey struct{}

func WithRequestContext(ctx context.Context, requestID string, sessionID string) context.Context {
	return context.WithValue(ctx, requestContextKey{}, requestContext{
		requestID: requestID,
		sessionID: sessionID,
	})
}

func RequestContextFrom(ctx context.Context) (requestID string, sessionID string) {
	value, ok := ctx.Value(requestContextKey{}).(requestContext)
	if !ok {
		return "", ""
	}
	return value.requestID, value.sessionID
}
